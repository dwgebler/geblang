package bytecode_test

import "testing"

func TestParityStringPadNeverTruncatesAndCyclesPad(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
io.println("hello".padStart(3));
io.println("hello".padEnd(3));
io.println("x".padStart(4, "ab"));
io.println("x".padEnd(4, "ab"));
io.println("x".padStart(4, "\u{e9}-"));
io.println("x".padEnd(4, "\u{e9}-"));
io.println("\u{e9}".padStart(3, "0"));
io.println("x".padStart(-1) + "|");
try {
    "x".padStart(3, "");
} catch (Error e) {
    io.println(e.message);
}
`)
	want := "hello\nhello\nabax\nxaba\né-éx\nxé-é\n00é\nx|\nstring.padStart: pad must be a non-empty string\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
