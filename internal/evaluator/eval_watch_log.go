package evaluator

import (
	"encoding/json"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchWaitPollHook, when set, fires once per watchWait poll iteration (0-based, before the snapshot comparison) so a test can pin a mutation to a specific tick instead of racing an external sleep against the deadline.
var watchWaitPollHook func(iteration int)

func watchSnapshot(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	return watchSnapshotValue(path), nil
}

func watchWait(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects path, optional timeoutMs, optional intervalMs", call.Callee.String())
	}
	path, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s path must be string", call.Callee.String())
	}
	timeout := int64(30000)
	interval := int64(250)
	if len(args) >= 2 {
		n, ok := native.AsInt64(args[1])
		if !ok {
			return nil, fmt.Errorf("%s timeoutMs must be int", call.Callee.String())
		}
		timeout = n
	}
	if len(args) == 3 {
		n, ok := native.AsInt64(args[2])
		if !ok {
			return nil, fmt.Errorf("%s intervalMs must be int", call.Callee.String())
		}
		interval = n
	}
	if timeout < 0 || interval <= 0 {
		return nil, fmt.Errorf("%s timeoutMs must be >= 0 and intervalMs must be > 0", call.Callee.String())
	}
	before := watchSnapshotValue(path.Value)
	deadline := time.Now().Add(time.Duration(timeout) * time.Millisecond)
	for iteration := 0; ; iteration++ {
		if watchWaitPollHook != nil {
			watchWaitPollHook(iteration)
		}
		after := watchSnapshotValue(path.Value)
		if !watchSnapshotsEqual(before, after) {
			return watchResult(true, before, after), nil
		}
		if time.Now().After(deadline) || timeout == 0 {
			return watchResult(false, before, after), nil
		}
		time.Sleep(time.Duration(interval) * time.Millisecond)
	}
}

func watchSnapshotValue(path string) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "path", runtime.String{Value: path})
	info, err := os.Stat(path)
	if err != nil {
		putDict(entries, "exists", runtime.Bool{Value: false})
		putDict(entries, "size", runtime.NewInt64(0))
		putDict(entries, "mode", runtime.NewInt64(0))
		putDict(entries, "isDir", runtime.Bool{Value: false})
		putDict(entries, "modUnixNano", runtime.NewInt64(0))
		return runtime.Dict{Entries: entries}
	}
	putDict(entries, "exists", runtime.Bool{Value: true})
	putDict(entries, "size", runtime.NewInt64(info.Size()))
	putDict(entries, "mode", runtime.NewInt64(int64(info.Mode().Perm())))
	putDict(entries, "isDir", runtime.Bool{Value: info.IsDir()})
	putDict(entries, "modUnixNano", runtime.NewInt64(info.ModTime().UnixNano()))
	return runtime.Dict{Entries: entries}
}

func watchResult(changed bool, before runtime.Dict, after runtime.Dict) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "changed", runtime.Bool{Value: changed})
	putDict(entries, "before", before)
	putDict(entries, "after", after)
	return runtime.Dict{Entries: entries}
}

func watchSnapshotsEqual(left runtime.Dict, right runtime.Dict) bool {
	for _, key := range []string{"exists", "size", "mode", "isDir", "modUnixNano"} {
		leftValue, _ := dictField(left, key)
		rightValue, _ := dictField(right, key)
		if !valuesEqualSimple(leftValue, rightValue) {
			return false
		}
	}
	return true
}

// watchHandle owns an fsnotify watcher and the dispatch goroutine that
// invokes the user's callback on each event. The done channel is
// closed by stopWatchHandle to unblock the goroutine; the WaitGroup
// lets stop callers wait for in-flight callbacks to finish so reads
// from the parent goroutine happen-after the last callback write.
type watchHandle struct {
	watcher *fsnotify.Watcher
	done    chan struct{}
	wg      sync.WaitGroup
	stopped bool
}

// fsnotifyEventType maps fsnotify's bitmask to the protocol's
// "create" | "write" | "remove" | "rename" | "chmod" string.
func fsnotifyEventType(op fsnotify.Op) string {
	switch {
	case op.Has(fsnotify.Create):
		return "create"
	case op.Has(fsnotify.Write):
		return "write"
	case op.Has(fsnotify.Remove):
		return "remove"
	case op.Has(fsnotify.Rename):
		return "rename"
	case op.Has(fsnotify.Chmod):
		return "chmod"
	default:
		return "unknown"
	}
}

func (e *Evaluator) watchStart(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects path, callback, optional options", call.Callee.String())
	}
	path, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s path must be string", call.Callee.String())
	}
	callback, ok := args[1].(runtime.Function)
	if !ok {
		return nil, fmt.Errorf("%s callback must be a function", call.Callee.String())
	}
	recursive := false
	if len(args) == 3 {
		opts, ok := args[2].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s options must be a dict", call.Callee.String())
		}
		if value, found := dictField(opts, "recursive"); found {
			b, ok := value.(runtime.Bool)
			if !ok {
				return nil, fmt.Errorf("%s options.recursive must be bool", call.Callee.String())
			}
			recursive = b.Value
		}
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if recursive {
		if err := filepath.Walk(path.Value, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return watcher.Add(p)
			}
			return nil
		}); err != nil {
			_ = watcher.Close()
			return nil, err
		}
	} else {
		if err := watcher.Add(path.Value); err != nil {
			_ = watcher.Close()
			return nil, err
		}
	}
	handle := &watchHandle{watcher: watcher, done: make(chan struct{})}
	e.watchMu.Lock()
	e.nextWatchID++
	id := e.nextWatchID
	e.watches[id] = handle
	e.watchMu.Unlock()
	handle.wg.Add(1)
	go func() {
		defer handle.wg.Done()
		e.dispatchWatchEvents(handle, callback)
	}()
	return runtime.NewInt64(id), nil
}

// dispatchWatchEvents loops on the fsnotify channels until the handle
// is stopped, invoking the user's callback for each event. The
// callback runs in a child evaluator (for stack-frame isolation
// across goroutines) but the closure itself is NOT cloned, so
// mutations to captured globals propagate back to the parent. The
// `async.run` callback pattern uses the same approach.
func (e *Evaluator) dispatchWatchEvents(handle *watchHandle, callback runtime.Function) {
	child := e.childForCallback()
	defer child.Cleanup()
	defer child.startDebugThread("watch callback")()
	for {
		select {
		case event, ok := <-handle.watcher.Events:
			if !ok {
				return
			}
			eventDict := runtime.Dict{Entries: map[string]runtime.DictEntry{}}
			putDictV(eventDict, "path", runtime.String{Value: event.Name})
			putDictV(eventDict, "type", runtime.String{Value: fsnotifyEventType(event.Op)})
			_, _ = child.applyFunction(callback, []runtime.Value{eventDict})
		case _, ok := <-handle.watcher.Errors:
			if !ok {
				return
			}
		case <-handle.done:
			return
		}
	}
}

func (e *Evaluator) watchStop(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a watch handle", call.Callee.String())
	}
	id, err := rawInt64(args[0], "watch handle")
	if err != nil {
		return nil, err
	}
	e.watchMu.Lock()
	handle, ok := e.watches[id]
	if ok {
		delete(e.watches, id)
	}
	e.watchMu.Unlock()
	if !ok {
		if e.parent != nil {
			return e.parent.watchStop(call, args)
		}
		return runtime.Null{}, nil
	}
	return runtime.Null{}, stopWatchHandle(handle)
}

func stopWatchHandle(handle *watchHandle) error {
	if handle == nil || handle.stopped {
		return nil
	}
	handle.stopped = true
	close(handle.done)
	err := handle.watcher.Close()
	// Wait for the dispatch goroutine to finish its current callback
	// (and exit) before returning, so the caller's subsequent reads
	// from any callback-touched state happen after the last write.
	handle.wg.Wait()
	return err
}

func (e *Evaluator) logStdout(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	return e.registerLogger(&loggerHandle{target: "stdout", writer: e.stdout}), nil
}

func (e *Evaluator) logStderr(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	return e.registerLogger(&loggerHandle{target: "stderr", writer: e.stderr}), nil
}

func (e *Evaluator) logFile(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		return nil, err
	}
	return e.registerLogger(&loggerHandle{target: path, writer: file, closer: file}), nil
}

// logToStream returns a logger that writes JSON log lines to the
// given IOStream. Accepts either a raw IOStream native handle or
// an IOStream class instance (the wrapper from stdlib/streams.gb).
// The stream's lifetime is owned by the caller - closing the
// logger does NOT close the underlying stream, so the same stream
// can back multiple loggers or remain open for non-log traffic
// after the logger is discarded.
func (e *Evaluator) logToStream(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	streamValue := args[0]
	if inst, ok := streamValue.(*runtime.Instance); ok {
		// Unwrap a streams.IOStream class instance by reading its
		// `handle` field.
		if h, ok := inst.Fields["handle"]; ok {
			streamValue = h
		}
	}
	stream, err := e.ioStreamHandle(streamValue)
	if err != nil {
		return nil, err
	}
	if stream.writer == nil {
		return nil, fmt.Errorf("%s stream is not writable", call.Callee.String())
	}
	return e.registerLogger(&loggerHandle{target: stream.name, writer: stream.writer}), nil
}

func (e *Evaluator) logCustom(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	handler, ok := args[0].(*runtime.Instance)
	if !ok {
		return nil, fmt.Errorf("%s handler must be object", call.Callee.String())
	}
	if _, ok := lookupMethod(handler.Class, "handle"); !ok {
		return nil, fmt.Errorf("%s handler must implement handle(level, message, fields)", call.Callee.String())
	}
	return e.registerLogger(&loggerHandle{target: handler.Class.Name, handler: handler}), nil
}

func (e *Evaluator) logInfo(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	return e.logMessage(call, args, "info")
}

func (e *Evaluator) logWarn(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	return e.logMessage(call, args, "warn")
}

func (e *Evaluator) logError(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	return e.logMessage(call, args, "error")
}

func (e *Evaluator) logDebug(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	return e.logMessage(call, args, "debug")
}

func (e *Evaluator) logMessage(call *ast.CallExpression, args []runtime.Value, level string) (runtime.Value, error) {
	// Shorthand forms: `log.info("msg")` and `log.info("msg", {fields})`
	// route through a process-default stderr logger (created lazily). The
	// long form `log.info(logger, "msg" [, {fields}])` keeps existing
	// behaviour.
	if len(args) >= 1 {
		if _, isString := args[0].(runtime.String); isString {
			return e.logShorthand(call, args, level)
		}
	}
	if len(args) != 2 && len(args) != 3 {
		return nil, fmt.Errorf("%s expects logger, message, and optional fields", call.Callee.String())
	}
	logger, err := e.loggerHandle(args[0])
	if err != nil {
		return nil, err
	}
	message, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s message must be string", call.Callee.String())
	}
	fields := runtime.Dict{Entries: map[string]runtime.DictEntry{}}
	if len(args) == 3 {
		var ok bool
		fields, ok = args[2].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s fields must be dict", call.Callee.String())
		}
	}
	if logger.handler != nil {
		method, _ := lookupMethod(logger.handler.Class, "handle")
		_, err := e.applyFunctionWithThis(method, []runtime.Value{runtime.String{Value: level}, message, fields}, logger.handler)
		return runtime.Null{}, err
	}
	line, err := formatLogLine(level, message.Value, fields)
	if err != nil {
		return nil, err
	}
	if logger.leveled != nil {
		return runtime.Null{}, logger.leveled.WriteLevel(level, line)
	}
	_, err = io.WriteString(logger.writer, line+"\n")
	return runtime.Null{}, err
}

// logShorthand handles `log.<level>("msg")` and `log.<level>("msg",
// {fields})` by writing to a lazily-created default stderr logger.
func (e *Evaluator) logShorthand(call *ast.CallExpression, args []runtime.Value, level string) (runtime.Value, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("%s expects message and optional fields", call.Callee.String())
	}
	message, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s message must be string", call.Callee.String())
	}
	fields := runtime.Dict{Entries: map[string]runtime.DictEntry{}}
	if len(args) == 2 {
		var ok bool
		fields, ok = args[1].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s fields must be dict", call.Callee.String())
		}
	}
	line, err := formatLogLine(level, message.Value, fields)
	if err != nil {
		return nil, err
	}
	_, err = io.WriteString(e.stderr, line+"\n")
	return runtime.Null{}, err
}

func (e *Evaluator) logClose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	handle, err := logHandleID(args[0])
	if err != nil {
		return nil, err
	}
	e.logMu.Lock()
	logger, ok := e.loggers[handle]
	if ok {
		delete(e.loggers, handle)
	}
	e.logMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown logger handle %d", handle)
	}
	if logger.closer != nil {
		return runtime.Null{}, logger.closer.Close()
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) registerLogger(logger *loggerHandle) runtime.Value {
	e.logMu.Lock()
	defer e.logMu.Unlock()
	e.nextLogID++
	e.loggers[e.nextLogID] = logger
	return runtime.NewInt64(e.nextLogID)
}

func (e *Evaluator) loggerHandle(value runtime.Value) (*loggerHandle, error) {
	handle, err := logHandleID(value)
	if err != nil {
		return nil, err
	}
	e.logMu.Lock()
	logger, ok := e.loggers[handle]
	e.logMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.loggerHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown logger handle %d", handle)
	}
	return logger, nil
}

func logHandleID(value runtime.Value) (int64, error) {
	id, ok := value.(runtime.Int)
	if !ok || !id.Value.IsInt64() {
		return 0, fmt.Errorf("logger handle must be int")
	}
	return id.Value.Int64(), nil
}

func formatLogLine(level string, message string, fields runtime.Dict) (string, error) {
	entries := map[string]any{
		"level":   level,
		"message": message,
		"time":    time.Now().UTC().Format(time.RFC3339Nano),
	}
	fieldValues, err := valueToJSON(fields)
	if err != nil {
		return "", err
	}
	entries["fields"] = fieldValues
	data, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
