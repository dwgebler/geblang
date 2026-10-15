package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSearchIndexSplitsPagesIntoLinkedSections(t *testing.T) {
	pages := []page{
		{Output: "types.html", Title: "Types", HTML: `<p>Lead &amp; intro.</p><h2 id="nullability">Nullability</h2><p>Use <code>?string</code> for null.</p><h3 id="coalescing">Coalescing</h3><p>The ?? operator.</p>`},
		{Output: "AGENTS.html", Title: "Agents", HTML: `<h2 id="x">Hidden</h2><p>agent text</p>`},
	}
	entries := buildSearchIndex(pages)
	if len(entries) != 3 {
		t.Fatalf("want lead + two sections, got %+v", entries)
	}
	if entries[0].URL != "types.html" || entries[0].Text != "Lead & intro." {
		t.Fatalf("lead entry wrong: %+v", entries[0])
	}
	if entries[1].URL != "types.html#nullability" || entries[1].Heading != "Nullability" || entries[1].Text != "Use ?string for null." {
		t.Fatalf("section entry wrong: %+v", entries[1])
	}
	if entries[2].URL != "types.html#coalescing" {
		t.Fatalf("subsection entry wrong: %+v", entries[2])
	}
}

func TestSearchIndexScriptIsLoadableData(t *testing.T) {
	js, err := searchIndexJS([]searchEntry{{Title: "T", Heading: "H", URL: "a.html#h", Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "window.__docsearch="
	if !strings.HasPrefix(js, prefix) || !strings.HasSuffix(js, ";\n") {
		t.Fatalf("unexpected wrapper: %q", js)
	}
	var decoded []map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(js, prefix), ";\n")), &decoded); err != nil {
		t.Fatalf("index payload is not JSON: %v", err)
	}
	if decoded[0]["u"] != "a.html#h" {
		t.Fatalf("payload keys wrong: %v", decoded)
	}
}

func TestNavbarSearchUsesClientBoxUnlessServerSearchConfigured(t *testing.T) {
	old := searchURL
	defer func() { searchURL = old }()
	searchURL = ""
	nested := navbarSearchHTML(page{Output: "stdlib/async.html"})
	if !strings.Contains(nested, `data-root="../"`) || !strings.Contains(nested, `src="../search.js"`) {
		t.Fatalf("nested page must resolve the index from the manual root: %s", nested)
	}
	if top := navbarSearchHTML(page{Output: "types.html"}); !strings.Contains(top, `data-root=""`) {
		t.Fatalf("top-level page root wrong: %s", top)
	}
	searchURL = "/search"
	if form := navbarSearchHTML(page{Output: "types.html"}); !strings.Contains(form, `action="/search"`) || strings.Contains(form, "docsearch") {
		t.Fatalf("server search must keep the form: %s", form)
	}
}
