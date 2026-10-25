package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAPIPagesBuildsVirtualPages(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "api.gb")
	if err := os.WriteFile(source, []byte(`module app.api;

## Handles an API status check.
export @route("GET", "/status")
func status(): dict<string, any> {
    return {"ok": true};
}
`), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	pages, err := loadAPIPages([]string{source})
	if err != nil {
		t.Fatalf("load api pages: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("pages: %#v", pages)
	}
	page := pages[0]
	if !strings.HasPrefix(page.Output, "api/") || !strings.HasSuffix(page.Output, ".html") {
		t.Fatalf("output: %q", page.Output)
	}
	for _, want := range []string{
		"API: api",
		"app.api",
		"Function",
		"status",
		"Handles an API status check.",
		"route(&quot;GET&quot;, &quot;/status&quot;)",
	} {
		if !strings.Contains(page.HTML, want) {
			t.Fatalf("html missing %q: %q", want, page.HTML)
		}
	}
}

func TestAPISlug(t *testing.T) {
	tests := map[string]string{
		"stdlib":               "stdlib",
		"examples/api_docs.gb": "examples-api-docs-gb",
		"./":                   "source",
	}
	for input, want := range tests {
		if got := apiSlug(input); got != want {
			t.Fatalf("apiSlug(%q): got %q, want %q", input, got, want)
		}
	}
}

func TestMarkdownLinkRewriterStripsPrefixAndChangesExtension(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"01-getting-started.md", "getting-started.html"},
		{"22-ffi.md", "ffi.html"},
		{"stdlib/19-env-ext.md", "stdlib/env-ext.html"},
		{"sibling.md", "sibling.html"},
		{"01-getting-started.md#install", "getting-started.html#install"},
		{"https://example.com/page.md", "https://example.com/page.md"},
		{"//example.com/page.md", "//example.com/page.md"},
		{"mailto:foo@bar.md", "mailto:foo@bar.md"},
		{"#section", "#section"},
		{"sibling.html", "sibling.html"},
		{"", ""},
	}
	for _, c := range cases {
		got := string(rewriteMarkdownLink(c.in))
		if got != c.want {
			t.Errorf("rewriteMarkdownLink(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMarkdownToHTMLRewritesLocalMDLinks(t *testing.T) {
	html := markdownToHTML([]byte("[ext](19-env-ext.md) and [home](01-getting-started.md)"))
	if !strings.Contains(html, `href="env-ext.html"`) {
		t.Errorf("expected env-ext.html in output: %s", html)
	}
	if !strings.Contains(html, `href="getting-started.html"`) {
		t.Errorf("expected getting-started.html in output: %s", html)
	}
}

func TestMarkdownToHTMLSupportsGitHubEmojiAndStyledQuotes(t *testing.T) {
	html := markdownToHTML([]byte("> :bulb: Use `:bulb:` in docs.\n\n`:warning:`\n"))
	if !strings.Contains(html, "&#x1f4a1;") {
		t.Fatalf("expected :bulb: to render as emoji: %q", html)
	}
	if strings.Contains(html, "<code>&#x26a0;&#xfe0f;</code>") {
		t.Fatalf("emoji inside code should not be substituted: %q", html)
	}
	if !strings.Contains(html, "<blockquote>") {
		t.Fatalf("expected blockquote HTML: %q", html)
	}
}

func TestLayoutIncludesGeblangSyntaxHighlighter(t *testing.T) {
	html := layout([]page{{Output: "index.html", Title: "Home", HTML: `<pre><code class="language-gb">let x = 1;</code></pre>`}}, page{Output: "index.html", Title: "Home", HTML: `<pre><code class="language-gb">let x = 1;</code></pre>`}, nil, nil)
	for _, want := range []string{
		"highlightGeblang",
		"pre code.language-gb",
		"hl-keyword",
		"hl-string",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("layout missing %q: %q", want, html)
		}
	}
}

func TestTableOfContentsListsHeadings(t *testing.T) {
	body := markdownToHTML([]byte("# Title\n\n## First Section\n\ntext\n\n### Sub Detail\n\n## Second Section\n\nmore\n"))
	toc := tableOfContents(body)
	for _, want := range []string{
		`class="toc"`,
		"On this page",
		`href="#first-section">First Section`,
		`toc-sub"><a href="#sub-detail">Sub Detail`,
		`href="#second-section">Second Section`,
	} {
		if !strings.Contains(toc, want) {
			t.Fatalf("TOC missing %q: %q", want, toc)
		}
	}
	// The h1 title is not a TOC entry.
	if strings.Contains(toc, ">Title<") {
		t.Errorf("TOC should not include the page h1: %q", toc)
	}
}

func TestTableOfContentsEmptyForFewHeadings(t *testing.T) {
	if toc := tableOfContents(markdownToHTML([]byte("# Only\n\n## One Section\n\ntext\n"))); toc != "" {
		t.Errorf("expected no TOC for a single section, got: %q", toc)
	}
}

func TestLayoutRendersTableOfContents(t *testing.T) {
	body := markdownToHTML([]byte("# Guide\n\n## Alpha\n\na\n\n## Beta\n\nb\n"))
	out := layout([]page{{Output: "guide.html", Title: "Guide", HTML: body}}, page{Output: "guide.html", Title: "Guide", HTML: body}, nil, nil)
	if !strings.Contains(out, "On this page") || !strings.Contains(out, `href="#alpha"`) {
		t.Fatalf("layout missing in-page TOC: %q", out)
	}
	if !strings.Contains(out, "col-lg-7") {
		t.Errorf("layout should narrow the article when a TOC is present")
	}
}

func TestLoadExamplePagesBuildsIndexGroupAndFilePages(t *testing.T) {
	dir := t.TempDir()
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "README.md"), []byte("# App Example\n\nA multi-file example.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `/**
 * @title App Main
 * @summary Runs the application.
 * @category Applications
 * @description Shows how a multi-file example can be documented.
 */
import io;

io.println("app");
`
	if err := os.WriteFile(filepath.Join(appDir, "main.gb"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "service.gb"), []byte(`/**
 * @title App Service
 * @summary Provides app services.
 * @category Applications
 */
export func name(): string { return "app"; }
`), 0o644); err != nil {
		t.Fatal(err)
	}

	pages, err := loadExamplePages(dir, filepath.Join(dir, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 4 {
		t.Fatalf("expected index, group and two file pages, got %#v", pages)
	}
	if pages[0].Title != "Examples" || pages[0].Output != "examples/index.html" {
		t.Fatalf("unexpected index page %#v", pages[0])
	}
	if pages[1].Output != "examples/app/index.html" {
		t.Fatalf("unexpected group output %q", pages[1].Output)
	}
	filePage := pages[2]
	if filePage.Title != "App Main" {
		t.Fatalf("file title = %q", filePage.Title)
	}
	for _, want := range []string{
		"Runs the application.",
		"Shows how a multi-file example can be documented.",
		filepath.ToSlash(filepath.Join(dir, "app", "main.gb")),
		`<pre><code class="language-gb">import io;`,
	} {
		if !strings.Contains(filePage.HTML, want) {
			t.Fatalf("example html missing %q: %q", want, filePage.HTML)
		}
	}
	if strings.Contains(filePage.HTML, "@title") {
		t.Fatalf("example code should omit metadata docblock: %q", filePage.HTML)
	}
}

func TestExampleReadmeLinksResolveToGeneratedPages(t *testing.T) {
	root := filepath.Join(t.TempDir(), "examples")
	docsSource := filepath.Join(filepath.Dir(root), "docs")
	readme := "[source](main.gb) [guide](../../docs/29-services-yaml.md#parameters) [external](https://example.com/a.md)"

	html := exampleReadmeHTML(readme, root, docsSource, "sample")

	for _, want := range []string{
		`href="main.html"`,
		`href="../../services-yaml.html#parameters"`,
		`href="https://example.com/a.md"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("example readme missing %q: %s", want, html)
		}
	}
}

func TestLoadPagesExcludesArchiveFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "01-intro.md"), []byte("# Intro\n\ntext\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "18-release-notes-archive.md"), []byte("# Archive\n\nold notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pages, err := loadPages(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Title != "Intro" {
		t.Fatalf("expected only the non-archive page, got %#v", pages)
	}
}

// --search/--search-scope inject a navbar form; absent, pages stay form-free.
func TestSearchFormInjection(t *testing.T) {
	searchURL = ""
	searchScope = ""
	if got := searchFormHTML(); got != "" {
		t.Fatalf("expected empty form without flags, got %q", got)
	}
	parseExtraArgs([]string{"--search", "/search", "--search-scope", "geblang"})
	defer func() { searchURL = ""; searchScope = "" }()
	form := searchFormHTML()
	for _, want := range []string{`action="/search"`, `name="q"`, `name="product" value="geblang"`} {
		if !strings.Contains(form, want) {
			t.Fatalf("form missing %q: %q", want, form)
		}
	}
}
