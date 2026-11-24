package project

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"ritchie.dev/internal/diagnostics"
)

// SourceSet represents the complete membership of one project build.
type SourceSet struct {
	Modules []ModuleFiles
}

// ModuleFiles represents one module's assigned source files.
type ModuleFiles struct {
	Name  string
	Files []string
}

func Members(fsys fs.FS, p Project) (SourceSet, []diagnostics.Record) {
	var diags []diagnostics.Record
	rootSet := make(map[string]bool, len(p.Modules))
	for _, m := range p.Modules {
		rootSet[m.Root] = true
	}
	files := make(map[string][]string, len(p.Modules))
	walked := make(map[string]bool, len(p.Modules))
	for _, m := range p.Modules {
		info, err := fs.Stat(fsys, m.Root)
		if err != nil || !info.IsDir() {
			diags = append(diags, p.record(diagnostics.ProjectMissingRoot, m.RootSpan,
				fmt.Sprintf("root %q is not an existing directory", m.Root)))
			continue
		}
		found, walkErr := scanRoot(fsys, m.Root, rootSet)
		files[m.Name] = found
		if walkErr != nil {
			diags = append(diags, p.record(diagnostics.ProjectMissingRoot, m.RootSpan,
				fmt.Sprintf("Ritchie could not scan root %q", m.Root)))
			continue
		}
		walked[m.Name] = true
	}
	for _, m := range p.Modules {
		if walked[m.Name] && len(files[m.Name]) == 0 {
			diags = append(diags, p.record(diagnostics.ProjectEmptyRoot, m.RootSpan,
				fmt.Sprintf("root %q assigns no .ri file to module %q", m.Root, m.Name)))
		}
	}
	if diags != nil {
		return SourceSet{}, diags
	}
	var set SourceSet
	for _, m := range p.Modules {
		slices.Sort(files[m.Name])
		set.Modules = append(set.Modules, ModuleFiles{Name: m.Name, Files: files[m.Name]})
	}
	slices.SortFunc(set.Modules, func(a, b ModuleFiles) int {
		return strings.Compare(a.Name, b.Name)
	})
	return set, nil
}

// scanRoot returns the regular .ri files beneath root.
func scanRoot(fsys fs.FS, root string, roots map[string]bool) ([]string, error) {
	var files []string
	var visit func(dir string) error
	visit = func(dir string) error {
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return err
		}
		if dir != root && hasProjectFile(entries) {
			return nil
		}
		for _, e := range entries {
			pth := path.Join(dir, e.Name())
			switch {
			case e.IsDir():
				if roots[pth] {
					continue
				}
				if err := visit(pth); err != nil {
					return err
				}
			case strings.HasSuffix(e.Name(), ".ri") && e.Type().IsRegular():
				files = append(files, pth)
			}
		}
		return nil
	}
	return files, visit(root)
}

// hasProjectFile reports whether a directory's entries include a regular project file.
func hasProjectFile(entries []fs.DirEntry) bool {
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), Suffix) && e.Type().IsRegular() {
			return true
		}
	}
	return false
}
