package main

import (
	"strings"
	"testing"
)

func TestLayoutLinksLocalStylesheetOnly(t *testing.T) {
	nested := page{Output: "api/stdlib.html", Title: "API", HTML: "<p>x</p>"}
	got := layout([]page{nested}, nested, nil, nil)
	if !strings.Contains(got, `<link rel="stylesheet" href="../manual.css">`) {
		t.Fatalf("nested page must link ../manual.css")
	}
	if strings.Contains(got, "https://cdn.") || strings.Contains(got, "<style>") {
		t.Fatalf("layout must not load third-party or inline CSS")
	}
}

func TestManualCSSCoversLayoutClasses(t *testing.T) {
	for _, class := range []string{
		".navbar-brand", ".list-group-item.active", ".col-lg-3", ".col-lg-7", ".col-sm-10",
		".d-lg-block", ".d-xl-inline", ".table-bordered", ".btn-primary", ".btn-outline-light",
		".form-control-sm", ".toc-item", ".hl-keyword", ".nav-depth-3",
	} {
		if !strings.Contains(manualCSS, class) {
			t.Fatalf("manual.css missing %s", class)
		}
	}
}
