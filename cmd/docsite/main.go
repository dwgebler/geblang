package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"geblang/internal/sourcedoc"

	"github.com/yuin/goldmark"
	emoji "github.com/yuin/goldmark-emoji"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	gtext "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type page struct {
	Source string
	Origin string
	Output string
	Title  string
	HTML   string
	Depth  int
}

var docMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM, emoji.Emoji),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
		parser.WithASTTransformers(util.Prioritized(mdLinkRewriter{}, 999)),
	),
	goldmark.WithRendererOptions(goldhtml.WithUnsafe()),
)

// mdLinkRewriter rewrites local markdown links so docsite output
// matches its slug convention: `01-getting-started.md` -> `getting-started.html`.
// External URLs (those with a scheme or starting with `//`) and
// anchors (`#section`) are untouched.
type mdLinkRewriter struct{}

func (mdLinkRewriter) Transform(node *gast.Document, reader gtext.Reader, _ parser.Context) {
	_ = gast.Walk(node, func(n gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}
		link, ok := n.(*gast.Link)
		if !ok {
			return gast.WalkContinue, nil
		}
		link.Destination = rewriteMarkdownLink(string(link.Destination))
		return gast.WalkContinue, nil
	})
}

func rewriteMarkdownLink(dest string) []byte {
	target := dest
	fragment := ""
	if i := strings.Index(target, "#"); i >= 0 {
		fragment = target[i:]
		target = target[:i]
	}
	if target == "" {
		return []byte(dest)
	}
	if strings.Contains(target, "://") || strings.HasPrefix(target, "//") || strings.HasPrefix(target, "mailto:") {
		return []byte(dest)
	}
	if !strings.HasSuffix(strings.ToLower(target), ".md") {
		return []byte(dest)
	}
	dir, base := filepath.Split(target)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	rewritten := filepath.ToSlash(filepath.Join(dir, slugBase(stem)+".html")) + fragment
	return []byte(rewritten)
}

func main() {
	srcDir := arg(1, "docs/user")
	outDir := arg(2, "docs/site")
	apiSources, exampleSource := parseExtraArgs(os.Args[3:])

	pages, err := loadPages(srcDir)
	if err != nil {
		fatal(err)
	}
	apiPages, err := loadAPIPages(apiSources)
	if err != nil {
		fatal(err)
	}
	pages = append(pages, apiPages...)
	examplePages, err := loadExamplePages(exampleSource)
	if err != nil {
		fatal(err)
	}
	pages = append(pages, examplePages...)
	if len(pages) == 0 {
		fatal(fmt.Errorf("no markdown files found in %s", srcDir))
	}
	if err := os.RemoveAll(outDir); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatal(err)
	}
	for i := range pages {
		if err := writePage(outDir, pages, i); err != nil {
			fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outDir, manualCSSFile), []byte(manualCSS), 0o644); err != nil {
		fatal(err)
	}
	if pages[0].Output != "index.html" {
		if err := writeRedirect(outDir, pages[0].Output); err != nil {
			fatal(err)
		}
	}
	if canonicalBase != "" {
		if err := os.WriteFile(filepath.Join(outDir, "sitemap.xml"), []byte(sitemapXML(pages, gitLastModified)), 0o644); err != nil {
			fatal(err)
		}
	}
}

// pageURL is the absolute URL of a page under --base-url.
func pageURL(p page) string {
	base := strings.TrimRight(canonicalBase, "/")
	out := filepath.ToSlash(p.Output)
	if out == "index.html" {
		return base + "/"
	}
	return base + "/" + out
}

func sitemapXML(pages []page, lastModified func(string) string) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, p := range pages {
		// The agent guide is published for tools, not as a manual page for search.
		if strings.EqualFold(filepath.Base(p.Output), "AGENTS.html") {
			continue
		}
		b.WriteString("  <url><loc>" + html.EscapeString(pageURL(p)) + "</loc>")
		if p.Origin != "" {
			if date := lastModified(p.Origin); date != "" {
				b.WriteString("<lastmod>" + date + "</lastmod>")
			}
		}
		b.WriteString("</url>\n")
	}
	b.WriteString("</urlset>\n")
	return b.String()
}

// gitLastModified returns the last commit date (YYYY-MM-DD) touching path, or "" outside git.
func gitLastModified(path string) string {
	out, err := exec.Command("git", "log", "-1", "--format=%cs", "--", path).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func parseExtraArgs(args []string) ([]string, string) {
	var apiSources []string
	exampleSource := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--examples" {
			if i+1 < len(args) {
				exampleSource = args[i+1]
				i++
			}
			continue
		}
		if args[i] == "--search" {
			if i+1 < len(args) {
				searchURL = args[i+1]
				i++
			}
			continue
		}
		if args[i] == "--search-scope" {
			if i+1 < len(args) {
				searchScope = args[i+1]
				i++
			}
			continue
		}
		if args[i] == "--site-name" {
			if i+1 < len(args) {
				siteName = args[i+1]
				i++
			}
			continue
		}
		if args[i] == "--og-image" {
			if i+1 < len(args) {
				ogImage = args[i+1]
				i++
			}
			continue
		}
		if args[i] == "--base-url" {
			if i+1 < len(args) {
				canonicalBase = args[i+1]
				i++
			}
			continue
		}
		apiSources = append(apiSources, args[i])
	}
	return apiSources, exampleSource
}

// searchURL/searchScope, when set via --search/--search-scope, inject a
// navbar search form into every page (used by the docs website build;
// plain static builds leave them empty).
var (
	searchURL     string
	searchScope   string
	canonicalBase string
	siteName      = "Geblang"
	ogImage       string
)

func searchFormHTML() string {
	if searchURL == "" {
		return ""
	}
	scope := ""
	if searchScope != "" {
		scope = `<input type="hidden" name="product" value="` + searchScope + `">`
	}
	return `
      <form class="d-flex ms-auto me-3" role="search" action="` + searchURL + `" method="get">
        <input class="form-control form-control-sm me-2" type="search" name="q" placeholder="Search this manual">` + scope + `
        <button class="btn btn-outline-light btn-sm" type="submit">Search</button>
      </form>`
}

func loadAPIPages(sources []string) ([]page, error) {
	pages := make([]page, 0, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source) == "" {
			continue
		}
		report, err := sourcedoc.Collect(source)
		if err != nil {
			return nil, fmt.Errorf("source api docs %s: %w", source, err)
		}
		title := "API: " + apiTitle(source)
		var markdown bytes.Buffer
		sourcedoc.WritePageMarkdown(&markdown, title, report)
		output := filepath.ToSlash(filepath.Join("api", apiSlug(source)+".html"))
		pages = append(pages, page{
			Source: source,
			Origin: source,
			Output: output,
			Title:  title,
			HTML:   markdownToHTML(markdown.Bytes()),
			Depth:  1,
		})
	}
	return pages, nil
}

type exampleDoc struct {
	Path        string
	Root        string
	Rel         string
	Title       string
	Summary     string
	Category    string
	Description string
	Code        string
}

func loadExamplePages(source string) ([]page, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	var files []string
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".gb" {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(files)
	examples := make([]exampleDoc, 0, len(files))
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(source, file)
		if err != nil {
			return nil, err
		}
		doc := parseExampleDoc(filepath.ToSlash(rel), string(content))
		doc.Path = file
		doc.Root = filepath.ToSlash(source)
		examples = append(examples, doc)
	}
	if len(examples) == 0 {
		return nil, nil
	}
	pages := []page{exampleIndexPage(source, examples)}
	pages = append(pages, exampleGroupPages(source, examples)...)
	for _, example := range examples {
		pages = append(pages, examplePage(example))
	}
	return pages, nil
}

func parseExampleDoc(rel, source string) exampleDoc {
	doc := exampleDoc{
		Rel:      rel,
		Title:    titleFromExamplePath(rel),
		Summary:  "A runnable Geblang example.",
		Category: categoryFromExamplePath(rel),
		Code:     source,
	}
	trimmed := strings.TrimLeft(source, "\ufeff\r\n\t ")
	if !strings.HasPrefix(trimmed, "/**") {
		return doc
	}
	end := strings.Index(trimmed, "*/")
	if end < 0 {
		return doc
	}
	block := trimmed[len("/**"):end]
	doc.Code = strings.TrimLeft(trimmed[end+len("*/"):], "\r\n")
	var description []string
	for _, rawLine := range strings.Split(block, "\n") {
		line := strings.TrimSpace(rawLine)
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, " ")
		if ok && strings.HasPrefix(key, "@") {
			value = strings.TrimSpace(value)
			switch strings.TrimPrefix(key, "@") {
			case "title":
				doc.Title = value
			case "summary":
				doc.Summary = value
			case "category":
				doc.Category = value
			case "description":
				if value != "" {
					description = append(description, value)
				}
			}
			continue
		}
		description = append(description, line)
	}
	doc.Description = strings.Join(description, "\n\n")
	return doc
}

func exampleIndexPage(root string, examples []exampleDoc) page {
	items := append([]exampleDoc(nil), examples...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category == items[j].Category {
			return items[i].Rel < items[j].Rel
		}
		return items[i].Category < items[j].Category
	})
	var body strings.Builder
	body.WriteString(`<h1>Examples</h1>`)
	body.WriteString(`<p>The examples are generated from runnable <code>.gb</code> files under <code>` + html.EscapeString(filepath.ToSlash(root)) + `</code>. Each page includes the example summary and source code.</p>`)
	groupNames := multiFileExampleGroupNames(root, examples)
	if len(groupNames) > 0 {
		body.WriteString(`<h2>Multi-file Examples</h2><div class="list-group mb-4">`)
		for _, name := range groupNames {
			title := titleFromExamplePath(name + ".gb")
			body.WriteString(`<a class="list-group-item list-group-item-action" href="` + relHref("examples/index.html", exampleGroupOutputPath(name)) + `">`)
			body.WriteString(`<div class="fw-semibold">` + html.EscapeString(title) + `</div>`)
			body.WriteString(`<div class="small text-muted">` + html.EscapeString(filepath.ToSlash(filepath.Join(root, name))) + `</div>`)
			body.WriteString(`<div>Browse this example's source tree as a grouped application.</div></a>`)
		}
		body.WriteString(`</div>`)
	}
	currentCategory := ""
	for _, example := range items {
		if example.Category != currentCategory {
			if currentCategory != "" {
				body.WriteString(`</div>`)
			}
			currentCategory = example.Category
			body.WriteString(`<h2>` + html.EscapeString(currentCategory) + `</h2><div class="list-group mb-4">`)
		}
		body.WriteString(`<a class="list-group-item list-group-item-action" href="` + relHref("examples/index.html", exampleOutputPath(example.Rel)) + `">`)
		body.WriteString(`<div class="fw-semibold">` + html.EscapeString(example.Title) + `</div>`)
		body.WriteString(`<div class="small text-muted">` + html.EscapeString(example.Rel) + `</div>`)
		body.WriteString(`<div>` + html.EscapeString(example.Summary) + `</div></a>`)
	}
	if currentCategory != "" {
		body.WriteString(`</div>`)
	}
	return page{Source: "examples", Origin: root, Output: "examples/index.html", Title: "Examples", HTML: body.String(), Depth: 0}
}

func exampleGroupPages(root string, examples []exampleDoc) []page {
	groups := exampleGroups(examples)
	names := multiFileExampleGroupNames(root, examples)
	pages := make([]page, 0, len(names))
	for _, name := range names {
		files := groups[name]
		sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
		title := titleFromExamplePath(name + ".gb")
		var body strings.Builder
		body.WriteString(`<h1>` + html.EscapeString(title) + `</h1>`)
		body.WriteString(`<p class="lead">This is a multi-file example. Use the links below to inspect each source file in the example tree.</p>`)
		if readme, ok := readExampleReadme(root, name); ok {
			body.WriteString(markdownToHTML([]byte(readme)))
		}
		body.WriteString(`<h2>Files</h2><div class="list-group mb-4">`)
		for _, file := range files {
			body.WriteString(`<a class="list-group-item list-group-item-action" href="` + relHref(exampleGroupOutputPath(name), exampleOutputPath(file.Rel)) + `">`)
			body.WriteString(`<div class="fw-semibold">` + html.EscapeString(file.Title) + `</div>`)
			body.WriteString(`<div class="small text-muted">` + html.EscapeString(file.Rel) + `</div>`)
			body.WriteString(`<div>` + html.EscapeString(file.Summary) + `</div></a>`)
		}
		body.WriteString(`</div>`)
		pages = append(pages, page{
			Source: filepath.ToSlash(filepath.Join("examples", name)),
			Origin: filepath.Join(root, name),
			Output: exampleGroupOutputPath(name),
			Title:  title,
			HTML:   body.String(),
			Depth:  1,
		})
	}
	return pages
}

func exampleGroups(examples []exampleDoc) map[string][]exampleDoc {
	groups := map[string][]exampleDoc{}
	for _, example := range examples {
		top := topExampleDir(example.Rel)
		if top == "" {
			continue
		}
		groups[top] = append(groups[top], example)
	}
	return groups
}

func multiFileExampleGroupNames(root string, examples []exampleDoc) []string {
	groups := exampleGroups(examples)
	var names []string
	for name, files := range groups {
		if len(files) > 1 && isGroupedExample(root, name, files) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func isGroupedExample(root, name string, files []exampleDoc) bool {
	if _, ok := readExampleReadme(root, name); ok {
		return true
	}
	for _, file := range files {
		if strings.Count(filepath.ToSlash(file.Rel), "/") > 1 {
			return true
		}
	}
	return false
}

func examplePage(example exampleDoc) page {
	var body strings.Builder
	body.WriteString(`<h1>` + html.EscapeString(example.Title) + `</h1>`)
	body.WriteString(`<p class="lead">` + html.EscapeString(example.Summary) + `</p>`)
	body.WriteString(`<dl class="row"><dt class="col-sm-2">Path</dt><dd class="col-sm-10"><code>` + html.EscapeString(filepath.ToSlash(filepath.Join(example.Root, example.Rel))) + `</code></dd>`)
	body.WriteString(`<dt class="col-sm-2">Category</dt><dd class="col-sm-10">` + html.EscapeString(example.Category) + `</dd></dl>`)
	if strings.TrimSpace(example.Description) != "" {
		body.WriteString(markdownToHTML([]byte(example.Description)))
	}
	body.WriteString(`<h2>Source</h2>`)
	body.WriteString(`<pre><code class="language-gb">` + html.EscapeString(example.Code) + `</code></pre>`)
	return page{
		Source: filepath.ToSlash(filepath.Join("examples", example.Rel)),
		Origin: filepath.Join(example.Root, example.Rel),
		Output: exampleOutputPath(example.Rel),
		Title:  example.Title,
		HTML:   body.String(),
		Depth:  1 + strings.Count(filepath.ToSlash(example.Rel), "/"),
	}
}

func topExampleDir(rel string) string {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	if len(parts) <= 1 {
		return ""
	}
	return parts[0]
}

func exampleGroupOutputPath(name string) string {
	return filepath.ToSlash(filepath.Join("examples", slugBase(name), "index.html"))
}

func readExampleReadme(root, name string) (string, bool) {
	for _, filename := range []string{"README.md", "readme.md"} {
		data, err := os.ReadFile(filepath.Join(root, name, filename))
		if err == nil {
			return string(data), true
		}
	}
	return "", false
}

func exampleOutputPath(rel string) string {
	rel = filepath.ToSlash(rel)
	ext := filepath.Ext(rel)
	base := strings.TrimSuffix(rel, ext)
	return filepath.ToSlash(filepath.Join("examples", slugDir(base)+".html"))
}

func titleFromExamplePath(rel string) string {
	base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	base = strings.ReplaceAll(base, "_", " ")
	base = strings.ReplaceAll(base, "-", " ")
	parts := strings.Fields(base)
	for i, part := range parts {
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	if len(parts) == 0 {
		return rel
	}
	return strings.Join(parts, " ")
}

func categoryFromExamplePath(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." || dir == "" {
		return "General"
	}
	parts := strings.Split(dir, "/")
	if len(parts) > 0 && parts[0] != "" {
		return titleFromExamplePath(parts[0] + ".gb")
	}
	return "General"
}

func arg(index int, fallback string) string {
	if len(os.Args) > index && os.Args[index] != "" {
		return os.Args[index]
	}
	return fallback
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "docsite:", err)
	os.Exit(1)
}

func loadPages(srcDir string) ([]page, error) {
	var files []string
	err := filepath.WalkDir(srcDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".md" {
			// *-archive.md stays in-repo for history but is left out of the site.
			if strings.HasSuffix(strings.TrimSuffix(filepath.Base(path), ".md"), "-archive") {
				return nil
			}
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	pages := make([]page, 0, len(files))
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		title := titleFromMarkdown(content, file)
		rel, err := filepath.Rel(srcDir, file)
		if err != nil {
			return nil, err
		}
		out := outputPath(rel)
		pages = append(pages, page{
			Source: file,
			Origin: file,
			Output: out,
			Title:  title,
			HTML:   markdownToHTML(content),
			Depth:  strings.Count(filepath.ToSlash(rel), "/"),
		})
	}
	return pages, nil
}

func outputPath(rel string) string {
	dir := filepath.Dir(rel)
	base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	slug := slugBase(base)
	name := slug + ".html"
	if slug == "index" {
		name = "index.html"
	}
	if dir == "." {
		return filepath.ToSlash(name)
	}
	return filepath.ToSlash(filepath.Join(slugDir(dir), name))
}

func slugDir(dir string) string {
	if dir == "." || dir == "" {
		return dir
	}
	parts := strings.Split(filepath.ToSlash(dir), "/")
	for i, part := range parts {
		parts[i] = slugBase(part)
	}
	return filepath.Join(parts...)
}

func titleFromMarkdown(content []byte, file string) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
}

func slugBase(name string) string {
	if idx := strings.Index(name, "-"); idx >= 0 && idx+1 < len(name) {
		return name[idx+1:]
	}
	return name
}

func apiTitle(source string) string {
	clean := filepath.ToSlash(filepath.Clean(source))
	base := filepath.Base(clean)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if clean == "." {
		return "Source"
	}
	if base == "." || base == string(filepath.Separator) || base == "" {
		return clean
	}
	return base
}

func apiSlug(source string) string {
	clean := filepath.ToSlash(filepath.Clean(source))
	clean = strings.Trim(clean, "/.")
	if clean == "" {
		return "source"
	}
	var out strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(clean) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			out.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(out.String(), "-")
}

func markdownToHTML(content []byte) string {
	var buf bytes.Buffer
	_ = docMarkdown.Convert(content, &buf)
	result := buf.String()
	result = strings.ReplaceAll(result, "<table>", `<div class="table-responsive"><table class="table table-bordered table-sm">`)
	result = strings.ReplaceAll(result, "</table>", "</table></div>")
	return result
}

var (
	headingPattern = regexp.MustCompile(`(?s)<h([23]) id="([^"]+)">(.*?)</h[23]>`)
	tagPattern     = regexp.MustCompile(`<[^>]+>`)
)

// tableOfContents builds an "On this page" nav from the h2/h3 headings; "" when fewer than two.
func tableOfContents(body string) string {
	matches := headingPattern.FindAllStringSubmatch(body, -1)
	if len(matches) < 2 {
		return ""
	}
	var items strings.Builder
	for _, m := range matches {
		label := strings.TrimSpace(tagPattern.ReplaceAllString(m[3], ""))
		if label == "" {
			continue
		}
		sub := ""
		if m[1] == "3" {
			sub = " toc-sub"
		}
		items.WriteString(`<li class="toc-item` + sub + `"><a href="#` + m[2] + `">` + label + `</a></li>`)
	}
	return `<nav class="toc"><div class="toc-title">On this page</div><ul class="toc-list">` + items.String() + `</ul></nav>`
}

func writePage(outDir string, pages []page, index int) error {
	p := pages[index]
	var prev, next *page
	if index > 0 {
		prev = &pages[index-1]
	}
	if index+1 < len(pages) {
		next = &pages[index+1]
	}
	content := layout(pages, p, prev, next)
	path := filepath.Join(outDir, p.Output)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func writeRedirect(outDir, target string) error {
	content := "<!doctype html><meta charset=\"utf-8\"><meta http-equiv=\"refresh\" content=\"0; url=" + html.EscapeString(target) + "\"><a href=\"" + html.EscapeString(target) + "\">" + html.EscapeString(siteName) + " Reference Manual</a>"
	return os.WriteFile(filepath.Join(outDir, "index.html"), []byte(content), 0o644)
}

func canonicalLinkHTML(current page) string {
	if canonicalBase == "" {
		return ""
	}
	return "\n  <link rel=\"canonical\" href=\"" + html.EscapeString(pageURL(current)) + "\">"
}

var paragraphPattern = regexp.MustCompile(`(?s)<p(?:\s[^>]*)?>(.*?)</p>`)

// pageDescription joins the leading prose paragraphs into a search-snippet-sized summary.
func pageDescription(body string) string {
	const want, limit = 100, 155
	text := ""
	for _, m := range paragraphPattern.FindAllStringSubmatch(body, -1) {
		para := strings.ReplaceAll(html.UnescapeString(tagPattern.ReplaceAllString(m[1], "")), "`", "")
		para = strings.Join(strings.Fields(para), " ")
		if para == "" || strings.HasSuffix(para, ":") {
			continue
		}
		text = strings.TrimSpace(text + " " + para)
		if len(text) >= want {
			break
		}
	}
	if len(text) <= limit {
		return text
	}
	cut := strings.LastIndex(text[:limit], " ")
	if cut <= 0 {
		cut = limit
	}
	return strings.TrimRight(text[:cut], " ,;:.") + "..."
}

func pageMetaHTML(current page) string {
	title := current.Title + " - " + siteName + " Manual"
	desc := pageDescription(current.HTML)
	var b strings.Builder
	meta := func(attr, key, value string) {
		b.WriteString("\n  <meta " + attr + "=\"" + key + "\" content=\"" + html.EscapeString(value) + "\">")
	}
	if desc != "" {
		meta("name", "description", desc)
	}
	meta("property", "og:type", "article")
	meta("property", "og:site_name", siteName+" Manual")
	meta("property", "og:title", title)
	if desc != "" {
		meta("property", "og:description", desc)
	}
	card := "summary"
	if ogImage != "" {
		card = "summary_large_image"
		meta("property", "og:image", ogImage)
		meta("name", "twitter:image", ogImage)
	}
	meta("name", "twitter:card", card)
	meta("name", "twitter:title", title)
	if desc != "" {
		meta("name", "twitter:description", desc)
	}
	if canonicalBase == "" {
		return b.String()
	}
	url := pageURL(current)
	meta("property", "og:url", url)
	home := pageURL(page{Output: "index.html"})
	manual := siteName + " Reference Manual"
	article := map[string]any{
		"@context":   "https://schema.org",
		"@type":      "TechArticle",
		"headline":   current.Title,
		"url":        url,
		"inLanguage": "en",
		"isPartOf":   map[string]any{"@type": "CreativeWork", "name": manual, "url": home},
	}
	if desc != "" {
		article["description"] = desc
	}
	crumbs := []map[string]any{{"@type": "ListItem", "position": 1, "name": manual, "item": home}}
	if url != home {
		crumbs = append(crumbs, map[string]any{"@type": "ListItem", "position": 2, "name": current.Title, "item": url})
	}
	breadcrumb := map[string]any{"@context": "https://schema.org", "@type": "BreadcrumbList", "itemListElement": crumbs}
	for _, data := range []any{article, breadcrumb} {
		encoded, err := json.Marshal(data)
		if err == nil {
			b.WriteString("\n  <script type=\"application/ld+json\">" + string(encoded) + "</script>")
		}
	}
	return b.String()
}

func layout(pages []page, current page, prev, next *page) string {
	var nav strings.Builder
	for _, p := range pages {
		active := ""
		if p.Output == current.Output {
			active = " active"
		}
		depthClass := ""
		if p.Depth > 0 {
			depthClass = fmt.Sprintf(" nav-depth-%d", p.Depth)
		}
		nav.WriteString(`<a class="list-group-item list-group-item-action` + active + depthClass + `" href="` + relHref(current.Output, p.Output) + `">` + html.EscapeString(p.Title) + `</a>`)
	}
	pager := `<div class="d-flex justify-content-between border-top pt-4 mt-5">`
	if prev != nil {
		pager += `<a class="btn btn-outline-secondary" href="` + relHref(current.Output, prev.Output) + `">&larr; ` + html.EscapeString(prev.Title) + `</a>`
	} else {
		pager += `<span></span>`
	}
	if next != nil {
		pager += `<a class="btn btn-primary" href="` + relHref(current.Output, next.Output) + `">` + html.EscapeString(next.Title) + ` &rarr;</a>`
	}
	pager += `</div>`

	articleClass := "col-lg-9"
	tocAside := ""
	if toc := tableOfContents(current.HTML); toc != "" {
		articleClass = "col-lg-7"
		tocAside = `
      <aside class="col-lg-2 d-none d-lg-block">
        <div class="toc-wrap">` + toc + `</div>
      </aside>`
	}

	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + html.EscapeString(current.Title) + ` - ` + html.EscapeString(siteName) + ` Manual</title>` + canonicalLinkHTML(current) + pageMetaHTML(current) + `
  <link rel="stylesheet" href="` + relHref(current.Output, manualCSSFile) + `">
</head>
<body>
  <nav class="navbar navbar-expand-lg navbar-dark bg-dark">
    <div class="container-fluid manual-shell">
      <a class="navbar-brand fw-semibold" href="` + relHref(current.Output, "index.html") + `">` + html.EscapeString(siteName) + ` Reference Manual</a>` + searchFormHTML() + `
      <span class="navbar-text d-none d-xl-inline">Static manual and source API reference</span>
    </div>
  </nav>
  <main class="container-fluid manual-shell py-4">
    <div class="row g-4">
      <aside class="col-lg-3">
        <div class="sidebar">
          <div class="list-group shadow-sm">` + nav.String() + `</div>
        </div>
      </aside>
      <article class="` + articleClass + `">
        <div class="content p-4 p-lg-5 shadow-sm">
` + current.HTML + pager + `
        </div>
      </article>` + tocAside + `
    </div>
  </main>
  <script>
  (function () {
    const controlKeywords = new Set(["if","else","while","for","return","break","continue","match","case","try","catch","finally","throw","yield","await","in","by","as","instanceof"]);
    const declarationKeywords = new Set(["func","class","interface","enum","module","import","export","let","const","type","async","static","extends","implements"]);
    const constants = new Set(["true","false","null","this","super"]);
    const primitiveTypes = new Set(["int","float","decimal","string","bool","bytes","void","any","auto","callable","generator","iterable"]);
    const operators = new Set(["++","--","==","!=","<=",">=","&&","||","?.","??","..","..<","=>","+=","-=","*=","/=","//=","%=","**=","&=","|=","^=","<<=",">>=","**","//","<<",">>","+","-","*","/","%","<",">","!","=","&","|","^","~","?",":",".",",",";","(",")","[","]","{","}","@"]);

    function esc(text) {
      return text.replace(/[&<>"']/g, function (ch) {
        return {"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[ch];
      });
    }
    function span(cls, text) {
      return '<span class="' + cls + '">' + esc(text) + '</span>';
    }
    function isAlpha(ch) {
      return /[A-Za-z_]/.test(ch);
    }
    function isIdent(ch) {
      return /[A-Za-z0-9_]/.test(ch);
    }
    function readString(src, i, quote) {
      let j = i + 1;
      while (j < src.length) {
        if (src[j] === "\\") {
          j += 2;
          continue;
        }
        if (src[j] === quote) {
          j++;
          break;
        }
        j++;
      }
      return j;
    }
    function readTripleString(src, i, quote) {
      const marker = quote + quote + quote;
      const end = src.indexOf(marker, i + 3);
      return end < 0 ? src.length : end + 3;
    }
    function readNumber(src, i) {
      let j = i;
      if (src.slice(i, i + 2).match(/^0[xX]/)) {
        j += 2;
        while (j < src.length && /[0-9A-Fa-f_]/.test(src[j])) j++;
        return j;
      }
      if (src.slice(i, i + 2).match(/^0[bB]/)) {
        j += 2;
        while (j < src.length && /[01_]/.test(src[j])) j++;
        return j;
      }
      if (src.slice(i, i + 2).match(/^0[oO]/)) {
        j += 2;
        while (j < src.length && /[0-7_]/.test(src[j])) j++;
        return j;
      }
      while (j < src.length && /[0-9_]/.test(src[j])) j++;
      if (src[j] === "." && src[j + 1] !== "." && /[0-9]/.test(src[j + 1] || "")) {
        j++;
        while (j < src.length && /[0-9_]/.test(src[j])) j++;
      }
      if (/[eE]/.test(src[j] || "")) {
        let k = j + 1;
        if (/[+-]/.test(src[k] || "")) k++;
        if (/[0-9]/.test(src[k] || "")) {
          j = k + 1;
          while (j < src.length && /[0-9_]/.test(src[j])) j++;
        }
      }
      if (/[fF]/.test(src[j] || "")) j++;
      return j;
    }
    function readOperator(src, i) {
      for (let len = 3; len >= 1; len--) {
        const op = src.slice(i, i + len);
        if (operators.has(op)) return i + len;
      }
      return i + 1;
    }
    function highlightGeblang(src) {
      let out = "";
      let i = 0;
      while (i < src.length) {
        const ch = src[i];
        if (ch === "#") {
          const j = src.indexOf("\n", i);
          const end = j < 0 ? src.length : j;
          out += span("hl-comment", src.slice(i, end));
          i = end;
          continue;
        }
        if (src.slice(i, i + 2) === "/*") {
          const end = src.indexOf("*/", i + 2);
          const j = end < 0 ? src.length : end + 2;
          out += span("hl-comment", src.slice(i, j));
          i = j;
          continue;
        }
        if ((ch === '"' || ch === "'") && src.slice(i, i + 3) === ch + ch + ch) {
          const j = readTripleString(src, i, ch);
          out += span("hl-string", src.slice(i, j));
          i = j;
          continue;
        }
        if (ch === '"' || ch === "'") {
          const j = readString(src, i, ch);
          out += span("hl-string", src.slice(i, j));
          i = j;
          continue;
        }
        if (/[0-9]/.test(ch)) {
          const j = readNumber(src, i);
          out += span("hl-number", src.slice(i, j));
          i = j;
          continue;
        }
        if (ch === "@" && isAlpha(src[i + 1] || "")) {
          let j = i + 1;
          while (j < src.length && isIdent(src[j])) j++;
          out += span("hl-decorator", src.slice(i, j));
          i = j;
          continue;
        }
        if (isAlpha(ch)) {
          let j = i + 1;
          while (j < src.length && isIdent(src[j])) j++;
          const word = src.slice(i, j);
          let k = j;
          while (k < src.length && /\s/.test(src[k])) k++;
          if (controlKeywords.has(word)) out += span("hl-keyword", word);
          else if (declarationKeywords.has(word)) out += span("hl-declaration", word);
          else if (constants.has(word)) out += span("hl-constant", word);
          else if (primitiveTypes.has(word) || /^[A-Z]/.test(word)) out += span("hl-type", word);
          else if (src[k] === "(") out += span("hl-function", word);
          else out += esc(word);
          i = j;
          continue;
        }
        if (operators.has(ch) || "+-*/%=!<>&|^~?:.,;()[]{}".includes(ch)) {
          const j = readOperator(src, i);
          out += span("hl-operator", src.slice(i, j));
          i = j;
          continue;
        }
        out += esc(ch);
        i++;
      }
      return out;
    }
    document.querySelectorAll("pre code.language-gb, pre code.language-geblang").forEach(function (block) {
      block.innerHTML = highlightGeblang(block.textContent);
    });
  })();
  </script>
</body>
</html>
`
}

func relHref(from, to string) string {
	base := filepath.Dir(from)
	if base == "." {
		return to
	}
	rel, err := filepath.Rel(base, to)
	if err != nil {
		return to
	}
	return filepath.ToSlash(rel)
}
