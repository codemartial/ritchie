// This file builds a generated Go program beside the embedded rlr sources, installs
// the executable, and runs it with the streams of the command.
package driver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	ritchie "ritchie.dev"
	"ritchie.dev/rlr"
)

const goMod = "module ritchie.dev\n\ngo 1.27\n"

const executableCacheLimit = 6 << 30

// cacheDir returns the ritchie directory under XDG_CACHE_HOME when that is an absolute path and under the user cache directory of the host otherwise, or no directory when the host has none.
func cacheDir() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "ritchie")
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "ritchie")
	}
	return ""
}

// build writes the build tree of a compiled source set, with its provider composition when one is selected, into a fresh directory and places its executable there, built or copied from the executable cache, returning the directory and the executable's path.
func build(set *sourceSet) (string, string, error) {
	cache := cacheDir()
	if cache != "" {
		if err := os.MkdirAll(cache, 0o755); err != nil {
			return "", "", err
		}
	}
	dir, err := os.MkdirTemp(cache, "build-")
	if err != nil {
		return "", "", err
	}
	key := sha256.New()
	fmt.Fprintf(key, "%s\n%s\n", runtime.Version(), goMod)
	write := func(name string, data []byte) error {
		fmt.Fprintf(key, "%s %d\n", name, len(data))
		key.Write(data)
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o644)
	}
	err = fs.WalkDir(ritchie.RLRSources, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasPrefix(path, "monolith/") && len(set.target) == 0 {
			return nil
		}
		data, err := ritchie.RLRSources.ReadFile(path)
		if err != nil {
			return err
		}
		return write(path, data)
	})
	if err == nil && len(set.providers) > 0 {
		err = write("rlr/providers.go", set.providers)
	}
	if err == nil && len(set.target) > 0 {
		err = write("target.go", set.target)
	}
	if err == nil {
		err = write("go.mod", []byte(goMod))
	}
	if err == nil {
		err = write("main.go", set.gosrc)
	}
	if err != nil {
		os.RemoveAll(dir)
		return "", "", err
	}
	exe := filepath.Join(dir, "program")
	cached := ""
	if cache != "" {
		cached = filepath.Join(cache, "bin", hex.EncodeToString(key.Sum(nil)))
		if copyFile(cached, exe, 0o755) == nil {
			now := time.Now()
			os.Chtimes(cached, now, now)
			return dir, exe, nil
		}
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", exe, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "GOPROXY=off", "CGO_ENABLED=0")
	if os.Getenv("GOCACHE") == "" && cache != "" {
		cmd.Env = append(cmd.Env, "GOCACHE="+filepath.Join(cache, "go-build"))
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		os.RemoveAll(dir)
		return "", "", fmt.Errorf("internal error: the generated Go does not build: %v\n%s", err, strings.TrimSpace(out.String()))
	}
	if cached != "" {
		remember(exe, cached)
	}
	return dir, exe, nil
}

// copyFile copies the file at src to dst with the mode, so that an executable is complete at dst or absent, and holds the read side of syscall.ForkLock while the staged file is open for writing, so that no child process that another goroutine forks inherits the descriptor, because an inherited writable descriptor makes a concurrent exec of the executable fail with ETXTBSY.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	syscall.ForkLock.RLock()
	staged, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+"-")
	if err != nil {
		syscall.ForkLock.RUnlock()
		return err
	}
	_, err = io.Copy(staged, in)
	if cerr := staged.Close(); err == nil {
		err = cerr
	}
	syscall.ForkLock.RUnlock()
	if err == nil {
		err = os.Chmod(staged.Name(), mode)
	}
	if err == nil {
		err = os.Rename(staged.Name(), dst)
	}
	if err != nil {
		os.Remove(staged.Name())
	}
	return err
}

// remember stores a built executable under its key in the executable cache, whose least recently used executables it removes beyond the limit of executableCacheLimit bytes, and a failure to store costs only the next build.
func remember(exe, cached string) {
	dir := filepath.Dir(cached)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	if err := copyFile(exe, cached, 0o755); err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type entry struct {
		path string
		info fs.FileInfo
	}
	var kept []entry
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		kept = append(kept, entry{filepath.Join(dir, e.Name()), info})
		total += info.Size()
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].info.ModTime().Before(kept[j].info.ModTime()) })
	for _, e := range kept {
		if total <= executableCacheLimit {
			break
		}
		if os.Remove(e.path) == nil {
			total -= e.info.Size()
		}
	}
}

// install builds a compiled source set and replaces its artifact in a directory only once the build is complete.
func install(set *sourceSet, dir string) error {
	tree, exe, err := build(set)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tree)
	if set.units {
		return installBundle(exe, filepath.Join(dir, set.name+BundleSuffix))
	}
	target := filepath.Join(dir, set.name)
	if err := os.Rename(exe, target); err == nil {
		return nil
	}
	return copyFile(exe, target, 0o755)
}

// run builds a compiled source set and runs it with the program arguments, returning its exit status, or 128 plus the signal that ended it.
func run(set *sourceSet, h Host, args []string) (int, error) {
	tree, exe, err := build(set)
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tree)
	return start(exe, args, h)
}

// start runs an executable with the streams of the host, forwarding an interrupt or termination signal that the command receives, and returns its exit status, or 128 plus the signal that ended it.
func start(exe string, args []string, h Host) (int, error) {
	cmd := exec.Command(exe, args...)
	cmd.Dir = h.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = h.Stdin, h.Stdout, h.Stderr
	cmd.Env = append(os.Environ(), rlr.ParentEnv+"="+strconv.Itoa(os.Getpid()))
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-signals:
				cmd.Process.Signal(s)
			case <-done:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return ExitOK, nil
	case errors.As(err, &exit):
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal()), nil
		}
		return exit.ExitCode(), nil
	}
	return 0, err
}
