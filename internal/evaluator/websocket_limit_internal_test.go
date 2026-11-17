package evaluator

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	gruntime "geblang/internal/runtime"

	gorillawebsocket "github.com/gorilla/websocket"
)

// limitedEchoServer echoes each message's length until a read fails, then reports that error.
func limitedEchoServer(t *testing.T, e *Evaluator, limits handlerLimits) (string, <-chan error, func()) {
	t.Helper()
	readErr := make(chan error, 1)
	handler := gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
		wsHandler := gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
			h, err := e.websocketHandle(args[0])
			if err != nil {
				return nil, err
			}
			for {
				_, data, err := h.conn.ReadMessage()
				if err != nil {
					readErr <- err
					return gruntime.Null{}, nil
				}
				if err := h.writeMessage(gorillawebsocket.TextMessage, []byte(strings.Repeat("k", len(data)%7+1))); err != nil {
					readErr <- err
					return gruntime.Null{}, nil
				}
			}
		}}
		entries := map[string]gruntime.DictEntry{}
		putDict(entries, "websocket", wsHandler)
		return gruntime.DictFromEntries(entries, nil), nil
	}}
	server := newLocalHTTPTestServer(t, e.httpHandler(handler, nil, nil, 0, false, false, limits))
	return "ws" + strings.TrimPrefix(server.URL, "http"), readErr, server.Close
}

func writeFragmented(t *testing.T, conn *gorillawebsocket.Conn, total, frame int) {
	t.Helper()
	w, err := conn.NextWriter(gorillawebsocket.BinaryMessage)
	if err != nil {
		t.Fatalf("next writer: %v", err)
	}
	chunk := make([]byte, frame)
	for sent := 0; sent < total; sent += frame {
		n := frame
		if total-sent < n {
			n = total - sent
		}
		if _, err := w.Write(chunk[:n]); err != nil {
			t.Fatalf("write fragment: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
}

func TestWebSocketMessageLimitClosesWith1009(t *testing.T) {
	e := New(io.Discard)
	url, readErr, stop := limitedEchoServer(t, e, handlerLimits{ws: wsOptions{maxMessageBytes: 1024}})
	defer stop()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	writeFragmented(t, conn, 1024, 100)
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("message at the limit was rejected: %v", err)
	}

	// Each frame is under the limit; the assembled message is not.
	writeFragmented(t, conn, 1025, 100)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err = conn.ReadMessage()
	var closeErr *gorillawebsocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != gorillawebsocket.CloseMessageTooBig {
		t.Fatalf("oversize message: client got %v, want close 1009", err)
	}
	select {
	case serverErr := <-readErr:
		if !errors.Is(serverErr, gorillawebsocket.ErrReadLimit) {
			t.Fatalf("server read error = %v, want read limit", serverErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server read did not fail on the oversize message")
	}
}

func TestWebSocketMessageLimitZeroIsUnlimited(t *testing.T) {
	e := New(io.Discard)
	url, _, stop := limitedEchoServer(t, e, handlerLimits{})
	defer stop()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	writeFragmented(t, conn, 3<<20, 64<<10)
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("large message rejected with no limit: %v", err)
	}
}

func TestParseWebSocketOptions(t *testing.T) {
	if ws, err := parseWebSocketOptions(gruntime.Null{}, "http.listen"); err != nil || ws.maxMessageBytes != 1<<20 ||
		ws.pingInterval != 25*time.Second || ws.idleTimeout != 60*time.Second || ws.writeTimeout != 10*time.Second {
		t.Fatalf("defaults = %+v, %v", ws, err)
	}
	if ws, err := parseWebSocketOptions(optsDict(t, map[string]gruntime.Value{"maxWebSocketMessageBytes": gruntime.NewInt64(0)}), "http.listen"); err != nil || ws.maxMessageBytes != 0 {
		t.Fatalf("zero = %d, %v", ws.maxMessageBytes, err)
	}
	set := optsDict(t, map[string]gruntime.Value{
		"webSocketPingIntervalMs": gruntime.NewInt64(0),
		"webSocketIdleTimeoutMs":  gruntime.NewInt64(1500),
		"webSocketWriteTimeoutMs": gruntime.NewInt64(250),
	})
	if ws, err := parseWebSocketOptions(set, "http.listen"); err != nil || ws.pingInterval != 0 || ws.idleTimeout != 1500*time.Millisecond || ws.writeTimeout != 250*time.Millisecond {
		t.Fatalf("set = %+v, %v", ws, err)
	}
	for _, name := range []string{"maxWebSocketMessageBytes", "webSocketPingIntervalMs", "webSocketIdleTimeoutMs", "webSocketWriteTimeoutMs"} {
		for _, bad := range []gruntime.Value{gruntime.NewInt64(-1), gruntime.String{Value: "1"}} {
			_, err := parseWebSocketOptions(optsDict(t, map[string]gruntime.Value{name: bad}), "http.listen")
			if err == nil || !strings.Contains(err.Error(), "opts."+name+" must be a non-negative int") {
				t.Fatalf("%s=%v: error = %v", name, bad, err)
			}
		}
	}
}
