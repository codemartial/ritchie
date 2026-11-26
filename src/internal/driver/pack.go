// This file builds a program from a runtime pack, which is a directory holding
// the compiled archives of the rlr packages and of every package they import.
package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	ritchie "ritchie.dev"
)

const (
	PackEnv      = "RITCHIE_RUNTIME_PACK"
	PackManifest = "manifest.json"
	PackImports  = "importcfg"
	PackArchives = "pkg"
)

const packMain = "package main\n\nimport _ \"ritchie.dev/rlr\"\n\nfunc main() {}\n"

// Pack is the manifest of a runtime pack, which identifies the toolchain and the rlr sources that produced its archives and states how to run its compile and link tools for a main package.
type Pack struct {
	GoVersion    string   `json:"goVersion"`
	GOOS         string   `json:"goos"`
	GOARCH       string   `json:"goarch"`
	Compiler     string   `json:"compiler"`
	RLR          string   `json:"rlr"`
	Compile      string   `json:"compile"`
	Link         string   `json:"link"`
	CompileFlags []string `json:"compileFlags"`
	LinkFlags    []string `json:"linkFlags"`
}

var rlrDigest = sync.OnceValue(func() string {
	sum := sha256.New()
	fs.WalkDir(ritchie.RLRSources, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := ritchie.RLRSources.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(sum, "%s %d\n", path, len(data))
		sum.Write(data)
		return nil
	})
	return hex.EncodeToString(sum.Sum(nil))
})

// RLRDigest returns the SHA-256 digest of the names and contents of the embedded rlr and monolith sources, in the order in which a build tree receives them.
func RLRDigest() string { return rlrDigest() }

// PackTree writes into a directory the build tree of a program with the monolith target and an empty main function, whose imported packages a runtime pack holds.
func PackTree(dir string) error {
	write := func(name string, data []byte) error {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o644)
	}
	err := fs.WalkDir(ritchie.RLRSources, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := ritchie.RLRSources.ReadFile(path)
		if err != nil {
			return err
		}
		return write(path, data)
	})
	if err == nil {
		err = write("go.mod", []byte(goMod))
	}
	if err == nil {
		err = write("target.go", []byte(monolithTarget))
	}
	if err == nil {
		err = write("main.go", []byte(packMain))
	}
	return err
}

// packed builds the executable of a build tree with the tools and archives of the runtime pack that PackEnv selects, and reports false, leaving the build to the go command, unless the pack matches the driver, the source set carries no providers, and both tools succeed.
func packed(set *sourceSet, dir, exe string) bool {
	root := os.Getenv(PackEnv)
	if root == "" || len(set.providers) > 0 {
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, PackManifest))
	if err != nil {
		return false
	}
	var p Pack
	if json.Unmarshal(data, &p) != nil {
		return false
	}
	if p.GoVersion != runtime.Version() || p.GOOS != runtime.GOOS || p.GOARCH != runtime.GOARCH || p.RLR != RLRDigest() {
		return false
	}
	imports := filepath.Join(root, PackImports)
	archive := filepath.Join(dir, "main.a")
	args := []string{"-o", archive, "-trimpath", dir + "=>ritchie.dev", "-p", "main", "-importcfg", imports}
	args = append(args, p.CompileFlags...)
	args = append(args, "./main.go")
	if len(set.target) > 0 {
		args = append(args, "./target.go")
	}
	compile := exec.Command(p.Compile, args...)
	compile.Dir = dir
	if compile.Run() != nil {
		os.Remove(archive)
		return false
	}
	args = append([]string{"-o", exe, "-importcfg", imports}, p.LinkFlags...)
	link := exec.Command(p.Link, append(args, archive)...)
	link.Dir = dir
	link.Env = append(os.Environ(), "GOROOT=")
	err = link.Run()
	os.Remove(archive)
	if err != nil {
		os.Remove(exe)
		return false
	}
	return true
}
