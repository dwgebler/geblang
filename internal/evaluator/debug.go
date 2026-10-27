package evaluator

import (
	"errors"
	"sync/atomic"

	"geblang/internal/ast"
	"geblang/internal/runtime"
)

var ErrDebugEvaluationCancelled = errors.New("debug evaluation cancelled")

// DebugLocation is a source position reported to the debug hook.
type DebugLocation struct {
	Path string
	Line int
}

// DebugFrame is one entry in the call stack exposed to the debugger.
type DebugFrame struct {
	Name string
	Path string
	Line int
}

// DebugVariable is a name/value pair snapshotted at a pause point.
type DebugVariable struct {
	Name  string
	Value string
	Type  string
}

// DebugPause is the full state sent to the debugger when execution pauses.
type DebugPause struct {
	Loc       DebugLocation
	Frames    []DebugFrame
	Vars      []DebugVariable        // variables for the innermost (current) frame
	FrameVars [][]DebugVariable      // per-frame variables: FrameVars[i] corresponds to Frames[i]
	FrameEnvs []*runtime.Environment // per-frame environments: FrameEnvs[i] corresponds to Frames[i]
	Env       *runtime.Environment   // current environment (for evaluate/setVariable)
	Reason    string                 // "breakpoint", "step", "pause", "exception"
	ThreadID  int                    // goroutine thread id; main = 1
	Eval      *Evaluator             // evaluator that paused (for evaluate/condition in the right frame)
}

// DebugHookFunc is called before each statement; it may block to pause execution.
type DebugHookFunc func(pause DebugPause)

// debugState is shared by reference so a hook installed on the parent reaches worker goroutines.
type debugState struct {
	hook          DebugHookFunc
	sourcePath    string
	seq           atomic.Int64 // worker thread-id allocator; main thread is 1
	onThreadStart func(id int, name string)
	onThreadExit  func(id int)
}

// SetDebugHook installs a debug hook and marks this evaluator as main thread (id 1).
func (e *Evaluator) SetDebugHook(hook DebugHookFunc) {
	if e.debug == nil {
		e.debug = &debugState{}
	}
	e.debug.hook = hook
	e.threadID = 1
}

// SetDebugSourcePath sets the file path reported in debug locations.
func (e *Evaluator) SetDebugSourcePath(path string) {
	if e.debug == nil {
		e.debug = &debugState{}
	}
	e.debug.sourcePath = path
	e.debugPath = path
}

func (e *Evaluator) currentDebugPath() string {
	if n := len(e.callStack); n > 0 && e.callStack[n-1].path != "" {
		return e.callStack[n-1].path
	}
	return e.debugPath
}

// SuppressDebug toggles debug-hook suppression; nested calls nest.
func (e *Evaluator) SuppressDebug(on bool) {
	if on {
		e.debugSuppressed++
	} else if e.debugSuppressed > 0 {
		e.debugSuppressed--
	}
}

// BeginDebugEvaluation suppresses hooks and cancels evaluator work when done closes.
func (e *Evaluator) BeginDebugEvaluation(done <-chan struct{}) func() {
	previous := e.debugEvalCancel
	e.debugEvalCancel = done
	e.SuppressDebug(true)
	return func() {
		e.SuppressDebug(false)
		e.debugEvalCancel = previous
	}
}

func (e *Evaluator) debugEvaluationCancelled() bool {
	if e.debugEvalCancel == nil {
		return false
	}
	select {
	case <-e.debugEvalCancel:
		return true
	default:
		return false
	}
}

// SetDebugThreadHooks installs lifecycle callbacks fired when a worker goroutine starts/exits.
func (e *Evaluator) SetDebugThreadHooks(onStart func(id int, name string), onExit func(int)) {
	if e.debug == nil {
		e.debug = &debugState{}
	}
	e.debug.onThreadStart = onStart
	e.debug.onThreadExit = onExit
}

// startDebugThread assigns a fresh thread id and fires the start/exit lifecycle hooks.
func (e *Evaluator) startDebugThread(name string) func() {
	if e.debug == nil || e.debugSuppressed > 0 {
		return func() {}
	}
	id := int(e.debug.seq.Add(1)) + 1 // main thread is 1; workers start at 2
	e.threadID = id
	if e.debug.onThreadStart != nil {
		e.debug.onThreadStart(id, name)
	}
	return func() {
		if e.debug != nil && e.debug.onThreadExit != nil {
			e.debug.onThreadExit(id)
		}
	}
}

// callDebugHook is called from evalStatements for each statement.
func (e *Evaluator) callDebugHook(stmt ast.Statement, env *runtime.Environment, reason string) {
	if e.debug == nil || e.debug.hook == nil || e.debugSuppressed > 0 {
		return
	}
	line := statementLine(stmt)
	if line == 0 {
		return
	}
	path := e.currentDebugPath()
	if path == "" {
		path = e.debug.sourcePath
	}
	frames, frameVars, frameEnvs := e.snapshotFrames(path, line, env)
	pause := DebugPause{
		Loc:       DebugLocation{Path: path, Line: line},
		Frames:    frames,
		Vars:      snapshotEnv(env),
		FrameVars: frameVars,
		FrameEnvs: frameEnvs,
		Env:       env,
		Reason:    reason,
		ThreadID:  e.threadID,
		Eval:      e,
	}
	e.debug.hook(pause)
}

// snapshotFrames builds a call frame list, per-frame variable snapshots, and per-frame envs.
func (e *Evaluator) snapshotFrames(currentPath string, currentLine int, currentEnv *runtime.Environment) ([]DebugFrame, [][]DebugVariable, []*runtime.Environment) {
	frames := make([]DebugFrame, 0, len(e.callStack)+1)
	frameVars := make([][]DebugVariable, 0, len(e.callStack)+1)
	frameEnvs := make([]*runtime.Environment, 0, len(e.callStack)+1)

	// Current position as the innermost frame
	frames = append(frames, DebugFrame{Name: "<top level>", Path: currentPath, Line: currentLine})
	frameVars = append(frameVars, snapshotEnv(currentEnv))
	frameEnvs = append(frameEnvs, currentEnv)

	for i := len(e.callStack) - 1; i >= 0; i-- {
		f := e.callStack[i]
		name := f.name
		if name == "" {
			name = "<closure>"
		}
		path := f.path
		if path == "" {
			path = currentPath
		}
		frames = append(frames, DebugFrame{Name: name, Path: path, Line: f.line})
		frameVars = append(frameVars, snapshotEnv(f.env))
		frameEnvs = append(frameEnvs, f.env)
	}
	return frames, frameVars, frameEnvs
}

// snapshotEnv captures all variables in the current (innermost) scope.
func snapshotEnv(env *runtime.Environment) []DebugVariable {
	if env == nil {
		return nil
	}
	names := env.VisibleNames()
	vars := make([]DebugVariable, 0, len(names))
	for _, name := range names {
		val, ok := env.Get(name)
		if !ok {
			continue
		}
		vars = append(vars, DebugVariable{
			Name:  name,
			Value: val.Inspect(),
			Type:  val.TypeName(),
		})
	}
	return vars
}

func statementLine(stmt ast.Statement) int {
	switch s := stmt.(type) {
	case *ast.ModuleStatement:
		return s.Token.Line
	case *ast.ImportStatement:
		return s.Token.Line
	case *ast.FromImportStatement:
		return s.Token.Line
	case *ast.ExportStatement:
		return s.Token.Line
	case *ast.InitStatement:
		return s.Token.Line
	case *ast.TypeAliasStatement:
		return s.Token.Line
	case *ast.DeclarationStatement:
		return s.Token.Line
	case *ast.DestructuringStatement:
		return s.Token.Line
	case *ast.FunctionStatement:
		return s.Token.Line
	case *ast.ClassStatement:
		return s.Token.Line
	case *ast.InterfaceStatement:
		return s.Token.Line
	case *ast.EnumStatement:
		return s.Token.Line
	case *ast.ExpressionStatement:
		return s.Token.Line
	case *ast.ReturnStatement:
		return s.Token.Line
	case *ast.YieldStatement:
		return s.Token.Line
	case *ast.SimpleStatement:
		return s.Token.Line
	case *ast.IfStatement:
		return s.Token.Line
	case *ast.WhileStatement:
		return s.Token.Line
	case *ast.ForStatement:
		return s.Token.Line
	case *ast.TryStatement:
		return s.Token.Line
	case *ast.MatchStatement:
		return s.Token.Line
	case *ast.SelectStatement:
		return s.Token.Line
	case *ast.WithStatement:
		return s.Token.Line
	case *ast.DelStatement:
		return s.Token.Line
	case *ast.BlockStatement:
		return s.Token.Line
	}
	return 0
}
