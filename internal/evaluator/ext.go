package evaluator

// ext module: subprocess-based extension connections.
//
// Protocol: length-prefixed frames over a Unix socket or TCP connection.
//   Frame = [uint32 big-endian length][uint8 type][payload bytes]
//   Type 0x00 = JSON (UTF-8), Type 0x01 = binary blob
//
// A logical message is one JSON frame optionally followed by N binary frames.
// The JSON frame declares N via "slots":N; binary values in JSON are referenced
// as {"$type":"bytes","slot":0}. Extension object handles are {"$type":"handle","id":42}.
//
// Lifecycle:
//   - Managed (ext.load): geblang writes a temp socket path into GEBLANG_EXT_SOCKET,
//     starts the process, polls for the socket to appear, connects.
//   - Pre-started (ext.connect): geblang connects to an existing socket or host:port.
//   - On ext.close for managed: send __shutdown__ request, wait 2s, SIGTERM, 2s, SIGKILL.
//   - On ext.close for pre-started: close the connection only.

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"geblang/internal/ast"
	"geblang/internal/runtime"
)

type extHandle struct {
	mu        sync.Mutex
	conn      net.Conn
	name      string
	functions map[string]bool
	managed   bool
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	sockPath  string // temp socket path to remove on close
	sockDir   string // temp socket dir to remove on close
	nextReqID int64
}

// — evaluator fields wired in evaluator.go —
// extMu     sync.Mutex
// nextExtID int64
// extConns  map[int64]*extHandle

func (e *Evaluator) registerExtHandle(h *extHandle) runtime.Value {
	e.extMu.Lock()
	defer e.extMu.Unlock()
	e.nextExtID++
	ensureMap(&e.extConns)[e.nextExtID] = h
	return runtime.NewInt64(e.nextExtID)
}

func (e *Evaluator) extHandle(value runtime.Value) (*extHandle, error) {
	id, err := rawInt64(value, "ext handle")
	if err != nil {
		return nil, err
	}
	e.extMu.Lock()
	h, ok := e.extConns[id]
	e.extMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.extHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown ext handle %d", id)
	}
	return h, nil
}

// extBuiltins returns the "ext" module's builtin function map.
func (e *Evaluator) extBuiltins() map[string]builtinFunc {
	return map[string]builtinFunc{
		"load":            e.extLoad,
		"connect":         e.extConnect,
		"call":            e.extCall,
		"callWithOptions": e.extCallWithOptions,
		"close":           e.extClose,
		"functions": func(_ *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("ext.functions expects a connection handle")
			}
			h, err := e.extHandle(args[0])
			if err != nil {
				return nil, err
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			elems := make([]runtime.Value, 0, len(h.functions))
			for fn := range h.functions {
				elems = append(elems, runtime.String{Value: fn})
			}
			sort.Slice(elems, func(i, j int) bool {
				return elems[i].(runtime.String).Value < elems[j].(runtime.String).Value
			})
			return &runtime.List{Elements: elems}, nil
		},
	}
}

// ext.load("name") — managed extension, config from geblang.yaml
func (e *Evaluator) extLoad(_ *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("ext.load expects an extension name")
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("ext.load name must be string")
	}
	cfg, err := e.findExtensionConfig(name.Value)
	if err != nil {
		return nil, fmt.Errorf("ext.load %q: %w", name.Value, err)
	}
	if cfg.Command == nil || len(cfg.Command) == 0 {
		return nil, fmt.Errorf("ext.load %q: no command configured; use ext.connect for pre-started extensions", name.Value)
	}

	sockDir, err := os.MkdirTemp("", "geblang-ext-*")
	if err != nil {
		return nil, fmt.Errorf("ext.load %q: temp dir: %w", name.Value, err)
	}
	sockPath := filepath.Join(sockDir, "ext.sock")

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	if cfg.Root != "" {
		cmd.Dir = cfg.Root
	}
	cmd.Env = append(os.Environ(), "GEBLANG_EXT_SOCKET="+sockPath)
	if cfg.Env != nil {
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.RemoveAll(sockDir)
		return nil, fmt.Errorf("ext.load %q: start: %w", name.Value, err)
	}

	timeoutMs := cfg.StartupTimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 10000
	}
	conn, err := waitForUnixSocket(sockPath, time.Duration(timeoutMs)*time.Millisecond)
	if err != nil {
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.RemoveAll(sockDir)
		return nil, fmt.Errorf("ext.load %q: %w", name.Value, err)
	}

	h := &extHandle{conn: conn, managed: true, cmd: cmd, cancel: cancel, sockPath: sockPath, sockDir: sockDir}
	if err := extHandshake(h); err != nil {
		conn.Close()
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.RemoveAll(sockDir)
		return nil, fmt.Errorf("ext.load %q: handshake: %w", name.Value, err)
	}
	return e.registerExtHandle(h), nil
}

// ext.connect(address) — pre-started extension
func (e *Evaluator) extConnect(_ *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("ext.connect expects an address (unix socket path or host:port)")
	}
	addr, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("ext.connect address must be string")
	}

	var conn net.Conn
	var err error
	if isUnixSocketAddress(addr.Value) {
		conn, err = net.Dial("unix", addr.Value)
	} else {
		conn, err = net.Dial("tcp", addr.Value)
	}
	if err != nil {
		return nil, fmt.Errorf("ext.connect %q: %w", addr.Value, err)
	}

	h := &extHandle{conn: conn, managed: false}
	if err := extHandshake(h); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ext.connect %q: handshake: %w", addr.Value, err)
	}
	return e.registerExtHandle(h), nil
}

// ext.call(conn, "fn", arg1, arg2, ...) or ext.call(conn, "fn", namedArg: val)
func (e *Evaluator) extCall(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	return e.extCallInternal(call, args, 0, 0)
}

// ext.callWithOptions(conn, "fn", {"timeoutMs": 1000, "maxResponseBytes": 1048576}, ...)
func (e *Evaluator) extCallWithOptions(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("ext.callWithOptions expects a connection handle, function name, options, and optional arguments")
	}
	options, ok := args[2].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("ext.callWithOptions options must be dict")
	}
	timeoutMs, maxResponseBytes, err := extCallOptions(options)
	if err != nil {
		return nil, fmt.Errorf("ext.callWithOptions: %w", err)
	}
	callArgs := append([]runtime.Value{args[0], args[1]}, args[3:]...)
	rewritten := rewriteExtCallArguments(call, 3)
	return e.extCallInternal(rewritten, callArgs, timeoutMs, maxResponseBytes)
}

func (e *Evaluator) extCallInternal(call *ast.CallExpression, args []runtime.Value, timeoutMs int64, maxResponseBytes int64) (runtime.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("ext.call expects a connection handle and function name")
	}
	h, err := e.extHandle(args[0])
	if err != nil {
		return nil, err
	}
	fnName, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("ext.call function name must be string")
	}

	// Collect positional and named args (args[2:]).
	positional := []runtime.Value{}
	named := map[string]runtime.Value{}
	if call != nil {
		// Reconstruct named args from AST — same pattern as other builtins.
		argIdx := 2 // skip conn and fn name
		for i, callArg := range call.Arguments {
			if i < 2 {
				continue
			}
			if argIdx >= len(args) {
				return nil, fmt.Errorf("ext.call internal argument mismatch")
			}
			if callArg.Name != nil {
				named[callArg.Name.Value] = args[argIdx]
			} else {
				positional = append(positional, args[argIdx])
			}
			argIdx++
		}
	} else {
		positional = args[2:]
	}

	h.mu.Lock()
	h.nextReqID++
	reqID := h.nextReqID
	h.mu.Unlock()

	req, binarySlots, err := extMarshalCall(reqID, fnName.Value, positional, named)
	if err != nil {
		return nil, fmt.Errorf("ext.call %q: marshal: %w", fnName.Value, err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if timeoutMs > 0 {
		deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
		if err := h.conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("ext.call %q: set timeout: %w", fnName.Value, err)
		}
		defer h.conn.SetDeadline(time.Time{})
	}

	if err := extWriteFrame(h.conn, frameTypeJSON, req); err != nil {
		return nil, fmt.Errorf("ext.call %q: send: %w", fnName.Value, err)
	}
	for _, blob := range binarySlots {
		if err := extWriteFrame(h.conn, frameTypeBinary, blob); err != nil {
			return nil, fmt.Errorf("ext.call %q: send binary: %w", fnName.Value, err)
		}
	}

	return extReadResponse(h.conn, reqID, fnName.Value, maxResponseBytes)
}

func extCallOptions(options runtime.Dict) (int64, int64, error) {
	var timeoutMs int64
	var maxResponseBytes int64
	if value, ok := dictField(options, "timeoutMs"); ok {
		n, err := rawInt64(value, "timeoutMs")
		if err != nil {
			return 0, 0, err
		}
		if n < 0 {
			return 0, 0, fmt.Errorf("timeoutMs must be non-negative")
		}
		timeoutMs = n
	}
	if value, ok := dictField(options, "maxResponseBytes"); ok {
		n, err := rawInt64(value, "maxResponseBytes")
		if err != nil {
			return 0, 0, err
		}
		if n < 0 {
			return 0, 0, fmt.Errorf("maxResponseBytes must be non-negative")
		}
		maxResponseBytes = n
	}
	return timeoutMs, maxResponseBytes, nil
}

func rewriteExtCallArguments(call *ast.CallExpression, skip int) *ast.CallExpression {
	if call == nil {
		return nil
	}
	rewritten := *call
	if len(call.Arguments) <= skip {
		rewritten.Arguments = nil
		return &rewritten
	}
	rewritten.Arguments = append([]ast.CallArgument{}, call.Arguments[:2]...)
	rewritten.Arguments = append(rewritten.Arguments, call.Arguments[skip:]...)
	return &rewritten
}

// ext.close(conn)
func (e *Evaluator) extClose(_ *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("ext.close expects a connection handle")
	}
	id, err := rawInt64(args[0], "ext handle")
	if err != nil {
		return nil, err
	}
	e.closeExtID(id)
	return runtime.Null{}, nil
}

func (e *Evaluator) closeExtID(id int64) bool {
	e.extMu.Lock()
	h, ok := e.extConns[id]
	if ok {
		delete(e.extConns, id)
	}
	e.extMu.Unlock()
	if ok {
		closeExtHandle(h)
		return true
	}
	if e.parent != nil {
		return e.parent.closeExtID(id)
	}
	return false
}

func closeExtHandle(h *extHandle) {
	if h.managed && h.conn != nil {
		// Best-effort graceful shutdown.
		shutdownJSON, _ := json.Marshal(map[string]interface{}{
			"id": 0, "fn": "__shutdown__", "args": []interface{}{}, "kwargs": map[string]interface{}{},
		})
		_ = extWriteFrame(h.conn, frameTypeJSON, shutdownJSON)
	}
	if h.conn != nil {
		_ = h.conn.Close()
	}
	if h.managed {
		if h.cmd != nil && h.cmd.Process != nil {
			done := make(chan struct{})
			go func() {
				_ = h.cmd.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				_ = h.cmd.Process.Signal(syscall.SIGTERM)
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					_ = h.cmd.Process.Kill()
				}
			}
		}
		if h.cancel != nil {
			h.cancel()
		}
		if h.sockPath != "" {
			_ = os.Remove(h.sockPath)
		}
		if h.sockDir != "" {
			_ = os.RemoveAll(h.sockDir)
		}
	}
}

func isUnixSocketAddress(address string) bool {
	return strings.HasPrefix(address, "/") ||
		strings.HasPrefix(address, "./") ||
		strings.HasPrefix(address, "../") ||
		strings.Contains(address, "/") ||
		strings.HasSuffix(address, ".sock")
}

// — frame I/O —

const (
	frameTypeJSON   byte = 0x00
	frameTypeBinary byte = 0x01
	maxExtFrameSize      = 64 << 20
)

func extWriteFrame(w io.Writer, ftype byte, data []byte) error {
	if len(data) > maxExtFrameSize {
		return fmt.Errorf("extension frame too large: %d bytes exceeds %d", len(data), maxExtFrameSize)
	}
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header[:4], uint32(len(data)))
	header[4] = ftype
	if _, err := writeFull(w, header); err != nil {
		return err
	}
	_, err := writeFull(w, data)
	return err
}

func writeFull(w io.Writer, data []byte) (int, error) {
	written := 0
	for written < len(data) {
		n, err := w.Write(data[written:])
		written += n
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func extReadFrame(r io.Reader) (byte, []byte, error) {
	return extReadFrameWithLimit(r, maxExtFrameSize)
}

func extReadFrameWithLimit(r io.Reader, limit int64) (byte, []byte, error) {
	if limit <= 0 || limit > maxExtFrameSize {
		limit = maxExtFrameSize
	}
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(header[:4])
	ftype := header[4]
	if int64(length) > limit {
		return 0, nil, fmt.Errorf("extension frame too large: %d bytes exceeds %d", length, limit)
	}
	if length == 0 {
		return ftype, nil, nil
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, nil, err
	}
	return ftype, data, nil
}

// — handshake —

func extHandshake(h *extHandle) error {
	ftype, data, err := extReadFrame(h.conn)
	if err != nil {
		return fmt.Errorf("read handshake: %w", err)
	}
	if ftype != frameTypeJSON {
		return fmt.Errorf("expected JSON handshake frame, got type %d", ftype)
	}
	var hs struct {
		V         int      `json:"v"`
		Name      string   `json:"name"`
		Functions []string `json:"functions"`
	}
	if err := json.Unmarshal(data, &hs); err != nil {
		return fmt.Errorf("parse handshake: %w", err)
	}
	if hs.V != 1 {
		return fmt.Errorf("unsupported protocol version %d", hs.V)
	}
	h.name = hs.Name
	h.functions = make(map[string]bool, len(hs.Functions))
	for _, fn := range hs.Functions {
		h.functions[fn] = true
	}
	return nil
}

// — value marshaling —

// extMarshalCall builds the JSON request frame and collects binary slots.
func extMarshalCall(id int64, fn string, positional []runtime.Value, named map[string]runtime.Value) ([]byte, [][]byte, error) {
	var slots [][]byte

	marshalArgs := make([]interface{}, len(positional))
	for i, v := range positional {
		m, err := extMarshalValue(v, &slots)
		if err != nil {
			return nil, nil, err
		}
		marshalArgs[i] = m
	}

	marshalKwargs := map[string]interface{}{}
	for k, v := range named {
		m, err := extMarshalValue(v, &slots)
		if err != nil {
			return nil, nil, err
		}
		marshalKwargs[k] = m
	}

	req := map[string]interface{}{
		"id":     id,
		"fn":     fn,
		"args":   marshalArgs,
		"kwargs": marshalKwargs,
	}
	if len(slots) > 0 {
		req["slots"] = len(slots)
	}

	data, err := json.Marshal(req)
	return data, slots, err
}

// extMarshalValue converts a Geblang value to a JSON-serializable form.
// runtime.Bytes values are replaced with {"$type":"bytes","slot":N} and appended to slots.
func extMarshalValue(v runtime.Value, slots *[][]byte) (interface{}, error) {
	switch val := v.(type) {
	case runtime.Null:
		return nil, nil
	case runtime.Bool:
		return val.Value, nil
	case runtime.SmallInt:
		return val.Value, nil
	case runtime.Int:
		if val.Value == nil {
			return 0, nil
		}
		if val.Value.IsInt64() {
			return val.Value.Int64(), nil
		}
		return map[string]interface{}{"$type": "int", "value": val.Value.String()}, nil
	case runtime.Float:
		return val.Value, nil
	case runtime.Decimal:
		if val.Value == nil {
			return 0, nil
		}
		return map[string]interface{}{"$type": "decimal", "value": val.Value.RatString()}, nil
	case runtime.String:
		return val.Value, nil
	case runtime.Bytes:
		slot := len(*slots)
		*slots = append(*slots, val.Value)
		return map[string]interface{}{"$type": "bytes", "slot": slot}, nil
	case *runtime.List:
		arr := make([]interface{}, len(val.Elements))
		for i, elem := range val.Elements {
			m, err := extMarshalValue(elem, slots)
			if err != nil {
				return nil, err
			}
			arr[i] = m
		}
		return arr, nil
	case runtime.Dict:
		// Check for extension handle marker.
		if marker, ok := dictField(val, "$ext_handle"); ok {
			if id, ok := marker.(runtime.Int); ok && id.Value != nil {
				return map[string]interface{}{"$type": "handle", "id": id.Value.Int64()}, nil
			}
		}
		obj := map[string]interface{}{}
		for _, __dk := range val.EntryKeys() {
			entry, _ := val.GetEntry(__dk)
			k, ok := entry.Key.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("extension dict keys must be strings, got %s", entry.Key.TypeName())
			}
			m, err := extMarshalValue(entry.Value, slots)
			if err != nil {
				return nil, err
			}
			obj[k.Value] = m
		}
		return obj, nil
	default:
		return nil, fmt.Errorf("unsupported extension value type %s", v.TypeName())
	}
}

// extUnmarshalValue converts a JSON-decoded value back into a Geblang runtime value.
// Slot N is resolved from binarySlots[N].
func extUnmarshalValue(v interface{}, binarySlots [][]byte) (runtime.Value, error) {
	if v == nil {
		return runtime.Null{}, nil
	}
	switch val := v.(type) {
	case bool:
		return runtime.Bool{Value: val}, nil
	case float64:
		// JSON numbers decode as float64; use int if it's a whole number.
		if val >= float64(-1<<63) && val <= float64(1<<63-1) && val == float64(int64(val)) {
			return runtime.Int{Value: big.NewInt(int64(val))}, nil
		}
		return runtime.Float{Value: val}, nil
	case string:
		return runtime.String{Value: val}, nil
	case []interface{}:
		elems := make([]runtime.Value, len(val))
		for i, elem := range val {
			rv, err := extUnmarshalValue(elem, binarySlots)
			if err != nil {
				return nil, err
			}
			elems[i] = rv
		}
		return &runtime.List{Elements: elems}, nil
	case map[string]interface{}:
		// Check for special $type markers.
		if typ, ok := val["$type"].(string); ok {
			switch typ {
			case "bytes":
				slot, ok := val["slot"].(float64)
				if !ok || slot != float64(int(slot)) {
					return nil, fmt.Errorf("invalid bytes slot marker")
				}
				idx := int(slot)
				if idx < 0 || idx >= len(binarySlots) {
					return nil, fmt.Errorf("binary slot %d out of range", idx)
				}
				return runtime.Bytes{Value: binarySlots[idx]}, nil
			case "handle":
				id, ok := val["id"].(float64)
				if !ok || id != float64(int64(id)) {
					return nil, fmt.Errorf("invalid handle id marker")
				}
				return extHandleVal(int64(id)), nil
			case "int":
				text, ok := val["value"].(string)
				if !ok {
					return nil, fmt.Errorf("invalid int marker")
				}
				return runtime.NewIntLiteral(text)
			case "decimal":
				text, ok := val["value"].(string)
				if !ok {
					return nil, fmt.Errorf("invalid decimal marker")
				}
				return runtime.NewDecimalLiteral(text)
			}
		}
		entries := map[string]runtime.DictEntry{}
		for k, v := range val {
			rv, err := extUnmarshalValue(v, binarySlots)
			if err != nil {
				return nil, err
			}
			putDict(entries, k, rv)
		}
		return runtime.DictFromEntries(entries, nil), nil
	default:
		return runtime.String{Value: fmt.Sprintf("%v", v)}, nil
	}
}

// extHandleVal creates a Geblang dict value representing an opaque extension handle.
func extHandleVal(id int64) runtime.Value {
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "$ext_handle", runtime.Int{Value: big.NewInt(id)})
	putDict(entries, "id", runtime.NewInt64(id))
	return runtime.DictFromEntries(entries, nil)
}

// — response reading —

func extReadResponse(conn net.Conn, reqID int64, fnName string, maxResponseBytes int64) (runtime.Value, error) {
	// Read JSON frame.
	ftype, data, err := extReadFrameWithLimit(conn, maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("ext.call %q: read response: %w", fnName, err)
	}
	if ftype != frameTypeJSON {
		return nil, fmt.Errorf("ext.call %q: expected JSON response frame", fnName)
	}

	var resp struct {
		ID     int64           `json:"id"`
		OK     bool            `json:"ok"`
		Value  json.RawMessage `json:"value"`
		Error  string          `json:"error"`
		Detail string          `json:"detail"`
		Slots  int             `json:"slots"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("ext.call %q: parse response: %w", fnName, err)
	}
	if resp.ID != reqID {
		return nil, fmt.Errorf("ext.call %q: response ID mismatch (got %d, want %d)", fnName, resp.ID, reqID)
	}

	// Read binary slots if present.
	binarySlots := make([][]byte, resp.Slots)
	for i := range binarySlots {
		ft, blob, err := extReadFrameWithLimit(conn, maxResponseBytes)
		if err != nil {
			return nil, fmt.Errorf("ext.call %q: read binary slot %d: %w", fnName, i, err)
		}
		if ft != frameTypeBinary {
			return nil, fmt.Errorf("ext.call %q: expected binary frame for slot %d", fnName, i)
		}
		binarySlots[i] = blob
	}

	if !resp.OK {
		msg := resp.Error
		if resp.Detail != "" {
			msg += "\n" + resp.Detail
		}
		return nil, fmt.Errorf("ext.call %q: %s", fnName, msg)
	}

	if resp.Value == nil || string(resp.Value) == "null" {
		return runtime.Null{}, nil
	}

	var raw interface{}
	if err := json.Unmarshal(resp.Value, &raw); err != nil {
		return nil, fmt.Errorf("ext.call %q: decode value: %w", fnName, err)
	}
	return extUnmarshalValue(raw, binarySlots)
}

// — helpers —

func waitForUnixSocket(path string, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			conn, err := net.Dial("unix", path)
			if err == nil {
				return conn, nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("extension socket %q did not appear within %v", path, timeout)
}

// extConfig holds the config for a single extension from the package manifest.
type extConfig struct {
	Command          []string          `yaml:"command"`
	Socket           string            `yaml:"socket"`
	Host             string            `yaml:"host"`
	StartupTimeoutMs int               `yaml:"startup_timeout_ms"`
	Env              map[string]string `yaml:"env"`
	Root             string            `yaml:"-"`
}

func (e *Evaluator) findExtensionConfig(name string) (*extConfig, error) {
	searchRoots := append([]string(nil), e.modulePaths...)
	if cwd, err := os.Getwd(); err == nil {
		searchRoots = append(searchRoots, cwd)
	}
	seen := map[string]bool{}
	for _, root := range searchRoots {
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		manifest, err := e.findPackageManifest(root)
		if err != nil || manifest == nil {
			continue
		}
		cfg, ok := manifest.Extensions[name]
		if ok {
			cfg.Root = filepath.Dir(manifest.Path)
			return cfg, nil
		}
	}
	return nil, fmt.Errorf("no extension %q found; ext.load requires a geblang.yaml with an extensions section", name)
}
