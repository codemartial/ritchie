// This file selects the source set of an invocation and runs it through the stages:
// parse, collect, type check, the limits pass, lower with raise-set inference between
// the phases, kernel check, and generate.
package driver

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"ritchie.dev/internal/check"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/gen"
	"ritchie.dev/internal/limits"
	"ritchie.dev/internal/lower"
	"ritchie.dev/internal/project"
	"ritchie.dev/internal/source"
	"ritchie.dev/internal/syntax"
)

// sourceSet is a selected source set: its files, the name of its artifact, and after compilation its Go program, main routine, whether it has resident units, and its provider and target composition.
type sourceSet struct {
	files     []*source.File
	name      string
	main      *syntax.Func
	units     bool
	gosrc     []byte
	providers []byte
	target    []byte
}

// compile selects and compiles the source set of an invocation, returning nil with the exit status when it is rejected or fails.
func (inv *invocation) compile() (*sourceSet, int) {
	set, kernel, status := inv.lowered()
	if set == nil {
		return nil, status
	}
	set.main = mainOf(kernel)
	opts := inv.options(set)
	set.units = len(gen.Units(kernel, opts).Units) > 0
	if set.main == nil && !set.units {
		return set, ExitOK
	}
	src, err := gen.Program(kernel, opts)
	if err != nil {
		return nil, inv.failure(err.Error())
	}
	set.gosrc, set.providers = src, inv.h.Providers
	if set.units {
		set.target = inv.h.Target
		if len(set.target) == 0 {
			set.target = []byte(monolithTarget)
		}
	}
	return set, ExitOK
}

const monolithTarget = "package main\n\nimport (\n\t\"ritchie.dev/monolith\"\n\t\"ritchie.dev/rlr\"\n)\n\nfunc init() { rlr.InstallTarget(monolith.New) }\n"

// options are the generation inputs of a compiled source set under the host.
func (inv *invocation) options(set *sourceSet) gen.Options {
	return gen.Options{Main: set.main, Publication: inv.h.Publication, Publications: inv.h.Publications, Route: set.name, EveryCopy: inv.h.EveryCopy}
}

// Inspect selects and checks the source set of a compile command's arguments and returns the inventory that its build would carry, or the diagnostics that reject it.
func Inspect(args []string, h Host) (gen.Inventory, []diagnostics.Record, error) {
	inv, _ := parse(args, h, false)
	if inv == nil {
		return gen.Inventory{}, nil, fmt.Errorf("the arguments %v are not a compile command", args)
	}
	set, diags, err := inv.load()
	if err != nil || len(diags) > 0 {
		return gen.Inventory{}, diags, err
	}
	kernel, diags, err := pipeline(set.files, inv.checks(), inv.h.Sequential)
	if err != nil || len(diags) > 0 {
		return gen.Inventory{}, diags, err
	}
	set.main = mainOf(kernel)
	return gen.Units(kernel, inv.options(set)), nil, nil
}

// lowered selects the source set of an invocation and runs it through every stage before generation, returning nil with the exit status when it is rejected or fails.
func (inv *invocation) lowered() (*sourceSet, *check.Program, int) {
	set, diags, err := inv.load()
	if err != nil {
		return nil, nil, inv.failure(err.Error())
	}
	if len(diags) > 0 {
		return nil, nil, inv.reject(diags)
	}
	kernel, diags, err := pipeline(set.files, inv.checks(), inv.h.Sequential)
	if err != nil {
		return nil, nil, inv.failure(err.Error())
	}
	if len(diags) > 0 {
		return nil, nil, inv.reject(diags)
	}
	return set, kernel, ExitOK
}

// checks are the checker options of an invocation, with the declarations of the fixture of its host.
func (inv *invocation) checks() check.Options {
	return check.Options{Debug: inv.debug, Mutating: inv.h.Mutating, Entities: inv.h.Entities}
}

// pipeline runs a source set through every stage before generation under the schedule of the build and returns the checked kernel program, stopping at the first stage that reports.
func pipeline(files []*source.File, opts check.Options, sequential bool) (*check.Program, []diagnostics.Record, error) {
	trees, diags := parseAll(files)
	if len(diags) > 0 {
		return nil, diags, nil
	}
	checked, diags, err := check.Check(trees, opts)
	if err != nil || len(diags) > 0 {
		return nil, diags, err
	}
	if diags := limits.Check(checked); len(diags) > 0 {
		return nil, diags, nil
	}
	return lower.Lower(checked, sequential)
}

func parseAll(files []*source.File) ([]*syntax.File, []diagnostics.Record) {
	var trees []*syntax.File
	var diags []diagnostics.Record
	for _, f := range files {
		tree, ds := syntax.Parse(f)
		trees = append(trees, tree)
		diags = append(diags, ds...)
	}
	diagnostics.Sort(diags)
	return trees, diags
}

// mainOf returns the main routine of a program, or nil.
func mainOf(p *check.Program) *syntax.Func {
	for _, f := range p.Files {
		for _, d := range f.Decls {
			if fn, ok := d.(*syntax.Func); ok && fn.Receiver == nil && fn.Name == "main" {
				return fn
			}
		}
	}
	return nil
}

// load selects the source set of an invocation: the project found from the working directory, a named project file, or ad-hoc source files.
func (inv *invocation) load() (*sourceSet, []diagnostics.Record, error) {
	switch {
	case len(inv.inputs) == 0:
		manifest, err := discover(inv.h.Dir)
		if err != nil {
			return nil, nil, err
		}
		return inv.project(manifest)
	case len(inv.inputs) == 1 && strings.HasSuffix(inv.inputs[0], project.Suffix):
		return inv.project(inv.inputs[0])
	}
	set := &sourceSet{}
	var diags []diagnostics.Record
	seen := map[string]bool{}
	for i, in := range inv.inputs {
		if strings.HasSuffix(in, project.Suffix) {
			return nil, nil, fmt.Errorf("ad-hoc files and a project file cannot be combined")
		}
		path := inv.path(in)
		if seen[path] {
			return nil, nil, fmt.Errorf("the file %s is given more than once", in)
		}
		seen[path] = true
		if in != "-" && filepath.Ext(in) != ".ri" {
			return nil, nil, fmt.Errorf("the input %s is not a .ri file or a project file", in)
		}
		data, err := inv.read(in)
		if err != nil {
			return nil, nil, err
		}
		f, rec := source.New(in, i, data)
		if rec != nil {
			diags = append(diags, *rec)
			continue
		}
		if rec := project.Embedded(f); rec != nil {
			diags = append(diags, *rec)
		}
		set.files = append(set.files, f)
		if i == 0 {
			set.name = strings.TrimSuffix(filepath.Base(in), ".ri")
		}
	}
	return set, diags, nil
}

// read returns the contents of an input, where "-" is standard input.
func (inv *invocation) read(in string) ([]byte, error) {
	if in == "-" {
		if inv.h.Stdin == nil {
			return nil, fmt.Errorf("no standard input")
		}
		return io.ReadAll(inv.h.Stdin)
	}
	return os.ReadFile(inv.path(in))
}

// path resolves a path of the command line against the working directory.
func (inv *invocation) path(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(inv.h.Dir, p)
}

// discover finds the project of the working directory: the first directory toward the root that contains exactly one project file.
func discover(dir string) (string, error) {
	for d := dir; ; d = filepath.Dir(d) {
		found, _ := filepath.Glob(filepath.Join(d, "*"+project.Suffix))
		if len(found) == 1 {
			return found[0], nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("no input given and no project file found from %s", dir)
		}
	}
}

// project loads the source set of a project file, with each file assigned to its module.
func (inv *invocation) project(manifest string) (*sourceSet, []diagnostics.Record, error) {
	data, err := os.ReadFile(inv.path(manifest))
	if err != nil {
		return nil, nil, err
	}
	rel := manifest
	if r, err := filepath.Rel(inv.h.Dir, inv.path(manifest)); err == nil {
		rel = r
	}
	p, diags := project.Read(rel, data)
	if len(diags) > 0 {
		return nil, diags, nil
	}
	dir := filepath.Dir(inv.path(manifest))
	members, diags := project.Members(os.DirFS(dir), p)
	if len(diags) > 0 {
		return nil, diags, nil
	}
	type member struct{ module, file string }
	var all []member
	for _, m := range members.Modules {
		for _, file := range m.Files {
			all = append(all, member{m.Name, filepath.ToSlash(file)})
		}
	}
	slices.SortFunc(all, func(a, b member) int { return strings.Compare(a.file, b.file) })
	set := &sourceSet{name: p.Name}
	for i, m := range all {
		data, err := os.ReadFile(filepath.Join(dir, m.file))
		if err != nil {
			return nil, nil, err
		}
		f, rec := source.New(filepath.ToSlash(filepath.Join(filepath.Dir(rel), m.file)), project.FileOrdinal+1+i, data)
		if rec != nil {
			diags = append(diags, *rec)
			continue
		}
		f.Module = m.module
		set.files = append(set.files, f)
	}
	return set, diags, nil
}
