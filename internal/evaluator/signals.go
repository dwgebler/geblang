package evaluator

import (
	"errors"
	"fmt"
	"os"
	gosignal "os/signal"
	"strings"
	"sync"

	"geblang/internal/ast"
	"geblang/internal/runtime"
)

// signalSubscription owns one signal.Notify channel and the goroutine
// dispatching it to the registered Geblang handler.
type signalSubscription struct {
	ch    chan os.Signal
	done  chan struct{}
	once  sync.Once
	owner *Evaluator
}

func (s *signalSubscription) stop() {
	s.once.Do(func() {
		gosignal.Stop(s.ch)
		close(s.done)
	})
}

func canonicalSignalName(name string) string {
	return "SIG" + strings.ToUpper(strings.TrimPrefix(strings.ToUpper(name), "SIG"))
}

// sysOnSignal registers a handler for a named signal, replacing any
// previous handler for that signal. The handler runs isolated (the
// HTTP-callback model): share state through `store`, or call
// `sys.exit` to terminate the process.
func (e *Evaluator) sysOnSignal(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("sys.onSignal expects a signal name and a handler")
	}
	nameVal, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("sys.onSignal signal name must be a string")
	}
	handler, ok := args[1].(runtime.Function)
	if !ok {
		return nil, fmt.Errorf("sys.onSignal handler must be a function")
	}
	canonical := canonicalSignalName(nameVal.Value)
	if canonical == "SIGKILL" {
		return nil, fmt.Errorf("SIGKILL cannot be trapped")
	}
	sig, err := signalByName(canonical)
	if err != nil {
		return nil, err
	}

	sub := &signalSubscription{ch: make(chan os.Signal, 4), done: make(chan struct{}), owner: e}
	e.signalMu.Lock()
	if e.signalHandlers == nil {
		e.signalHandlers = map[string]*signalSubscription{}
	}
	if prev, exists := e.signalHandlers[canonical]; exists {
		prev.stop()
	}
	e.signalHandlers[canonical] = sub
	e.signalMu.Unlock()

	// Isolate the handler now, on the evaluator's goroutine: cloning at
	// delivery time would walk the captured environment while the main
	// goroutine is still mutating it.
	if handler.Native == nil {
		handler = runtime.CloneFunction(handler)
	}
	gosignal.Notify(sub.ch, sig)
	go e.dispatchSignals(canonical, handler, sub)
	return runtime.Null{}, nil
}

func (e *Evaluator) dispatchSignals(name string, handler runtime.Function, sub *signalSubscription) {
	for {
		select {
		case <-sub.done:
			return
		case _, ok := <-sub.ch:
			if !ok {
				return
			}
			e.invokeSignalHandler(name, handler)
		}
	}
}

func (e *Evaluator) invokeSignalHandler(name string, handler runtime.Function) {
	child := e.childForCallback()
	defer child.Cleanup()
	if child.debug != nil {
		defer child.startDebugThread("signal " + name)()
	}
	result, err := runtime.CallRecovering(func() (runtime.Value, error) {
		return child.applyFunction(handler, []runtime.Value{runtime.String{Value: name}})
	})
	if err != nil {
		// A VM-side handler surfaces sys.exit as an exit-coded error.
		var coder interface{ ExitCode() int }
		if errors.As(err, &coder) {
			_ = e.Cleanup()
			os.Exit(coder.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "geblang: %s handler: %v\n", name, err)
		return
	}
	if exit, ok := result.(exitValue); ok {
		_ = e.Cleanup()
		os.Exit(exit.code)
	}
}

// sysClearSignal restores default delivery for a named signal.
func (e *Evaluator) sysClearSignal(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	name, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	canonical := canonicalSignalName(name)
	e.signalMu.Lock()
	sub := e.signalHandlers[canonical]
	delete(e.signalHandlers, canonical)
	e.signalMu.Unlock()
	if sub != nil {
		sub.stop()
	}
	return runtime.Null{}, nil
}

// sysRaise sends a named signal to the current process.
func (e *Evaluator) sysRaise(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	name, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	sig, err := signalByName(name)
	if err != nil {
		return nil, err
	}
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		return nil, err
	}
	if err := proc.Signal(sig); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}
