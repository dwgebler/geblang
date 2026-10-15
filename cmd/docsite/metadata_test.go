package main

import (
	"strings"
	"testing"
)

func withDocsiteFlags(t *testing.T, base, name, image string) {
	t.Helper()
	oldBase, oldName, oldImage := canonicalBase, siteName, ogImage
	canonicalBase, siteName, ogImage = base, name, image
	t.Cleanup(func() { canonicalBase, siteName, ogImage = oldBase, oldName, oldImage })
}

func TestPageDescriptionUsesFirstParagraphText(t *testing.T) {
	body := `<h1>Title</h1><p></p><p class="lead">Uses ` + "`" + `<code>a &amp; b</code>` + "`" + `
	  across   lines.</p><p>Second.</p>`
	if got := pageDescription(body); got != "Uses a & b across lines. Second." {
		t.Fatalf("got %q", got)
	}
}

func TestPageDescriptionSkipsCodeAndLeadInsAndJoinsShortParagraphs(t *testing.T) {
	body := `<p>Import bytes:</p><pre><code>import bytes;</code></pre><p>Conditions must be bool.</p><p>Loops use for and while, and break or continue control the innermost loop body.</p>`
	want := "Conditions must be bool. Loops use for and while, and break or continue control the innermost loop body."
	if got := pageDescription(body); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestPageDescriptionTruncatesAtWordBoundary(t *testing.T) {
	got := pageDescription("<p>" + strings.Repeat("word ", 60) + "</p>")
	if len(got) > 158 || !strings.HasSuffix(got, "word...") {
		t.Fatalf("got %d chars: %q", len(got), got)
	}
}

func TestPageMetaHTMLWithBaseURL(t *testing.T) {
	withDocsiteFlags(t, "https://example.com/docs", "Gebweb", "https://example.com/og.png")
	got := pageMetaHTML(page{Output: "routing.html", Title: "Routing", HTML: "<h1>Routing</h1><p>Routes map \"paths\" to handlers.</p>"})
	for _, want := range []string{
		`<meta name="description" content="Routes map &#34;paths&#34; to handlers.">`,
		`<meta property="og:type" content="article">`,
		`<meta property="og:title" content="Routing - Gebweb Manual">`,
		`<meta property="og:url" content="https://example.com/docs/routing.html">`,
		`<meta property="og:image" content="https://example.com/og.png">`,
		`<meta name="twitter:card" content="summary_large_image">`,
		`"@type":"TechArticle"`,
		`"headline":"Routing"`,
		`"name":"Gebweb Reference Manual"`,
		`"@type":"BreadcrumbList"`,
		`"item":"https://example.com/docs/routing.html"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("meta missing %q:\n%s", want, got)
		}
	}
}

func TestPageMetaHTMLWithoutBaseURLOmitsAbsoluteURLs(t *testing.T) {
	withDocsiteFlags(t, "", "Geblang", "")
	got := pageMetaHTML(page{Output: "x.html", Title: "X", HTML: "<p>Body.</p>"})
	if strings.Contains(got, "og:url") || strings.Contains(got, "ld+json") || strings.Contains(got, "og:image") {
		t.Fatalf("unexpected absolute metadata:\n%s", got)
	}
	if !strings.Contains(got, `<meta name="twitter:card" content="summary">`) {
		t.Fatalf("expected summary card:\n%s", got)
	}
}

func TestLayoutUsesSiteName(t *testing.T) {
	withDocsiteFlags(t, "", "Gebweb", "")
	pages := []page{{Output: "index.html", Title: "Intro", HTML: "<p>Hi.</p>"}}
	got := layout(pages, pages[0], nil, nil)
	if !strings.Contains(got, "<title>Intro - Gebweb Manual</title>") || !strings.Contains(got, "Gebweb Reference Manual</a>") {
		t.Fatalf("site name not applied")
	}
	if strings.Contains(got, "Geblang Manual") || strings.Contains(got, "Geblang Reference Manual") {
		t.Fatalf("hard-coded Geblang name remains")
	}
}
