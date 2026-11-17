package evaluator

import (
	"net/http"
	"testing"
	"time"
)

func TestHTTPClientDefaults(t *testing.T) {
	if defaultHTTPClient.Timeout != 30*time.Second {
		t.Fatalf("default client timeout = %v", defaultHTTPClient.Timeout)
	}
	if defaultMaxHTTPResponseBody != 64<<20 {
		t.Fatalf("default response cap = %d", defaultMaxHTTPResponseBody)
	}
	if streamingHTTPClient.Timeout != 0 {
		t.Fatalf("streaming client must not bound the body, timeout = %v", streamingHTTPClient.Timeout)
	}
	if tr, ok := streamingHTTPClient.Transport.(*http.Transport); !ok || tr.ResponseHeaderTimeout != 30*time.Second {
		t.Fatalf("streaming client header timeout not 30s: %#v", streamingHTTPClient.Transport)
	}
}
