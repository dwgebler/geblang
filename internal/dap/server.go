package dap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	gevruntime "geblang/internal/runtime"

	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/semantic"
)

// stepMode controls what the evaluator pauses on next.
type stepMode int

const (
	modeContinue stepMode = iota // pause only at breakpoints
	modeNext                     // step over: pause at same or outer depth
	modeStepIn                   // step into: pause at every statement
	modeStepOut                  // step out: pause when stack depth decreases
)

// breakpointInfo stores per-breakpoint metadata.
type breakpointInfo struct {
	condition string // empty = unconditional
}

type frameRef struct {
	thread     int
	index      int
	generation int
}

// pausedCmd is a request sent to a paused thread's hook goroutine.
type pausedCmd struct {
	kind       string // "evaluate", "setVariable"
	expr       string
	frame      int
	frameIndex int
	name       string
	value      string
	thread     int
}

// pausedCmdResult is the response from the hook goroutine.
type pausedCmdResult struct {
	result string
	typ    string
	err    string
}

// threadInfo holds the per-thread debug state for one goroutine.
type threadInfo struct {
	name          string
	mode          stepMode
	stepDepth     int
	lastPause     *evaluator.DebugPause
	frameVars     map[int][]Variable
	frameIDs      []int
	resume        chan stepMode
	evalReq       chan pausedCmd
	evalRep       chan pausedCmdResult
	parked        bool
	stopped       bool
	stoppedGen    int    // matches Server.stopGen at the time of this thread's current stop
	inCondition   bool   // a conditional-breakpoint condition is evaluating on this thread; suppress re-entry
	pendingReason string // set by the pause handler to override the default step/breakpoint reason
}

// Server is a DAP debug adapter server.
type Server struct {
	r io.Reader
	w io.Writer

	mu  sync.Mutex
	seq int

	// breakpoints: absolute path -> line -> breakpoint info
	breakpoints map[string]map[int]breakpointInfo
	sourcePaths map[string]string // normalized runtime path -> client-facing path

	// debug session state
	scriptPath  string
	cwd         string
	scriptArgs  []string
	stopOnEntry bool

	pauseCh      chan evaluator.DebugPause // worker -> DAP loop: a thread paused
	terminatedCh chan error
	done         chan struct{} // closed on disconnect/terminate to release every goroutine

	threads     map[int]*threadInfo // id -> per-thread debug state (guarded by mu)
	stopped     bool                // stop-the-world: a thread is paused
	focused     int                 // thread id currently presented to the client
	stopGen     int                 // incremented each time a thread commits a stop; guards stale resume tokens
	nextFrameID int
	frameRefs   map[int]frameRef
	terminating bool // disconnect/terminate in progress

	lastException string
	exitCode      int
	terminated    bool
}

// ServeTCP advertises "IP:PORT\n" to portOut, accepts one connection, serves DAP over it.
func ServeTCP(portOut io.Writer, port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	port = ln.Addr().(*net.TCPAddr).Port
	fmt.Fprintf(portOut, "%s:%d\n", tcpAdvertiseIP(), port)
	conn, err := ln.Accept()
	ln.Close()
	if err != nil {
		return fmt.Errorf("accept: %w", err)
	}
	defer conn.Close()
	return Serve(conn, conn)
}

// tcpAdvertiseIP returns the first non-loopback IPv4 (reachable from a WSL2 host), else 127.0.0.1.
func tcpAdvertiseIP() string {
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip4 := ip.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return "127.0.0.1"
}

// Serve runs the DAP protocol loop on r/w until the session ends.
func Serve(r io.Reader, w io.Writer) error {
	s := &Server{
		r:            r,
		w:            w,
		breakpoints:  map[string]map[int]breakpointInfo{},
		sourcePaths:  map[string]string{},
		pauseCh:      make(chan evaluator.DebugPause),
		terminatedCh: make(chan error, 1),
		done:         make(chan struct{}),
		threads:      map[int]*threadInfo{},
		frameRefs:    map[int]frameRef{},
	}
	return s.run()
}

func (s *Server) run() error {
	requestCh := make(chan *Message, 4)
	defer s.beginTerminate() // release workers on any exit path

	go func() {
		br := bufio.NewReader(s.r)
		for {
			msg, err := readMessage(br)
			if err != nil {
				s.beginTerminate() // unblock a loop wedged on a paused eval so the session can tear down
				close(requestCh)
				return
			}
			if msg.Command == "disconnect" || msg.Command == "terminate" {
				s.beginTerminate() // tear down even if the loop is blocked on a paused eval
			}
			requestCh <- msg
		}
	}()

	// Drain stops on a dedicated goroutine so a worker's pause-send never blocks the eval handshake.
	go s.emitStopEvents()

	for {
		select {
		case req, ok := <-requestCh:
			if !ok {
				return nil
			}
			if err := s.handleRequest(req); err != nil {
				return err
			}
		case err := <-s.terminatedCh:
			if err != nil {
				s.sendEvent("output", OutputEventBody{Category: "stderr", Output: err.Error() + "\n"})
			}
			s.mu.Lock()
			if err != nil && s.exitCode == 0 {
				s.exitCode = 1
			}
			exitCode := s.exitCode
			s.terminated = true
			s.mu.Unlock()
			s.sendEvent("exited", ExitedEventBody{ExitCode: exitCode})
			s.sendEvent("terminated", nil)
			return nil
		}
	}
}

// emitStopEvents writes stopped events for paused workers; a ready receiver keeps onPause's pauseCh send from blocking the eval handshake.
func (s *Server) emitStopEvents() {
	for {
		select {
		case pause := <-s.pauseCh:
			s.mu.Lock()
			skip := s.terminating
			desc := ""
			if pause.Reason == "exception" {
				desc = s.lastException
			}
			s.mu.Unlock()
			if skip {
				continue // a worker pausing as the session ends must not emit a stopped after terminated
			}
			s.sendEvent("stopped", StoppedEventBody{
				Reason:            pause.Reason,
				Description:       desc,
				ThreadID:          pause.ThreadID,
				AllThreadsStopped: false,
				Text:              desc,
			})
		case <-s.done:
			return
		}
	}
}

func (s *Server) handleRequest(req *Message) error {
	switch req.Command {
	case "initialize":
		s.sendResponse(req, InitializeResponseBody{
			SupportsConfigurationDoneRequest:      true,
			SupportsSingleThreadExecutionRequests: true,
			SupportsEvaluateForHovers:             true,
			SupportsTerminateRequest:              true,
			SupportsSetVariable:                   true,
			SupportsConditionalBreakpoints:        true,
			SupportsExceptionInfoRequest:          true,
		})
		s.sendEvent("initialized", nil)

	case "setBreakpoints":
		var args SetBreakpointsArgs
		if err := remarshal(req.Arguments, &args); err != nil {
			s.sendErrorResponse(req, err.Error())
			return nil
		}
		path, err := normalizePath(args.Source.Path, args.Source.Name, s.cwd)
		if err != nil {
			s.sendErrorResponse(req, err.Error())
			return nil
		}
		s.mu.Lock()
		lines := map[int]breakpointInfo{}
		for _, bp := range args.Breakpoints {
			lines[bp.Line] = breakpointInfo{condition: bp.Condition}
		}
		s.breakpoints[path] = lines
		s.recordClientSourcePathLocked(path, args.Source.Path, args.Source.Name)
		s.mu.Unlock()
		result := make([]Breakpoint, len(args.Breakpoints))
		for i, bp := range args.Breakpoints {
			result[i] = Breakpoint{Verified: true, Line: bp.Line}
		}
		s.sendResponse(req, SetBreakpointsResponseBody{Breakpoints: result})

	case "configurationDone":
		s.sendResponse(req, nil)
		go s.runScript()

	case "launch":
		var args LaunchArgs
		if err := remarshal(req.Arguments, &args); err != nil {
			s.sendErrorResponse(req, err.Error())
			return nil
		}
		cwd, err := normalizeCwd(args.Cwd)
		if err != nil {
			s.sendErrorResponse(req, err.Error())
			return nil
		}
		s.cwd = cwd
		scriptPath, err := normalizePath(args.Program, "", cwd)
		if err != nil {
			s.sendErrorResponse(req, err.Error())
			return nil
		}
		s.scriptPath = scriptPath
		s.mu.Lock()
		s.recordClientSourcePathLocked(scriptPath, args.Program, "")
		s.mu.Unlock()
		s.scriptArgs = args.Args
		s.stopOnEntry = args.StopOnEntry
		s.sendResponse(req, nil)

	case "threads":
		s.mu.Lock()
		out := make([]Thread, 0, len(s.threads))
		for id, ti := range s.threads {
			out = append(out, Thread{ID: id, Name: ti.name})
		}
		s.mu.Unlock()
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		s.sendResponse(req, ThreadsResponseBody{Threads: out})

	case "stackTrace":
		var args StackTraceArgs
		_ = remarshal(req.Arguments, &args)
		s.mu.Lock()
		tid := args.ThreadID
		if tid == 0 {
			tid = s.focused
		}
		ti := s.threads[tid]
		var pause *evaluator.DebugPause
		if ti != nil && ti.stopped && tid == s.focused && ti.stoppedGen == s.stopGen {
			pause = ti.lastPause
			if pause != nil && len(ti.frameIDs) != len(pause.Frames) {
				if s.frameRefs == nil {
					s.frameRefs = map[int]frameRef{}
				}
				ti.frameIDs = make([]int, len(pause.Frames))
				for i := range pause.Frames {
					s.nextFrameID++
					ti.frameIDs[i] = s.nextFrameID
					s.frameRefs[s.nextFrameID] = frameRef{thread: tid, index: i, generation: s.stopGen}
				}
			}
		}
		frameIDs := append([]int(nil), tiFrameIDs(ti)...)
		s.mu.Unlock()
		if pause == nil {
			s.sendResponse(req, StackTraceResponseBody{})
			return nil
		}
		frames := make([]StackFrame, len(pause.Frames))
		frameVars := map[int][]Variable{}
		for i, f := range pause.Frames {
			id := frameIDs[i]
			sourcePath := s.clientSourcePath(f.Path)
			src := Source{Path: sourcePath, Name: sourceBase(sourcePath)}
			line := f.Line
			if i == 0 {
				line = pause.Loc.Line
			}
			frames[i] = StackFrame{ID: id, Name: f.Name, Source: src, Line: line, Column: 1}
			if i < len(pause.FrameVars) {
				frameVars[id] = dapVariables(pause.FrameVars[i])
			} else {
				frameVars[id] = []Variable{}
			}
		}
		s.mu.Lock()
		if ti = s.threads[tid]; ti != nil {
			ti.frameVars = frameVars
		}
		s.mu.Unlock()
		s.sendResponse(req, StackTraceResponseBody{StackFrames: frames, TotalFrames: len(frames)})

	case "scopes":
		var args ScopesArgs
		if err := remarshal(req.Arguments, &args); err != nil {
			s.sendErrorResponse(req, err.Error())
			return nil
		}
		s.mu.Lock()
		ref := args.FrameID
		_, inspectable := s.currentFrameLocked(ref)
		s.mu.Unlock()
		if !inspectable {
			s.sendErrorResponse(req, "invalid or stale frame id")
			return nil
		}
		s.sendResponse(req, ScopesResponseBody{
			Scopes: []Scope{{Name: "Locals", VariablesReference: ref, Expensive: false}},
		})
	case "variables":
		var args VariablesArgs
		if err := remarshal(req.Arguments, &args); err != nil {
			s.sendErrorResponse(req, err.Error())
			return nil
		}
		s.mu.Lock()
		frame, valid := s.currentFrameLocked(args.VariablesReference)
		var vars []Variable
		if valid {
			if fv, ok := s.threads[frame.thread].frameVars[args.VariablesReference]; ok {
				vars = fv
			}
		}
		s.mu.Unlock()
		if !valid {
			s.sendErrorResponse(req, "invalid or stale frame id")
			return nil
		}
		if vars == nil {
			vars = []Variable{}
		}
		s.sendResponse(req, VariablesResponseBody{Variables: vars})

	case "setVariable":
		var args SetVariableArgs
		if err := remarshal(req.Arguments, &args); err != nil {
			s.sendErrorResponse(req, err.Error())
			return nil
		}
		s.mu.Lock()
		frame, ok := s.currentFrameLocked(args.VariablesReference)
		ti := s.threads[frame.thread]
		s.mu.Unlock()
		if !ok {
			s.sendErrorResponse(req, "invalid or stale frame id")
			return nil
		}
		select {
		case ti.evalReq <- pausedCmd{
			kind: "setVariable", name: args.Name, value: args.Value,
			thread: frame.thread, frame: args.VariablesReference, frameIndex: frame.index,
		}:
		case <-s.done:
			s.sendErrorResponse(req, "session terminated")
			return nil
		}
		var rep pausedCmdResult
		select {
		case rep = <-ti.evalRep:
		case <-s.done:
			s.sendErrorResponse(req, "session terminated")
			return nil
		}
		if rep.err != "" {
			s.sendErrorResponse(req, rep.err)
			return nil
		}
		s.sendResponse(req, SetVariableResponseBody{Value: rep.result, Type: rep.typ})

	case "evaluate":
		var args EvaluateArgs
		if err := remarshal(req.Arguments, &args); err != nil {
			s.sendErrorResponse(req, err.Error())
			return nil
		}
		s.mu.Lock()
		tid := args.ThreadID
		frameIndex := 0
		ok := false
		if args.FrameID > 0 {
			if frame, valid := s.currentFrameLocked(args.FrameID); valid {
				tid = frame.thread
				frameIndex = frame.index
				ok = true
			}
		} else {
			if tid == 0 {
				tid = s.focused
			}
			ti := s.threads[tid]
			ok = ti != nil && ti.stopped && tid == s.focused && ti.stoppedGen == s.stopGen
		}
		ti := s.threads[tid]
		s.mu.Unlock()
		if !ok {
			s.sendErrorResponse(req, "invalid or stale frame id")
			return nil
		}
		select {
		case ti.evalReq <- pausedCmd{
			kind: "evaluate", expr: args.Expression, frame: args.FrameID,
			frameIndex: frameIndex, thread: tid,
		}:
		case <-s.done:
			s.sendResponse(req, EvaluateResponseBody{Result: "(terminated)", VariablesReference: 0})
			return nil
		}
		var rep pausedCmdResult
		select {
		case rep = <-ti.evalRep:
		case <-s.done:
			s.sendResponse(req, EvaluateResponseBody{Result: "(terminated)", VariablesReference: 0})
			return nil
		}
		if rep.err != "" {
			s.sendErrorResponse(req, rep.err)
			return nil
		}
		s.sendResponse(req, EvaluateResponseBody{Result: rep.result, Type: rep.typ, VariablesReference: 0})

	case "exceptionInfo":
		s.mu.Lock()
		exc := s.lastException
		s.mu.Unlock()
		if exc == "" {
			exc = "unknown exception"
		}
		s.sendResponse(req, ExceptionInfoResponseBody{
			ExceptionID: "exception",
			Description: exc,
			BreakMode:   "always",
		})

	case "continue":
		var a ContinueArgs
		_ = remarshal(req.Arguments, &a)
		if a.SingleThread {
			if !s.resumeThreadOnly(a.ThreadID) {
				s.sendErrorResponse(req, "can only single-thread continue the focused stopped thread")
				return nil
			}
			s.sendResponse(req, ContinueResponseBody{AllThreadsContinued: false})
			return nil
		}
		s.resumeAll()
		s.sendResponse(req, ContinueResponseBody{AllThreadsContinued: true})

	case "next":
		s.stepThread(req, modeNext)

	case "stepIn":
		s.stepThread(req, modeStepIn)

	case "stepOut":
		s.stepThread(req, modeStepOut)

	case "pause":
		var pauseArgs PauseArgs
		if err := remarshal(req.Arguments, &pauseArgs); err != nil {
			s.sendErrorResponse(req, err.Error())
			return nil
		}
		s.mu.Lock()
		ti := s.threads[pauseArgs.ThreadID]
		if ti == nil {
			s.mu.Unlock()
			s.sendErrorResponse(req, "unknown thread id")
			return nil
		}
		ti.mode = modeStepIn
		ti.pendingReason = "pause"
		s.mu.Unlock()
		s.sendResponse(req, nil)

	case "disconnect":
		s.sendResponse(req, nil)
		s.beginTerminate()
		return io.EOF

	case "terminate":
		s.sendResponse(req, nil)
		s.beginTerminate()
		return io.EOF

	default:
		s.sendErrorResponse(req, fmt.Sprintf("unknown command: %s", req.Command))
	}
	return nil
}

// beginTerminate releases every blocked or parked goroutine so ev.Eval returns.
func (s *Server) beginTerminate() {
	s.mu.Lock()
	if !s.terminating {
		s.terminating = true
		close(s.done)
	}
	s.mu.Unlock()
	s.resumeAll()
}

// resumeAll clears the stop and releases the focused thread plus every parked worker.
func (s *Server) resumeAll() {
	s.mu.Lock()
	s.stopped = false
	s.clearFrameRefsLocked()
	focused := s.focused
	var wake []chan stepMode
	for id, ti := range s.threads {
		ti.mode = modeContinue // clear step mode even for threads not yet in onPause
		ti.pendingReason = ""  // a pending pause must not survive past this resume onto a later stop
		if id == focused {
			continue
		}
		if ti.parked || ti.stopped {
			ti.stopped = false // mark not-paused now so a racing evaluate/setVariable does not send to a gone worker
			ti.parked = false
			wake = append(wake, ti.resume)
		}
	}
	ft := s.threads[focused]
	ftWasStopped := ft != nil && ft.stopped
	var ftResume chan stepMode
	if ft != nil {
		ft.stopped = false // close the continue-then-evaluate TOCTOU before the worker leaves onPause
		ftResume = ft.resume
	}
	s.mu.Unlock()
	if ftWasStopped {
		select {
		case ftResume <- modeContinue:
		default:
		}
	}
	for _, ch := range wake {
		select {
		case ch <- modeContinue:
		default:
		}
	}
}

// resumeThreadOnly releases just the focused stopped thread; the world stays stopped so parked workers remain parked.
func (s *Server) resumeThreadOnly(tid int) bool {
	s.mu.Lock()
	if tid == 0 {
		tid = s.focused
	}
	ti := s.threads[tid]
	if ti == nil || !ti.stopped || tid != s.focused || ti.stoppedGen != s.stopGen {
		s.mu.Unlock()
		return false
	}
	ti.mode = modeContinue
	ti.pendingReason = ""
	ti.stopped = false // close the continue-then-evaluate TOCTOU before the worker leaves onPause
	s.clearFrameRefsForThreadLocked(tid)
	ch := ti.resume // fresh one-shot channel from this stop; a stale token cannot reach it
	s.mu.Unlock()
	select {
	case ch <- modeContinue:
	default:
	}
	return true
}

// stepThread advances the focused thread while allowing dependencies in other threads to run.
func (s *Server) stepThread(req *Message, mode stepMode) {
	var a NextArgs
	_ = remarshal(req.Arguments, &a)
	tid := a.ThreadID
	s.mu.Lock()
	if tid == 0 {
		tid = s.focused
	}
	ti := s.threads[tid]
	if ti == nil || !ti.stopped || tid != s.focused || ti.stoppedGen != s.stopGen {
		s.mu.Unlock()
		s.sendErrorResponse(req, "can only step the focused stopped thread")
		return
	}
	depth := 0
	if ti.lastPause != nil {
		depth = len(ti.lastPause.Frames) - 1
	}
	ti.mode = mode
	ti.stepDepth = depth
	ti.stopped = false
	ti.pendingReason = "" // a pending pause must not survive a step onto this thread's next stop
	s.clearFrameRefsLocked()
	ch := ti.resume
	var wake []chan stepMode
	if !a.SingleThread {
		// Default step unfreezes the world so other threads can satisfy the stepped thread's dependencies.
		s.stopped = false
		for id, other := range s.threads {
			if id == tid {
				continue
			}
			other.mode = modeContinue
			other.pendingReason = ""
			if other.parked || other.stopped {
				other.parked = false
				other.stopped = false
				wake = append(wake, other.resume)
			}
		}
	}
	s.mu.Unlock()
	s.sendResponse(req, nil)
	for _, parked := range wake {
		select {
		case parked <- modeContinue:
		default:
		}
	}
	select {
	case ch <- mode:
	default:
	}
}

// registerThread adds a worker goroutine to the registry and announces it.
func (s *Server) registerThread(id int, name string) {
	s.mu.Lock()
	if _, ok := s.threads[id]; !ok {
		s.threads[id] = newThreadInfo(name)
	}
	s.mu.Unlock()
	s.sendEvent("thread", ThreadEventBody{Reason: "started", ThreadID: id})
}

// unregisterThread removes an exited worker; releases the world if it owned the stop.
func (s *Server) unregisterThread(id int) {
	s.mu.Lock()
	ti := s.threads[id]
	if ti != nil && ti.parked {
		ti.parked = false
	}
	delete(s.threads, id)
	var wake []chan stepMode
	if s.stopped && s.focused == id {
		s.stopped = false
		for _, other := range s.threads {
			if other.parked || other.stopped {
				other.parked = false
				other.stopped = false
				wake = append(wake, other.resume)
			}
		}
	}
	s.mu.Unlock()
	for _, ch := range wake {
		select {
		case ch <- modeContinue:
		default:
		}
	}
	s.sendEvent("thread", ThreadEventBody{Reason: "exited", ThreadID: id})
}

func newThreadInfo(name string) *threadInfo {
	return &threadInfo{
		name:    name,
		resume:  make(chan stepMode, 1),
		evalReq: make(chan pausedCmd),
		evalRep: make(chan pausedCmdResult),
	}
}

func tiFrameIDs(ti *threadInfo) []int {
	if ti == nil {
		return nil
	}
	return ti.frameIDs
}

func (s *Server) currentFrameLocked(id int) (frameRef, bool) {
	ref, ok := s.frameRefs[id]
	if !ok || ref.generation != s.stopGen || ref.thread != s.focused {
		return frameRef{}, false
	}
	ti := s.threads[ref.thread]
	if ti == nil || !ti.stopped || ti.stoppedGen != s.stopGen || ti.lastPause == nil {
		return frameRef{}, false
	}
	if ref.index < 0 || ref.index >= len(ti.lastPause.Frames) {
		return frameRef{}, false
	}
	return ref, true
}

func (s *Server) clearFrameRefsLocked() {
	s.frameRefs = map[int]frameRef{}
	for _, ti := range s.threads {
		ti.frameIDs = nil
		ti.frameVars = nil
	}
}

// clearFrameRefsForThreadLocked drops just one thread's frame refs, leaving other stopped threads inspectable.
func (s *Server) clearFrameRefsForThreadLocked(tid int) {
	for id, ref := range s.frameRefs {
		if ref.thread == tid {
			delete(s.frameRefs, id)
		}
	}
	if ti := s.threads[tid]; ti != nil {
		ti.frameIDs = nil
		ti.frameVars = nil
	}
}

// runScript starts the evaluator goroutine. Called after configurationDone.
func (s *Server) runScript() {
	src, err := os.ReadFile(s.scriptPath)
	if err != nil {
		s.terminatedCh <- fmt.Errorf("cannot read %s: %w", s.scriptPath, err)
		return
	}

	p := parser.New(lexer.New(string(src)))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		for _, e := range errs {
			s.sendEvent("output", OutputEventBody{Category: "stderr", Output: e + "\n"})
		}
		s.terminatedCh <- fmt.Errorf("parse errors in %s", s.scriptPath)
		return
	}

	// Pre-flight static checks so a real error aborts before executing partway.
	hasSemanticError := false
	for _, diag := range semantic.New().Analyze(program) {
		prefix := "error: "
		if diag.Severity == semantic.SeverityWarning {
			prefix = "warning: "
		} else {
			hasSemanticError = true
		}
		msg := prefix
		if diag.Line > 0 {
			msg += fmt.Sprintf("line %d:%d: ", diag.Line, diag.Column)
		}
		msg += diag.Message + "\n"
		s.sendEvent("output", OutputEventBody{Category: "stderr", Output: msg})
	}
	if hasSemanticError {
		s.terminatedCh <- fmt.Errorf("static analysis failed for %s", s.scriptPath)
		return
	}
	// Secondary static-check pass; parity gaps are not real errors and proceed.
	if _, compileErr := bytecode.Compile(program, src, s.scriptPath); compileErr != nil && !bytecode.IsParityError(compileErr) {
		s.sendEvent("output", OutputEventBody{Category: "stderr", Output: compileErr.Error() + "\n"})
		s.terminatedCh <- compileErr
		return
	}

	modulePaths := []string{}
	if s.cwd != "" {
		modulePaths = append(modulePaths, s.cwd)
	}
	ev := evaluator.NewWithArgsAndModulePaths(s.outputWriter(), s.scriptArgs, modulePaths)
	ev.SetDebugSourcePath(s.scriptPath)

	s.mu.Lock()
	s.threads[1] = newThreadInfo("main")
	s.focused = 1
	if s.stopOnEntry {
		s.threads[1].mode = modeStepIn
	}
	s.mu.Unlock()
	ev.SetDebugThreadHooks(s.registerThread, s.unregisterThread)
	ev.SetDebugHook(s.onPause)

	result, err := ev.Eval(program)
	if err != nil {
		s.mu.Lock()
		s.lastException = err.Error()
		s.mu.Unlock()
		s.terminatedCh <- err
		return
	}
	if result.ExitCode != 0 {
		s.sendEvent("output", OutputEventBody{
			Category: "stderr",
			Output:   fmt.Sprintf("process exited with code %d\n", result.ExitCode),
		})
	}
	s.mu.Lock()
	s.exitCode = result.ExitCode
	s.mu.Unlock()
	s.terminatedCh <- nil
}

// onPause runs per statement on the paused thread; it parks while another thread owns the stop, else freezes the world and blocks until resumed.
func (s *Server) onPause(pause evaluator.DebugPause) {
	tid := pause.ThreadID
	depth := len(pause.Frames) - 1
	for {
		s.mu.Lock()
		ti := s.threads[tid]
		if ti == nil || s.terminating {
			s.mu.Unlock()
			return
		}
		// A condition eval that re-enters the hook on this same thread must not re-pause.
		if ti.inCondition {
			s.mu.Unlock()
			return
		}
		// A nested hook from an in-frame evaluate on the focused thread must not re-pause.
		if ti.stopped && s.focused == tid {
			s.mu.Unlock()
			return
		}
		if s.stopped && s.focused != tid {
			ti.parked = true
			ti.resume = make(chan stepMode, 1)
			resume := ti.resume
			s.mu.Unlock()
			m := <-resume
			s.mu.Lock()
			ti.parked = false
			ti.mode = m
			s.mu.Unlock()
			continue // re-check this same statement now the world is running
		}
		mode, stepDepth := ti.mode, ti.stepDepth
		s.mu.Unlock()

		shouldPause, bpInfo := s.shouldPauseAt(pause.Loc, mode, stepDepth, depth)
		if !shouldPause {
			return
		}
		if bpInfo != nil && bpInfo.condition != "" && pause.Env != nil && pause.Eval != nil {
			s.mu.Lock()
			ti.inCondition = true
			s.mu.Unlock()
			pause.Eval.SuppressDebug(true) // a condition that spawns a worker must not fire debug hooks
			val, err := pause.Eval.EvalExpression(bpInfo.condition, pause.Env)
			pause.Eval.SuppressDebug(false)
			s.mu.Lock()
			ti.inCondition = false
			s.mu.Unlock()
			if err != nil || !isTruthy(val) {
				return
			}
		}

		reason := pause.Reason
		if reason == "step" {
			s.mu.Lock()
			_, atBp := s.breakpoints[pause.Loc.Path][pause.Loc.Line]
			s.mu.Unlock()
			if atBp {
				reason = "breakpoint"
			}
		}
		pause.Reason = reason

		s.mu.Lock()
		if s.stopped && s.focused != tid {
			// Another thread claimed the stop while the condition evaluated.
			ti.parked = true
			ti.resume = make(chan stepMode, 1)
			resume := ti.resume
			s.mu.Unlock()
			m := <-resume
			s.mu.Lock()
			ti.parked = false
			ti.mode = m
			s.mu.Unlock()
			continue
		}
		s.stopped = true
		s.focused = tid
		s.clearFrameRefsLocked()
		s.stopGen++
		ti.stopped = true
		ti.stoppedGen = s.stopGen
		ti.lastPause = &pause
		ti.resume = make(chan stepMode, 1) // one-shot: a stale token sent to a prior stop's channel cannot reach this stop
		resumeCh := ti.resume
		if ti.pendingReason != "" {
			pause.Reason = ti.pendingReason
			ti.pendingReason = ""
		}
		s.mu.Unlock()
		select {
		case s.pauseCh <- pause: // DAP loop emits the stopped event
		case <-s.done:
			return
		}

		for resumed := false; !resumed; {
			select {
			case mode := <-resumeCh:
				s.mu.Lock()
				ti.mode = mode
				ti.stepDepth = depth
				ti.stopped = false
				s.mu.Unlock()
				resumed = true
			case cmd := <-ti.evalReq:
				rep := s.handlePausedCmd(cmd, pause)
				select {
				case ti.evalRep <- rep:
				case <-s.done:
					return
				}
			case <-s.done:
				return
			}
		}
		return
	}
}

// frameEnv returns the environment for a validated frame index.
func frameEnv(frameIndex int, pause evaluator.DebugPause) *gevruntime.Environment {
	if frameIndex >= 0 && frameIndex < len(pause.FrameEnvs) && pause.FrameEnvs[frameIndex] != nil {
		return pause.FrameEnvs[frameIndex]
	}
	if frameIndex == 0 && len(pause.FrameEnvs) == 0 {
		return pause.Env
	}
	return nil
}

// handlePausedCmd runs evaluate/setVariable on the paused thread's own goroutine and evaluator.
func (s *Server) handlePausedCmd(cmd pausedCmd, pause evaluator.DebugPause) pausedCmdResult {
	env := frameEnv(cmd.frameIndex, pause)
	if env == nil || pause.Eval == nil {
		return pausedCmdResult{err: "no environment available"}
	}
	restore := pause.Eval.BeginDebugEvaluation(s.done)
	defer restore()
	switch cmd.kind {
	case "evaluate":
		val, err := pause.Eval.EvalExpression(cmd.expr, env)
		if err != nil {
			return pausedCmdResult{err: err.Error()}
		}
		return pausedCmdResult{result: val.Inspect(), typ: val.TypeName()}

	case "setVariable":
		val, err := pause.Eval.EvalExpression(cmd.value, env)
		if err != nil {
			return pausedCmdResult{err: fmt.Sprintf("invalid value: %s", err.Error())}
		}
		if err := env.Assign(cmd.name, val); err != nil {
			return pausedCmdResult{err: err.Error()}
		}
		newVars := rebuildVars(env)
		s.mu.Lock()
		if ti := s.threads[cmd.thread]; ti != nil {
			// Persist into the frame's snapshot so a later stackTrace refresh does not revert the value.
			if ti.lastPause != nil && cmd.frameIndex >= 0 && cmd.frameIndex < len(ti.lastPause.FrameVars) {
				ti.lastPause.FrameVars[cmd.frameIndex] = newVars
			}
			if ti.frameVars != nil {
				ti.frameVars[cmd.frame] = dapVariables(newVars)
			}
		}
		s.mu.Unlock()
		return pausedCmdResult{result: val.Inspect(), typ: val.TypeName()}
	}
	return pausedCmdResult{err: "unknown command"}
}

// rebuildVars re-snapshots variables from the environment after mutation.
func rebuildVars(env *gevruntime.Environment) []evaluator.DebugVariable {
	if env == nil {
		return nil
	}
	names := env.VisibleNames()
	vars := make([]evaluator.DebugVariable, 0, len(names))
	for _, name := range names {
		val, ok := env.Get(name)
		if !ok {
			continue
		}
		vars = append(vars, evaluator.DebugVariable{
			Name:  name,
			Value: val.Inspect(),
			Type:  val.TypeName(),
		})
	}
	return vars
}

// isTruthy mirrors the evaluator's truthiness rules for conditional breakpoints.
func isTruthy(v gevruntime.Value) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case gevruntime.Bool:
		return val.Value
	case gevruntime.Null:
		return false
	case gevruntime.Int:
		return val.Value != nil && val.Value.Sign() != 0
	case gevruntime.Float:
		return val.Value != 0
	case gevruntime.String:
		return val.Value != ""
	}
	return true
}

// shouldPauseAt reports whether to pause here; bpInfo is non-nil when at a breakpoint.
func (s *Server) shouldPauseAt(loc DebugLocation, mode stepMode, stepDepth, currentDepth int) (bool, *breakpointInfo) {
	s.mu.Lock()
	bps := s.breakpoints[loc.Path]
	s.mu.Unlock()
	if info, ok := bps[loc.Line]; ok {
		return true, &info
	}

	switch mode {
	case modeContinue:
		return false, nil
	case modeStepIn:
		return true, nil
	case modeNext:
		return currentDepth <= stepDepth, nil
	case modeStepOut:
		return currentDepth < stepDepth, nil
	}
	return false, nil
}

// outputWriter returns an io.Writer that sends output to the DAP client.
func (s *Server) outputWriter() io.Writer {
	return &dacOutput{s: s}
}

type dacOutput struct{ s *Server }

func (o *dacOutput) Write(p []byte) (int, error) {
	o.s.sendEvent("output", OutputEventBody{Category: "stdout", Output: string(p)})
	return len(p), nil
}

func dapVariables(vars []evaluator.DebugVariable) []Variable {
	out := make([]Variable, 0, len(vars))
	for _, v := range vars {
		out = append(out, Variable{
			Name:               v.Name,
			Value:              v.Value,
			Type:               v.Type,
			VariablesReference: 0,
		})
	}
	return out
}

// ---- protocol framing ----

func readMessage(r *bufio.Reader) (*Message, error) {
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
			v := strings.TrimPrefix(line, "Content-Length: ")
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length: %w", err)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s *Server) writeMessage(msg any) {
	data, _ := json.Marshal(msg)
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.w, "Content-Length: %d\r\n\r\n", len(data))
	s.w.Write(data)
}

func (s *Server) nextSeq() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return s.seq
}

func (s *Server) sendResponse(req *Message, body any) {
	s.writeMessage(map[string]any{
		"seq":         s.nextSeq(),
		"type":        "response",
		"request_seq": req.Seq,
		"command":     req.Command,
		"success":     true,
		"body":        body,
	})
}

func (s *Server) sendErrorResponse(req *Message, message string) {
	s.writeMessage(map[string]any{
		"seq":         s.nextSeq(),
		"type":        "response",
		"request_seq": req.Seq,
		"command":     req.Command,
		"success":     false,
		"message":     message,
	})
}

func (s *Server) sendEvent(event string, body any) {
	s.writeMessage(map[string]any{
		"seq":   s.nextSeq(),
		"type":  "event",
		"event": event,
		"body":  body,
	})
}

func (s *Server) recordClientSourcePathLocked(runtimePath, clientPath, fallbackName string) {
	if s.sourcePaths == nil {
		s.sourcePaths = map[string]string{}
	}
	displayPath := strings.TrimSpace(clientPath)
	if displayPath == "" {
		displayPath = strings.TrimSpace(fallbackName)
	}
	if displayPath == "" {
		displayPath = runtimePath
	}
	s.sourcePaths[runtimePath] = displayPath
}

func (s *Server) clientSourcePath(runtimePath string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sourcePath, ok := s.sourcePaths[runtimePath]; ok && sourcePath != "" {
		return sourcePath
	}
	return runtimePath
}

func sourceBase(path string) string {
	path = strings.TrimRight(path, `\/`)
	if path == "" {
		return ""
	}
	lastSlash := strings.LastIndex(path, "/")
	lastBackslash := strings.LastIndex(path, `\`)
	last := lastSlash
	if lastBackslash > last {
		last = lastBackslash
	}
	if last >= 0 && last+1 < len(path) {
		return path[last+1:]
	}
	return filepath.Base(path)
}

func remarshal(src, dst any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func normalizeCwd(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", nil
	}
	return normalizePath(cwd, "", "")
}

func normalizePath(path, fallbackName, cwd string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = strings.TrimSpace(fallbackName)
	}
	if path == "" {
		return "", fmt.Errorf("missing source path")
	}
	if runtime.GOOS != "windows" {
		if linuxPath, ok := wslUNCToLinuxPath(path); ok {
			path = linuxPath
		} else if linuxPath, ok := windowsDriveToWSLPath(path); ok {
			path = linuxPath
		}
	}
	if strings.HasPrefix(path, "file://") {
		trimmed := strings.TrimPrefix(path, "file://")
		if strings.HasPrefix(trimmed, "/") && looksLikeWindowsDrive(trimmed[1:]) {
			trimmed = trimmed[1:]
		}
		path = trimmed
	}
	path = filepath.FromSlash(path)
	if cwd != "" && !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func looksLikeWindowsDrive(path string) bool {
	return len(path) >= 2 && unicode.IsLetter(rune(path[0])) && path[1] == ':'
}

func wslUNCToLinuxPath(path string) (string, bool) {
	normalized := strings.ReplaceAll(path, "/", "\\")
	for _, prefix := range []string{`\\wsl.localhost\`, `\\wsl$\`, `\\wsl\`} {
		if !strings.HasPrefix(strings.ToLower(normalized), strings.ToLower(prefix)) {
			continue
		}
		rest := normalized[len(prefix):]
		parts := strings.SplitN(rest, `\`, 2)
		if len(parts) != 2 || parts[1] == "" {
			return "", false
		}
		return "/" + strings.ReplaceAll(parts[1], `\`, "/"), true
	}
	return "", false
}

func windowsDriveToWSLPath(path string) (string, bool) {
	if !looksLikeWindowsDrive(path) {
		return "", false
	}
	drive := unicode.ToLower(rune(path[0]))
	rest := path[2:]
	rest = strings.TrimLeft(rest, `\/`)
	rest = strings.ReplaceAll(rest, `\`, "/")
	if rest == "" {
		return "/mnt/" + string(drive), true
	}
	return "/mnt/" + string(drive) + "/" + rest, true
}

// DebugLocation aliases the evaluator type for internal use.
type DebugLocation = evaluator.DebugLocation
