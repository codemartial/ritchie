package gen

import (
	"go/token"
	"strings"
	"testing"
)

const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_!@$%"

// name is one Ritchie name of one kind, with the parts that its kind reads.
type name struct {
	kind                uint8
	module, owner, part string
}

// nameOf returns the name of a kind whose parts are drawn from the identifier alphabet.
func nameOf(kind uint8, module, owner, part string) name {
	n := name{kind % 8, inAlphabet(module), inAlphabet(owner), inAlphabet(part)}
	switch n.kind {
	case 0, 1, 4, 7:
		n.owner = ""
	case 5, 6:
		n.module, n.owner = "", ""
	}
	return n
}

// inAlphabet replaces each byte outside the identifier alphabet by a character of it.
func inAlphabet(s string) string {
	b := []byte(s)
	for i, c := range b {
		if strings.IndexByte(alphabet, c) < 0 {
			b[i] = alphabet[int(c)%len(alphabet)]
		}
	}
	return string(b)
}

// goName is the Go identifier that the generator writes for a name.
func (n name) goName() string {
	switch n.kind {
	case 0:
		return typeName(n.module, n.part)
	case 1:
		return freeName(n.module, n.part)
	case 2:
		return methodName(n.module, n.owner, n.part)
	case 3:
		return memberName(n.module, n.owner, n.part)
	case 4:
		return decoderName(n.module, n.part)
	case 5:
		return localName(n.part)
	case 7:
		return constName(n.module, n.part)
	default:
		return fieldName(n.part)
	}
}

func TestEachRitchieNameHasItsStatedGoIdentifier(t *testing.T) {
	table := []struct {
		name name
		want string
	}{
		{name{0, "", "", "Dollar__code"}, "T_d_dDollar_u_ucode"},
		{name{0, "bank", "", "Low!"}, "P_dbank_dLow_x"},
		{name{0, "", "", "@Auth"}, "T_d_d_sAuth"},
		{name{1, "", "", "give"}, "F_d_dgive"},
		{name{1, "geo", "", "area"}, "F_dgeo_darea"},
		{name{1, "", "", "geo__area"}, "F_d_dgeo_u_uarea"},
		{name{2, "", "Dollar", "code"}, "M_d_dDollar_dcode"},
		{name{3, "", "Suit", "HEARTS"}, "N_d_dSuit_dHEARTS"},
		{name{4, "", "", "Dollar"}, "D_d_dDollar"},
		{name{5, "", "", "count"}, "v_count"},
		{name{6, "", "", "x_m"}, "f_x_um"},
		{name{7, "geo", "", "TABLE"}, "C_dgeo_dTABLE"},
	}
	for _, row := range table {
		if got := row.name.goName(); got != row.want {
			t.Errorf("%+v: Go name %q, want %q", row.name, got, row.want)
		}
	}
}

func FuzzDistinctRitchieNamesHaveDistinctGoIdentifiers(f *testing.F) {
	f.Add(uint8(1), "", "", "a_b", uint8(1), "", "", "a")
	f.Add(uint8(1), "a_b", "", "c", uint8(1), "a", "", "b_c")
	f.Add(uint8(1), "m", "", "Broken!", uint8(1), "m", "", "Broken_x")
	f.Add(uint8(1), "m", "", "_hidden", uint8(1), "m", "", "_uhidden")
	f.Add(uint8(2), "", "Dollar", "code", uint8(0), "", "", "Dollar__code")
	f.Add(uint8(1), "geo", "", "area", uint8(1), "", "", "geo__area")
	f.Add(uint8(3), "", "Suit", "HEARTS", uint8(2), "", "Suit", "HEARTS")
	f.Add(uint8(5), "", "", "x_m", uint8(6), "", "", "x")
	f.Fuzz(func(t *testing.T, k1 uint8, m1, o1, p1 string, k2 uint8, m2, o2, p2 string) {
		a, b := nameOf(k1, m1, o1, p1), nameOf(k2, m2, o2, p2)
		ga, gb := a.goName(), b.goName()
		for _, g := range []string{ga, gb} {
			if !token.IsIdentifier(g) {
				t.Errorf("Go name %q is not a Go identifier", g)
			}
		}
		if a != b && ga == gb {
			t.Errorf("%+v and %+v have the same Go name %q", a, b, ga)
		}
	})
}
