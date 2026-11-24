// Package rlr is the Ritchie Logical Runtime.
package rlr

import (
	"cmp"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ritchie.dev/rlr/mesh"
)

var Diag io.Writer = os.Stderr

var Out io.Writer = os.Stdout

var Exit func(code int) = os.Exit

// Run invokes the program's main routine and completes the process.
func Run(module string, main func() error) {
	prepare()
	Exit(guarded(mesh.Contains(), func() int {
		if _, ok := configured(); !ok {
			return 2
		}
		return enter(module, main)
	}))
}

var host func([]mesh.Hosted) mesh.Realization

// InstallTarget installs the target of the process.
func InstallTarget(build func([]mesh.Hosted) mesh.Realization) { host = build }

const ServeOption = "--serve"

const DefaultServePort = 7824

// ServeAddress is the loopback address of the local exposure on port.
func ServeAddress(port int) string { return "127.0.0.1:" + strconv.Itoa(port) }

// ServePort reads the serve options among args and reports whether any asks to serve, where port is the port that an option names or 0 when none names one, and the error states a port outside 1 to 65535 or a second, different port.
func ServePort(args []string) (port int, serve bool, err error) {
	for _, a := range args {
		if a == ServeOption {
			serve = true
			continue
		}
		text, found := strings.CutPrefix(a, ServeOption+"=")
		if !found {
			continue
		}
		serve = true
		n, perr := strconv.Atoi(text)
		if perr != nil || n < 1 || n > 65535 || strconv.Itoa(n) != text {
			return 0, true, errors.New(ServeOption + "=PORT needs a port from 1 to 65535, not " + strconv.Quote(text))
		}
		if port != 0 && n != port {
			return 0, true, errors.New(ServeOption + " names two ports, " + strconv.Itoa(port) + " and " + text)
		}
		port = n
	}
	return port, serve, nil
}

// listenFailure states why the local exposure cannot serve on port, where port 0 is DefaultServePort.
func listenFailure(port int, err error) string {
	where := "port " + strconv.Itoa(port)
	if port == 0 {
		where = "the default port " + strconv.Itoa(DefaultServePort)
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		text := "cannot serve on " + where + " because another process is using it"
		if port == 0 {
			text += "; give " + ServeOption + "=PORT to serve on another port"
		}
		return text
	}
	var sys *os.SyscallError
	if errors.As(err, &sys) {
		err = sys.Err
	}
	return "cannot serve on " + where + ": " + err.Error()
}

// Start constructs every unit on the bound target and then exposes and runs the application.
func Start(app Application) {
	prepare()
	port, serve, invalid := ServePort(os.Args[1:])
	Exit(guarded(mesh.Contains(), func() int {
		if invalid != nil {
			io.WriteString(Diag, "ritchie runtime: "+invalid.Error()+"\n")
			return 2
		}
		if app.Main == nil && !serve {
			io.WriteString(Diag, "ritchie runtime: the program declares no main entry point; give "+ServeOption+" to serve its services\n")
			return 2
		}
		limits, ok := configured()
		if !ok {
			return 2
		}
		rt := newRuntime(app, host)
		rt.Limit = limits.limit
		if serve {
			if err := expose(rt, app, limits.budget, cmp.Or(port, DefaultServePort)); err != nil {
				io.WriteString(Diag, "ritchie runtime: "+listenFailure(port, err)+"\n")
				return 2
			}
		}
		if app.Main != nil {
			client := &Scope{inv: rt.Entry(limits.budget)}
			status := enter(app.Module, func() error { return app.Main(client) })
			if !serve {
				return status
			}
		}
		return awaitSignal()
	}))
}

// configured reads the settings and reports an invalid value, after which no entry runs, or settles the reporting level, where level 0 discards the diagnostic output.
func configured() (settings, bool) {
	s, err := readSettings()
	if err != nil {
		io.WriteString(Diag, "ritchie runtime: "+err.Error()+"\n")
		return s, false
	}
	level = s.level
	if level == 0 {
		Diag = io.Discard
	}
	return s, true
}

// prepare installs the system clock and entropy sources when none are installed, before any program code runs.
func prepare() {
	if now == nil {
		now = time.Now
	}
	if entropy == nil {
		entropy = rand.Uint64
	}

	signal.Ignore(syscall.SIGPIPE)
	watchParent()
}

const ParentEnv = "RITCHIE_PARENT"

// watchParent ends the process when the command that started it, whose process id the environment variable ParentEnv holds, is no longer its parent, so that a command killed by a timeout leaves no running program.
func watchParent() {
	parent, err := strconv.Atoi(os.Getenv(ParentEnv))
	if err != nil || parent != os.Getppid() {
		return
	}
	go func() {
		for range time.Tick(200 * time.Millisecond) {
			if os.Getppid() != parent {
				Exit(128 + int(syscall.SIGKILL))
			}
		}
	}()
}

// enter runs main and reports the error that escapes it.
func enter(module string, main func() error) int {
	if err := main(); err != nil {
		report(terminalLevel, module, err.(RitchieError))
		return 1
	}
	return 0
}

// guarded converts host faults from an entry, or from the body of a task, into defect reports when contains is set, and otherwise lets a host fault end the process with its own stack.
func guarded(contains bool, entry func() int) (status int) {
	defer func() {
		if !contains {
			return
		}
		if fault := recover(); fault != nil {
			if _, halted := fault.(mesh.Halt); halted {
				return
			}
			mesh.Defect(Diag, fault)
			status = 2
		}
	}()
	return entry()
}

// awaitSignal blocks until an interrupt or termination signal and reports the conventional status of a process that signal ended.
func awaitSignal() int {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	return 128 + int((<-signals).(syscall.Signal))
}

// Print writes the value's default display form and one line break to the program's output.
func Print(module string, v any) {
	io.WriteString(Out, render(v, module)+"\n")
}

// Log writes the value's default display form and one line break to the diagnostic output.
func Log(module string, v any) {
	io.WriteString(Diag, render(v, module)+"\n")
}
