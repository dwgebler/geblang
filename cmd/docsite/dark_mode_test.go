package main

import (
	"strings"
	"testing"
)

func TestManualCSSHasDarkPaletteForSystemAndToggle(t *testing.T) {
	for _, want := range []string{
		`@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) {`,
		`:root[data-theme="dark"] {`,
		"--m-bg: #0d141b;",
		"background: var(--m-surface);",
	} {
		if !strings.Contains(manualCSS, want) {
			t.Fatalf("manual CSS missing %q", want)
		}
	}
	if strings.Count(manualCSS, manualDarkTokens) != 2 {
		t.Fatalf("dark tokens should apply under the media query and the explicit dark theme")
	}
}

func TestManualPagesCarryThemeToggle(t *testing.T) {
	withSiteChrome(t, "", "", "", "", "Home")
	pages := chromePages()
	got := layout(pages, pages[2], &pages[0], nil)
	for _, want := range []string{
		`localStorage.getItem("theme")`,
		`<button class="theme-toggle" type="button" aria-label="Toggle dark mode" title="Toggle dark mode"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg></button>`,
		`localStorage.setItem("theme", next)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("manual page missing %q", want)
		}
	}
	if strings.Index(got, `localStorage.getItem("theme")`) > strings.Index(got, "manual.css") {
		t.Fatalf("theme must be applied before the stylesheet loads")
	}
}
