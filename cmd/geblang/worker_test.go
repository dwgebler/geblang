package main

import (
	"bytes"
	"testing"
)

func TestWorkerCommandRejectsMissingAndInvalidEntries(t *testing.T) {
	for _, args := range [][]string{nil, {"--entry"}, {"--entry", ""}, {"--entry", "a;import sys"}, {"--entry", "a", "--entry", "b"}} {
		var output bytes.Buffer

		code, err := runWorkerCommand(args, &output, &output)

		if code != 2 || err == nil {
			t.Fatalf("args %q: code=%d err=%v", args, code, err)
		}
	}
}

func TestWorkerCommandHelpNeedsNoApplication(t *testing.T) {
	var output bytes.Buffer

	code, err := runWorkerCommand([]string{"--help"}, &output, &output)

	if code != 0 || err != nil || !bytes.Contains(output.Bytes(), []byte("--entry")) {
		t.Fatalf("code=%d err=%v output=%q", code, err, output.String())
	}
}
