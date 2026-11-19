package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withSiteChrome(t *testing.T, header, footer, css, root, rootName string) {
	t.Helper()
	old := [5]string{siteHeader, siteFooter, siteCSS, siteRoot, siteRootName}
	siteHeader, siteFooter, siteCSS, siteRoot, siteRootName = header, footer, css, root, rootName
	t.Cleanup(func() {
		siteHeader, siteFooter, siteCSS, siteRoot, siteRootName = old[0], old[1], old[2], old[3], old[4]
	})
}

func chromePages() []page {
	return []page{
		{Output: "index.html", Title: "Index", HTML: "<h1>Index</h1><p>Start.</p>"},
		{Output: "AGENTS.html", Title: "Agents", HTML: "<h1>Agents</h1><p>For tools.</p>"},
		{Output: "types.html", Title: "Types", HTML: "<h1>Types</h1><p>Types.</p>"},
	}
}

func TestPlainBuildHasNoSiteChrome(t *testing.T) {
	withSiteChrome(t, "", "", "", "", "Home")
	pages := chromePages()
	got := layout(pages, pages[2], &pages[0], nil)
	if strings.Contains(got, "site-css") || strings.Contains(got, `<link rel="stylesheet" href="/`) {
		t.Fatalf("plain build gained a site stylesheet:\n%s", got)
	}
	if !strings.Contains(got, "<body>\n  <nav class=\"navbar") || !strings.Contains(got, "</script>\n</body>") {
		t.Fatalf("plain build changed the page shell:\n%s", got)
	}
}

func TestSiteChromeIsInjected(t *testing.T) {
	withSiteChrome(t, "\n<header class=\"site-bar\">HEADER</header>", "\n<footer class=\"site-foot\">FOOTER</footer>", "/site.css", "", "Home")
	pages := chromePages()
	got := layout(pages, pages[2], &pages[0], nil)
	for _, want := range []string{
		"<body>\n<header class=\"site-bar\">HEADER</header>\n  <nav class=\"navbar",
		"</script>\n<footer class=\"site-foot\">FOOTER</footer>\n</body>",
		`<link rel="stylesheet" href="/site.css">`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("layout missing %q:\n%s", want, got)
		}
	}
}

func TestSiteRootPrependsBreadcrumb(t *testing.T) {
	withDocsiteFlags(t, "https://example.com/docs", "Geblang", "")
	withSiteChrome(t, "", "", "", "https://example.com/", "Geblang")
	got := pageMetaHTML(page{Output: "types.html", Title: "Types", HTML: "<p>Types.</p>"})
	want := `"itemListElement":[{"@type":"ListItem","item":"https://example.com/","name":"Geblang","position":1},` +
		`{"@type":"ListItem","item":"https://example.com/docs/","name":"Geblang Reference Manual","position":2},` +
		`{"@type":"ListItem","item":"https://example.com/docs/types.html","name":"Types","position":3}]`
	if !strings.Contains(got, want) {
		t.Fatalf("breadcrumb missing site root:\n%s", got)
	}
}

func TestBreadcrumbWithoutSiteRootStartsAtManual(t *testing.T) {
	withDocsiteFlags(t, "https://example.com/docs", "Geblang", "")
	withSiteChrome(t, "", "", "", "", "Home")
	got := pageMetaHTML(page{Output: "types.html", Title: "Types", HTML: "<p>Types.</p>"})
	if !strings.Contains(got, `"itemListElement":[{"@type":"ListItem","item":"https://example.com/docs/","name":"Geblang Reference Manual","position":1}`) {
		t.Fatalf("breadcrumb should start at the manual:\n%s", got)
	}
}

func TestAgentsPageIsNoindexAndOutOfNavigation(t *testing.T) {
	withSiteChrome(t, "", "", "", "", "Home")
	pages := chromePages()
	if got := pageMetaHTML(pages[1]); !strings.Contains(got, `<meta name="robots" content="noindex">`) {
		t.Fatalf("agents page is not noindex:\n%s", got)
	}
	if got := pageMetaHTML(pages[2]); strings.Contains(got, "noindex") {
		t.Fatalf("manual page marked noindex:\n%s", got)
	}
	nav := layout(pages, pages[0], nil, &pages[2])
	if strings.Contains(nav, `href="AGENTS.html"`) {
		t.Fatalf("agents page is in the sidebar:\n%s", nav)
	}
}

func TestPagerSkipsAgentsPage(t *testing.T) {
	withSiteChrome(t, "", "", "", "", "Home")
	dir := t.TempDir()
	pages := chromePages()
	if err := writePage(dir, pages, 0); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `href="types.html">Types &rarr;</a>`) {
		t.Fatalf("next link should skip the agents page:\n%s", out)
	}
}

func TestSiteFlagsParse(t *testing.T) {
	old := [5]string{siteHeaderFile, siteFooterFile, siteCSS, siteRoot, siteRootName}
	t.Cleanup(func() {
		siteHeaderFile, siteFooterFile, siteCSS, siteRoot, siteRootName = old[0], old[1], old[2], old[3], old[4]
	})
	api, _ := parseExtraArgs([]string{"--site-header", "h.html", "--site-footer", "f.html", "--site-css", "/s.css", "--site-root", "https://x/", "--site-root-name", "X", "stdlib"})
	if siteHeaderFile != "h.html" || siteFooterFile != "f.html" || siteCSS != "/s.css" || siteRoot != "https://x/" || siteRootName != "X" {
		t.Fatalf("flags not parsed: %q %q %q %q %q", siteHeaderFile, siteFooterFile, siteCSS, siteRoot, siteRootName)
	}
	if len(api) != 1 || api[0] != "stdlib" {
		t.Fatalf("api sources = %v", api)
	}
}
