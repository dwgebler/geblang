package evaluator

import (
	"net/http"
	neturl "net/url"
	"testing"

	"geblang/internal/runtime"
)

func TestCleanRequestPath(t *testing.T) {
	cases := map[string]string{
		"/admin":           "/admin",
		"//admin":          "/admin",
		"//admin/x":        "/admin/x",
		"///admin/x":       "/admin/x",
		"/admin/../secret": "/secret",
		"/admin/./x":       "/admin/x",
		"/a//b":            "/a/b",
		"":                 "/",
		"admin":            "/admin",
		"/admin/":          "/admin/",
		"/a/b/":            "/a/b/",
		"/":                "/",
		"//":               "/",
	}
	for in, want := range cases {
		if got := cleanRequestPath(in); got != want {
			t.Errorf("cleanRequestPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// A "//admin" path must reach the handler as "/admin" so a startsWith("/admin") gate cannot be bypassed.
func TestHTTPRequestPathCanonicalized(t *testing.T) {
	req := &http.Request{Method: "GET", URL: &neturl.URL{Path: "//admin/downloads"}, Header: http.Header{}}
	entries := httpRequestEntries(req, nil)
	got := entries[dictKey(runtime.String{Value: "path"})].Value.(runtime.String).Value
	if got != "/admin/downloads" {
		t.Fatalf("req path = %q, want /admin/downloads", got)
	}
}
