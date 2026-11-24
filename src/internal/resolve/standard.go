// This file enters the built-in types with their methods, the built-in error catalog,
// and the standard module r from the rows of the builtin table.
package resolve

import (
	"strings"

	"ritchie.dev/internal/builtin"
)

// addStandard enters the built-in types, the error catalog, and the standard modules into the table.
func (t *Table) addStandard() {
	owners := map[string]*Entry{}
	owner := func(text string) *Entry {
		if e := owners[text]; e != nil {
			return e
		}
		module, name, qualified := strings.Cut(text, "::")
		if !qualified {
			module, name = "", text
		}
		e := &Entry{Kind: BuiltinKind, Module: module, Name: name, members: map[string]*Entry{}}
		if qualified {
			e.Kind, e.Published = StructKind, true
			t.modules[module] = true
			t.entries[Key{module, name}] = e
		} else {
			t.builtins[name] = e
		}
		owners[text] = e
		return e
	}
	for _, r := range builtin.Rows {
		switch r.Form {
		case builtin.TypeRow:
			owner(r.Name)
		case builtin.ErrorRow:
			t.builtins[r.Name] = &Entry{Kind: ErrorKind, Name: r.Name}
		case builtin.TypeMethod, builtin.Method:
			e := owner(r.Owner)
			member := e.members[r.Member]
			if member == nil {
				member = &Entry{Kind: MethodKind, Module: e.Module, Name: r.Member, Owner: e, Routine: StructMethod, Level: levelOfForm(r.Form)}
				e.members[r.Member] = member
			} else if member.Level != levelOfForm(r.Form) {
				member.Level = EitherLevel
			}
			member.Standalone = member.Standalone || r.Standalone
		}
	}
}

// levelOfForm is the level of a built-in method from the form of its row.
func levelOfForm(f builtin.Form) Level {
	if f == builtin.TypeMethod {
		return TypeLevel
	}
	return InstanceLevel
}
