package evaluator

import (
	"errors"
	"io"
	"sort"

	"geblang/internal/ast"
	"geblang/internal/runtime"
)

type Session struct {
	evaluator *Evaluator
	env       *runtime.Environment
	stdout    io.Writer
	args      []string
	paths     []string
}

type SessionResult struct {
	Value    runtime.Value
	HasValue bool
	IsVoid   bool // true when result is null from a void function call (suppress REPL output)
	ExitCode int
	Exited   bool
}

type ExtensionInfo struct {
	ID        int64
	Name      string
	Managed   bool
	Functions []string
}

func NewSession(stdout io.Writer, args []string, modulePaths []string) (*Session, error) {
	e := NewWithArgsAndModulePaths(stdout, args, modulePaths)
	env := runtime.NewEnvironment()
	if err := e.installBuiltinTypes(env); err != nil {
		return nil, err
	}
	return &Session{evaluator: e, env: env, stdout: stdout, args: append([]string(nil), args...), paths: append([]string(nil), modulePaths...)}, nil
}

// isCallLikeExpression reports whether expr is a function or method call at the top level.
// Used to distinguish void call results (suppress REPL output) from explicit null values.
func isCallLikeExpression(expr ast.Expression) bool {
	_, ok := expr.(*ast.CallExpression)
	return ok
}

func sessionResult(value runtime.Value, expr ast.Expression) SessionResult {
	_, isNull := value.(runtime.Null)
	isVoid := isNull && isCallLikeExpression(expr)
	return SessionResult{Value: value, HasValue: true, IsVoid: isVoid}
}

func (s *Session) Eval(program *ast.Program) (SessionResult, error) {
	if len(program.Statements) == 1 {
		if stmt, ok := program.Statements[0].(*ast.ExpressionStatement); ok && stmt.Expression != nil {
			value, err := s.evaluator.evalExpression(stmt.Expression, s.env)
			if err != nil {
				var thrown thrownError
				if errors.As(err, &thrown) {
					return SessionResult{}, uncaughtFromThrown(thrown.value)
				}
				return SessionResult{}, sessionUncaught(err)
			}
			if exit, ok := value.(exitValue); ok {
				return SessionResult{ExitCode: exit.code, Exited: true}, nil
			}
			return sessionResult(value, stmt.Expression), nil
		}
	}
	if len(program.Statements) > 1 {
		if stmt, ok := program.Statements[len(program.Statements)-1].(*ast.ExpressionStatement); ok && stmt.Expression != nil {
			s.evaluator.pushDeferFrame()
			sig, err := s.evaluator.evalStatements(program.Statements[:len(program.Statements)-1], s.env)
			if err != nil {
				_, _ = s.evaluator.runAndPopDefers(signal{})
				return SessionResult{}, uncaughtFromError(err)
			}
			if sig.kind != "" || sig.exited {
				sig, err = s.evaluator.runAndPopDefers(sig)
				if err != nil {
					return SessionResult{}, uncaughtFromError(err)
				}
				if sig.exited {
					return SessionResult{ExitCode: sig.exitCode, Exited: true}, nil
				}
				if sig.kind == "throw" && sig.thrown != nil {
					return SessionResult{}, uncaughtFromThrown(*sig.thrown)
				}
				return SessionResult{}, nil
			}
			value, err := s.evaluator.evalExpression(stmt.Expression, s.env)
			if err != nil {
				_, _ = s.evaluator.runAndPopDefers(signal{})
				var thrown thrownError
				if errors.As(err, &thrown) {
					return SessionResult{}, uncaughtFromThrown(thrown.value)
				}
				return SessionResult{}, sessionUncaught(err)
			}
			sig, err = s.evaluator.runAndPopDefers(signal{})
			if err != nil {
				return SessionResult{}, uncaughtFromError(err)
			}
			if sig.exited {
				return SessionResult{ExitCode: sig.exitCode, Exited: true}, nil
			}
			if sig.kind == "throw" && sig.thrown != nil {
				return SessionResult{}, uncaughtFromThrown(*sig.thrown)
			}
			if exit, ok := value.(exitValue); ok {
				return SessionResult{ExitCode: exit.code, Exited: true}, nil
			}
			return sessionResult(value, stmt.Expression), nil
		}
	}
	s.evaluator.pushDeferFrame()
	sig, err := s.evaluator.evalStatements(program.Statements, s.env)
	if err != nil {
		_, _ = s.evaluator.runAndPopDefers(signal{})
		return SessionResult{}, uncaughtFromError(err)
	}
	sig, err = s.evaluator.runAndPopDefers(sig)
	if err != nil {
		return SessionResult{}, uncaughtFromError(err)
	}
	if sig.exited {
		return SessionResult{ExitCode: sig.exitCode, Exited: true}, nil
	}
	if sig.kind == "throw" && sig.thrown != nil {
		return SessionResult{}, uncaughtFromThrown(*sig.thrown)
	}
	return SessionResult{}, nil
}

func (s *Session) Reset() error {
	next, err := NewSession(s.stdout, s.args, s.paths)
	if err != nil {
		return err
	}
	_ = s.Close()
	*s = *next
	return nil
}

func (s *Session) Names() []string {
	return s.env.Names()
}

// TypeBindings returns each session-scope binding paired with its
// declared (or inferred) type name. The REPL uses this to re-seed a
// fresh analyzer with prior-prompt bindings so identifier references
// in the new prompt resolve. Names without a recorded type fall back
// to "any".
func (s *Session) TypeBindings() map[string]string {
	out := map[string]string{}
	for _, name := range s.env.Names() {
		t, ok := s.env.GetTypeBinding(name)
		if !ok || t == "" {
			t = "any"
		}
		out[name] = t
	}
	return out
}

func (s *Session) Imports() []string {
	names := make([]string, 0, len(s.evaluator.importNames))
	for alias, canonical := range s.evaluator.importNames {
		if alias == canonical {
			names = append(names, alias)
		} else {
			names = append(names, alias+"="+canonical)
		}
	}
	sort.Strings(names)
	return names
}

func (s *Session) StdlibModules() []string {
	names := make([]string, 0, len(s.evaluator.builtins))
	for name := range s.evaluator.builtins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *Session) LoadedExtensions() []ExtensionInfo {
	s.evaluator.extMu.Lock()
	defer s.evaluator.extMu.Unlock()
	extensions := make([]ExtensionInfo, 0, len(s.evaluator.extConns))
	for id, handle := range s.evaluator.extConns {
		handle.mu.Lock()
		functions := make([]string, 0, len(handle.functions))
		for name := range handle.functions {
			functions = append(functions, name)
		}
		sort.Strings(functions)
		extensions = append(extensions, ExtensionInfo{
			ID:        id,
			Name:      handle.name,
			Managed:   handle.managed,
			Functions: functions,
		})
		handle.mu.Unlock()
	}
	sort.Slice(extensions, func(i, j int) bool {
		return extensions[i].ID < extensions[j].ID
	})
	return extensions
}

func (s *Session) MemberNames(name string) []string {
	if names, ok := s.evaluator.dirImportedModule(name); ok {
		return names
	}
	value, ok := s.env.Get(name)
	if !ok {
		return nil
	}
	return dirValue(value)
}

func (s *Session) StdlibMemberNames(name string) []string {
	canonical := name
	if imported, ok := s.evaluator.importNames[name]; ok {
		canonical = imported
	}
	functions, ok := s.evaluator.builtins[canonical]
	if !ok {
		return nil
	}
	names := make([]string, 0, len(functions))
	for member := range functions {
		names = append(names, member)
	}
	names = append(names, s.evaluator.builtinModuleTypeExportNames(canonical)...)
	sort.Strings(names)
	return names
}

func (s *Session) Close() error {
	return s.evaluator.Cleanup()
}

// sessionUncaught keeps the class of a classified runtime fault (TypeError, ValueError, ...).
func sessionUncaught(err error) *runtime.UncaughtError {
	failure := runtime.NewRecoverableError(err)
	return &runtime.UncaughtError{Class: failure.Class, Message: failure.Message}
}
