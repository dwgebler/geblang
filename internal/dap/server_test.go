package dap

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"geblang/internal/evaluator"
)

// TestRunScriptAbortsOnStaticError verifies the pre-flight aborts on a no-matching-overload compile error.
func TestRunScriptAbortsOnStaticError(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "bad_overload.gb")
	if err := os.WriteFile(scriptPath, []byte(`import io;
func describe(string v): string { return "s:" + v; }
func describe(int v): string { return "i:" + (v as string); }
io.println("would run if pre-flight skipped");
io.println(describe(1.0f));
`), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	var out bytes.Buffer
	s := &Server{
		w:            &out,
		breakpoints:  map[string]map[int]breakpointInfo{},
		sourcePaths:  map[string]string{},
		terminatedCh: make(chan error, 1),
		scriptPath:   scriptPath,
	}
	s.runScript()

	err := <-s.terminatedCh
	if err == nil {
		t.Fatal("expected terminate-with-error from runScript")
	}
	if !strings.Contains(err.Error(), "no matching overload for describe") {
		t.Fatalf("error: got %v", err)
	}
	if !strings.Contains(out.String(), "no matching overload for describe") {
		t.Fatalf("debug-console output should include the diagnostic, got %q", out.String())
	}
	if strings.Contains(out.String(), "would run if pre-flight skipped") {
		t.Fatalf("script body must not execute; got %q", out.String())
	}
}

// TestRunScriptAbortsOnSemanticError verifies the pre-flight aborts on a semantic error (free-standing module statement).
func TestRunScriptAbortsOnSemanticError(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "bad_module.gb")
	if err := os.WriteFile(scriptPath, []byte(`module bad;
import io;
io.println("free-standing at module top level");
`), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	var out bytes.Buffer
	s := &Server{
		w:            &out,
		breakpoints:  map[string]map[int]breakpointInfo{},
		sourcePaths:  map[string]string{},
		terminatedCh: make(chan error, 1),
		scriptPath:   scriptPath,
	}
	s.runScript()

	err := <-s.terminatedCh
	if err == nil {
		t.Fatal("expected terminate-with-error from runScript")
	}
	if !strings.Contains(err.Error(), "static analysis failed") {
		t.Fatalf("error: got %v", err)
	}
	if !strings.Contains(out.String(), "free-standing top-level") {
		t.Fatalf("debug-console output should include the diagnostic, got %q", out.String())
	}
	if strings.Contains(out.String(), "free-standing at module top level") {
		t.Fatalf("script body must not execute; got %q", out.String())
	}
}

func TestNormalizePathUsesCwdForRelativeProgram(t *testing.T) {
	cwd := filepath.Join("tmp", "project")
	got, err := normalizePath("src/main.gb", "", cwd)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Join(cwd, "src", "main.gb"))
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("normalizePath = %q, want %q", got, want)
	}
}

func TestNormalizePathHandlesFileURI(t *testing.T) {
	got, err := normalizePath("file:///tmp/app/main.gb", "", "")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Join(string(filepath.Separator), "tmp", "app", "main.gb"))
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("normalizePath = %q, want %q", got, want)
	}
}

func TestNormalizePathHandlesWSLLocalhostUNC(t *testing.T) {
	got, err := normalizePath(`\\wsl.localhost\Ubuntu\home\daveg\projects\geblang\examples\functions.gb`, "", "/home/daveg/projects/geblang/examples")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(string(filepath.Separator), "home", "daveg", "projects", "geblang", "examples", "functions.gb")
	if got != filepath.Clean(want) {
		t.Fatalf("normalizePath = %q, want %q", got, want)
	}
}

func TestNormalizePathHandlesWSLDollarUNC(t *testing.T) {
	got, err := normalizePath(`\\wsl$\Ubuntu\home\daveg\projects\geblang\examples\functions.gb`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(string(filepath.Separator), "home", "daveg", "projects", "geblang", "examples", "functions.gb")
	if got != filepath.Clean(want) {
		t.Fatalf("normalizePath = %q, want %q", got, want)
	}
}

func TestWindowsDriveToWSLPath(t *testing.T) {
	got, ok := windowsDriveToWSLPath(`C:\Users\dave\project\main.gb`)
	if !ok {
		t.Fatal("expected Windows drive path to be recognized")
	}
	want := "/mnt/c/Users/dave/project/main.gb"
	if got != want {
		t.Fatalf("windowsDriveToWSLPath = %q, want %q", got, want)
	}
}

func TestStackTraceReturnsClientSourcePath(t *testing.T) {
	var out bytes.Buffer
	s := &Server{
		w:           &out,
		sourcePaths: map[string]string{},
		focused:     1,
		threads: map[int]*threadInfo{1: {name: "main", stopped: true, lastPause: &evaluator.DebugPause{Loc: evaluator.DebugLocation{
			Path: "/home/daveg/projects/geblang/examples/functions.gb",
			Line: 10,
		}, Frames: []evaluator.DebugFrame{{
			Name: "<top level>",
			Path: "/home/daveg/projects/geblang/examples/functions.gb",
			Line: 10,
		}}, Vars: []evaluator.DebugVariable{
			{Name: "answer", Value: "42", Type: "int"},
		}}}},
	}
	clientPath := `\\wsl.localhost\Ubuntu\home\daveg\projects\geblang\examples\functions.gb`
	s.recordClientSourcePathLocked("/home/daveg/projects/geblang/examples/functions.gb", clientPath, "")

	if err := s.handleRequest(&Message{Seq: 1, Command: "stackTrace"}); err != nil {
		t.Fatal(err)
	}
	response := readDAPResponse(t, out.String())
	body := response["body"].(map[string]any)
	frames := body["stackFrames"].([]any)
	frame := frames[0].(map[string]any)
	source := frame["source"].(map[string]any)
	if source["path"] != clientPath {
		t.Fatalf("source path = %q, want %q", source["path"], clientPath)
	}
	if source["name"] != "functions.gb" {
		t.Fatalf("source name = %q, want functions.gb", source["name"])
	}
	if frame["id"].(float64) <= 0 {
		t.Fatalf("frame id = %v, want positive id", frame["id"])
	}
}

func TestScopesAndVariablesReturnPausedLocals(t *testing.T) {
	var out bytes.Buffer
	s := &Server{
		w:       &out,
		focused: 1,
		threads: map[int]*threadInfo{1: {name: "main", stopped: true, lastPause: &evaluator.DebugPause{
			Frames: []evaluator.DebugFrame{{Name: "<top level>", Path: "/tmp/main.gb", Line: 1}},
			Vars:   []evaluator.DebugVariable{{Name: "name", Value: "Geblang", Type: "string"}},
		}}},
	}
	if err := s.handleRequest(&Message{Seq: 1, Command: "stackTrace"}); err != nil {
		t.Fatal(err)
	}
	stackResponse := readDAPResponse(t, out.String())
	stackFrames := stackResponse["body"].(map[string]any)["stackFrames"].([]any)
	frameID := int(stackFrames[0].(map[string]any)["id"].(float64))

	out.Reset()
	if err := s.handleRequest(&Message{Seq: 2, Command: "scopes", Arguments: map[string]any{"frameId": frameID}}); err != nil {
		t.Fatal(err)
	}
	scopeResponse := readDAPResponse(t, out.String())
	scopes := scopeResponse["body"].(map[string]any)["scopes"].([]any)
	scope := scopes[0].(map[string]any)
	if int(scope["variablesReference"].(float64)) != frameID {
		t.Fatalf("variablesReference = %v, want %d", scope["variablesReference"], frameID)
	}

	out.Reset()
	if err := s.handleRequest(&Message{Seq: 3, Command: "variables", Arguments: map[string]any{"variablesReference": frameID}}); err != nil {
		t.Fatal(err)
	}
	varResponse := readDAPResponse(t, out.String())
	variables := varResponse["body"].(map[string]any)["variables"].([]any)
	if len(variables) != 1 {
		t.Fatalf("expected one variable, got %#v", variables)
	}
	variable := variables[0].(map[string]any)
	if variable["name"] != "name" || variable["value"] != "Geblang" || variable["type"] != "string" {
		t.Fatalf("unexpected variable %#v", variable)
	}
}

func readDAPResponse(t *testing.T, raw string) map[string]any {
	t.Helper()
	parts := strings.SplitN(raw, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("invalid DAP response %q", raw)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(parts[1]), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

// ---- in-process session-driver harness ----

type stoppedEvent struct {
	Reason            string
	ThreadID          int
	AllThreadsStopped bool
}

type testSession struct {
	t            *testing.T
	enc          *io.PipeWriter
	seq          int
	mu           sync.Mutex
	resp         map[int]chan map[string]any
	stops        chan stoppedEvent
	output       chan string
	threadEvents chan map[string]any
	term         chan struct{}
	closed       chan struct{}
	termOnce     sync.Once
	closeOnce    sync.Once
	outMu        sync.Mutex
	outBuf       strings.Builder
}

// newTestSession starts Serve over io.Pipe and performs the initialize+launch handshake.
func newTestSession(t *testing.T, scriptPath string) *testSession {
	t.Helper()
	clientR, clientW := io.Pipe()
	serverR, serverW := io.Pipe()

	s := &testSession{
		t:            t,
		enc:          clientW,
		resp:         map[int]chan map[string]any{},
		stops:        make(chan stoppedEvent, 10),
		output:       make(chan string, 100),
		threadEvents: make(chan map[string]any, 10),
		term:         make(chan struct{}),
		closed:       make(chan struct{}),
	}

	go func() {
		defer serverW.Close()
		Serve(clientR, serverW)
	}()

	go s.readLoop(serverR)

	s.request("initialize", map[string]any{"clientName": "test"})
	s.request("launch", map[string]any{
		"program": scriptPath,
		"cwd":     filepath.Dir(scriptPath),
	})
	return s
}

// readLoop reads DAP frames from the server and routes them to the right channels.
func (s *testSession) readLoop(r *io.PipeReader) {
	defer close(s.closed)
	br := bufio.NewReader(r)
	for {
		msg, err := readTestFrame(br)
		if err != nil {
			return
		}
		msgType, _ := msg["type"].(string)
		switch msgType {
		case "response":
			reqSeq := int(msg["request_seq"].(float64))
			s.mu.Lock()
			ch, ok := s.resp[reqSeq]
			if ok {
				delete(s.resp, reqSeq)
			}
			s.mu.Unlock()
			if ok {
				ch <- msg
			}
		case "event":
			event, _ := msg["event"].(string)
			body, _ := msg["body"].(map[string]any)
			switch event {
			case "stopped":
				reason, _ := body["reason"].(string)
				tid := 0
				if v, ok := body["threadId"].(float64); ok {
					tid = int(v)
				}
				allStopped, _ := body["allThreadsStopped"].(bool)
				s.stops <- stoppedEvent{Reason: reason, ThreadID: tid, AllThreadsStopped: allStopped}
			case "output":
				category, _ := body["category"].(string)
				if category == "stdout" {
					text, _ := body["output"].(string)
					select {
					case s.output <- text:
					default:
					}
				}
			case "thread":
				select {
				case s.threadEvents <- body:
				default:
				}
			case "terminated":
				s.termOnce.Do(func() { close(s.term) })
			}
		}
	}
}

// readTestFrame reads one Content-Length-framed JSON message.
func readTestFrame(r *bufio.Reader) (map[string]any, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length: ")))
			if err != nil {
				return nil, err
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var msg map[string]any
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// request sends a DAP request and blocks until the response arrives.
func (s *testSession) request(command string, args map[string]any) map[string]any {
	s.mu.Lock()
	s.seq++
	seq := s.seq
	ch := make(chan map[string]any, 1)
	s.resp[seq] = ch
	s.mu.Unlock()

	envelope := map[string]any{
		"seq":     seq,
		"type":    "request",
		"command": command,
	}
	if args != nil {
		envelope["arguments"] = args
	}
	data, _ := json.Marshal(envelope)
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(data), data)
	s.enc.Write([]byte(frame))

	select {
	case resp := <-ch:
		return resp
	case <-s.closed:
		// Server exited before responding; return empty to let close() proceed.
		return map[string]any{}
	case <-time.After(10 * time.Second):
		s.t.Fatalf("timeout waiting for response to %q (seq %d)", command, seq)
		return nil
	}
}

// send writes a request frame and returns its seq without waiting for the response, enabling true wire-level pipelining.
func (s *testSession) send(command string, args map[string]any) int {
	s.mu.Lock()
	s.seq++
	seq := s.seq
	ch := make(chan map[string]any, 1)
	s.resp[seq] = ch
	s.mu.Unlock()

	envelope := map[string]any{"seq": seq, "type": "request", "command": command}
	if args != nil {
		envelope["arguments"] = args
	}
	data, _ := json.Marshal(envelope)
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(data), data)
	s.enc.Write([]byte(frame))
	return seq
}

func (s *testSession) setBreakpoints(path string, lines ...int) {
	bps := make([]map[string]any, len(lines))
	for i, l := range lines {
		bps[i] = map[string]any{"line": l}
	}
	s.request("setBreakpoints", map[string]any{
		"source":      map[string]any{"path": path},
		"breakpoints": bps,
	})
}

// setBreakpointsRaw sends setBreakpoints with caller-supplied breakpoint objects (supports condition, etc.).
func (s *testSession) setBreakpointsRaw(path string, bps []map[string]any) {
	s.request("setBreakpoints", map[string]any{
		"source":      map[string]any{"path": path},
		"breakpoints": bps,
	})
}

func (s *testSession) configurationDone() {
	s.request("configurationDone", nil)
}

func (s *testSession) waitStopped() stoppedEvent {
	select {
	case ev := <-s.stops:
		return ev
	case <-time.After(10 * time.Second):
		s.t.Fatal("timeout waiting for stopped event")
		return stoppedEvent{}
	}
}

func (s *testSession) threads() []map[string]any {
	resp := s.request("threads", nil)
	body, _ := resp["body"].(map[string]any)
	raw, _ := body["threads"].([]any)
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

func (s *testSession) stackTrace(threadID int) []map[string]any {
	resp := s.request("stackTrace", map[string]any{"threadId": threadID})
	body, _ := resp["body"].(map[string]any)
	raw, _ := body["stackFrames"].([]any)
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

func (s *testSession) variables(ref int) map[string]string {
	resp := s.request("variables", map[string]any{"variablesReference": ref})
	body, _ := resp["body"].(map[string]any)
	raw, _ := body["variables"].([]any)
	result := map[string]string{}
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		value, _ := m["value"].(string)
		result[name] = value
	}
	return result
}

// evaluate sends an evaluate request; threadID is forwarded for Task 5 reuse.
func (s *testSession) evaluate(threadID, frameID int, expr string) string {
	resp := s.request("evaluate", map[string]any{
		"expression": expr,
		"frameId":    frameID,
		"threadId":   threadID,
	})
	body, _ := resp["body"].(map[string]any)
	result, _ := body["result"].(string)
	return result
}

func (s *testSession) continueAll() {
	s.request("continue", map[string]any{"threadId": 0})
}

func (s *testSession) continueThread(threadID int) {
	s.request("continue", map[string]any{"threadId": threadID})
}

func (s *testSession) setVariableReq(frameID int, name, value string) map[string]any {
	return s.request("setVariable", map[string]any{
		"variablesReference": frameID,
		"name":               name,
		"value":              value,
	})
}

func (s *testSession) next(threadID int) {
	s.request("next", map[string]any{"threadId": threadID})
}

func (s *testSession) nextResp(threadID int) map[string]any {
	return s.request("next", map[string]any{"threadId": threadID})
}

func (s *testSession) stepIn(threadID int) {
	s.request("stepIn", map[string]any{"threadId": threadID})
}

func (s *testSession) stepOut(threadID int) {
	s.request("stepOut", map[string]any{"threadId": threadID})
}

// waitThreadStarted returns true when the next "started" thread event arrives within d.
func (s *testSession) waitThreadStarted(d time.Duration) bool {
	deadline := time.After(d)
	for {
		select {
		case body := <-s.threadEvents:
			if reason, _ := body["reason"].(string); reason == "started" {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// waitThreadStartedID returns the threadId of the next started thread event within d.
func (s *testSession) waitThreadStartedID(d time.Duration) (int, bool) {
	deadline := time.After(d)
	for {
		select {
		case body := <-s.threadEvents:
			if reason, _ := body["reason"].(string); reason == "started" {
				id := 0
				if v, ok := body["threadId"].(float64); ok {
					id = int(v)
				}
				return id, true
			}
		case <-deadline:
			return 0, false
		}
	}
}

// waitStoppedTimeout returns the next stopped event, or ok=false if none arrives within d.
func (s *testSession) waitStoppedTimeout(d time.Duration) (stoppedEvent, bool) {
	select {
	case ev := <-s.stops:
		return ev, true
	case <-time.After(d):
		return stoppedEvent{}, false
	}
}

// continueUntilTerminated resumes every thread that stops until the program ends.
func (s *testSession) continueUntilTerminated() {
	for {
		select {
		case <-s.stops:
			s.continueAll()
		case <-s.term:
			return
		case <-time.After(10 * time.Second):
			s.t.Fatal("timeout waiting for termination")
			return
		}
	}
}

func (s *testSession) waitTerminated() {
	select {
	case <-s.term:
	case <-time.After(10 * time.Second):
		s.t.Fatal("timeout waiting for terminated event")
	}
}

func (s *testSession) collectedOutput() string {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	for {
		select {
		case text := <-s.output:
			s.outBuf.WriteString(text)
		default:
			return s.outBuf.String()
		}
	}
}

// close sends disconnect and waits for the server goroutine; idempotent.
func (s *testSession) close() {
	s.closeOnce.Do(func() {
		// If server already exited (e.g. after waitTerminated), skip disconnect.
		select {
		case <-s.closed:
			s.enc.Close()
			return
		default:
		}
		s.request("disconnect", nil)
		s.enc.Close()
		select {
		case <-s.closed:
		case <-time.After(5 * time.Second):
			// A hung Serve goroutine is the signature of a coordinator deadlock.
			s.t.Errorf("timeout waiting for server cleanup: Serve goroutine did not exit")
		}
	})
}

// ---- end-to-end test ----

func TestMainThreadDebugSession(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "main.gb")
	if err := os.WriteFile(scriptPath, []byte("import io;\nlet x = 1;\nlet y = x + 1;\nio.println(y);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestSession(t, scriptPath)
	defer s.close()

	s.setBreakpoints(scriptPath, 3) // pause before `let y = x + 1` executes
	s.configurationDone()

	st := s.waitStopped()
	if st.ThreadID != 1 {
		t.Fatalf("expected main thread id 1, got %d", st.ThreadID)
	}
	threads := s.threads()
	if len(threads) != 1 {
		t.Fatalf("single-thread server should report 1 thread, got %#v", threads)
	}
	frames := s.stackTrace(1)
	if len(frames) == 0 {
		t.Fatal("no stack frames at breakpoint")
	}
	frameID := int(frames[0]["id"].(float64))
	vars := s.variables(frameID)
	if vars["x"] != "1" {
		t.Fatalf("expected x=1 at breakpoint, got %q (%#v)", vars["x"], vars)
	}
	if got := s.evaluate(1, frameID, "x + 1"); got != "2" {
		t.Fatalf("evaluate x+1 = %q, want 2", got)
	}
	s.continueAll()
	s.waitTerminated()
	if !strings.Contains(s.collectedOutput(), "2") {
		t.Fatalf("expected program to print 2, output was %q", s.collectedOutput())
	}
}

// ---- multi-thread (worker) tests ----

// TestThreadsListsLiveWorkers asserts threads enumerates main plus live async workers.
func TestThreadsListsLiveWorkers(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "workers.gb")
	if err := os.WriteFile(scriptPath, []byte(`import async;
let a = async.run(func(): int {
    let x = 1;
    return x;
});
let b = async.run(func(): int {
    let y = 2;
    return y;
});
await a;
await b;
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()

	sess.setBreakpoints(scriptPath, 3, 7) // the `let x`/`let y` worker-body lines
	sess.configurationDone()

	st := sess.waitStopped()
	if st.ThreadID < 2 {
		t.Fatalf("expected a worker thread (>= 2) to stop, got %d", st.ThreadID)
	}
	threads := sess.threads()
	if len(threads) < 2 {
		t.Fatalf("expected main + at least one worker thread, got %#v", threads)
	}
	foundMain := false
	for _, th := range threads {
		if int(th["id"].(float64)) == 1 {
			foundMain = true
		}
	}
	if !foundMain {
		t.Fatalf("main thread (id 1) missing from %#v", threads)
	}
	sess.continueAll()
	sess.continueUntilTerminated() // the other worker stops at its breakpoint too
}

// TestBreakpointInsideAsyncWorkerStopsAndResumes stops in a worker, inspects, resumes.
func TestBreakpointInsideAsyncWorkerStopsAndResumes(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "worker.gb")
	if err := os.WriteFile(scriptPath, []byte(`import async;
let t = async.run(func(): int {
    let total = 0;
    total = total + 1;
    return total;
});
let _ = await t;
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, 4) // `total = total + 1`
	sess.configurationDone()

	st := sess.waitStopped()
	if st.ThreadID < 2 {
		t.Fatalf("expected a worker thread (>= 2) to stop, got %d", st.ThreadID)
	}
	frames := sess.stackTrace(st.ThreadID)
	if len(frames) == 0 {
		t.Fatalf("no frames for worker thread %d", st.ThreadID)
	}
	frameID := int(frames[0]["id"].(float64))
	vars := sess.variables(frameID)
	if vars["total"] != "0" {
		t.Fatalf("expected total=0 at breakpoint, got %q (%#v)", vars["total"], vars)
	}
	sess.continueThread(st.ThreadID)
	sess.waitTerminated()
}

const twoWorkerBarrierScript = `import async;
import async.channel as ch;
let ready = ch.Channel<int>(2);
let gate = ch.Channel<int>(0);
func work(int n): int {
    let x = n;
    let y = x + 1;
    return y;
}
let a = async.run(func(): int { ready.send(1); gate.recv(); return work(10); });
let b = async.run(func(): int { ready.send(1); gate.recv(); return work(20); });
ready.recv();
ready.recv();
gate.close();
await a;
await b;
`

const twoWorkerBreakpointLine = 7

// TestStopTheWorldFreezesOtherWorkers proves a second worker cannot stop while the first holds the stop, and only does so after continue (no shared-mutable reads, so race-free).
func TestStopTheWorldFreezesOtherWorkers(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "freeze.gb")
	if err := os.WriteFile(scriptPath, []byte(twoWorkerBarrierScript), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, twoWorkerBreakpointLine)
	sess.configurationDone()

	// Confirm both workers are alive before the freeze assertion so silence proves freeze.
	if !sess.waitThreadStarted(5 * time.Second) {
		t.Fatal("worker A did not start")
	}
	if !sess.waitThreadStarted(5 * time.Second) {
		t.Fatal("worker B did not start")
	}
	first := sess.waitStopped()
	if first.ThreadID < 2 {
		t.Fatalf("expected a worker thread to stop first, got %d", first.ThreadID)
	}
	// Round-trip gives the scheduler time to run worker B to its first statement and park.
	_ = sess.threads()
	// silence means "frozen"; residual: false-pass only if worker B takes >2s to reach its first statement
	if ev, ok := sess.waitStoppedTimeout(2 * time.Second); ok {
		t.Fatalf("worker %d stopped while worker %d held the stop: world not frozen", ev.ThreadID, first.ThreadID)
	}
	// Inspecting the focused worker's own frame is race-free.
	frames := sess.stackTrace(first.ThreadID)
	if len(frames) == 0 {
		t.Fatalf("no frames for focused worker %d", first.ThreadID)
	}
	if got := sess.evaluate(first.ThreadID, int(frames[0]["id"].(float64)), "x + 1"); got != "11" && got != "21" {
		t.Fatalf("evaluate x+1 in focused worker = %q, want 11 or 21", got)
	}
	// Releasing the world lets the frozen worker proceed and stop at the line.
	sess.continueAll()
	second := sess.waitStopped()
	if second.ThreadID == first.ThreadID {
		t.Fatalf("same worker %d stopped twice; expected the other worker to stop after continue", second.ThreadID)
	}
	if second.ThreadID < 2 {
		t.Fatalf("expected the second worker (>= 2) to stop after continue, got %d", second.ThreadID)
	}
	sess.continueAll()
	sess.waitTerminated()
}

// TestDisconnectWhileStoppedReleasesWorkers disconnects mid-stop; close() fails if a goroutine keeps Serve alive.
func TestDisconnectWhileStoppedReleasesWorkers(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "disc.gb")
	if err := os.WriteFile(scriptPath, []byte(`import async;
let a = async.run(func(): int {
    let x = 1;
    return x;
});
let b = async.run(func(): int {
    let y = 2;
    return y;
});
await a;
await b;
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	sess.setBreakpoints(scriptPath, 3, 7)
	sess.configurationDone()
	sess.waitStopped()
	sess.close() // disconnect while one worker is stopped and the other frozen
}

// TestContinueReleasesParkedWorkers is the regression for the park-mode bug (parked worker retained modeStepIn after continue).
func TestContinueReleasesParkedWorkers(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "park.gb")
	// time.sleep gives a reliable window to send pause before the worker finishes.
	if err := os.WriteFile(scriptPath, []byte(`import async;
import time;
func countTo(int n): int {
    let sum = 0;
    let i = 0;
    while (i < n) {
        sum = sum + i;
        i = i + 1;
    }
    return sum;
}
let a = async.run(func(): int {
    time.sleep(500);
    return countTo(100);
});
let b = async.run(func(): int {
    time.sleep(500);
    return countTo(100);
});
await a;
await b;
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.configurationDone()

	// Capture worker A's ID at registration; sleep in the worker gives us a reliable window.
	workerID, ok := sess.waitThreadStartedID(5 * time.Second)
	if !ok {
		t.Fatal("worker A did not start")
	}
	if !sess.waitThreadStarted(5 * time.Second) {
		t.Fatal("worker B did not start")
	}
	sess.request("pause", map[string]any{"threadId": workerID})
	// One worker stops as focused; the other parks because s.stopped is true.
	sess.waitStopped()
	// continue must deliver modeContinue to the parked worker so it does not re-stop.
	sess.continueAll()
	// With the bug the parked worker re-stops immediately and the program never ends.
	sess.waitTerminated()
}

// TestChannelWorkerExampleDebugSession is the end-to-end test on the website channel-worker example.
func TestChannelWorkerExampleDebugSession(t *testing.T) {
	scriptPath, err := filepath.Abs("testdata/channel_worker.gb")
	if err != nil {
		t.Fatal(err)
	}

	s := newTestSession(t, scriptPath)
	defer s.close()

	s.setBreakpoints(scriptPath, 14) // `total = total + (job as int);`
	s.configurationDone()

	// First stop must be in a worker thread.
	st := s.waitStopped()
	if st.ThreadID < 2 {
		t.Fatalf("expected worker thread (>= 2) to stop, got %d", st.ThreadID)
	}

	frames := s.stackTrace(st.ThreadID)
	if len(frames) == 0 {
		t.Fatalf("no frames for worker thread %d", st.ThreadID)
	}
	frameID := int(frames[0]["id"].(float64))
	vars := s.variables(frameID)
	if _, ok := vars["total"]; !ok {
		t.Fatalf("expected 'total' in variables, got %v", vars)
	}
	if _, ok := vars["job"]; !ok {
		t.Fatalf("expected 'job' in variables, got %v", vars)
	}
	if vars["total"] != "0" {
		t.Fatalf("expected total=0 at first hit, got %q", vars["total"])
	}
	if vars["job"] != "1" {
		t.Fatalf("expected job=1 at first hit (first channel recv), got %q", vars["job"])
	}

	// Release the first stop; drain the remaining breakpoint fires (3 more) then wait for termination.
	s.continueAll()
	s.continueUntilTerminated()

	if !strings.Contains(s.collectedOutput(), "10") {
		t.Fatalf("expected output to contain '10', got %q", s.collectedOutput())
	}
}

func TestChannelWorkerStepReleasesThreadThatClosesChannel(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "channel_step.gb")
	if err := os.WriteFile(scriptPath, []byte(`import io;
import async;
import async.channel as channel;

let jobs = channel.Channel<int>(4);

let worker = async.run(func(): int {
    let total = 0;
    while (true) {
        let job = jobs.recv();
        if (job == null) {
            break;
        }
        total = total + (job as int);
    }
    return total;
});

for (int n = 1; n <= 4; n++) {
    jobs.send(n);
}
await async.sleep(1000);
jobs.close();

io.println(await worker);
`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newTestSession(t, scriptPath)
	defer s.close()
	s.setBreakpoints(scriptPath, 14)
	s.configurationDone()

	st := s.waitStopped()
	tid := st.ThreadID
	for steps := 0; steps < 40; steps++ {
		s.next(tid)
		select {
		case next := <-s.stops:
			tid = next.ThreadID
		case <-s.term:
			if !strings.Contains(s.collectedOutput(), "10") {
				t.Fatalf("expected output to contain '10', got %q", s.collectedOutput())
			}
			return
		case <-time.After(3 * time.Second):
			t.Fatal("step deadlocked while the worker waited for another thread to close its channel")
		}
	}
	t.Fatal("channel worker did not terminate after 40 step requests")
}

// TestEvaluateInWorkerFrame evaluates in a stopped worker frame, resolving its locals.
func TestEvaluateInWorkerFrame(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "ev.gb")
	if err := os.WriteFile(scriptPath, []byte(`import async;
let t = async.run(func(): int {
    let secret = 41;
    let secret2 = secret + 1;
    return secret2;
});
let _ = await t;
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, 4) // `let secret2 = secret + 1`
	sess.configurationDone()
	st := sess.waitStopped()
	if st.ThreadID < 2 {
		t.Fatalf("expected a worker thread (>= 2) to stop, got %d", st.ThreadID)
	}
	got := sess.evaluate(st.ThreadID, 0, "secret + 1")
	if got != "42" {
		t.Fatalf("evaluate in worker frame = %q, want 42", got)
	}
	sess.continueThread(st.ThreadID)
	sess.waitTerminated()
}

// TestEvaluateAsyncExprWhilePausedDoesNotDeadlock: eval-spawned worker must not park behind world stop.
func TestEvaluateAsyncExprWhilePausedDoesNotDeadlock(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "ev.gb")
	if err := os.WriteFile(scriptPath, []byte("import async;\nlet x = 1;\nlet y = x + 1;\nlet _ = y;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestSession(t, scriptPath)
	defer s.close()
	s.setBreakpoints(scriptPath, 3) // pause on `let y = x + 1`
	s.configurationDone()
	st := s.waitStopped()
	got := s.evaluate(st.ThreadID, 0, "await async.run(func(): int { return 42; })")
	if got != "42" {
		t.Fatalf("evaluate async expr = %q, want 42", got)
	}
	s.continueAll()
	s.waitTerminated()
}

// continueRaceScript stops once, then runs to completion so a following inspect races the resuming worker (the I3 TOCTOU window).
const continueRaceScript = "import io;\nlet x = 41;\nlet y = x + 1;\nio.println(y);\n"

// continueThenInspectTrial breakpoints once, continues, then immediately inspects; the fail-safe in request() catches a strand.
func continueThenInspectTrial(t *testing.T, kind string) {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "race.gb")
	if err := os.WriteFile(scriptPath, []byte(continueRaceScript), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, 2) // `let x = 41`, hit exactly once
	sess.configurationDone()

	st := sess.waitStopped()
	frames := sess.stackTrace(st.ThreadID)
	frameID := int(frames[0]["id"].(float64))
	sess.continueAll()
	if kind == "evaluate" {
		_ = sess.evaluate(st.ThreadID, 0, "1 + 1")
	} else {
		_ = sess.setVariableReq(frameID, "x", "99")
	}
	sess.waitTerminated()
}

// TestContinueThenEvaluateDoesNotHang is the I3 regression: continue then evaluate must not strand the DAP loop on a resumed worker.
func TestContinueThenEvaluateDoesNotHang(t *testing.T) {
	for trial := 0; trial < 12; trial++ {
		continueThenInspectTrial(t, "evaluate")
	}
}

// TestContinueThenSetVariableDoesNotHang is the setVariable arm of I3.
func TestContinueThenSetVariableDoesNotHang(t *testing.T) {
	for trial := 0; trial < 12; trial++ {
		continueThenInspectTrial(t, "setVariable")
	}
}

// loopBreakpointScript stops on line 5 every iteration so continue triggers an immediate re-stop.
const loopBreakpointScript = `import io;
let total = 0;
let i = 0;
while (i < 40) {
    total = total + i;
    i = i + 1;
}
io.println(total);
`

// TestContinueThenEvaluatePipelinedDoesNotDeadlock pipelines continue+inspect so the loop could pick the inspect before draining the worker's re-stop; the session must still progress to termination.
func TestContinueThenEvaluatePipelinedDoesNotDeadlock(t *testing.T) {
	for _, kind := range []string{"evaluate", "setVariable"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			scriptPath := filepath.Join(dir, "loopbp.gb")
			if err := os.WriteFile(scriptPath, []byte(loopBreakpointScript), 0o644); err != nil {
				t.Fatal(err)
			}
			sess := newTestSession(t, scriptPath)
			defer sess.close()
			sess.setBreakpoints(scriptPath, 5) // re-fires every loop iteration
			sess.configurationDone()

			first := sess.waitStopped()
			tid := first.ThreadID
			frames := sess.stackTrace(tid)
			frameID := int(frames[0]["id"].(float64))

			for {
				// Pipeline continue immediately followed by an inspect, without waiting for the next stop.
				sess.send("continue", map[string]any{"threadId": 0})
				if kind == "evaluate" {
					sess.send("evaluate", map[string]any{"expression": "1 + 1", "threadId": tid})
				} else {
					sess.send("setVariable", map[string]any{
						"variablesReference": frameID,
						"name":               "total",
						"value":              "0",
					})
				}
				select {
				case <-sess.stops:
					// Worker re-stopped at the next iteration; drive another pipelined pair.
					frames = sess.stackTrace(tid)
					frameID = int(frames[0]["id"].(float64))
				case <-sess.term:
					return
				case <-time.After(10 * time.Second):
					t.Fatal("deadlock: no progress after pipelined continue+" + kind)
				}
			}
		})
	}
}

// TestConditionalBreakpointInWorker verifies a conditional breakpoint stops only when the condition holds.
func TestConditionalBreakpointInWorker(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "cond.gb")
	if err := os.WriteFile(scriptPath, []byte(`import async;
let r = async.run(func(): int {
    let sum = 0;
    let job = 1;
    while (job <= 5) {
        sum = sum + job;
        job = job + 1;
    }
    return sum;
});
let _ = await r;
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	// condition job == 3; line 6 is "        sum = sum + job;"
	sess.setBreakpointsRaw(scriptPath, []map[string]any{
		{"line": 6, "condition": "job == 3"},
	})
	sess.configurationDone()

	st := sess.waitStopped()
	if st.ThreadID < 2 {
		t.Fatalf("expected worker thread (>= 2) to stop, got %d", st.ThreadID)
	}
	frames := sess.stackTrace(st.ThreadID)
	if len(frames) == 0 {
		t.Fatal("no frames at conditional breakpoint stop")
	}
	frameID := int(frames[0]["id"].(float64))
	vars := sess.variables(frameID)
	if vars["job"] != "3" {
		t.Fatalf("conditional bp should stop when job==3, got job=%q", vars["job"])
	}
	if vars["sum"] != "3" {
		t.Fatalf("expected sum=3 (1+2) when job==3, got %q", vars["sum"])
	}
	sess.continueAll()
	sess.waitTerminated()
}

// TestConditionalBreakpointOnMainThread verifies a conditional breakpoint on the main thread.
func TestConditionalBreakpointOnMainThread(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "condmain.gb")
	if err := os.WriteFile(scriptPath, []byte(`import io;
let sum = 0;
let i = 0;
while (i < 5) {
    sum = sum + i;
    i = i + 1;
}
io.println(sum);
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	// line 5 is "    sum = sum + i;" with condition i == 3
	sess.setBreakpointsRaw(scriptPath, []map[string]any{
		{"line": 5, "condition": "i == 3"},
	})
	sess.configurationDone()

	st := sess.waitStopped()
	if st.ThreadID != 1 {
		t.Fatalf("expected main thread (1), got %d", st.ThreadID)
	}
	frames := sess.stackTrace(1)
	if len(frames) == 0 {
		t.Fatal("no frames at conditional breakpoint stop")
	}
	vars := sess.variables(int(frames[0]["id"].(float64)))
	if vars["i"] != "3" {
		t.Fatalf("conditional bp should stop when i==3, got i=%q", vars["i"])
	}
	if vars["sum"] != "3" {
		t.Fatalf("expected sum=3 (0+1+2) when i==3, got %q", vars["sum"])
	}
	sess.continueAll()
	sess.waitTerminated()
	if !strings.Contains(sess.collectedOutput(), "10") {
		t.Fatalf("expected output 10 (0+1+2+3+4), got %q", sess.collectedOutput())
	}
}

// TestSetVariableInWorkerFrame stops in a worker, mutates a local via setVariable, and asserts the mutation took effect.
func TestSetVariableInWorkerFrame(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "setvar.gb")
	if err := os.WriteFile(scriptPath, []byte(`import async;
import io;
let result = async.run(func(): int {
    let x = 10;
    return x;
});
io.println(await result);
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, 5) // "    return x;"
	sess.configurationDone()

	st := sess.waitStopped()
	if st.ThreadID < 2 {
		t.Fatalf("expected worker thread (>= 2), got %d", st.ThreadID)
	}
	frames := sess.stackTrace(st.ThreadID)
	if len(frames) == 0 {
		t.Fatal("no frames in stopped worker")
	}
	frameID := int(frames[0]["id"].(float64))
	vars := sess.variables(frameID)
	if vars["x"] != "10" {
		t.Fatalf("expected x=10 before mutation, got %q", vars["x"])
	}
	resp := sess.setVariableReq(frameID, "x", "99")
	if success, _ := resp["success"].(bool); !success {
		t.Fatalf("setVariable failed: %v", resp["message"])
	}
	body, _ := resp["body"].(map[string]any)
	if val, _ := body["value"].(string); val != "99" {
		t.Fatalf("setVariable response value = %q, want 99", val)
	}
	sess.continueAll()
	sess.waitTerminated()
	if !strings.Contains(sess.collectedOutput(), "99") {
		t.Fatalf("expected output 99 after setVariable mutation, got %q", sess.collectedOutput())
	}
}

// TestNestedWorkerBreakpoint stops inside an async worker that spawned another async worker, asserts distinct thread ids.
func TestNestedWorkerBreakpoint(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "nested.gb")
	if err := os.WriteFile(scriptPath, []byte(`import async;
import io;
let outer = async.run(func(): int {
    let inner = async.run(func(): int {
        let z = 42;
        return z;
    });
    return await inner;
});
io.println(await outer);
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, 6) // "        return z;" in inner worker
	sess.configurationDone()

	st := sess.waitStopped()
	if st.ThreadID < 2 {
		t.Fatalf("expected inner worker thread (>= 2), got %d", st.ThreadID)
	}
	// at least main + outer + inner in the thread list
	threads := sess.threads()
	if len(threads) < 3 {
		t.Fatalf("expected >= 3 threads (main + outer + inner), got %#v", threads)
	}
	frames := sess.stackTrace(st.ThreadID)
	if len(frames) == 0 {
		t.Fatalf("no frames for inner worker thread %d", st.ThreadID)
	}
	vars := sess.variables(int(frames[0]["id"].(float64)))
	if vars["z"] != "42" {
		t.Fatalf("inner worker z should be 42, got %q", vars["z"])
	}
	// outer worker is a different thread from inner
	outerSeen := false
	for _, th := range threads {
		id := int(th["id"].(float64))
		if id >= 2 && id != st.ThreadID {
			outerSeen = true
		}
	}
	if !outerSeen {
		t.Fatalf("outer worker thread not found alongside inner in %#v", threads)
	}
	sess.continueAll()
	sess.waitTerminated()
	if !strings.Contains(sess.collectedOutput(), "42") {
		t.Fatalf("expected output 42 from nested workers, got %q", sess.collectedOutput())
	}
}

// TestStepInsideAsyncWorker stops at a breakpoint in a worker, steps over one line, and asserts the advance.
func TestStepInsideAsyncWorker(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "step.gb")
	if err := os.WriteFile(scriptPath, []byte(`import async;
let result = async.run(func(): int {
    let a = 1;
    let b = a + 1;
    let c = b + 1;
    return c;
});
let _ = await result;
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, 4) // "    let b = a + 1;"
	sess.configurationDone()

	st := sess.waitStopped()
	if st.ThreadID < 2 {
		t.Fatalf("expected worker thread (>= 2), got %d", st.ThreadID)
	}
	tid := st.ThreadID
	frames := sess.stackTrace(tid)
	if len(frames) == 0 {
		t.Fatal("no frames at initial breakpoint")
	}
	// a is in scope (line 3 already ran), b is not yet defined
	vars := sess.variables(int(frames[0]["id"].(float64)))
	if vars["a"] != "1" {
		t.Fatalf("expected a=1 at breakpoint, got %q", vars["a"])
	}

	// step over: should stop at line 5 ("    let c = b + 1;") in the SAME thread
	sess.next(tid)
	st2 := sess.waitStopped()
	if st2.ThreadID != tid {
		t.Fatalf("next changed thread from %d to %d", tid, st2.ThreadID)
	}
	frames2 := sess.stackTrace(tid)
	if len(frames2) == 0 {
		t.Fatal("no frames after next")
	}
	line2 := int(frames2[0]["line"].(float64))
	if line2 != 5 {
		t.Fatalf("after next, expected line 5, got %d", line2)
	}
	vars2 := sess.variables(int(frames2[0]["id"].(float64)))
	if vars2["b"] != "2" {
		t.Fatalf("after step, expected b=2 (a+1), got %q", vars2["b"])
	}

	// stepOut from the closure top level runs the closure to completion
	sess.stepOut(tid)
	sess.waitTerminated()
}

// TestGeneratorParkedOnContinueExitsCleanly verifies a generator worker exits cleanly when continued from a breakpoint.
func TestGeneratorParkedOnContinueExitsCleanly(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "genpark.gb")
	if err := os.WriteFile(scriptPath, []byte(`import io;
func seq(): generator<int> {
    let i = 0;
    while (i < 5) {
        yield i;
        i = i + 1;
    }
}
let total = 0;
for (v in seq()) {
    total = total + v;
}
io.println(total);
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	// line 5 is "        yield i;" inside the generator worker
	sess.setBreakpoints(scriptPath, 5)
	sess.configurationDone()

	st := sess.waitStopped()
	if st.ThreadID < 2 {
		t.Fatalf("expected generator worker thread (>= 2), got %d", st.ThreadID)
	}
	sess.continueAll()
	sess.continueUntilTerminated()
	if !strings.Contains(sess.collectedOutput(), "10") {
		t.Fatalf("expected output 10 (0+1+2+3+4), got %q", sess.collectedOutput())
	}
}

// TestStepNonFocusedWorkerRejected: sending next for a non-focused worker must error, not wedge the session.
func TestStepNonFocusedWorkerRejected(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "twowrk.gb")
	if err := os.WriteFile(scriptPath, []byte(twoWorkerBarrierScript), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, twoWorkerBreakpointLine)
	sess.configurationDone()

	// Ensure both workers are running before the first stop so one can be parked.
	if !sess.waitThreadStarted(5 * time.Second) {
		t.Fatal("worker A did not start")
	}
	if !sess.waitThreadStarted(5 * time.Second) {
		t.Fatal("worker B did not start")
	}

	first := sess.waitStopped()
	if first.ThreadID < 2 {
		t.Fatalf("expected a worker thread (>= 2) to stop first, got %d", first.ThreadID)
	}
	focusedID := first.ThreadID

	// Pick a live thread that is NOT the focused one.
	var otherID int
	for _, th := range sess.threads() {
		id := int(th["id"].(float64))
		if id != focusedID {
			otherID = id
			break
		}
	}
	if otherID == 0 {
		t.Skip("only one thread visible; cannot test non-focused step rejection")
	}

	// Stepping a non-focused worker must be rejected.
	resp := sess.nextResp(otherID)
	if success, _ := resp["success"].(bool); success {
		t.Errorf("next for non-focused worker %d returned success=true; expected error", otherID)
	}

	// Session must still make progress after continue (no wedge).
	sess.continueAll()
	sess.continueUntilTerminated()
}

// TestResumeAllNoStaleTokenWhenRunning: resumeAll must not buffer a resume token when the focused thread is already running (ft.stopped=false).
func TestResumeAllNoStaleTokenWhenRunning(t *testing.T) {
	s := &Server{
		threads:     map[int]*threadInfo{},
		focused:     1,
		stopped:     false, // thread is running; no active stop
		breakpoints: map[string]map[int]breakpointInfo{},
	}
	ti := newThreadInfo("main")
	s.threads[1] = ti

	s.resumeAll()

	select {
	case <-ti.resume:
		t.Fatal("resumeAll delivered a token to a non-stopped thread (stale-buffer bug)")
	default:
	}
}

func TestDuplicateResumeNoStaleTokenForParkedWorker(t *testing.T) {
	s := &Server{
		threads: map[int]*threadInfo{},
		focused: 1,
		stopped: true,
	}
	parked := newThreadInfo("parked")
	parked.parked = true
	s.threads[2] = parked

	s.resumeAll()
	<-parked.resume

	// The worker consumed its wake but has not yet returned from the receive.
	s.resumeAll()

	select {
	case <-parked.resume:
		t.Fatal("duplicate resume left a stale token for the parked worker")
	default:
	}
}

// TestDuplicateContinueNoStaleAutoResume: loop breakpoint fires twice; send two rapid continues; second stop must still arrive.
func TestDuplicateContinueNoStaleAutoResume(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "dup.gb")
	script := `import io;
let total = 0;
let i = 0;
while (i < 3) {
    total = total + i;
    i = i + 1;
}
io.println(total);
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, 5) // fires every iteration
	sess.configurationDone()

	sess.waitStopped()

	// two rapid sends; if the race fires between their processing the second delivers a stale token
	sess.send("continue", map[string]any{"threadId": 0})
	sess.send("continue", map[string]any{"threadId": 0})

	ev, ok := sess.waitStoppedTimeout(5 * time.Second)
	if !ok {
		t.Fatal("stop 1 was not received: stale continue token auto-resumed it")
	}
	_ = ev

	sess.continueAll()
	sess.continueUntilTerminated()
	if !strings.Contains(sess.collectedOutput(), "3") {
		t.Fatalf("expected output 3 (0+1+2), got %q", sess.collectedOutput())
	}
}

// TestNonFocusedThreadNotInspectable: allThreadsStopped:false; non-focused stackTrace returns zero frames.
func TestNonFocusedThreadNotInspectable(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "twowrk.gb")
	if err := os.WriteFile(scriptPath, []byte(twoWorkerBarrierScript), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, twoWorkerBreakpointLine)
	sess.configurationDone()

	if !sess.waitThreadStarted(5 * time.Second) {
		t.Fatal("worker A did not start")
	}
	if !sess.waitThreadStarted(5 * time.Second) {
		t.Fatal("worker B did not start")
	}

	first := sess.waitStopped()
	if first.ThreadID < 2 {
		t.Fatalf("expected a worker thread (>= 2) to stop, got %d", first.ThreadID)
	}
	// before fix: AllThreadsStopped == true; after fix: false
	if first.AllThreadsStopped {
		t.Errorf("stopped event has allThreadsStopped:true; want false")
	}

	threads := sess.threads()
	if len(threads) < 2 {
		t.Fatalf("expected >= 2 threads listed, got %v", threads)
	}

	focusedID := first.ThreadID
	var otherID int
	for _, th := range threads {
		id := int(th["id"].(float64))
		if id != focusedID && id >= 2 {
			otherID = id
			break
		}
	}
	if otherID == 0 {
		t.Skip("second worker not yet visible; cannot assert non-focused inspection")
	}

	// focused thread must return real frames
	focusedFrames := sess.stackTrace(focusedID)
	if len(focusedFrames) == 0 {
		t.Fatalf("focused thread %d returned no stack frames", focusedID)
	}

	// non-focused live worker must return zero frames (not stale ones)
	otherFrames := sess.stackTrace(otherID)
	if len(otherFrames) != 0 {
		t.Fatalf("non-focused thread %d returned %d frame(s); want 0", otherID, len(otherFrames))
	}

	sess.continueAll()
	sess.continueUntilTerminated()
}

// TestInitializeDoesNotAdvertiseSingleThread verifies the capability is not advertised.
func TestInitializeDoesNotAdvertiseSingleThread(t *testing.T) {
	clientR, clientW := io.Pipe()
	serverR, serverW := io.Pipe()
	t.Cleanup(func() {
		clientW.Close()
		serverR.Close()
	})

	go func() {
		defer serverW.Close()
		Serve(clientR, serverW)
	}()

	req := map[string]any{
		"seq": 1, "type": "request", "command": "initialize",
		"arguments": map[string]any{"clientName": "test"},
	}
	data, _ := json.Marshal(req)
	fmt.Fprintf(clientW, "Content-Length: %d\r\n\r\n%s", len(data), data)

	br := bufio.NewReader(serverR)
	for {
		msg, err := readTestFrame(br)
		if err != nil {
			t.Fatalf("reading response: %v", err)
		}
		if msgType, _ := msg["type"].(string); msgType != "response" {
			continue
		}
		if reqSeq, _ := msg["request_seq"].(float64); int(reqSeq) != 1 {
			continue
		}
		body, _ := msg["body"].(map[string]any)
		if body == nil {
			t.Fatal("initialize response has no body")
		}
		if v, _ := body["supportsSingleThreadExecutionRequests"].(bool); v {
			t.Error("initialize response advertises supportsSingleThreadExecutionRequests:true; want false")
		}
		return
	}
}

// TestPauseTargetsThreadWithPauseReason: pause arms only the target thread and stopped reason is "pause".
func TestPauseTargetsThreadWithPauseReason(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "pausetgt.gb")
	if err := os.WriteFile(scriptPath, []byte(`import async;
import time;
let r = async.run(func(): int {
    let x = 1;
    time.sleep(500);
    x = x + 1;
    return x;
});
let _ = await r;
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.configurationDone()

	workerID, ok := sess.waitThreadStartedID(5 * time.Second)
	if !ok {
		t.Fatal("worker did not start")
	}
	if workerID < 2 {
		t.Fatalf("expected worker threadId >= 2, got %d", workerID)
	}

	sess.request("pause", map[string]any{"threadId": workerID})

	ev, ok := sess.waitStoppedTimeout(5 * time.Second)
	if !ok {
		t.Fatal("no stopped event after pause request")
	}
	if ev.ThreadID != workerID {
		t.Errorf("stopped event threadId=%d; want %d", ev.ThreadID, workerID)
	}
	if ev.Reason != "pause" {
		t.Errorf("stopped event reason=%q; want \"pause\"", ev.Reason)
	}

	sess.continueAll()
	sess.waitTerminated()
}

func TestPauseRejectsUnknownThread(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "pauseunknown.gb")
	if err := os.WriteFile(scriptPath, []byte("let x = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()

	resp := sess.request("pause", map[string]any{"threadId": 999})
	if success, _ := resp["success"].(bool); success {
		t.Fatal("pause accepted an unknown thread id")
	}
}

// TestEvaluateOuterFrame: evaluate against an outer frame resolves that frame's locals, not the top frame's.
func TestEvaluateOuterFrame(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "outerframe.gb")
	if err := os.WriteFile(scriptPath, []byte(`import io;
func f(): void {
    let inner = 99;
    io.println("inside f");
}
func run(int outer): void {
    f();
    io.println(outer);
}
run(42);
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, 4) // io.println; inner already defined at this point
	sess.configurationDone()

	st := sess.waitStopped()
	if st.ThreadID != 1 {
		t.Fatalf("expected main thread (1), got %d", st.ThreadID)
	}
	frames := sess.stackTrace(st.ThreadID)
	if len(frames) < 3 {
		t.Fatalf("expected >= 3 frames (current + f + run), got %d: %#v", len(frames), frames)
	}

	topFrameID := int(frames[0]["id"].(float64))
	outerFrameID := int(frames[len(frames)-1]["id"].(float64))

	if got := sess.evaluate(st.ThreadID, topFrameID, "inner"); got != "99" {
		t.Fatalf("evaluate(\"inner\", topFrameId) = %q, want \"99\"", got)
	}
	// outer is run's parameter; f's closure does not capture run's scope.
	if got := sess.evaluate(st.ThreadID, outerFrameID, "outer"); got != "42" {
		t.Fatalf("evaluate(\"outer\", outerFrameId) = %q, want \"42\"", got)
	}
	resp := sess.request("setVariable", map[string]any{
		"variablesReference": outerFrameID,
		"name":               "outer",
		"value":              "77",
	})
	if success, _ := resp["success"].(bool); !success {
		t.Fatalf("setVariable outer frame failed: %v", resp)
	}
	if got := sess.evaluate(st.ThreadID, outerFrameID, "outer"); got != "77" {
		t.Fatalf("evaluate mutated outer = %q, want \"77\"", got)
	}

	sess.continueAll()
	sess.waitTerminated()
	if !strings.Contains(sess.collectedOutput(), "77") {
		t.Fatalf("outer-frame mutation was not observed: %q", sess.collectedOutput())
	}
}

func TestStaleFrameRejectedAfterNextStop(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "staleframe.gb")
	if err := os.WriteFile(scriptPath, []byte(`let i = 0;
while (i < 2) {
    i = i + 1;
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, 3)
	sess.configurationDone()

	first := sess.waitStopped()
	firstFrames := sess.stackTrace(first.ThreadID)
	staleID := int(firstFrames[0]["id"].(float64))
	sess.continueAll()

	second := sess.waitStopped()
	secondFrames := sess.stackTrace(second.ThreadID)
	currentID := int(secondFrames[0]["id"].(float64))
	if currentID == staleID {
		t.Fatalf("frame id reused across stops: %d", currentID)
	}
	resp := sess.request("evaluate", map[string]any{
		"expression": "i",
		"frameId":    staleID,
		"threadId":   second.ThreadID,
	})
	if success, _ := resp["success"].(bool); success {
		t.Fatal("evaluate accepted a stale frame id")
	}

	sess.continueAll()
	sess.waitTerminated()
}

// TestPauseThenContinueDoesNotMislabelBreakpoint: a pause armed then cleared by continue must not relabel a later real breakpoint stop.
func TestPauseThenContinueDoesNotMislabelBreakpoint(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "pausecont.gb")
	if err := os.WriteFile(scriptPath, []byte(`import async;
let r = async.run(func(): int {
    let a = 1;
    let b = 2;
    let c = 3;
    return a + b + c;
});
let _ = await r;
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	defer sess.close()
	sess.setBreakpoints(scriptPath, 3, 5) // bp1 "let a = 1;", bp2 "let c = 3;"
	sess.configurationDone()

	first := sess.waitStopped()
	if first.ThreadID < 2 {
		t.Fatalf("expected worker thread (>= 2) to stop, got %d", first.ThreadID)
	}
	if first.Reason != "breakpoint" {
		t.Fatalf("first stop reason = %q; want \"breakpoint\"", first.Reason)
	}
	workerID := first.ThreadID

	// Arm pause on the stopped worker, then continue: continue must clear the pending pause reason.
	sess.request("pause", map[string]any{"threadId": workerID})
	sess.continueAll()

	second := sess.waitStopped()
	if second.ThreadID != workerID {
		t.Fatalf("expected worker %d to stop at bp2, got %d", workerID, second.ThreadID)
	}
	if second.Reason != "breakpoint" {
		t.Fatalf("stopped reason = %q; want \"breakpoint\" (stale pause reason leaked)", second.Reason)
	}
	sess.continueAll()
	sess.waitTerminated()
}

// TestOneShotResumeIgnoresStalePreCommitToken: a token left on a thread's pre-commit resume channel cannot auto-resume the stop it later commits.
func TestOneShotResumeIgnoresStalePreCommitToken(t *testing.T) {
	path := "/tmp/oneshot.gb"
	s := &Server{
		breakpoints: map[string]map[int]breakpointInfo{path: {2: {}}},
		threads:     map[int]*threadInfo{},
		pauseCh:     make(chan evaluator.DebugPause, 1),
		done:        make(chan struct{}),
	}
	ti := newThreadInfo("worker")
	s.threads[2] = ti
	old := ti.resume
	old <- modeContinue // a stale token buffered on the pre-commit channel from an earlier wakeup

	finished := make(chan struct{})
	go func() {
		s.onPause(evaluator.DebugPause{
			Loc:      evaluator.DebugLocation{Path: path, Line: 2},
			Frames:   []evaluator.DebugFrame{{Name: "<top level>", Path: path, Line: 2}},
			Reason:   "breakpoint",
			ThreadID: 2,
		})
		close(finished)
	}()

	select {
	case <-s.pauseCh:
	case <-time.After(2 * time.Second):
		t.Fatal("onPause never committed a stop")
	}
	// The committed stop must block on its fresh channel, not drain the stale pre-commit token.
	select {
	case <-finished:
		t.Fatal("onPause auto-resumed from a stale pre-commit token (one-shot invariant broken)")
	case <-time.After(200 * time.Millisecond):
	}

	// A resume on the current channel releases the stop cleanly.
	s.resumeAll()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("resumeAll did not release the committed stop")
	}
}

// TestDisconnectRecoversWedgedEval: a disconnect must tear down the adapter even when the DAP loop is wedged on a non-terminating paused evaluate.
func TestDisconnectRecoversWedgedEval(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "wedge.gb")
	if err := os.WriteFile(scriptPath, []byte(`import io;
func spin(): int {
    let i = 0;
    while (true) {
        i = i + 1;
    }
    return i;
}
let x = 1;
let y = x + 1;
io.println(y);
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := newTestSession(t, scriptPath)
	sess.setBreakpoints(scriptPath, 10) // pause on "let y = x + 1;"
	sess.configurationDone()
	st := sess.waitStopped()

	// A non-terminating evaluate wedges the DAP loop on <-evalRep with only <-done as escape.
	sess.send("evaluate", map[string]any{"expression": "spin()", "threadId": st.ThreadID})

	// disconnect must recover the adapter; close() fails the test if Serve does not exit.
	sess.close()
}
