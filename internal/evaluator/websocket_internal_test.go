package evaluator

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	gruntime "geblang/internal/runtime"

	gorillawebsocket "github.com/gorilla/websocket"
)

func TestHTTPHandlerUpgradesWebSocket(t *testing.T) {
	e := New(io.Discard)
	handler := gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
		wsHandler := gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
			h, err := e.websocketHandle(args[0])
			if err != nil {
				return nil, err
			}
			messageType, data, err := h.conn.ReadMessage()
			if err != nil {
				return nil, err
			}
			if err := h.writeMessage(messageType, []byte("echo:"+string(data))); err != nil {
				return nil, err
			}
			return gruntime.Null{}, nil
		}}
		entries := map[string]gruntime.DictEntry{}
		putDict(entries, "websocket", wsHandler)
		return gruntime.DictFromEntries(entries, nil), nil
	}}

	server := newLocalHTTPTestServer(t, e.httpHandler(handler, nil, nil, 0, false, false))
	defer server.Close()

	conn, _, err := gorillawebsocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(gorillawebsocket.TextMessage, []byte("hello")); err != nil {
		t.Fatalf("write websocket message: %v", err)
	}
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket message: %v", err)
	}
	if messageType != gorillawebsocket.TextMessage {
		t.Fatalf("message type: got %d, want %d", messageType, gorillawebsocket.TextMessage)
	}
	if string(data) != "echo:hello" {
		t.Fatalf("message: got %q, want %q", string(data), "echo:hello")
	}
}

func TestWebSocketWritesAreSerialisedAcrossGoroutines(t *testing.T) {
	e := New(io.Discard)

	const senders = 32
	const messagesPerSender = 16
	var hValue gruntime.Value
	ready := make(chan struct{})
	done := make(chan struct{})

	handler := gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
		wsHandler := gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
			hValue = args[0]
			close(ready)
			<-done
			return gruntime.Null{}, nil
		}}
		entries := map[string]gruntime.DictEntry{}
		putDict(entries, "websocket", wsHandler)
		return gruntime.DictFromEntries(entries, nil), nil
	}}

	server := newLocalHTTPTestServer(t, e.httpHandler(handler, nil, nil, 0, false, false))
	defer server.Close()

	conn, _, err := gorillawebsocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	<-ready

	h, err := e.websocketHandle(hValue)
	if err != nil {
		t.Fatalf("websocketHandle: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerSender; j++ {
				msg := fmt.Sprintf("sender%d/msg%d", id, j)
				if err := h.writeMessage(gorillawebsocket.TextMessage, []byte(msg)); err != nil {
					t.Errorf("writeMessage: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	expected := senders * messagesPerSender
	seen := map[string]bool{}
	for i := 0; i < expected; i++ {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if mt != gorillawebsocket.TextMessage {
			t.Fatalf("message %d: type %d not text", i, mt)
		}
		msg := string(data)
		if seen[msg] {
			t.Fatalf("duplicate message: %s", msg)
		}
		if !strings.HasPrefix(msg, "sender") || !strings.Contains(msg, "/msg") {
			t.Fatalf("corrupted frame: %q", msg)
		}
		seen[msg] = true
	}
	if len(seen) != expected {
		t.Fatalf("got %d messages, want %d", len(seen), expected)
	}

	close(done)
}
