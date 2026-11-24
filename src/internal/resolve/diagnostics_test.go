package resolve

import (
	"slices"
	"testing"

	"ritchie.dev/internal/testsrc"
)

func TestEachDeclarationFaultIsRejectedWithItsStatedCodes(t *testing.T) {
	tests := []struct {
		fact  string
		files []testsrc.File
		codes []string
	}{
		{"a name declared twice in one module is a duplicate declaration",
			[]testsrc.File{{Text: "Thing {}\nThing {}\n"}},
			[]string{"module.duplicate-declaration"}},
		{"a method declared twice on one receiver is a duplicate method",
			[]testsrc.File{{Text: "Tool {}\nTool.same() {}\nTool.same() {}\n"}},
			[]string{"method.duplicate-declaration"}},
		{"a method on a type of another module is rejected",
			[]testsrc.File{{Module: "lib", Path: "lib.ri", Text: "Tool {}\nTool.publish() {}\n"}, {Module: "app", Path: "app.ri", Text: "lib::Tool.extra() {}\n"}},
			[]string{"method.receiver-not-owned"}},
		{"a written type that names no declaration is unknown",
			[]testsrc.File{{Text: "use(Missing value) {}\n"}},
			[]string{"type.unknown"}},
		{"an error declared with the name of a built-in error is reserved",
			[]testsrc.File{{Text: "OutOfRange! {}\n"}},
			[]string{"name.reserved-name"}},
		{"an error field called history is reserved",
			[]testsrc.File{{Text: "Fault! { Int history }\n"}},
			[]string{"name.reserved-name"}},
		{"a qualifier that names no module is unknown",
			[]testsrc.File{{Text: "Local = missing::Thing\n"}},
			[]string{"module.unknown-module"}},
		{"a qualified name that its module does not declare is unknown",
			[]testsrc.File{{Module: "lib", Path: "lib.ri", Text: "Known {}\n"}, {Module: "app", Path: "app.ri", Text: "Local = lib::Missing\n"}},
			[]string{"module.unknown-member"}},
		{"a regular type without a published method is private to its module",
			[]testsrc.File{{Module: "lib", Path: "lib.ri", Text: "Secret {}\nSecret._make() {}\n"}, {Module: "app", Path: "app.ri", Text: "use(lib::Secret value) {}\n"}},
			[]string{"module.unpublished-member"}},
		{"modules that refer to each other form a cycle",
			[]testsrc.File{{Module: "a", Path: "a.ri", Text: "Aaa!\nFromB! = b::Bee!\n"}, {Module: "b", Path: "b.ri", Text: "Bee!\nFromA! = a::Aaa!\n"}},
			[]string{"module.dependency-cycle"}},
		{"main takes no parameters",
			[]testsrc.File{{Text: "main(Int value) {}\n"}},
			[]string{"main.invalid-signature"}},
		{"main in two root modules is rejected at every site",
			[]testsrc.File{{Module: "a", Path: "a.ri", Text: "main() {}\n"}, {Module: "b", Path: "b.ri", Text: "main() {}\n"}},
			[]string{"main.multiple-entry", "main.multiple-entry"}},
		{"two parameters of one routine have distinct names",
			[]testsrc.File{{Text: "same(Int value, Bool value) {}\n"}},
			[]string{"name.duplicate-name"}},
		{"parameters with defaults form a trailing suffix",
			[]testsrc.File{{Text: "bad(Int first = 1, Int required) {}\n"}},
			[]string{"parameter.default-order"}},
		{"a struct that contains itself through a fixed array is a by-value cycle",
			[]testsrc.File{{Text: "Bad { Bad[1] children }\n"}},
			[]string{"type.dependency-cycle"}},
		{"a struct may contain itself through a nullable field",
			[]testsrc.File{{Text: "Tree { Int value, Tree? next }\n"}},
			nil},
		{"a service method parameter cannot hold a random generator",
			[]testsrc.File{{Text: "Holder { r::Random random }\nHolder.marker() -> Int { return 1 }\n@Service { }\n@Service.nested(Holder value) -> Int { return 2 }\n"}},
			[]string{"service.boundary-type"}},
		{"a service method parameter cannot be a type private to another module",
			[]testsrc.File{{Module: "model", Path: "model.ri", Text: "Hidden { Int value }\n"}, {Module: "app", Path: "app.ri", Text: "@Service { }\n@Service.echo(Int value) -> model::Hidden { return value }\n"}},
			[]string{"module.unpublished-member"}},
	}
	for _, tt := range tests {
		for i := range tt.files {
			if tt.files[i].Path == "" {
				tt.files[i].Path = "case.ri"
			}
		}
		diags := collect(tt.files)
		var codes []string
		for _, d := range diags {
			codes = append(codes, d.Reason)
		}
		slices.Sort(codes)
		if !slices.Equal(codes, tt.codes) {
			t.Errorf("%s: got %v, want %v (%s)", tt.fact, codes, tt.codes, testsrc.Describe(diags))
		}
	}
}
