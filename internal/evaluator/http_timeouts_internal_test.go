package evaluator

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"geblang/internal/concurrent"
	gruntime "geblang/internal/runtime"
)

func okHandler(delay time.Duration) gruntime.Function {
	return gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
		time.Sleep(delay)
		return gruntime.String{Value: "ok"}, nil
	}}
}

func optsDict(t *testing.T, fields map[string]gruntime.Value) gruntime.Dict {
	t.Helper()
	entries := map[string]gruntime.DictEntry{}
	for name, value := range fields {
		putDict(entries, name, value)
	}
	return gruntime.DictFromEntries(entries, nil)
}

// A client that stalls mid-body must not hold a concurrency slot past the body-read timeout.
func TestHTTPStalledBodyTimesOutAndFreesSlot(t *testing.T) {
	e := New(io.Discard)
	pool := concurrent.NewPool(1, 0, concurrent.Reject)
	server := newLocalHTTPTestServer(t, e.httpHandler(okHandler(0), pool, nil, 0, false, false, handlerLimits{bodyTimeout: 150 * time.Millisecond}))
	defer server.Close()

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\na"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("stalled request got no response: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("stalled body status = %d, want 408", resp.StatusCode)
	}
	if !resp.Close {
		t.Fatalf("stalled connection should be closed by the server")
	}
	// The server must drop the connection itself, not wait on the stalled client.
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatalf("server did not close the stalled connection: %v", err)
	}

	healthy, err := http.Post(server.URL, "text/plain", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("healthy request: %v", err)
	}
	_ = healthy.Body.Close()
	if healthy.StatusCode != http.StatusOK {
		t.Fatalf("healthy request after timeout = %d, want 200 (slot not released)", healthy.StatusCode)
	}
}

// The deadline bounds the body read only: a handler slower than it, and a later request on the same connection, still succeed.
func TestHTTPBodyTimeoutDoesNotLimitHandlerOrKeepAlive(t *testing.T) {
	e := New(io.Discard)
	server := newLocalHTTPTestServer(t, e.httpHandler(okHandler(400*time.Millisecond), nil, nil, 0, false, false, handlerLimits{bodyTimeout: 100 * time.Millisecond}))
	defer server.Close()

	client := &http.Client{Transport: &http.Transport{MaxConnsPerHost: 1}}
	defer client.CloseIdleConnections()
	for i := 0; i < 2; i++ {
		resp, err := client.Post(server.URL, "application/octet-stream", bytes.NewReader(bytes.Repeat([]byte("x"), 1<<16)))
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(body) != "ok" {
			t.Fatalf("request %d = %d %q, want 200 ok", i, resp.StatusCode, body)
		}
	}
}

func TestParseServerTimeouts(t *testing.T) {
	defaults, err := parseServerTimeouts(gruntime.Null{}, "http.listen")
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if defaults.header != 10*time.Second || defaults.body != 60*time.Second || defaults.idle != 120*time.Second {
		t.Fatalf("defaults = %+v", defaults)
	}

	custom, err := parseServerTimeouts(optsDict(t, map[string]gruntime.Value{
		"readHeaderTimeoutMs": gruntime.NewInt64(2500),
		"readBodyTimeoutMs":   gruntime.NewInt64(0),
	}), "http.listen")
	if err != nil {
		t.Fatalf("custom: %v", err)
	}
	if custom.header != 2500*time.Millisecond || custom.body != 0 || custom.idle != 120*time.Second {
		t.Fatalf("custom = %+v", custom)
	}

	server := &http.Server{}
	custom.apply(server)
	if server.ReadHeaderTimeout != 2500*time.Millisecond || server.IdleTimeout != 120*time.Second || server.ReadTimeout != 0 || server.WriteTimeout != 0 {
		t.Fatalf("applied = header %v idle %v read %v write %v", server.ReadHeaderTimeout, server.IdleTimeout, server.ReadTimeout, server.WriteTimeout)
	}

	for _, name := range []string{"readHeaderTimeoutMs", "readBodyTimeoutMs", "idleTimeoutMs"} {
		for _, bad := range []gruntime.Value{gruntime.NewInt64(-1), gruntime.String{Value: "5"}} {
			_, err := parseServerTimeouts(optsDict(t, map[string]gruntime.Value{name: bad}), "http.listen")
			if err == nil || !strings.Contains(err.Error(), "opts."+name+" must be a non-negative int") {
				t.Fatalf("%s=%v error = %v", name, bad, err)
			}
		}
	}
}
