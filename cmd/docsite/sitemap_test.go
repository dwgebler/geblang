package main

import (
	"strings"
	"testing"
)

func TestSitemapListsEveryPageWithLastmod(t *testing.T) {
	old := canonicalBase
	canonicalBase = "https://example.com/docs/"
	defer func() { canonicalBase = old }()

	pages := []page{
		{Output: "index.html", Origin: "docs/user/00-index.md"},
		{Output: "control-flow.html", Origin: "docs/user/04-control-flow.md"},
		{Output: "api/a&b.html", Origin: ""},
	}
	dates := map[string]string{"docs/user/04-control-flow.md": "2026-10-01"}
	got := sitemapXML(pages, func(path string) string { return dates[path] })

	for _, want := range []string{
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`,
		"<url><loc>https://example.com/docs/</loc></url>",
		"<url><loc>https://example.com/docs/control-flow.html</loc><lastmod>2026-10-01</lastmod></url>",
		"<url><loc>https://example.com/docs/api/a&amp;b.html</loc></url>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("sitemap missing %q:\n%s", want, got)
		}
	}
}

func TestGitLastModifiedOutsideRepoIsEmpty(t *testing.T) {
	if got := gitLastModified(t.TempDir() + "/missing.md"); got != "" {
		t.Fatalf("expected empty date, got %q", got)
	}
}

func TestSitemapOmitsAgentGuide(t *testing.T) {
	pages := []page{
		{Output: "index.html"},
		{Output: "AGENTS.html", Origin: "docs/user/AGENTS.md"},
	}
	got := sitemapXML(pages, func(string) string { return "" })
	if strings.Contains(got, "AGENTS") {
		t.Fatalf("sitemap lists the agent guide:\n%s", got)
	}
	if !strings.Contains(got, "<loc>") {
		t.Fatalf("sitemap lost the manual pages:\n%s", got)
	}
}
