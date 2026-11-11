package evaluator

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"geblang/internal/ast"
	"geblang/internal/concurrent"
	gruntime "geblang/internal/runtime"
	"geblang/internal/token"
)

// concurrencyHandler builds a minimal Geblang Function (non-native, so the clone
// path is exercised) whose body calls a native "_block" function captured in its
// environment. This lets the test synchronise two concurrent HTTP requests.
func concurrencyHandler(blocker func(*gruntime.Instance, []gruntime.Value) (gruntime.Value, error)) gruntime.Function {
	env := gruntime.NewEnvironment()
	_ = env.Define("_block", gruntime.Function{Native: blocker}, true)

	body := &ast.BlockStatement{
		Statements: []ast.Statement{
			&ast.ExpressionStatement{
				Expression: &ast.CallExpression{
					Token:  token.Token{Type: token.LParen},
					Callee: &ast.Identifier{Value: "_block"},
				},
			},
			&ast.ReturnStatement{
				Token: token.Token{Type: token.Return},
				Value: &ast.Literal{Token: token.Token{Type: token.Null, Literal: "null"}},
			},
		},
	}
	return gruntime.Function{
		Name: "handler",
		Body: body,
		Env:  env,
		Parameters: []ast.Parameter{
			{Name: &ast.Identifier{Value: "req"}},
		},
	}
}

func TestHTTPHandlerAllowsConcurrentCallbacks(t *testing.T) {
	e := New(io.Discard)
	var active int32
	var maxActive int32
	release := make(chan struct{})
	entered := make(chan struct{}, 2)

	// Native handler — exercises the native short-circuit path (no clone).
	handler := gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
		current := atomic.AddInt32(&active, 1)
		for {
			previous := atomic.LoadInt32(&maxActive)
			if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		atomic.AddInt32(&active, -1)
		return gruntime.String{Value: "ok"}, nil
	}}

	server := newLocalHTTPTestServer(t, e.httpHandler(handler, nil, nil, 0, false, false))
	defer server.Close()

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(server.URL)
			if err != nil {
				t.Errorf("get: %v", err)
				return
			}
			_ = resp.Body.Close()
		}()
	}

	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("timed out waiting for concurrent handlers")
		}
	}
	if got := atomic.LoadInt32(&maxActive); got < 2 {
		close(release)
		t.Fatalf("max concurrent handlers: got %d, want at least 2", got)
	}
	close(release)
	wg.Wait()
}

func TestHTTPClosureHandlerAllowsConcurrentCallbacks(t *testing.T) {
	e := New(io.Discard)
	var active int32
	var maxActive int32
	release := make(chan struct{})
	entered := make(chan struct{}, 2)

	blocker := func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
		current := atomic.AddInt32(&active, 1)
		for {
			previous := atomic.LoadInt32(&maxActive)
			if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		atomic.AddInt32(&active, -1)
		return gruntime.Null{}, nil
	}

	// Geblang closure handler — exercises the clone path.
	handler := concurrencyHandler(blocker)

	server := newLocalHTTPTestServer(t, e.httpHandler(handler, nil, nil, 0, false, false))
	defer server.Close()

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(server.URL)
			if err != nil {
				t.Errorf("get: %v", err)
				return
			}
			_ = resp.Body.Close()
		}()
	}

	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("timed out waiting for concurrent closure handlers")
		}
	}
	if got := atomic.LoadInt32(&maxActive); got < 2 {
		close(release)
		t.Fatalf("max concurrent closure handlers: got %d, want at least 2", got)
	}
	close(release)
	wg.Wait()
}

func TestHTTPHandlerReceivesRequestObjectWhenTyped(t *testing.T) {
	e := New(io.Discard)
	env := gruntime.NewEnvironment()
	if err := e.installBuiltinTypes(env); err != nil {
		t.Fatalf("install builtins: %v", err)
	}
	var sawRequest bool
	handler := gruntime.Function{
		Name: "handler",
		Env:  env,
		Parameters: []ast.Parameter{{
			Name: &ast.Identifier{Value: "req"},
			Type: &ast.TypeRef{Name: "http.Request"},
		}},
		Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
			req, ok := args[0].(*gruntime.Instance)
			if !ok || req.Class.Name != "Request" {
				t.Fatalf("request arg: got %T, want Request instance", args[0])
			}
			if method, ok := req.Fields["method"].(gruntime.String); !ok || method.Value != "POST" {
				t.Fatalf("method field: %#v", req.Fields["method"])
			}
			sawRequest = true
			return gruntime.String{Value: "ok"}, nil
		},
	}
	req, err := http.NewRequest(http.MethodPost, "http://example.test/items?q=1", bytes.NewBufferString("body"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.RemoteAddr = "127.0.0.1:1234"
	response, err := e.callHTTPHandler(handler, req, []byte("body"), nil, false)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !sawRequest {
		t.Fatal("handler was not called")
	}
	if text, ok := response.(gruntime.String); !ok || text.Value != "ok" {
		t.Fatalf("response: %#v", response)
	}
}

func TestWriteHTTPResponseAcceptsResponseObject(t *testing.T) {
	e := New(io.Discard)
	env := gruntime.NewEnvironment()
	if err := e.installBuiltinTypes(env); err != nil {
		t.Fatalf("install builtins: %v", err)
	}
	headers := map[string]gruntime.DictEntry{}
	putDict(headers, "X-Test", gruntime.String{Value: "yes"})
	response := newResponseInstance(
		e.httpResponseClass,
		gruntime.NewInt64(http.StatusCreated),
		gruntime.String{Value: "created"},
		gruntime.DictFromEntries(headers, nil),
		gruntime.String{},
	)
	recorder := httptest.NewRecorder()
	writeHTTPResponse(recorder, response)
	result := recorder.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d, want %d", result.StatusCode, http.StatusCreated)
	}
	if got := result.Header.Get("X-Test"); got != "yes" {
		t.Fatalf("header: got %q, want yes", got)
	}
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "created" {
		t.Fatalf("body: got %q, want created", string(body))
	}
}

func TestWriteHTTPResponseStreamsFileAndBufferBodies(t *testing.T) {
	e := New(io.Discard)
	env := gruntime.NewEnvironment()
	if err := e.installBuiltinTypes(env); err != nil {
		t.Fatalf("install builtins: %v", err)
	}

	path := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(path, []byte("file-body"), 0o600); err != nil {
		t.Fatalf("write body: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open body: %v", err)
	}
	defer file.Close()
	e.fileMu.Lock()
	e.nextFileID++
	fileID := e.nextFileID
	ensureMap(&e.files)[fileID] = file
	e.fileMu.Unlock()

	fileResponse := newResponseInstance(e.httpResponseClass, gruntime.NewInt64(http.StatusOK), gruntime.NewInt64(fileID), nil, gruntime.String{})
	fileRecorder := httptest.NewRecorder()
	e.writeHTTPResponseValue(fileRecorder, fileResponse)
	fileResult := fileRecorder.Result()
	defer fileResult.Body.Close()
	fileBody, err := io.ReadAll(fileResult.Body)
	if err != nil {
		t.Fatalf("read file response: %v", err)
	}
	if string(fileBody) != "file-body" {
		t.Fatalf("file response body: got %q, want file-body", string(fileBody))
	}

	e.bufferMu.Lock()
	e.nextBufferID++
	bufferID := e.nextBufferID
	buffer := &bytes.Buffer{}
	buffer.WriteString("buffer-body")
	ensureMap(&e.buffers)[bufferID] = buffer
	e.bufferMu.Unlock()

	bufferResponse := newResponseInstance(e.httpResponseClass, gruntime.NewInt64(http.StatusOK), gruntime.NativeObject{Kind: "IOBuffer", ID: bufferID}, nil, gruntime.String{})
	bufferRecorder := httptest.NewRecorder()
	e.writeHTTPResponseValue(bufferRecorder, bufferResponse)
	bufferResult := bufferRecorder.Result()
	defer bufferResult.Body.Close()
	bufferBody, err := io.ReadAll(bufferResult.Body)
	if err != nil {
		t.Fatalf("read buffer response: %v", err)
	}
	if string(bufferBody) != "buffer-body" {
		t.Fatalf("buffer response body: got %q, want buffer-body", string(bufferBody))
	}
}

func TestHTTPHandlerRejectsOverloadWhenCapped(t *testing.T) {
	e := New(io.Discard)
	release := make(chan struct{})
	entered := make(chan struct{}, 4)

	handler := gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
		entered <- struct{}{}
		<-release
		return gruntime.String{Value: "ok"}, nil
	}}

	pool := concurrent.NewPool(2, 0, concurrent.Reject)
	server := newLocalHTTPTestServer(t, e.httpHandler(handler, pool, nil, 0, false, false))
	defer server.Close()

	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := http.Get(server.URL)
			if err != nil {
				t.Errorf("request %d: %v", i, err)
				return
			}
			codes[i] = resp.StatusCode
			_ = resp.Body.Close()
		}(i)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("expected two handlers to enter")
		}
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	ok, rejected := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusServiceUnavailable:
			rejected++
		}
	}
	if ok != 2 || rejected != 2 {
		t.Fatalf("status codes: ok=%d rejected=%d codes=%v want ok=2 rejected=2", ok, rejected, codes)
	}
	stats := pool.Stats()
	if stats.Rejected != 2 {
		t.Fatalf("pool.Stats.Rejected: got %d want 2", stats.Rejected)
	}
}

func TestHTTPHandlerWaitsForSlotUnderWaitPolicy(t *testing.T) {
	e := New(io.Discard)
	release := make(chan struct{})
	var handled int32

	handler := gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
		<-release
		atomic.AddInt32(&handled, 1)
		return gruntime.String{Value: "ok"}, nil
	}}

	pool := concurrent.NewPool(1, 0, concurrent.Wait)
	server := newLocalHTTPTestServer(t, e.httpHandler(handler, pool, nil, 0, false, false))
	defer server.Close()

	var wg sync.WaitGroup
	codes := make([]int, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := http.Get(server.URL)
			if err != nil {
				t.Errorf("request %d: %v", i, err)
				return
			}
			codes[i] = resp.StatusCode
			_ = resp.Body.Close()
		}(i)
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("request %d: got %d want 200 (wait policy should not reject)", i, code)
		}
	}
	if atomic.LoadInt32(&handled) != 3 {
		t.Fatalf("handler invocations: got %d want 3", atomic.LoadInt32(&handled))
	}
}
