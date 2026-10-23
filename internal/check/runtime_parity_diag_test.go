package check

import (
	"path/filepath"
	"strings"
	"testing"
)

// 1.37.7: geblang check reports two calls both backends refuse at runtime.

// A field holding a callable is not a method: obj.field(...) throws
// "unknown method"; (obj.field)(...) calls the value and stays clean.
func TestSourceFlagsMethodCallOfCallableField(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "class Reg {\n    callable make;\n    func Reg(callable make) { this.make = make; }\n    func run(): any { return this.make(1); }\n}\n" +
		"Reg r = Reg(func(int x): int { return x; });\n(r.make)(1);\nr.make(2);\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	var got []string
	for _, d := range diags {
		if d.Rule == "semantic" && strings.Contains(d.Message, "is a field, not a method") {
			got = append(got, d.Message)
		}
	}
	if len(got) != 1 || !strings.Contains(got[0], "Reg.make is a field, not a method; call the value it holds with (r.make)(...)") {
		t.Fatalf("diagnostics: %v (all: %+v)", got, diags)
	}
}

// Lists, dicts and sets have no arithmetic or bitwise operators; a class
// that overloads one for a list argument is not flagged.
func TestSourceFlagsArithmeticOnCollections(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "list<int> a = [1];\nlist<int> b = [2];\nlet c = a + b;\nlet d = {\"k\": 1} + {\"j\": 2};\nlet e = \"x\" + [1];\nlet f = [1] == [1];\n" +
		"class Bag {\n    func __add(list<int> more): Bag { return this; }\n}\nlet g = Bag() + [1];\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	var got []string
	for _, d := range diags {
		if d.Rule == "semantic" && strings.HasPrefix(d.Message, "cannot use ") {
			got = append(got, d.Message)
		}
	}
	if len(got) != 3 || !strings.Contains(got[0], "lists have no + operator") {
		t.Fatalf("diagnostics: %v", got)
	}
}
