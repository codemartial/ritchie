// Package driver connects the stages of the compiler into the pipeline of the
// architecture document and implements the commands rcc, rcrun, and ritchie.
package driver

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/rlr"
)

const (
	ExitOK       = 0
	ExitRejected = 1
	ExitFailure  = 2
)

const (
	NoTracebackOption = "--no-traceback"
	DebugOption       = "--debug"
	diagnosticsOption = "--diagnostics="
	helpOption        = "--help"
	mutatingOption    = "--mutating="
	entitiesOption    = "--entities="
)

// Host is the environment of one command: its name for messages, its standard streams, its working directory, and its build inputs, which are the rlr file installing the clock and entropy providers, the main-package file installing the target of an application, the publication of the root modules and of each dependency module, the service methods that a fixture declares mutating, each written as its service name, a dot, and its method name, the services that a fixture declares as entity-type targets, each written as its name, whether the test facility asks for the sequential build, and whether it asks for every copy.
type Host struct {
	Name         string
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	Dir          string
	Providers    []byte
	Target       []byte
	Publication  string
	Publications map[string]string
	Mutating     []string
	Entities     []string
	Sequential   bool
	EveryCopy    bool
}

// OSHost returns the host of a command run by the operating system in the process's working directory.
func OSHost(name string, stdout, stderr io.Writer) Host {
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	return Host{Name: name, Stdout: stdout, Stderr: stderr, Dir: dir}
}

// invocation is a parsed command line: the inputs and the options, where serve is the serve option that the program receives, or empty, and debug admits the construction of a catalog error in application code.
type invocation struct {
	h      Host
	inputs []string
	jsonl  bool
	serve  string
	debug  bool
}

// parse reads the options and inputs of a compile or run command in any order and returns a nil invocation with the exit status when the command line asks for the usage text or holds an unknown option, and accepts NoTracebackOption without effect.
func parse(args []string, h Host, run bool) (*invocation, int) {
	inv := &invocation{h: h}
	var serves []string
	for _, a := range args {
		switch {
		case a == helpOption || a == "-h":
			fmt.Fprint(h.Stdout, usage([]string{h.Name}, nil, !run, run))
			return nil, ExitOK
		case a == NoTracebackOption:
		case a == DebugOption:
			inv.debug = true
		case a == diagnosticsOption+"human":
			inv.jsonl = false
		case a == diagnosticsOption+"jsonl":
			inv.jsonl = true
		case run && (a == rlr.ServeOption || strings.HasPrefix(a, rlr.ServeOption+"=")):
			serves = append(serves, a)
		case a == "-" && run:
			inv.inputs = append(inv.inputs, a)
		case strings.HasPrefix(a, "-"):
			return nil, inv.failure(fmt.Sprintf("unknown option %q", a))
		default:
			inv.inputs = append(inv.inputs, a)
		}
	}
	if len(serves) > 0 {
		port, _, err := rlr.ServePort(serves)
		if err != nil {
			return nil, inv.failure(err.Error())
		}
		inv.serve = rlr.ServeOption
		if port != 0 {
			inv.serve += "=" + strconv.Itoa(port)
		}
	}
	return inv, ExitOK
}

// usageEntry is one command, input form, or option of the usage text with its description and whether only the run commands accept it.
type usageEntry struct {
	form, text string
	run        bool
}

var (
	commandEntries = []usageEntry{
		{"compile", "check the input and install its executable or bundle in the working directory", false},
		{"run", "compile the input and start it on the local monolith target", false},
		{"kernel print", "write the kernel program that compilation builds from the input", false},
	}
	inputEntries = []usageEntry{
		{"(none)", "the project of the nearest enclosing directory that holds exactly one *.ritchie.toml file", false},
		{"<project>.ritchie.toml", "that project", false},
		{"<source.ri>...", "an ad-hoc source set of one anonymous module", false},
		{"<bundle>" + BundleSuffix, "a compiled monolith bundle", true},
		{"-", "one source file read from standard input", true},
	}
	optionEntries = []usageEntry{
		{diagnosticsOption + "human|jsonl", "write diagnostics as human-readable text (default) or JSON Lines", false},
		{DebugOption, "admit the construction of catalog errors in application code", false},
		{rlr.ServeOption + "[=PORT]", "expose internal service routes over HTTP on PORT of the loopback interface, 7824 by default", true},
		{"-h, " + helpOption, "print this usage text and exit", false},
	}
)

// usage returns the usage text of the commands of the given names, with their descriptions when commands is given, and the input forms and options of compilation or running or both.
func usage(names []string, commands []usageEntry, compile, run bool) string {
	var b strings.Builder
	for i, name := range names {
		lead := "usage: "
		if i > 0 {
			lead = "       "
		}
		fmt.Fprintf(&b, "%s%s [options] [input]\n", lead, name)
	}
	section := func(title string, entries []usageEntry) {
		fmt.Fprintf(&b, "\n%s:\n", title)
		for _, e := range entries {
			switch {
			case e.run && !run:
				continue
			case e.run && compile:
				e.text += " (run only)"
			}
			fmt.Fprintf(&b, "  %-26s %s\n", e.form, e.text)
		}
	}
	if commands != nil {
		section("Commands", commands)
	}
	section("Input", inputEntries)
	section("Options", optionEntries)
	return b.String()
}

// Usage returns the usage text of the public command and its compile, run, and kernel print subcommands.
func Usage() string {
	return usage([]string{"ritchie compile", "ritchie run", "ritchie kernel print"}, commandEntries, true, true)
}

// failure reports an invocation or compiler failure in the selected diagnostics form.
func (inv *invocation) failure(message string) int {
	if inv.jsonl {
		diagnostics.WriteFailureJSON(inv.h.Stderr, message)
	} else {
		fmt.Fprintf(inv.h.Stderr, "%s: %s\n", inv.h.Name, message)
	}
	return ExitFailure
}

// reject writes the diagnostics of rejected source.
func (inv *invocation) reject(diags []diagnostics.Record) int {
	inv.write(diags)
	return ExitRejected
}

// write writes diagnostics to standard error in the selected form.
func (inv *invocation) write(diags []diagnostics.Record) {
	if inv.jsonl {
		diagnostics.WriteJSONL(inv.h.Stderr, diags)
	} else {
		diagnostics.WriteHuman(inv.h.Stderr, diags)
	}
}

// Main compiles the selected source set and installs its artifact in the working directory.
func Main(args []string, h Host) int {
	inv, status := parse(args, h, false)
	if inv == nil {
		return status
	}
	set, status := inv.compile()
	if set == nil {
		return status
	}
	if set.main == nil && !set.units {
		return ExitOK
	}
	if err := install(set, inv.h.Dir); err != nil {
		return inv.failure(err.Error())
	}
	return ExitOK
}

// RunMain compiles the selected source set and runs it with the streams of the command.
func RunMain(args []string, h Host) int {
	inv, status := parse(args, h, true)
	if inv == nil {
		return status
	}
	var programArgs []string
	if inv.serve != "" {
		programArgs = []string{inv.serve}
	}
	if slices.ContainsFunc(inv.inputs, func(in string) bool { return strings.HasSuffix(in, BundleSuffix) }) {
		if len(inv.inputs) != 1 {
			return inv.failure("a " + BundleSuffix + " bundle is the only input of a run")
		}
		status, err := runBundle(inv.path(inv.inputs[0]), h, programArgs)
		if err != nil {
			return inv.failure(err.Error())
		}
		return status
	}
	set, status := inv.compile()
	if set == nil {
		return status
	}
	if set.main == nil && !set.units {
		return inv.failure("the program declares no main entry point")
	}
	status, err := run(set, h, programArgs)
	if err != nil {
		return inv.failure(err.Error())
	}
	return status
}

// KernelMain implements the kernel commands: print writes the lowered kernel program and accepts the options of compilation, and the unadvertised compare reports whether lowering a source tree, or the given inputs within it, gives a kernel tree, where the options --mutating= and --entities= give the declarations of a fixture as lists separated by commas, because the kernel text of a read depends on them.
func KernelMain(args []string, h Host) int {
	inv := &invocation{h: h}
	args = slices.DeleteFunc(slices.Clone(args), func(a string) bool {
		list, mutating := strings.CutPrefix(a, mutatingOption)
		if mutating {
			inv.h.Mutating = strings.Split(list, ",")
		}
		list, entities := strings.CutPrefix(a, entitiesOption)
		if entities {
			inv.h.Entities = strings.Split(list, ",")
		}
		inv.debug = inv.debug || a == DebugOption
		return mutating || entities || a == DebugOption
	})
	if len(args) == 0 {
		return inv.failure("usage: ritchie kernel print [options] [input]")
	}
	switch args[0] {
	case "print":
		host := inv.h
		host.Name = "ritchie kernel print"
		printing, status := parse(args[1:], host, false)
		if printing == nil {
			return status
		}
		printing.debug = printing.debug || inv.debug
		return printing.print()
	case "compare":
		if len(args) < 3 {
			return inv.failure("usage: ritchie kernel compare <kernel-dir> <source-dir> [<input>...]")
		}
		return inv.compare(args[1], args[2], args[3:])
	}
	return inv.failure(fmt.Sprintf("unknown kernel command %q", args[0]))
}
