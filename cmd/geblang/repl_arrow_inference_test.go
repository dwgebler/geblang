package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestREPLArrowsInferFromEarlierLines(t *testing.T) {
	in := strings.NewReader("let xs = [1, 2, 3];\n" +
		"xs.map((x) => x * 10)\n" +
		"list<string> ws = [\"a\", \"b\"];\n" +
		"ws.map((w) => w.upper())\n" +
		"let parts = \"p,q\".split(\",\");\n" +
		"parts.filter((p) => p == \"q\")\n" +
		"let f = (y) => y;\n" +
		":quit\n")
	var out, errOut bytes.Buffer
	if code := runREPL(in, &out, &errOut, replConfig{}); code != 0 {
		t.Fatalf("repl exit code %d (stderr %q)", code, errOut.String())
	}
	for _, want := range []string{"[10, 20, 30]", "[\"A\", \"B\"]", "[\"q\"]"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("repl stdout missing %q:\n%s\nstderr:\n%s", want, out.String(), errOut.String())
		}
	}
	if !strings.Contains(errOut.String(), "arrow parameter y needs a type here") {
		t.Fatalf("untyped arrow without context not rejected:\n%s", errOut.String())
	}
	if strings.Count(errOut.String(), "needs a type") != 1 {
		t.Fatalf("unexpected inference errors:\n%s", errOut.String())
	}
}
