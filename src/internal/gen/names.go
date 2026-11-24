// This file constructs the Go identifiers of generated programs, which a kind
// prefix and escaped parts keep distinct for distinct Ritchie names.
package gen

import "strings"

// esc returns one part of a Ritchie name as a Go identifier fragment, in which "_d" does not occur.
func esc(part string) string {
	var b strings.Builder
	for _, c := range part {
		switch c {
		case '_':
			b.WriteString("_u")
		case '!':
			b.WriteString("_x")
		case '@':
			b.WriteString("_s")
		case '$':
			b.WriteString("_e")
		case '%':
			b.WriteString("_q")
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// typeName is the Go name of a declared struct, enum, or service type, or of the payload struct of an error type.
func typeName(module, name string) string {
	if strings.HasSuffix(name, "!") {
		return "P_d" + esc(module) + "_d" + esc(name)
	}
	return "T_d" + esc(module) + "_d" + esc(name)
}

// freeName is the Go name of a free function.
func freeName(module, name string) string { return "F_d" + esc(module) + "_d" + esc(name) }

// methodName is the Go name of a method of an owner type.
func methodName(module, owner, name string) string {
	return "M_d" + esc(module) + "_d" + esc(owner) + "_d" + esc(name)
}

// memberName is the Go name of the constant of an enum member.
func memberName(module, enum, member string) string {
	return "N_d" + esc(module) + "_d" + esc(enum) + "_d" + esc(member)
}

// constName is the Go name of the variable of a module constant.
func constName(module, name string) string { return "C_d" + esc(module) + "_d" + esc(name) }

// decoderName is the Go name of the JSON decoder of a struct type.
func decoderName(module, name string) string { return "D_d" + esc(module) + "_d" + esc(name) }

// localName is the Go name of a local variable, parameter, or arm binding.
func localName(name string) string { return "v_" + esc(name) }

// fieldName is the Go name of a struct or payload field.
func fieldName(name string) string { return "f_" + esc(name) }
