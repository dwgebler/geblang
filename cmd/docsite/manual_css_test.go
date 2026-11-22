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
		".form-control-sm", ".toc-item", ".toc-inline", ".d-lg-none", ".chapter-nav", ".pager", ".navbar-search", ".hl-keyword", ".nav-depth-3",
	} {
		if !strings.Contains(manualCSS, class) {
			t.Fatalf("manual.css missing %s", class)
		}
	}
}

func TestManualCSSResponsiveDisplayOverridesBase(t *testing.T) {
	base := strings.Index(manualCSS, ".d-none {")
	lg := strings.Index(manualCSS, ".d-lg-block {")
	lgNone := strings.Index(manualCSS, ".d-lg-none {")
	if base < 0 || lg < 0 || lg < base || lgNone < 0 {
		t.Fatalf(".d-lg-block must follow .d-none so wide screens can reveal hidden columns")
	}
}
