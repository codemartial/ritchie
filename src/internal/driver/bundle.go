// This file writes and starts the one-process bundle of an application with resident
// units: an archive of its manifest and its executable.
package driver

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const BundleSuffix = ".run"

// manifest describes the target and platform that a bundle was built for.
type manifest struct {
	Format int    `json:"format"`
	Target string `json:"target"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
}

const (
	bundleFormat  = 1
	manifestEntry = "manifest.json"
	programEntry  = "program"
	bundleTarget  = "monolith"
)

// installBundle writes the bundle of an executable at path, complete or not at all.
func installBundle(exe, path string) error {
	program, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	meta, err := json.Marshal(manifest{Format: bundleFormat, Target: bundleTarget, OS: runtime.GOOS, Arch: runtime.GOARCH})
	if err != nil {
		return err
	}
	staged, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-")
	if err != nil {
		return err
	}
	w := tar.NewWriter(staged)
	for _, entry := range []struct {
		name string
		mode int64
		data []byte
	}{{manifestEntry, 0o644, meta}, {programEntry, 0o755, program}} {
		if err = w.WriteHeader(&tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.data))}); err != nil {
			break
		}
		if _, err = w.Write(entry.data); err != nil {
			break
		}
	}
	if err == nil {
		err = w.Close()
	}
	if cerr := staged.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(staged.Name(), path)
	}
	if err != nil {
		os.Remove(staged.Name())
	}
	return err
}

// unbundle returns the executable of a bundle built for this target and platform.
func unbundle(bundle io.Reader) ([]byte, error) {
	r := tar.NewReader(bundle)
	var meta *manifest
	var program []byte
	for {
		hdr, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the input is not a %s bundle: %w", BundleSuffix, err)
		}
		content, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("the input is not a %s bundle: %w", BundleSuffix, err)
		}
		switch hdr.Name {
		case manifestEntry:
			meta = &manifest{}
			if err := json.Unmarshal(content, meta); err != nil {
				return nil, fmt.Errorf("the bundle manifest cannot be read: %w", err)
			}
		case programEntry:
			program = content
		}
	}
	switch {
	case meta == nil || program == nil:
		return nil, fmt.Errorf("the input is not a %s bundle: it has no manifest or program", BundleSuffix)
	case meta.Format != bundleFormat || meta.Target != bundleTarget:
		return nil, fmt.Errorf("the bundle is for target %q in format %d; this command starts a %s bundle in format %d", meta.Target, meta.Format, bundleTarget, bundleFormat)
	case meta.OS != runtime.GOOS || meta.Arch != runtime.GOARCH:
		return nil, fmt.Errorf("the bundle is for %s/%s and does not run on %s/%s", meta.OS, meta.Arch, runtime.GOOS, runtime.GOARCH)
	}
	return program, nil
}

// runBundle starts the executable of a bundle from a private directory with the program arguments.
func runBundle(path string, h Host, args []string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	program, err := unbundle(f)
	if err != nil {
		return 0, err
	}
	dir, err := os.MkdirTemp("", "ritchie-run-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	exe := filepath.Join(dir, programEntry)
	if err := os.WriteFile(exe, program, 0o755); err != nil {
		return 0, err
	}
	return start(exe, args, h)
}
