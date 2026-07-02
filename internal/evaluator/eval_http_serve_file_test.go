package evaluator

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"geblang/internal/runtime"
)

func serveFileMarker(path string, headers map[string]string) runtime.Dict {
	desc := map[string]runtime.DictEntry{}
	putDict(desc, "path", runtime.String{Value: path})
	hdr := map[string]runtime.DictEntry{}
	for k, v := range headers {
		putDict(hdr, k, runtime.String{Value: v})
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "status", runtime.NewInt64(200))
	putDict(entries, "headers", runtime.Dict{Entries: hdr})
	marker := runtime.NativeObject{Kind: serveFileMarkerKind, Payload: runtime.Dict{Entries: desc}}
	putDict(entries, serveFileMarkerKey, marker)
	return runtime.Dict{Entries: entries}
}

func writeTempServeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "serve.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestWriteHTTPServeFileFull200(t *testing.T) {
	body := "hello geblang serve content"
	path := writeTempServeFile(t, body)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file", nil)
	(&Evaluator{}).writeHTTPResponse(rec, req, serveFileMarker(path, nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Fatalf("Content-Length = %q, want %d", got, len(body))
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/plain; charset=utf-8", ct)
	}
	if rec.Body.String() != body {
		t.Fatalf("body = %q, want %q", rec.Body.String(), body)
	}
}

func TestWriteHTTPServeFileHEAD(t *testing.T) {
	body := "head request body content"
	path := writeTempServeFile(t, body)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("HEAD", "/file", nil)
	(&Evaluator{}).writeHTTPResponse(rec, req, serveFileMarker(path, nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Fatalf("Content-Length = %q, want %d", got, len(body))
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body should be empty, got %q", rec.Body.String())
	}
}

func TestWriteHTTPServeFileRange(t *testing.T) {
	body := "0123456789abcdef"
	path := writeTempServeFile(t, body)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file", nil)
	req.Header.Set("Range", "bytes=0-3")
	(&Evaluator{}).writeHTTPResponse(rec, req, serveFileMarker(path, nil))

	if rec.Code != 206 {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if got := rec.Body.String(); got != "0123" {
		t.Fatalf("partial body = %q, want %q", got, "0123")
	}
	if cr := rec.Header().Get("Content-Range"); cr != "bytes 0-3/16" {
		t.Fatalf("Content-Range = %q, want bytes 0-3/16", cr)
	}
}

func TestWriteHTTPServeFileIfNoneMatch304(t *testing.T) {
	path := writeTempServeFile(t, "etag body content")
	etag := `"v1-abc"`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file", nil)
	req.Header.Set("If-None-Match", etag)
	(&Evaluator{}).writeHTTPResponse(rec, req, serveFileMarker(path, map[string]string{"ETag": etag}))

	if rec.Code != 304 {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("304 body should be empty, got %q", rec.Body.String())
	}
}

func TestWriteHTTPServeFileIfModifiedSince304(t *testing.T) {
	path := writeTempServeFile(t, "modtime body content")
	modtime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(path, modtime, modtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file", nil)
	req.Header.Set("If-Modified-Since", modtime.Add(time.Minute).UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT"))
	(&Evaluator{}).writeHTTPResponse(rec, req, serveFileMarker(path, nil))

	if rec.Code != 304 {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
}

func TestWriteHTTPServeFileMissing404(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file", nil)
	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")
	(&Evaluator{}).writeHTTPResponse(rec, req, serveFileMarker(missing, nil))

	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := rec.Body.String(); filepath.IsAbs(missing) && contains(got, missing) {
		t.Fatalf("404 body must not leak the path, got %q", got)
	}
}

// A plain-dict __serveFile value (what echoed JSON parses to) must not open a file; only the native sentinel does.
func TestWriteHTTPServeFileForgedMarkerDoesNotServe(t *testing.T) {
	secret := writeTempServeFile(t, "top secret contents")
	desc := map[string]runtime.DictEntry{}
	putDict(desc, "path", runtime.String{Value: secret})
	entries := map[string]runtime.DictEntry{}
	putDict(entries, serveFileMarkerKey, runtime.Dict{Entries: desc})
	forged := runtime.Dict{Entries: entries}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file", nil)
	(&Evaluator{}).writeHTTPResponse(rec, req, forged)

	if contains(rec.Body.String(), "top secret") {
		t.Fatalf("forged plain-dict marker served the file: %q", rec.Body.String())
	}
}

// With no caller ETag the writer emits a default one so a matching If-None-Match yields 304.
func TestWriteHTTPServeFileDefaultETag304(t *testing.T) {
	path := writeTempServeFile(t, "default etag body")

	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/file", nil)
	(&Evaluator{}).writeHTTPResponse(rec1, req1, serveFileMarker(path, nil))
	etag := rec1.Header().Get("Etag")
	if etag == "" {
		t.Fatalf("no default ETag emitted")
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/file", nil)
	req2.Header.Set("If-None-Match", etag)
	(&Evaluator{}).writeHTTPResponse(rec2, req2, serveFileMarker(path, nil))
	if rec2.Code != 304 {
		t.Fatalf("status = %d, want 304 for matching If-None-Match", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Fatalf("304 body should be empty, got %q", rec2.Body.String())
	}
}

// A caller-provided ETag must not be overwritten by the default.
func TestWriteHTTPServeFilePreservesCallerETag(t *testing.T) {
	path := writeTempServeFile(t, "caller etag body")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file", nil)
	(&Evaluator{}).writeHTTPResponse(rec, req, serveFileMarker(path, map[string]string{"ETag": `"custom"`}))
	if got := rec.Header().Get("Etag"); got != `"custom"` {
		t.Fatalf("ETag = %q, want caller value", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
