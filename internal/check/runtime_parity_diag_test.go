package check

import (
	"os"
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

// Writing a member the class does not declare throws on both backends; __set, static writes and dynamic receivers stay clean.
func TestSourceFlagsUnknownMemberAssignment(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "class Svc {\n    callable hook;\n    static int count = 0;\n    func greet(): string { return \"x\"; }\n    func poke(): void { this.later = 1; }\n}\n" +
		"class Sub extends Svc {}\n" +
		"class Open {\n    func __set(string name, any value): void {}\n}\n" +
		"Svc s = Svc();\ns.hook = () => \"y\";\ns.greet = () => \"z\";\ns.nope = 1;\n" +
		"Sub b = Sub();\nb.hook = () => \"y\";\nb.extra = 2;\n" +
		"Open o = Open();\no.anything = 3;\nSvc.count = 4;\nlet d = s as any;\nd.whatever = 5;\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	var got []string
	for _, d := range diags {
		if d.Rule == "semantic" && (strings.Contains(d.Message, "has no field") || strings.Contains(d.Message, "cannot be assigned")) {
			got = append(got, d.Message)
		}
	}
	want := []string{
		"Svc.greet is a method and cannot be assigned; declare a callable field to swap behaviour",
		"Svc has no field nope",
		"Sub has no field extra",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("diagnostics: %v (all: %+v)", got, diags)
	}
}

func TestSourceFlagsUnknownMemberAssignmentCrossModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "donors.gb"), []byte("module donors;\n\nexport class Base {\n    int n = 0;\n    func greet(): string { return \"b\"; }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.gb")
	src := "import donors;\n\nclass Local extends donors.Base {}\n" +
		"Local l = Local();\nl.n = 3;\nl.nope = 2;\nl.greet = 4;\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	var got []string
	for _, d := range diags {
		if d.Rule == "semantic" && (strings.Contains(d.Message, "has no field") || strings.Contains(d.Message, "cannot be assigned")) {
			got = append(got, d.Message)
		}
	}
	if len(got) != 2 || !strings.Contains(got[0], "Local has no field nope") || !strings.Contains(got[1], "Local.greet is a method and cannot be assigned") {
		t.Fatalf("diagnostics: %v (all: %+v)", got, diags)
	}
}
