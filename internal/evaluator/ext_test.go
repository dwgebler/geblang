package evaluator

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"geblang/internal/ast"
	"geblang/internal/runtime"
)

// — frame I/O helpers —

func TestExtFrameRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	data := []byte("hello world")
	if err := extWriteFrame(&buf, frameTypeJSON, data); err != nil {
		t.Fatalf("write: %v", err)
	}
	ftype, got, err := extReadFrame(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ftype != frameTypeJSON {
		t.Errorf("type: got %d, want %d", ftype, frameTypeJSON)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("payload: got %q, want %q", got, data)
	}
}

func TestExtFrameBinary(t *testing.T) {
	var buf bytes.Buffer
	blob := []byte{0x00, 0xFF, 0x10, 0x20}
	if err := extWriteFrame(&buf, frameTypeBinary, blob); err != nil {
		t.Fatalf("write: %v", err)
	}
	ftype, got, err := extReadFrame(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ftype != frameTypeBinary {
		t.Errorf("type: got %d, want %d", ftype, frameTypeBinary)
	}
	if !bytes.Equal(got, blob) {
		t.Errorf("payload mismatch")
	}
}

func TestExtFrameEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := extWriteFrame(&buf, frameTypeJSON, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Header: 4 bytes length (0) + 1 byte type
	if buf.Len() != 5 {
		t.Errorf("empty frame should be 5 bytes, got %d", buf.Len())
	}
	ftype, got, err := extReadFrame(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ftype != frameTypeJSON {
		t.Errorf("type mismatch")
	}
	if len(got) != 0 {
		t.Errorf("expected empty payload")
	}
}

type shortWriter struct {
	buf bytes.Buffer
	n   int
}

func (w *shortWriter) Write(data []byte) (int, error) {
	if len(data) > w.n {
		data = data[:w.n]
	}
	return w.buf.Write(data)
}

func TestExtWriteFrameHandlesShortWrites(t *testing.T) {
	writer := &shortWriter{n: 2}
	payload := []byte("abcdef")
	if err := extWriteFrame(writer, frameTypeJSON, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	ftype, got, err := extReadFrame(&writer.buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ftype != frameTypeJSON || !bytes.Equal(got, payload) {
		t.Fatalf("frame: got type=%d payload=%q", ftype, got)
	}
}

func TestExtReadFrameRejectsOversizedFrames(t *testing.T) {
	var buf bytes.Buffer
	var header [5]byte
	binary.BigEndian.PutUint32(header[:4], uint32(maxExtFrameSize+1))
	header[4] = frameTypeJSON
	buf.Write(header[:])
	_, _, err := extReadFrame(&buf)
	if err == nil || !strings.Contains(err.Error(), "frame too large") {
		t.Fatalf("error: got %v, want frame too large", err)
	}
}

func TestExtWriteFrameRejectsOversizedFrames(t *testing.T) {
	err := extWriteFrame(&bytes.Buffer{}, frameTypeJSON, make([]byte, maxExtFrameSize+1))
	if err == nil || !strings.Contains(err.Error(), "frame too large") {
		t.Fatalf("error: got %v, want frame too large", err)
	}
}

// — value marshaling —

func TestExtMarshalPrimitives(t *testing.T) {
	cases := []struct {
		in   runtime.Value
		want interface{}
	}{
		{runtime.Null{}, nil},
		{runtime.Bool{Value: true}, true},
		{runtime.Bool{Value: false}, false},
		{runtime.Int{Value: big.NewInt(42)}, int64(42)},
		{runtime.Float{Value: 3.14}, 3.14},
		{runtime.String{Value: "hello"}, "hello"},
	}
	for _, c := range cases {
		var slots [][]byte
		got, err := extMarshalValue(c.in, &slots)
		if err != nil {
			t.Errorf("marshal %T: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("marshal %T: got %v, want %v", c.in, got, c.want)
		}
		if len(slots) != 0 {
			t.Errorf("unexpected slots for %T", c.in)
		}
	}
}

func TestExtMarshalBytes(t *testing.T) {
	var slots [][]byte
	blob := []byte{1, 2, 3, 4}
	got, err := extMarshalValue(runtime.Bytes{Value: blob}, &slots)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", got)
	}
	if m["$type"] != "bytes" {
		t.Errorf("$type: got %v, want bytes", m["$type"])
	}
	if m["slot"].(int) != 0 {
		t.Errorf("slot: got %v, want 0", m["slot"])
	}
	if len(slots) != 1 || !bytes.Equal(slots[0], blob) {
		t.Errorf("binary slot mismatch")
	}
}

func TestExtMarshalExactNumbers(t *testing.T) {
	var slots [][]byte
	large := runtime.Int{Value: new(big.Int).Exp(big.NewInt(2), big.NewInt(100), nil)}
	got, err := extMarshalValue(large, &slots)
	if err != nil {
		t.Fatalf("marshal int: %v", err)
	}
	intMarker, ok := got.(map[string]interface{})
	if !ok || intMarker["$type"] != "int" || intMarker["value"] != large.Value.String() {
		t.Fatalf("int marker: %#v", got)
	}

	decimal, err := runtime.NewDecimalLiteral("1.23")
	if err != nil {
		t.Fatalf("decimal: %v", err)
	}
	got, err = extMarshalValue(decimal, &slots)
	if err != nil {
		t.Fatalf("marshal decimal: %v", err)
	}
	decimalMarker, ok := got.(map[string]interface{})
	if !ok || decimalMarker["$type"] != "decimal" || decimalMarker["value"] != decimal.Value.RatString() {
		t.Fatalf("decimal marker: %#v", got)
	}
}

func TestExtMarshalList(t *testing.T) {
	var slots [][]byte
	list := &runtime.List{Elements: []runtime.Value{
		runtime.Int{Value: big.NewInt(1)},
		runtime.String{Value: "two"},
	}}
	got, err := extMarshalValue(list, &slots)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	arr, ok := got.([]interface{})
	if !ok || len(arr) != 2 {
		t.Errorf("expected []interface{} len 2, got %T %v", got, got)
	}
}

func TestExtMarshalSmallInt(t *testing.T) {
	var slots [][]byte
	got, err := extMarshalValue(runtime.SmallInt{Value: 19}, &slots)
	if err != nil {
		t.Fatalf("marshal SmallInt: %v", err)
	}
	if n, ok := got.(int64); !ok || n != 19 {
		t.Fatalf("expected int64(19), got %T %v", got, got)
	}
}

func TestExtMarshalRejectsUnsupportedValues(t *testing.T) {
	var slots [][]byte
	_, err := extMarshalValue(runtime.Type{Name: "Example"}, &slots)
	if err == nil || !strings.Contains(err.Error(), "unsupported extension value type") {
		t.Fatalf("unsupported error: %v", err)
	}

	dict := runtime.DictFromEntries(map[string]runtime.DictEntry{
		dictKey(runtime.NewInt64(1)): {Key: runtime.NewInt64(1), Value: runtime.String{Value: "one"}},
	}, nil)
	_, err = extMarshalValue(dict, &slots)
	if err == nil || !strings.Contains(err.Error(), "dict keys must be strings") {
		t.Fatalf("dict key error: %v", err)
	}
}

// — value unmarshaling —

func TestExtUnmarshalPrimitives(t *testing.T) {
	cases := []struct {
		in   interface{}
		want runtime.Value
	}{
		{nil, runtime.Null{}},
		{true, runtime.Bool{Value: true}},
		{float64(42), runtime.Int{Value: big.NewInt(42)}},
		{float64(3.14), runtime.Float{Value: 3.14}},
		{"hello", runtime.String{Value: "hello"}},
	}
	for _, c := range cases {
		got, err := extUnmarshalValue(c.in, nil)
		if err != nil {
			t.Errorf("unmarshal %v: %v", c.in, err)
			continue
		}
		switch want := c.want.(type) {
		case runtime.Null:
			if _, ok := got.(runtime.Null); !ok {
				t.Errorf("expected Null, got %T", got)
			}
		case runtime.Bool:
			b, ok := got.(runtime.Bool)
			if !ok || b.Value != want.Value {
				t.Errorf("bool: got %v, want %v", got, want)
			}
		case runtime.Int:
			n, ok := got.(runtime.Int)
			if !ok || n.Value.Cmp(want.Value) != 0 {
				t.Errorf("int: got %v, want %v", got, want)
			}
		case runtime.Float:
			f, ok := got.(runtime.Float)
			if !ok || f.Value != want.Value {
				t.Errorf("float: got %v, want %v", got, want)
			}
		case runtime.String:
			s, ok := got.(runtime.String)
			if !ok || s.Value != want.Value {
				t.Errorf("string: got %v, want %v", got, want)
			}
		}
	}
}

func TestExtUnmarshalBytes(t *testing.T) {
	blob := []byte{0xAB, 0xCD}
	raw := map[string]interface{}{"$type": "bytes", "slot": float64(0)}
	got, err := extUnmarshalValue(raw, [][]byte{blob})
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	b, ok := got.(runtime.Bytes)
	if !ok {
		t.Fatalf("expected Bytes, got %T", got)
	}
	if !bytes.Equal(b.Value, blob) {
		t.Errorf("bytes mismatch")
	}
}

func TestExtUnmarshalHandle(t *testing.T) {
	raw := map[string]interface{}{"$type": "handle", "id": float64(99)}
	got, err := extUnmarshalValue(raw, nil)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	d, ok := got.(runtime.Dict)
	if !ok {
		t.Fatalf("expected Dict, got %T", got)
	}
	marker, ok := dictField(d, "$ext_handle")
	if !ok {
		t.Fatalf("missing $ext_handle field")
	}
	n, ok := marker.(runtime.Int)
	if !ok || n.Value.Int64() != 99 {
		t.Errorf("handle id: got %v, want 99", marker)
	}
}

func TestExtUnmarshalExactNumberMarkers(t *testing.T) {
	got, err := extUnmarshalValue(map[string]interface{}{"$type": "int", "value": "1267650600228229401496703205376"}, nil)
	if err != nil {
		t.Fatalf("int marker: %v", err)
	}
	if value, ok := got.(runtime.Int); !ok || value.Value.String() != "1267650600228229401496703205376" {
		t.Fatalf("int marker value: %#v", got)
	}

	got, err = extUnmarshalValue(map[string]interface{}{"$type": "decimal", "value": "1.23"}, nil)
	if err != nil {
		t.Fatalf("decimal marker: %v", err)
	}
	if value, ok := got.(runtime.Decimal); !ok || value.Value.RatString() != "123/100" {
		t.Fatalf("decimal marker value: %#v", got)
	}
}

func TestExtUnmarshalRejectsMalformedMarkers(t *testing.T) {
	_, err := extUnmarshalValue(map[string]interface{}{"$type": "bytes", "slot": "zero"}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid bytes slot marker") {
		t.Fatalf("bytes slot error: %v", err)
	}
	_, err = extUnmarshalValue(map[string]interface{}{"$type": "handle", "id": "one"}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid handle id marker") {
		t.Fatalf("handle id error: %v", err)
	}
	_, err = extUnmarshalValue(map[string]interface{}{"$type": "decimal", "value": "not-a-decimal"}, nil)
	if err == nil {
		t.Fatal("expected decimal marker error")
	}
}

// — marshal roundtrip via JSON —

func TestExtMarshalCallRoundtrip(t *testing.T) {
	positional := []runtime.Value{
		runtime.Int{Value: big.NewInt(3)},
		runtime.String{Value: "foo"},
	}
	named := map[string]runtime.Value{}
	data, slots, err := extMarshalCall(7, "myFn", positional, named)
	if err != nil {
		t.Fatalf("marshal call: %v", err)
	}
	if len(slots) != 0 {
		t.Errorf("expected no slots")
	}

	var req map[string]interface{}
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if req["id"].(float64) != 7 {
		t.Errorf("id: got %v", req["id"])
	}
	if req["fn"].(string) != "myFn" {
		t.Errorf("fn: got %v", req["fn"])
	}
	args := req["args"].([]interface{})
	if len(args) != 2 {
		t.Errorf("args len: got %d", len(args))
	}
}

func TestExtMarshalCallWithBinary(t *testing.T) {
	blob := []byte{1, 2, 3}
	positional := []runtime.Value{runtime.Bytes{Value: blob}}
	data, slots, err := extMarshalCall(1, "upload", positional, nil)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(slots) != 1 || !bytes.Equal(slots[0], blob) {
		t.Errorf("binary slot mismatch")
	}
	var req map[string]interface{}
	_ = json.Unmarshal(data, &req)
	if req["slots"].(float64) != 1 {
		t.Errorf("slots field: got %v", req["slots"])
	}
}

func TestExtCallWithOptionsPassesNamedArguments(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		ftype, data, err := extReadFrame(server)
		if err != nil || ftype != frameTypeJSON {
			return
		}
		var req struct {
			ID     int64                      `json:"id"`
			Fn     string                     `json:"fn"`
			Kwargs map[string]json.RawMessage `json:"kwargs"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return
		}
		var name string
		_ = json.Unmarshal(req.Kwargs["name"], &name)
		resp, _ := json.Marshal(map[string]interface{}{"id": req.ID, "ok": true, "value": "hello " + name})
		_ = extWriteFrame(server, frameTypeJSON, resp)
	}()

	e := New(io.Discard)
	handle := e.registerExtHandle(&extHandle{conn: client, functions: map[string]bool{"greet": true}})
	call := &ast.CallExpression{Arguments: []ast.CallArgument{
		{Value: &ast.Literal{}},
		{Value: &ast.Literal{}},
		{Value: &ast.Literal{}},
		{Name: &ast.Identifier{Value: "name"}, Value: &ast.Literal{}},
	}}
	result, err := e.extCallWithOptions(call, []runtime.Value{
		handle,
		runtime.String{Value: "greet"},
		runtime.DictFromEntries(map[string]runtime.DictEntry{}, nil),
		runtime.String{Value: "Geblang"},
	})
	if err != nil {
		t.Fatalf("callWithOptions: %v", err)
	}
	if got, ok := result.(runtime.String); !ok || got.Value != "hello Geblang" {
		t.Fatalf("result: %#v", result)
	}
}

func TestExtCallWithOptionsRejectsOversizedResponse(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		ftype, data, err := extReadFrame(server)
		if err != nil || ftype != frameTypeJSON {
			return
		}
		var req struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(data, &req)
		resp, _ := json.Marshal(map[string]interface{}{"id": req.ID, "ok": true, "value": strings.Repeat("x", 128)})
		_ = extWriteFrame(server, frameTypeJSON, resp)
	}()

	e := New(io.Discard)
	handle := e.registerExtHandle(&extHandle{conn: client, functions: map[string]bool{"large": true}})
	options := runtime.DictFromEntries(map[string]runtime.DictEntry{}, nil)
	putDict(options.Entries, "maxResponseBytes", runtime.NewInt64(16))
	_, err := e.extCallWithOptions(nil, []runtime.Value{handle, runtime.String{Value: "large"}, options})
	if err == nil || !strings.Contains(err.Error(), "frame too large") {
		t.Fatalf("error: got %v, want frame too large", err)
	}
}

func TestExtCallWithOptionsTimesOut(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		_, _, _ = extReadFrame(server)
		time.Sleep(100 * time.Millisecond)
	}()

	e := New(io.Discard)
	handle := e.registerExtHandle(&extHandle{conn: client, functions: map[string]bool{"slow": true}})
	options := runtime.DictFromEntries(map[string]runtime.DictEntry{}, nil)
	putDict(options.Entries, "timeoutMs", runtime.NewInt64(10))
	_, err := e.extCallWithOptions(nil, []runtime.Value{handle, runtime.String{Value: "slow"}, options})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// — integration test: real extension subprocess —

func TestExtIntegration(t *testing.T) {
	if os.Getenv("GEBLANG_EXT_INTEGRATION") == "" {
		t.Skip("set GEBLANG_EXT_INTEGRATION=1 to run extension integration tests")
	}

	// Start a minimal Go extension inline as a goroutine-based server.
	sockPath := t.TempDir() + "/test.sock"

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Serve one connection.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		serveTestExtension(conn)
	}()

	// Give the goroutine a moment.
	time.Sleep(10 * time.Millisecond)

	// Connect from the evaluator.
	e := New(os.Stdout)
	handle, err := e.extConnect(nil, []runtime.Value{runtime.String{Value: sockPath}})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Call "add" with two ints.
	result, err := e.extCall(nil, []runtime.Value{handle, runtime.String{Value: "add"},
		runtime.Int{Value: big.NewInt(10)}, runtime.Int{Value: big.NewInt(32)}})
	if err != nil {
		t.Fatalf("call add: %v", err)
	}
	n, ok := result.(runtime.Int)
	if !ok || n.Value.Int64() != 42 {
		t.Errorf("add: got %v, want 42", result)
	}

	// Call "echo" with bytes.
	blob := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	result, err = e.extCall(nil, []runtime.Value{handle, runtime.String{Value: "echo"},
		runtime.Bytes{Value: blob}})
	if err != nil {
		t.Fatalf("call echo: %v", err)
	}
	b, ok := result.(runtime.Bytes)
	if !ok || !bytes.Equal(b.Value, blob) {
		t.Errorf("echo: got %v", result)
	}

	if _, err := e.extClose(nil, []runtime.Value{handle}); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// serveTestExtension implements a minimal extension server for integration tests.
func serveTestExtension(conn net.Conn) {
	// Send handshake.
	hs, _ := json.Marshal(map[string]interface{}{
		"v": 1, "name": "testext", "functions": []string{"add", "echo"},
	})
	_ = extWriteFrame(conn, frameTypeJSON, hs)

	for {
		ftype, data, err := extReadFrame(conn)
		if err != nil {
			return
		}
		if ftype != frameTypeJSON {
			continue
		}

		var req struct {
			ID    int64             `json:"id"`
			Fn    string            `json:"fn"`
			Args  []json.RawMessage `json:"args"`
			Slots int               `json:"slots"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			continue
		}

		// Read binary slots.
		binarySlots := make([][]byte, req.Slots)
		for i := range binarySlots {
			_, blob, _ := extReadFrame(conn)
			binarySlots[i] = blob
		}

		switch req.Fn {
		case "__shutdown__":
			return
		case "add":
			var a, b float64
			_ = json.Unmarshal(req.Args[0], &a)
			_ = json.Unmarshal(req.Args[1], &b)
			resp, _ := json.Marshal(map[string]interface{}{"id": req.ID, "ok": true, "value": a + b})
			_ = extWriteFrame(conn, frameTypeJSON, resp)
		case "echo":
			// Echo back binary data.
			var slotRef map[string]interface{}
			_ = json.Unmarshal(req.Args[0], &slotRef)
			slot := int(slotRef["slot"].(float64))
			blob := binarySlots[slot]
			resp, _ := json.Marshal(map[string]interface{}{
				"id": req.ID, "ok": true,
				"value": map[string]interface{}{"$type": "bytes", "slot": 0},
				"slots": 1,
			})
			_ = extWriteFrame(conn, frameTypeJSON, resp)
			_ = extWriteFrame(conn, frameTypeBinary, blob)
		default:
			resp, _ := json.Marshal(map[string]interface{}{
				"id": req.ID, "ok": false, "error": "unknown function: " + req.Fn,
			})
			_ = extWriteFrame(conn, frameTypeJSON, resp)
		}
	}
}

// — frame header format test —

func TestExtFrameHeaderFormat(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("test")
	_ = extWriteFrame(&buf, 0x01, payload)
	raw := buf.Bytes()
	if len(raw) != 9 {
		t.Fatalf("expected 9 bytes (4+1+4), got %d", len(raw))
	}
	length := binary.BigEndian.Uint32(raw[:4])
	if length != 4 {
		t.Errorf("length: got %d, want 4", length)
	}
	if raw[4] != 0x01 {
		t.Errorf("type byte: got %x, want 01", raw[4])
	}
}

func TestExtUnixSocketAddressDetection(t *testing.T) {
	cases := map[string]bool{
		"/tmp/ext.sock":    true,
		"./ext.sock":       true,
		"../run/ext.sock":  true,
		"run/ext.sock":     true,
		"127.0.0.1:9000":   false,
		"localhost:9000":   false,
		"extension.sock":   true,
		"not/a/tcp/target": true,
	}
	for address, want := range cases {
		if got := isUnixSocketAddress(address); got != want {
			t.Fatalf("%s: got %v, want %v", address, got, want)
		}
	}
}
