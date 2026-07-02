package evaluator

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/runtime"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type memoryStream struct {
	mu   sync.Mutex
	data []byte
	pos  int
}

func (m *memoryStream) Read(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pos >= len(m.data) {
		return 0, io.EOF
	}
	n := copy(p, m.data[m.pos:])
	m.pos += n
	return n, nil
}

func (m *memoryStream) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = append(m.data, p...)
	return len(p), nil
}

func (m *memoryStream) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return string(m.data)
}

func (m *memoryStream) Bytes() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.data...)
}

func (m *memoryStream) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = nil
	m.pos = 0
}

func closeIOStreamHandle(handle *ioStreamHandle) error {
	if handle == nil || handle.closed {
		return nil
	}
	handle.closed = true
	handle.bufReader = nil
	if handle.restore != nil {
		handle.restore()
		handle.restore = nil
	}
	if handle.closer != nil {
		if err := handle.closer.Close(); err != nil &&
			!errors.Is(err, os.ErrClosed) &&
			!errors.Is(err, net.ErrClosed) &&
			!strings.Contains(err.Error(), "use of closed network connection") {
			return err
		}
	}
	return nil
}

// isIOStreamKind reports whether a NativeObject is one of the stream
// kinds io.* helpers operate on. Centralises the previously-repeated
// `kind == "IOStream" || kind == "IOCapture"` check so a typo in any
// one site can't silently skip the branch.
func isIOStreamKind(value runtime.Value) (runtime.NativeObject, bool) {
	obj, ok := value.(runtime.NativeObject)
	if !ok {
		return runtime.NativeObject{}, false
	}
	if obj.Kind != "IOStream" && obj.Kind != "IOCapture" {
		return runtime.NativeObject{}, false
	}
	return obj, true
}

func pathJoin(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		value, ok := arg.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s arguments must be strings", call.Callee.String())
		}
		parts = append(parts, value.Value)
	}
	return runtime.String{Value: filepath.Join(parts...)}, nil
}

func pathClean(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	value, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: filepath.Clean(value)}, nil
}

func pathBase(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	value, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: filepath.Base(value)}, nil
}

func pathDir(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	value, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: filepath.Dir(value)}, nil
}

func pathExt(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	value, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: filepath.Ext(value)}, nil
}

func pathAbs(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	value, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: abs}, nil
}

// pathReal resolves symlinks and returns the canonical absolute path, so callers can root-jail a served file against its intended directory.
func pathReal(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	value, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: resolved}, nil
}

func pathRel(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	base, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s base must be string", call.Callee.String())
	}
	target, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s target must be string", call.Callee.String())
	}
	rel, err := filepath.Rel(base.Value, target.Value)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: rel}, nil
}

func pathGlob(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	pattern, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	matches, err := globRecursive(pattern)
	if err != nil {
		return nil, err
	}
	values := make([]runtime.Value, len(matches))
	for i, m := range matches {
		values[i] = runtime.String{Value: m}
	}
	return &runtime.List{Elements: values}, nil
}

// globRecursive extends filepath.Glob with Python-style `**` to match
// zero or more path segments. Paths containing `**` are split into
// prefix / suffix; each candidate under the prefix that satisfies the
// reduced pattern is included.
func globRecursive(pattern string) ([]string, error) {
	if !strings.Contains(pattern, "**") {
		return filepath.Glob(pattern)
	}
	// Find the longest static prefix (no glob meta) up to the first **.
	idx := strings.Index(pattern, "**")
	prefix := pattern[:idx]
	suffix := strings.TrimPrefix(pattern[idx+2:], "/")
	root := prefix
	if root == "" {
		root = "."
	} else if strings.HasSuffix(root, "/") {
		root = root[:len(root)-1]
	} else {
		// pattern like "a**b" - anchor the walk at the parent dir of
		// `prefix` and treat the rest as a per-name suffix.
		parent := filepath.Dir(root)
		if parent == "" {
			parent = "."
		}
		root = parent
	}
	var matches []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel := p
		if prefix != "" {
			if !strings.HasPrefix(p, strings.TrimSuffix(prefix, "/")) {
				return nil
			}
			rel = strings.TrimPrefix(p, strings.TrimSuffix(prefix, "/"))
			rel = strings.TrimPrefix(rel, "/")
		}
		if suffix == "" {
			matches = append(matches, p)
			return nil
		}
		ok, err := filepath.Match(suffix, filepath.Base(rel))
		if err != nil {
			return err
		}
		if ok {
			matches = append(matches, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

func ioReadText(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: string(data)}, nil
}

func ioWriteText(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	path, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s path must be string", call.Callee.String())
	}
	content, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s content must be string", call.Callee.String())
	}
	if err := os.WriteFile(path.Value, []byte(content.Value), 0o666); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func ioAppendText(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	path, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s path must be string", call.Callee.String())
	}
	content, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s content must be string", call.Callee.String())
	}
	file, err := os.OpenFile(path.Value, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := io.WriteString(file, content.Value); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) ioReadBytes(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) == 1 {
		path, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s path must be string", call.Callee.String())
		}
		data, err := os.ReadFile(path.Value)
		if err != nil {
			return nil, err
		}
		return runtime.Bytes{Value: data}, nil
	}
	if len(args) == 2 {
		n, ok := toInt64(args[1])
		if !ok {
			return nil, fmt.Errorf("%s byte count must be int", call.Callee.String())
		}
		if n < 0 || n > 1<<30 {
			return nil, fmt.Errorf("%s byte count out of range", call.Callee.String())
		}
		reader, err := e.ioReader(args[0])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, n)
		read, err := reader.Read(buf)
		if err != nil && err != io.EOF {
			return nil, err
		}
		return runtime.Bytes{Value: buf[:read]}, nil
	}
	return nil, fmt.Errorf("%s expects path or file and byte count", call.Callee.String())
}

func (e *Evaluator) ioWriteBytes(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	data, ok := args[1].(runtime.Bytes)
	if !ok {
		return nil, fmt.Errorf("%s content must be bytes", call.Callee.String())
	}
	if path, ok := args[0].(runtime.String); ok {
		if err := os.WriteFile(path.Value, data.Value, 0o666); err != nil {
			return nil, err
		}
		return runtime.Null{}, nil
	}
	writer, err := e.ioWriter(args[0])
	if err != nil {
		return nil, err
	}
	written, err := writer.Write(data.Value)
	if err != nil {
		return nil, err
	}
	return runtime.NewInt64(int64(written)), nil
}

func ioAppendBytes(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	path, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s path must be string", call.Callee.String())
	}
	data, ok := args[1].(runtime.Bytes)
	if !ok {
		return nil, fmt.Errorf("%s content must be bytes", call.Callee.String())
	}
	file, err := os.OpenFile(path.Value, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Write(data.Value); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func ioExists(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return runtime.Bool{Value: true}, nil
	}
	// ENOTDIR (a path component is a regular file) means "does not
	// exist" for a predicate, same as ENOENT.
	if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) {
		return runtime.Bool{Value: false}, nil
	}
	return nil, err
}

func ioTempFile(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	dir, pattern, err := tempArgs(call, args, "geblang-*")
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return nil, err
	}
	return runtime.String{Value: path}, nil
}

func ioTempDir(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	dir, pattern, err := tempArgs(call, args, "geblang-*")
	if err != nil {
		return nil, err
	}
	path, err := os.MkdirTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: path}, nil
}

func tempArgs(call *ast.CallExpression, args []runtime.Value, defaultPattern string) (string, string, error) {
	switch len(args) {
	case 0:
		return "", defaultPattern, nil
	case 1:
		pattern, ok := args[0].(runtime.String)
		if !ok {
			return "", "", fmt.Errorf("%s pattern must be string", call.Callee.String())
		}
		return "", pattern.Value, nil
	case 2:
		dir, ok := args[0].(runtime.String)
		if !ok {
			return "", "", fmt.Errorf("%s dir must be string", call.Callee.String())
		}
		pattern, ok := args[1].(runtime.String)
		if !ok {
			return "", "", fmt.Errorf("%s pattern must be string", call.Callee.String())
		}
		return dir.Value, pattern.Value, nil
	default:
		return "", "", fmt.Errorf("%s expects zero, one, or two arguments", call.Callee.String())
	}
}

func (e *Evaluator) ioOpen(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	path, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s path must be string", call.Callee.String())
	}
	mode, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s mode must be string", call.Callee.String())
	}
	flags, err := fileOpenFlags(mode.Value)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path.Value, flags, 0o666)
	if err != nil {
		return nil, err
	}
	e.fileMu.Lock()
	defer e.fileMu.Unlock()
	e.nextFileID++
	e.files[e.nextFileID] = file
	return runtime.NewInt64(e.nextFileID), nil
}

func (e *Evaluator) ioMemory(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s expects zero or one argument", call.Callee.String())
	}
	mem := &memoryStream{}
	if len(args) == 1 {
		switch value := args[0].(type) {
		case runtime.String:
			_, _ = mem.Write([]byte(value.Value))
		case runtime.Bytes:
			_, _ = mem.Write(value.Value)
		default:
			return nil, fmt.Errorf("%s initial value must be string or bytes", call.Callee.String())
		}
	}
	return e.registerIOStream(&ioStreamHandle{name: "memory", reader: mem, writer: mem, memory: mem}), nil
}

func (e *Evaluator) ioStdout(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	return e.registerIOStream(&ioStreamHandle{name: "stdout", writer: writerFunc(func(p []byte) (int, error) {
		return e.stdout.Write(p)
	})}), nil
}

func (e *Evaluator) ioStderr(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	return e.registerIOStream(&ioStreamHandle{name: "stderr", writer: writerFunc(func(p []byte) (int, error) {
		return e.stderr.Write(p)
	})}), nil
}

func (e *Evaluator) ioStdin(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	return e.registerIOStream(&ioStreamHandle{name: "stdin", reader: readerFunc(func(p []byte) (int, error) {
		return e.stdin.Read(p)
	})}), nil
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func (e *Evaluator) registerIOStream(handle *ioStreamHandle) runtime.Value {
	e.streamMu.Lock()
	defer e.streamMu.Unlock()
	e.nextStreamID++
	e.streams[e.nextStreamID] = handle
	return runtime.NativeObject{Kind: "IOStream", ID: e.nextStreamID}
}

func (e *Evaluator) ioStreamHandle(value runtime.Value) (*ioStreamHandle, error) {
	stream, ok := value.(runtime.NativeObject)
	if !ok || (stream.Kind != "IOStream" && stream.Kind != "IOCapture") {
		return nil, fmt.Errorf("stream handle must be IOStream")
	}
	e.streamMu.Lock()
	handle, ok := e.streams[stream.ID]
	e.streamMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.ioStreamHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown stream handle %d", stream.ID)
	}
	if handle.closed {
		return nil, fmt.Errorf("stream handle %d is closed", stream.ID)
	}
	return handle, nil
}

func (e *Evaluator) ioReader(value runtime.Value) (io.Reader, error) {
	if stream, ok := isIOStreamKind(value); ok {
		handle, err := e.ioStreamHandle(stream)
		if err != nil {
			return nil, err
		}
		if handle.reader == nil {
			return nil, fmt.Errorf("%s stream is not readable", handle.name)
		}
		if handle.bufReader != nil {
			return handle.bufReader, nil
		}
		return handle.reader, nil
	}
	return e.fileHandle(value)
}

func (e *Evaluator) ioWriter(value runtime.Value) (io.Writer, error) {
	if stream, ok := isIOStreamKind(value); ok {
		handle, err := e.ioStreamHandle(stream)
		if err != nil {
			return nil, err
		}
		if handle.writer == nil {
			return nil, fmt.Errorf("%s stream is not writable", handle.name)
		}
		return handle.writer, nil
	}
	return e.fileHandle(value)
}

func (e *Evaluator) ioRead(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	n, ok := toInt64(args[1])
	if !ok {
		return nil, fmt.Errorf("%s byte count must be int", call.Callee.String())
	}
	if n < 0 || n > 1<<30 {
		return nil, fmt.Errorf("%s byte count out of range", call.Callee.String())
	}
	reader, err := e.ioReader(args[0])
	if err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	read, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return runtime.String{Value: string(buf[:read])}, nil
}

func (e *Evaluator) ioReadAll(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	reader, err := e.ioReader(args[0])
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: string(data)}, nil
}

func (e *Evaluator) ioWrite(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	text, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s content must be string", call.Callee.String())
	}
	if buffer, ok := args[0].(runtime.NativeObject); ok && buffer.Kind == "IOBuffer" {
		return e.writeBuffer(buffer.ID, text.Value)
	}
	writer, err := e.ioWriter(args[0])
	if err != nil {
		return nil, err
	}
	written, err := io.WriteString(writer, text.Value)
	if err != nil {
		return nil, err
	}
	return runtime.NewInt64(int64(written)), nil
}

func (e *Evaluator) ioWriteln(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	text, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s content must be string", call.Callee.String())
	}
	line := text.Value + "\n"
	if buffer, ok := args[0].(runtime.NativeObject); ok && buffer.Kind == "IOBuffer" {
		return e.writeBuffer(buffer.ID, line)
	}
	writer, err := e.ioWriter(args[0])
	if err != nil {
		return nil, err
	}
	written, err := io.WriteString(writer, line)
	if err != nil {
		return nil, err
	}
	return runtime.NewInt64(int64(written)), nil
}

func (e *Evaluator) ioFlush(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	if buffer, ok := args[0].(runtime.NativeObject); ok && buffer.Kind == "IOBuffer" {
		if _, err := e.bufferHandle(buffer.ID); err != nil {
			return nil, err
		}
		return runtime.Null{}, nil
	}
	if stream, ok := isIOStreamKind(args[0]); ok {
		if _, err := e.ioStreamHandle(stream); err != nil {
			return nil, err
		}
		return runtime.Null{}, nil
	}
	file, err := e.fileHandle(args[0])
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, file.Sync()
}

func (e *Evaluator) ioSync(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	file, err := e.fileHandle(args[0])
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, file.Sync()
}

func (e *Evaluator) ioDataSync(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	file, err := e.fileHandle(args[0])
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, file.Sync()
}

func (e *Evaluator) ioLock(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	file, exclusive, err := e.fileLockArgs(call, args)
	if err != nil {
		return nil, err
	}
	if _, err := lockFile(file.Fd(), exclusive, false); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) ioTryLock(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	file, exclusive, err := e.fileLockArgs(call, args)
	if err != nil {
		return nil, err
	}
	acquired, err := lockFile(file.Fd(), exclusive, true)
	if err != nil {
		return nil, err
	}
	return runtime.Bool{Value: acquired}, nil
}

func (e *Evaluator) ioUnlock(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	file, err := e.fileHandle(args[0])
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, unlockFile(file.Fd())
}

func (e *Evaluator) fileLockArgs(call *ast.CallExpression, args []runtime.Value) (*os.File, bool, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, false, fmt.Errorf("%s expects file and optional mode", call.Callee.String())
	}
	file, err := e.fileHandle(args[0])
	if err != nil {
		return nil, false, err
	}
	mode := "exclusive"
	if len(args) == 2 {
		value, ok := args[1].(runtime.String)
		if !ok {
			return nil, false, fmt.Errorf("%s mode must be string", call.Callee.String())
		}
		mode = value.Value
	}
	switch mode {
	case "exclusive":
		return file, true, nil
	case "shared":
		return file, false, nil
	default:
		return nil, false, fmt.Errorf("%s mode must be \"exclusive\" or \"shared\"", call.Callee.String())
	}
}

func (e *Evaluator) ioClose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	if buffer, ok := args[0].(runtime.NativeObject); ok && buffer.Kind == "IOBuffer" {
		return e.closeBuffer(buffer.ID)
	}
	if stream, ok := isIOStreamKind(args[0]); ok {
		e.streamMu.Lock()
		handle, ok := e.streams[stream.ID]
		if ok {
			delete(e.streams, stream.ID)
		}
		e.streamMu.Unlock()
		if !ok && e.parent != nil {
			return e.parent.ioClose(call, args)
		}
		if !ok {
			return nil, fmt.Errorf("unknown stream handle %d", stream.ID)
		}
		return runtime.Null{}, closeIOStreamHandle(handle)
	}
	handle, err := fileHandleID(args[0])
	if err != nil {
		return nil, err
	}
	e.fileMu.Lock()
	file, ok := e.files[handle]
	if ok {
		delete(e.files, handle)
	}
	e.fileMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown file handle %d", handle)
	}
	if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return nil, err
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) ioToString(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	if buffer, ok := args[0].(runtime.NativeObject); ok && buffer.Kind == "IOBuffer" {
		return e.bufferString(buffer.ID)
	}
	handle, err := e.ioStreamHandle(args[0])
	if err != nil {
		return nil, err
	}
	if handle.memory == nil {
		return nil, fmt.Errorf("%s stream has no in-memory content", handle.name)
	}
	return runtime.String{Value: handle.memory.String()}, nil
}

func (e *Evaluator) ioCaptureStdout(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	mem := &memoryStream{}
	previous := e.stdout
	handle := &ioStreamHandle{name: "stdout capture", reader: mem, writer: mem, memory: mem}
	handle.restore = func() {
		e.stdout = previous
	}
	e.stdout = mem
	value := e.registerIOStream(handle).(runtime.NativeObject)
	value.Kind = "IOCapture"
	return value, nil
}

func (e *Evaluator) ioCaptureStderr(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	mem := &memoryStream{}
	previous := e.stderr
	handle := &ioStreamHandle{name: "stderr capture", reader: mem, writer: mem, memory: mem}
	handle.restore = func() {
		e.stderr = previous
	}
	e.stderr = mem
	value := e.registerIOStream(handle).(runtime.NativeObject)
	value.Kind = "IOCapture"
	return value, nil
}

func (e *Evaluator) ioRedirectStdout(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	writer, err := e.ioWriter(args[0])
	if err != nil {
		return nil, err
	}
	previous := e.stdout
	e.stdout = writer
	return runtime.Function{Name: "restoreStdout", Native: func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("stdout restore expects no arguments")
		}
		e.stdout = previous
		return runtime.Null{}, nil
	}}, nil
}

func (e *Evaluator) ioRedirectStderr(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	writer, err := e.ioWriter(args[0])
	if err != nil {
		return nil, err
	}
	previous := e.stderr
	e.stderr = writer
	return runtime.Function{Name: "restoreStderr", Native: func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("stderr restore expects no arguments")
		}
		e.stderr = previous
		return runtime.Null{}, nil
	}}, nil
}

func (e *Evaluator) ioRedirectStdin(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	reader, err := e.ioReader(args[0])
	if err != nil {
		return nil, err
	}
	previous := e.stdin
	previousReader := e.stdinReader
	e.stdin = reader
	e.stdinReader = nil
	return runtime.Function{Name: "restoreStdin", Native: func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("stdin restore expects no arguments")
		}
		e.stdin = previous
		e.stdinReader = previousReader
		return runtime.Null{}, nil
	}}, nil
}

func (e *Evaluator) ioBuffer(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	e.bufferMu.Lock()
	defer e.bufferMu.Unlock()
	e.nextBufferID++
	id := e.nextBufferID
	e.buffers[id] = &bytes.Buffer{}
	return runtime.NativeObject{Kind: "IOBuffer", ID: id}, nil
}

func (e *Evaluator) ioBufferToString(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	buffer, err := ioBufferObject(args[0])
	if err != nil {
		return nil, err
	}
	return e.bufferString(buffer.ID)
}

func (e *Evaluator) ioBufferReset(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	buffer, err := ioBufferObject(args[0])
	if err != nil {
		return nil, err
	}
	return e.resetBuffer(buffer.ID)
}

func ioReadCSV(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		return nil, err
	}
	rows := make([]runtime.Value, 0, len(records))
	for _, record := range records {
		row := make([]runtime.Value, 0, len(record))
		for _, field := range record {
			row = append(row, runtime.String{Value: field})
		}
		rows = append(rows, &runtime.List{Elements: row})
	}
	return &runtime.List{Elements: rows}, nil
}

func ioWriteCSV(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	path, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s path must be string", call.Callee.String())
	}
	rows, ok := args[1].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s rows must be list", call.Callee.String())
	}
	file, err := os.Create(path.Value)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	for _, rowValue := range rows.Elements {
		rowList, ok := rowValue.(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("%s rows must be list<list<any>>", call.Callee.String())
		}
		record := make([]string, 0, len(rowList.Elements))
		for _, field := range rowList.Elements {
			record = append(record, field.Inspect())
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) ioStdinReadAll(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	data, err := io.ReadAll(e.stdin)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: string(data)}, nil
}

func (e *Evaluator) ioStdinReadLine(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	if e.stdinReader == nil {
		e.stdinReader = bufio.NewReader(e.stdin)
	}
	line, err := e.stdinReader.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if err == io.EOF && line == "" {
		return runtime.Null{}, nil
	}
	return runtime.String{Value: line}, nil
}

func (e *Evaluator) fileBufReader(handle int64) (*bufio.Reader, error) {
	e.fileMu.Lock()
	defer e.fileMu.Unlock()
	if r, ok := e.bufReaders[handle]; ok {
		return r, nil
	}
	file, ok := e.files[handle]
	if !ok {
		if e.parent != nil {
			return e.parent.fileBufReader(handle)
		}
		return nil, fmt.Errorf("unknown file handle %d", handle)
	}
	r := bufio.NewReader(file)
	e.bufReaders[handle] = r
	return r, nil
}

func (e *Evaluator) ioReadLine(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	if stream, ok := isIOStreamKind(args[0]); ok {
		handle, err := e.ioStreamHandle(stream)
		if err != nil {
			return nil, err
		}
		if handle.reader == nil {
			return nil, fmt.Errorf("%s stream is not readable", handle.name)
		}
		if handle.bufReader == nil {
			handle.bufReader = bufio.NewReader(handle.reader)
		}
		line, err := handle.bufReader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if err == io.EOF && line == "" {
			return runtime.Null{}, nil
		}
		return runtime.String{Value: line}, nil
	}
	handle, err := fileHandleID(args[0])
	if err != nil {
		return nil, err
	}
	r, err := e.fileBufReader(handle)
	if err != nil {
		return nil, err
	}
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if err == io.EOF && line == "" {
		return runtime.Null{}, nil
	}
	return runtime.String{Value: line}, nil
}

func (e *Evaluator) ioReadLines(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	var r *bufio.Reader
	if stream, ok := isIOStreamKind(args[0]); ok {
		handle, err := e.ioStreamHandle(stream)
		if err != nil {
			return nil, err
		}
		if handle.reader == nil {
			return nil, fmt.Errorf("%s stream is not readable", handle.name)
		}
		if handle.bufReader == nil {
			handle.bufReader = bufio.NewReader(handle.reader)
		}
		r = handle.bufReader
	} else {
		handle, err := fileHandleID(args[0])
		if err != nil {
			return nil, err
		}
		r, err = e.fileBufReader(handle)
		if err != nil {
			return nil, err
		}
	}
	var lines []runtime.Value
	for {
		line, readErr := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line != "" || readErr == nil {
			lines = append(lines, runtime.String{Value: line})
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return &runtime.List{Elements: lines}, nil
}

func fileInfoDict(name string, info os.FileInfo) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "name", runtime.String{Value: name})
	putDict(entries, "size", runtime.NewInt64(info.Size()))
	putDict(entries, "mode", runtime.NewInt64(int64(info.Mode().Perm())))
	putDict(entries, "isDir", runtime.Bool{Value: info.IsDir()})
	putDict(entries, "isFile", runtime.Bool{Value: info.Mode().IsRegular()})
	putDict(entries, "isSymlink", runtime.Bool{Value: info.Mode()&os.ModeSymlink != 0})
	putDict(entries, "modUnix", runtime.NewInt64(info.ModTime().Unix()))
	putDict(entries, "modUnixNano", runtime.NewInt64(info.ModTime().UnixNano()))
	return runtime.Dict{Entries: entries}
}

func ioStat(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return fileInfoDict(info.Name(), info), nil
}

func ioLstat(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	return fileInfoDict(info.Name(), info), nil
}

func ioScanDir(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	values := make([]runtime.Value, 0, len(entries))
	for _, entry := range entries {
		d := map[string]runtime.DictEntry{}
		putDict(d, "name", runtime.String{Value: entry.Name()})
		putDict(d, "isDir", runtime.Bool{Value: entry.IsDir()})
		putDict(d, "isFile", runtime.Bool{Value: entry.Type().IsRegular()})
		putDict(d, "isSymlink", runtime.Bool{Value: entry.Type()&os.ModeSymlink != 0})
		values = append(values, runtime.Dict{Entries: d})
	}
	return &runtime.List{Elements: values}, nil
}

func ioChmod(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	path, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s path must be string", call.Callee.String())
	}
	modeVal, ok := toInt64(args[1])
	if !ok {
		return nil, fmt.Errorf("%s mode must be int", call.Callee.String())
	}
	return runtime.Null{}, os.Chmod(path.Value, os.FileMode(modeVal))
}

func ioChown(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("%s expects exactly three arguments", call.Callee.String())
	}
	path, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s path must be string", call.Callee.String())
	}
	uid, uidOK := toInt64(args[1])
	gid, gidOK := toInt64(args[2])
	if !uidOK || !gidOK {
		return nil, fmt.Errorf("%s uid and gid must be ints", call.Callee.String())
	}
	return runtime.Null{}, os.Chown(path.Value, int(uid), int(gid))
}

func ioMkdir(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	path, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s path must be string", call.Callee.String())
	}
	modeVal, ok := toInt64(args[1])
	if !ok {
		return nil, fmt.Errorf("%s mode must be int", call.Callee.String())
	}
	return runtime.Null{}, os.MkdirAll(path.Value, os.FileMode(modeVal))
}

func ioRemove(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, os.RemoveAll(path)
}

func ioRename(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	oldPath, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s old path must be string", call.Callee.String())
	}
	newPath, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s new path must be string", call.Callee.String())
	}
	return runtime.Null{}, os.Rename(oldPath.Value, newPath.Value)
}

func twoStringArgs(call *ast.CallExpression, args []runtime.Value) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	a, ok := args[0].(runtime.String)
	if !ok {
		return "", "", fmt.Errorf("%s first argument must be string", call.Callee.String())
	}
	b, ok := args[1].(runtime.String)
	if !ok {
		return "", "", fmt.Errorf("%s second argument must be string", call.Callee.String())
	}
	return a.Value, b.Value, nil
}

// copyFileContents streams src to dst (truncating dst), preserving src's mode.
func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode().Perm())
}

// copyTreeContents copies a file or directory tree, recreating symlinks as links.
func copyTreeContents(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if !info.IsDir() {
		return copyFileContents(src, dst)
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyTreeContents(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func ioCopy(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	src, dst, err := twoStringArgs(call, args)
	if err != nil {
		return nil, err
	}
	if err := copyFileContents(src, dst); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func ioCopyTree(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	src, dst, err := twoStringArgs(call, args)
	if err != nil {
		return nil, err
	}
	if err := copyTreeContents(src, dst); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func ioMove(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	src, dst, err := twoStringArgs(call, args)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(src, dst); err == nil {
		return runtime.Null{}, nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return nil, err
	}
	// Different filesystems: rename cannot cross devices, so copy then delete.
	if err := copyTreeContents(src, dst); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(src); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func ioTouch(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err == nil {
		return runtime.Null{}, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, f.Close()
}

func ioWriteTextAtomic(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, text, err := twoStringArgs(call, args)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".geb-atomic-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return nil, err
	}
	committed = true
	return runtime.Null{}, nil
}

func ioSymlink(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	target, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s target must be string", call.Callee.String())
	}
	linkPath, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s link path must be string", call.Callee.String())
	}
	return runtime.Null{}, os.Symlink(target.Value, linkPath.Value)
}

func ioReadLink(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	target, err := os.Readlink(path)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: target}, nil
}

func ioListDir(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	path, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	values := make([]runtime.Value, 0, len(entries))
	for _, entry := range entries {
		values = append(values, runtime.String{Value: entry.Name()})
	}
	return &runtime.List{Elements: values}, nil
}

func ioWalkDir(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	root, err := singleStringValue(call, args)
	if err != nil {
		return nil, err
	}
	var values []runtime.Value
	err = filepath.Walk(root, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		entries := map[string]runtime.DictEntry{}
		putDict(entries, "path", runtime.String{Value: p})
		putDict(entries, "name", runtime.String{Value: info.Name()})
		putDict(entries, "isDir", runtime.Bool{Value: info.IsDir()})
		putDict(entries, "size", runtime.NewInt64(info.Size()))
		putDict(entries, "modUnix", runtime.NewInt64(info.ModTime().Unix()))
		values = append(values, runtime.Dict{Entries: entries})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &runtime.List{Elements: values}, nil
}

func (e *Evaluator) fileHandle(value runtime.Value) (*os.File, error) {
	handle, err := fileHandleID(value)
	if err != nil {
		return nil, err
	}
	e.fileMu.Lock()
	file, ok := e.files[handle]
	e.fileMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.fileHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown file handle %d", handle)
	}
	return file, nil
}

// invalidateBufReader drops the cached line reader for a handle (across the
// parent chain) so a reposition or content change re-syncs the next readLine.
func (e *Evaluator) invalidateBufReader(value runtime.Value) {
	handle, err := fileHandleID(value)
	if err != nil {
		return
	}
	for cur := e; cur != nil; cur = cur.parent {
		cur.fileMu.Lock()
		delete(cur.bufReaders, handle)
		cur.fileMu.Unlock()
	}
}

// bufferedCount returns the bytes a cached line reader has read ahead but not yet
// yielded, so tell/atEnd can report the logical position rather than the file's.
func (e *Evaluator) bufferedCount(value runtime.Value) int {
	handle, err := fileHandleID(value)
	if err != nil {
		return 0
	}
	for cur := e; cur != nil; cur = cur.parent {
		cur.fileMu.Lock()
		r, ok := cur.bufReaders[handle]
		cur.fileMu.Unlock()
		if ok {
			return r.Buffered()
		}
	}
	return 0
}

func (e *Evaluator) ioSeek(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects a handle, offset, and optional whence", call.Callee.String())
	}
	offset, ok := toInt64(args[1])
	if !ok {
		return nil, fmt.Errorf("%s offset must be int", call.Callee.String())
	}
	whence := io.SeekStart
	if len(args) == 3 {
		mode, ok := args[2].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s whence must be a string", call.Callee.String())
		}
		w, err := seekWhence(mode.Value)
		if err != nil {
			return nil, err
		}
		whence = w
	}
	file, err := e.fileHandle(args[0])
	if err != nil {
		return nil, err
	}
	pos, err := file.Seek(offset, whence)
	if err != nil {
		return nil, err
	}
	e.invalidateBufReader(args[0])
	return runtime.NewInt64(pos), nil
}

func (e *Evaluator) ioTell(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	file, err := e.fileHandle(args[0])
	if err != nil {
		return nil, err
	}
	pos, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	return runtime.NewInt64(pos - int64(e.bufferedCount(args[0]))), nil
}

func (e *Evaluator) ioTruncate(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects a handle and a size", call.Callee.String())
	}
	size, ok := toInt64(args[1])
	if !ok || size < 0 {
		return nil, fmt.Errorf("%s size must be a non-negative int", call.Callee.String())
	}
	file, err := e.fileHandle(args[0])
	if err != nil {
		return nil, err
	}
	if err := file.Truncate(size); err != nil {
		return nil, err
	}
	e.invalidateBufReader(args[0])
	return runtime.Null{}, nil
}

func (e *Evaluator) ioAtEnd(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	if e.bufferedCount(args[0]) > 0 {
		return runtime.Bool{Value: false}, nil
	}
	file, err := e.fileHandle(args[0])
	if err != nil {
		return nil, err
	}
	pos, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	return runtime.Bool{Value: pos >= info.Size()}, nil
}

type streamSource struct {
	Reader io.Reader
	Text   string
}

func (e *Evaluator) streamSourceReader(call *ast.CallExpression, value runtime.Value) (streamSource, error) {
	switch value := value.(type) {
	case runtime.String:
		return streamSource{Reader: strings.NewReader(value.Value), Text: value.Value}, nil
	case runtime.Bytes:
		text := string(value.Value)
		return streamSource{Reader: bytes.NewReader(value.Value), Text: text}, nil
	case runtime.Int:
		file, err := e.fileHandle(value)
		if err != nil {
			return streamSource{}, err
		}
		return streamSource{Reader: file}, nil
	case runtime.NativeObject:
		switch value.Kind {
		case "IOBuffer":
			buffer, err := e.bufferHandle(value.ID)
			if err != nil {
				return streamSource{}, err
			}
			data := append([]byte(nil), buffer.Bytes()...)
			return streamSource{Reader: bytes.NewReader(data), Text: string(data)}, nil
		case "IOStream", "IOCapture":
			handle, err := e.ioStreamHandle(value)
			if err != nil {
				return streamSource{}, err
			}
			if handle.memory != nil {
				data := handle.memory.Bytes()
				return streamSource{Reader: bytes.NewReader(data), Text: string(data)}, nil
			}
			if handle.reader != nil {
				return streamSource{Reader: handle.reader}, nil
			}
			return streamSource{}, fmt.Errorf("%s source stream is not readable", call.Callee.String())
		default:
			return streamSource{}, fmt.Errorf("%s source native object must be IOBuffer or IOStream, got %s", call.Callee.String(), value.Kind)
		}
	default:
		return streamSource{}, fmt.Errorf("%s source must be string, bytes, file handle, IOBuffer, or IOStream", call.Callee.String())
	}
}

func ioBufferObject(value runtime.Value) (runtime.NativeObject, error) {
	buffer, ok := value.(runtime.NativeObject)
	if !ok || buffer.Kind != "IOBuffer" {
		return runtime.NativeObject{}, fmt.Errorf("buffer handle must be IOBuffer")
	}
	return buffer, nil
}

func (e *Evaluator) bufferHandle(id int64) (*bytes.Buffer, error) {
	e.bufferMu.Lock()
	buffer, ok := e.buffers[id]
	e.bufferMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.bufferHandle(id)
	}
	if !ok {
		return nil, fmt.Errorf("unknown buffer handle %d", id)
	}
	return buffer, nil
}

func (e *Evaluator) writeBuffer(id int64, text string) (runtime.Value, error) {
	e.bufferMu.Lock()
	buffer, ok := e.buffers[id]
	e.bufferMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.writeBuffer(id, text)
	}
	if !ok {
		return nil, fmt.Errorf("unknown buffer handle %d", id)
	}
	written, err := buffer.WriteString(text)
	if err != nil {
		return nil, err
	}
	return runtime.NewInt64(int64(written)), nil
}

func (e *Evaluator) bufferString(id int64) (runtime.Value, error) {
	buffer, err := e.bufferHandle(id)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: buffer.String()}, nil
}

func (e *Evaluator) resetBuffer(id int64) (runtime.Value, error) {
	e.bufferMu.Lock()
	buffer, ok := e.buffers[id]
	e.bufferMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.resetBuffer(id)
	}
	if !ok {
		return nil, fmt.Errorf("unknown buffer handle %d", id)
	}
	buffer.Reset()
	return runtime.Null{}, nil
}

func (e *Evaluator) closeBuffer(id int64) (runtime.Value, error) {
	e.bufferMu.Lock()
	_, ok := e.buffers[id]
	if ok {
		delete(e.buffers, id)
	}
	e.bufferMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.closeBuffer(id)
	}
	if !ok {
		return nil, fmt.Errorf("unknown buffer handle %d", id)
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) ioBufferMethod(buffer runtime.NativeObject, name string, args []runtime.Value) (runtime.Value, error) {
	switch name {
	case "write":
		if len(args) != 1 {
			return nil, fmt.Errorf("IOBuffer.write expects exactly one argument")
		}
		text, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("IOBuffer.write content must be string")
		}
		return e.writeBuffer(buffer.ID, text.Value)
	case "writeln":
		if len(args) != 1 {
			return nil, fmt.Errorf("IOBuffer.writeln expects exactly one argument")
		}
		text, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("IOBuffer.writeln content must be string")
		}
		return e.writeBuffer(buffer.ID, text.Value+"\n")
	case "toString":
		if len(args) != 0 {
			return nil, fmt.Errorf("IOBuffer.toString expects no arguments")
		}
		return e.bufferString(buffer.ID)
	case "reset":
		if len(args) != 0 {
			return nil, fmt.Errorf("IOBuffer.reset expects no arguments")
		}
		return e.resetBuffer(buffer.ID)
	case "length":
		if len(args) != 0 {
			return nil, fmt.Errorf("IOBuffer.length expects no arguments")
		}
		value, err := e.bufferHandle(buffer.ID)
		if err != nil {
			return nil, err
		}
		return runtime.NewInt64(int64(value.Len())), nil
	case "close":
		if len(args) != 0 {
			return nil, fmt.Errorf("IOBuffer.close expects no arguments")
		}
		return e.closeBuffer(buffer.ID)
	default:
		return nil, fmt.Errorf("IOBuffer has no method %s", name)
	}
}

func (e *Evaluator) ioStreamMethod(stream runtime.NativeObject, name string, args []runtime.Value) (runtime.Value, error) {
	call := &ast.CallExpression{Callee: &ast.SelectorExpression{Object: &ast.Identifier{Value: stream.Kind}, Name: &ast.Identifier{Value: name}}}
	switch name {
	case "write":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s.write expects exactly one argument", stream.Kind)
		}
		return e.ioWrite(call, []runtime.Value{stream, args[0]})
	case "writeln":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s.writeln expects exactly one argument", stream.Kind)
		}
		return e.ioWriteln(call, []runtime.Value{stream, args[0]})
	case "writeBytes":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s.writeBytes expects exactly one argument", stream.Kind)
		}
		return e.ioWriteBytes(call, []runtime.Value{stream, args[0]})
	case "read":
		if stream.Kind == "IOCapture" && len(args) == 0 {
			return e.ioToString(call, []runtime.Value{stream})
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("%s.read expects exactly one argument", stream.Kind)
		}
		return e.ioRead(call, []runtime.Value{stream, args[0]})
	case "readBytes":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s.readBytes expects exactly one argument", stream.Kind)
		}
		return e.ioReadBytes(call, []runtime.Value{stream, args[0]})
	case "readAll":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.%s expects no arguments", stream.Kind, name)
		}
		return e.ioReadAll(call, []runtime.Value{stream})
	case "toString":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.%s expects no arguments", stream.Kind, name)
		}
		return e.ioToString(call, []runtime.Value{stream})
	case "bytes":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.bytes expects no arguments", stream.Kind)
		}
		handle, err := e.ioStreamHandle(stream)
		if err != nil {
			return nil, err
		}
		if handle.memory == nil {
			return nil, fmt.Errorf("%s stream has no in-memory content", handle.name)
		}
		return runtime.Bytes{Value: handle.memory.Bytes()}, nil
	case "reset":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.reset expects no arguments", stream.Kind)
		}
		handle, err := e.ioStreamHandle(stream)
		if err != nil {
			return nil, err
		}
		if handle.memory == nil {
			return nil, fmt.Errorf("%s stream has no in-memory content", handle.name)
		}
		handle.memory.Reset()
		return runtime.Null{}, nil
	case "close":
		if len(args) != 0 {
			return nil, fmt.Errorf("%s.close expects no arguments", stream.Kind)
		}
		return e.ioClose(call, []runtime.Value{stream})
	default:
		return nil, fmt.Errorf("%s has no method %s", stream.Kind, name)
	}
}

func fileHandleID(value runtime.Value) (int64, error) {
	id, ok := value.(runtime.Int)
	if !ok || !id.Value.IsInt64() {
		return 0, fmt.Errorf("file handle must be int")
	}
	return id.Value.Int64(), nil
}

func fileOpenFlags(mode string) (int, error) {
	switch mode {
	case "r":
		return os.O_RDONLY, nil
	case "w":
		return os.O_CREATE | os.O_WRONLY | os.O_TRUNC, nil
	case "a":
		return os.O_CREATE | os.O_WRONLY | os.O_APPEND, nil
	case "r+":
		return os.O_RDWR, nil
	case "w+":
		return os.O_CREATE | os.O_RDWR | os.O_TRUNC, nil
	case "a+":
		return os.O_CREATE | os.O_RDWR | os.O_APPEND, nil
	case "x":
		return os.O_CREATE | os.O_EXCL | os.O_WRONLY, nil
	case "x+":
		return os.O_CREATE | os.O_EXCL | os.O_RDWR, nil
	case "rw": // alias of r+ with create
		return os.O_CREATE | os.O_RDWR, nil
	case "rw_trunc": // alias of w+
		return os.O_CREATE | os.O_RDWR | os.O_TRUNC, nil
	default:
		return 0, fmt.Errorf("unsupported file mode %q", mode)
	}
}

// seekWhence maps the string whence to an io.Seek* origin.
func seekWhence(whence string) (int, error) {
	switch whence {
	case "start":
		return io.SeekStart, nil
	case "current":
		return io.SeekCurrent, nil
	case "end":
		return io.SeekEnd, nil
	default:
		return 0, fmt.Errorf("invalid whence %q (want \"start\", \"current\", or \"end\")", whence)
	}
}
