package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestREPLUncaughtKeepsErrorClass(t *testing.T) {
	in := strings.NewReader("1 + \"2\"\n" +
		"list xs = [1]; xs[5]\n" +
		"\"abc\" as int\n" +
		"any x = 1;\n" +
		"any y = x + \"2\";\n" +
		":quit\n")
	var out, errOut bytes.Buffer
	if code := runREPL(in, &out, &errOut, replConfig{}); code != 0 {
		t.Fatalf("repl exit code %d (stderr %q)", code, errOut.String())
	}
	got := errOut.String()
	for _, want := range []string{
		"uncaught TypeError: unsupported operands for +: int and string\n  at <top level>\n",
		"uncaught ValueError: list index out of range\n  at <top level>\n",
		"uncaught ValueError: invalid integer literal \"abc\"\n  at <top level>\n",
		"uncaught TypeError: unsupported operands for +: int and string\n  at <top level> (line 1)\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("repl stderr missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "uncaught RuntimeError") {
		t.Fatalf("classified fault reported as RuntimeError:\n%s", got)
	}
}
