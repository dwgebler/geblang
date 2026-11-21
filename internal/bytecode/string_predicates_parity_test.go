package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParityStringPredicates(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import bytes;
io.println("${"0123".isDigits()} ${"".isDigits()} ${"-1".isDigits()} ${"\u{0663}".isDigits()}");
io.println("${"abcXYZ".isAlpha()} ${"ab1".isAlpha()} ${"\u{e9}".isAlpha()} ${"a1B2".isAlphanumeric()} ${"a_1".isAlphanumeric()}");
io.println("${"hello world 1".isLower()} ${"Hello".isLower()} ${"123".isLower()} ${"ABC-9".isUpper()} ${"ABc".isUpper()}");
io.println("${"plain text!".isAscii()} ${"caf\u{e9}".isAscii()} ${"".isAscii()}");
io.println("${"DEADbeef09".isHex()} ${"0xff".isHex()} ${"fg".isHex()}");
io.println("${"123e4567-E89B-12d3-a456-426614174000".isUuid()} ${"123e4567e89b12d3a456426614174000".isUuid()} ${"123e4567-e89b-12d3-a456-42661417400g".isUuid()}");
io.println("${"a.b+c@mail.example.org".isEmail()} ${"a@b".isEmail()} ${"a b@c.io".isEmail()} ${"a@@b.io".isEmail()} ${"a@b..io".isEmail()} ${"@b.io".isEmail()}");
io.println("${"https://example.com/p?q=1#f".isUrl()} ${"postgres://u:p@h:5432/db".isUrl()} ${"/rel".isUrl()} ${"mailto:a@b.io".isUrl()} ${"http://".isUrl()} ${"http://a b".isUrl()}");
io.println("${"ok".isUtf8()} ${"".isUtf8()} ${bytes.rawString(bytes.fromHex("ff")).isUtf8()}");
io.println("${bytes.fromString("hi").isUtf8()} ${bytes.fromHex("ff").isUtf8()} ${bytes.fromHex("").isUtf8()}");
`)
	want := "true false false false\n" +
		"true false false true false\n" +
		"true false false true false\n" +
		"true false false\n" +
		"true false false\n" +
		"true false false\n" +
		"true false false false false false\n" +
		"true true false false false false\n" +
		"true true false\n" +
		"true false true\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityStringPredicatesDispatchContexts(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
class Field {
    string value;
    func Field(string value) { this.value = value; }
    func valid(): bool { return this.value.isAlphanumeric(); }
}
class Slug extends Field {
    func Slug(string value) { parent(value); }
}
func check(string s): bool { return s.isDigits(); }
any dyn = "42";
string typed = "ab";
io.println("${check("7")} ${check("x")} ${dyn.isDigits()} ${typed.isAlpha()}");
io.println("${Field("ab1").valid()} ${Slug("a-b").valid()}");
io.println(["1", "a", "22"].filter((string s) => s.isDigits()));
bool flag = false;
flag = flag || "x@y.io".isEmail();
io.println(flag);
try {
    "x".isDigits(1);
} catch (Error e) {
    io.println(e.message);
}
`)
	want := "true false true true\ntrue false\n[\"1\", \"22\"]\ntrue\nstring.isDigits expects no arguments\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityStringPredicatesCrossModule(t *testing.T) {
	dir := t.TempDir()
	donor := `module checks;

export func isContact(string s): bool { return s.isEmail() || s.isUrl(); }

export class Validator {
    func id(string s): bool { return s.isUuid(); }
}
`
	if err := os.WriteFile(filepath.Join(dir, "checks.gb"), []byte(donor), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, `import io;
import checks;
class Strict extends checks.Validator {}
io.println("${checks.isContact("a@b.io")} ${checks.isContact("https://x.io")} ${checks.isContact("nope")}");
io.println("${Strict().id("123e4567-e89b-12d3-a456-426614174000")} ${Strict().id("x")}");
`)
	want := "true true false\ntrue false\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
