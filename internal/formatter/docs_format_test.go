package formatter_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"geblang/internal/formatter"
)

var gbFence = regexp.MustCompile("(?ms)^([ \t]*)```(?:gb|geblang)[ \t]*\n(.*?)^[ \t]*```[ \t]*$")

// Code blocks that parse must already be in geblang fmt form; fragments that do not parse are skipped.
func TestManualCodeBlocksAreFormatted(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "user")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() || !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, "18-release-notes") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(raw)
		for _, m := range gbFence.FindAllStringSubmatchIndex(src, -1) {
			indent := src[m[2]:m[3]]
			code := dedent(src[m[4]:m[5]], indent)
			out, err := formatter.Format([]byte(code))
			if err != nil {
				continue
			}
			if got := strings.TrimRight(string(out), "\n") + "\n"; got != code {
				line := strings.Count(src[:m[0]], "\n") + 1
				t.Errorf("%s:%d: code block is not geblang fmt formatted; want:\n%s", path, line, got)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func dedent(code, indent string) string {
	if indent == "" {
		return code
	}
	lines := strings.Split(code, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, indent)
	}
	return strings.Join(lines, "\n")
}
