package lsp

import (
	"runtime"
	"testing"
)

func TestURIToPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("WSL URI mapping applies to non-windows servers")
	}
	cases := []struct {
		uri  string
		want string
	}{
		{"file:///home/daveg/projects/geblang/examples/main.gb", "/home/daveg/projects/geblang/examples/main.gb"},
		{"file://wsl.localhost/Ubuntu/home/daveg/projects/geblang/examples/main.gb", "/home/daveg/projects/geblang/examples/main.gb"},
		{"file://wsl%24/Ubuntu/home/daveg/main.gb", "/home/daveg/main.gb"},
		{"file:///c:/Users/dave/main.gb", "/mnt/c/Users/dave/main.gb"},
		{"untitled:Untitled-1", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := uriToPath(c.uri); got != c.want {
			t.Errorf("uriToPath(%q) = %q, want %q", c.uri, got, c.want)
		}
	}
}
