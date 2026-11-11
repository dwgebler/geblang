package evaluator

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	gruntime "geblang/internal/runtime"
)

// throwingHandler is a native handler whose throw carries structured frames,
// mirroring what a Geblang handler produces on both backends.
func throwingHandler() gruntime.Function {
	return gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
		return nil, thrownError{value: gruntime.Error{
			Class:   "ValueError",
			Message: "handler boom",
			TraceFrames: []gruntime.StackFrame{
				{Name: "explode", CallLine: 0},
			},
			ErrorLine:    5,
			TopLevelLine: 9,
		}}
	}}
}

func serveOnce(t *testing.T, debug bool) (int, string, string) {
	t.Helper()
	e := New(io.Discard)
	var errBuf bytes.Buffer
	e.stderr = &errBuf
	server := httptest.NewServer(e.httpHandler(throwingHandler(), nil, nil, 0, debug, false))
	defer server.Close()
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body), errBuf.String()
}

func TestServeHandlerErrorProductionMode(t *testing.T) {
	status, body, logged := serveOnce(t, false)
	if status != http.StatusInternalServerError {
		t.Fatalf("status %d", status)
	}
	if body != "Internal Server Error\n" {
		t.Fatalf("production body must be generic, got %q", body)
	}
	if logged != "http.serve: handler error: ValueError: handler boom\n" {
		t.Fatalf("production log must be one line, got %q", logged)
	}
}

func TestServeHandlerErrorDebugOptsMode(t *testing.T) {
	status, body, logged := serveOnce(t, true)
	if status != http.StatusInternalServerError {
		t.Fatalf("status %d", status)
	}
	want := "uncaught ValueError: handler boom\n  at explode (line 5)\n  at <top level> (line 9)"
	if !strings.Contains(body, want) {
		t.Fatalf("debug body missing trace:\n%s", body)
	}
	if !strings.Contains(logged, want) {
		t.Fatalf("debug log missing trace:\n%s", logged)
	}
}

func TestServeDebugEnabledEnvSwitch(t *testing.T) {
	t.Setenv("GEBLANG_DEBUG", "1")
	if !serveDebugEnabled(false) {
		t.Fatal("GEBLANG_DEBUG=1 must enable debug")
	}
	t.Setenv("GEBLANG_DEBUG", "0")
	if serveDebugEnabled(false) {
		t.Fatal("GEBLANG_DEBUG=0 must not enable debug")
	}
	t.Setenv("GEBLANG_DEBUG", "")
	if serveDebugEnabled(false) {
		t.Fatal("empty GEBLANG_DEBUG must not enable debug")
	}
	if !serveDebugEnabled(true) {
		t.Fatal("opts debug must enable regardless of env")
	}
}

// extractFunction evals a top-level program on e and returns one of its declared functions.
func extractFunction(t *testing.T, e *Evaluator, src, name string) gruntime.Function {
	t.Helper()
	prog := parseForTest(t, src)
	env := gruntime.NewEnvironment()
	if err := e.installBuiltinTypes(env); err != nil {
		t.Fatalf("install builtins: %v", err)
	}
	e.pushDeferFrame()
	if _, err := e.evalTopLevelStatements(prog.Statements, env); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, ok := env.Get(name)
	if !ok {
		t.Fatalf("function %q not found", name)
	}
	fn, ok := v.(gruntime.Function)
	if !ok {
		t.Fatalf("%q is not a function: %T", name, v)
	}
	return fn
}

// TestStreamingHandlerGetsFreshThreadID is the C1 regression: a streaming handler must get a fresh thread id (>= 2), not inherit main (id 1).
func TestStreamingHandlerGetsFreshThreadID(t *testing.T) {
	e := New(io.Discard)
	streamBody := extractFunction(t, e, "func streamBody(any s): any { return s; }", "streamBody")

	var mu sync.Mutex
	started := map[int]string{}
	e.SetDebugSourcePath("test.gb")
	e.SetDebugThreadHooks(
		func(id int, name string) { mu.Lock(); started[id] = name; mu.Unlock() },
		func(id int) {},
	)
	e.SetDebugHook(func(p DebugPause) {})

	outer := gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
		entries := map[string]gruntime.DictEntry{}
		putDict(entries, "stream", streamBody)
		return gruntime.DictFromEntries(entries, nil), nil
	}}

	server := httptest.NewServer(e.httpHandler(outer, nil, nil, 0, false, false))
	defer server.Close()
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body) // drain so the stream handler fully completes before we inspect
	resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	streamID := 0
	for id, name := range started {
		if name == "stream" {
			streamID = id
		}
	}
	if streamID < 2 {
		t.Fatalf("streaming handler must get a fresh thread id >= 2, got %d (started: %#v)", streamID, started)
	}
}

func TestParseDebugFlag(t *testing.T) {
	if got, err := parseDebugFlag(gruntime.Null{}, "http.serve"); err != nil || got {
		t.Fatalf("non-dict opts: got %v err %v", got, err)
	}
	entries := map[string]gruntime.DictEntry{}
	putDict(entries, "debug", gruntime.Bool{Value: true})
	if got, err := parseDebugFlag(gruntime.DictFromEntries(entries, nil), "http.serve"); err != nil || !got {
		t.Fatalf("debug true: got %v err %v", got, err)
	}
	bad := map[string]gruntime.DictEntry{}
	putDict(bad, "debug", gruntime.String{Value: "yes"})
	if _, err := parseDebugFlag(gruntime.DictFromEntries(bad, nil), "http.serve"); err == nil {
		t.Fatal("non-bool debug must error")
	}
}
