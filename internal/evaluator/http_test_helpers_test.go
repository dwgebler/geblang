package evaluator

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newLocalHTTPTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local sockets unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	// Close the auto-bound listener being replaced so it cannot leak.
	server.Listener.Close()
	server.Listener = listener
	server.Start()
	waitForListenerReady(t, listener.Addr().String())
	return server
}

// waitForListenerReady blocks until the server accepts a connection: a fresh loopback listener can be transiently unreachable under package load.
func waitForListenerReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp4", addr, time.Second)
		if err == nil {
			conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("test server never became reachable at %s: %v", addr, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
