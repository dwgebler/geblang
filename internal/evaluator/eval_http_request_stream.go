package evaluator

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
)

// httpResponseStreamHandle keeps a response body open for line-at-a-time reads (read() yields lines, null at EOF).
type httpResponseStreamHandle struct {
	resp   *http.Response
	reader *bufio.Reader
	closed bool
}

func (h *httpResponseStreamHandle) closeBody() {
	if !h.closed {
		h.closed = true
		_ = h.resp.Body.Close()
	}
}

// closeResponseStream closes the body and drops the handle so it is not retained for the program's life. Caller holds httpResponseStreamMu.
func (e *Evaluator) closeResponseStream(this *runtime.Instance, h *httpResponseStreamHandle) {
	h.closeBody()
	if id, ok := this.Fields["handle"].(runtime.Int); ok {
		delete(e.httpResponseStreams, id.Value.Int64())
	}
}

func (e *Evaluator) httpRequestStream(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	options, ok := args[0].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s options must be dict", call.Callee.String())
	}
	method := "GET"
	if v, ok := dictStringField(options, "method"); ok {
		method = v
	}
	url, ok := dictStringField(options, "url")
	if !ok {
		return nil, fmt.Errorf("%s options.url is required", call.Callee.String())
	}
	var bodyReader io.Reader
	if v, found := dictField(options, "body"); found {
		var err error
		bodyReader, err = e.httpBodyReader(v, call.Callee.String())
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	if hv, ok := dictField(options, "headers"); ok {
		if err := applyRequestHeaders(call, req, hv); err != nil {
			return nil, err
		}
	}
	timeout := 0
	if v, ok := dictField(options, "timeoutMs"); ok {
		n, ok := native.AsInt64(v)
		if !ok {
			return nil, fmt.Errorf("%s options.timeoutMs must be int", call.Callee.String())
		}
		timeout = int(n)
	}
	applyDefaultUserAgent(req)
	client := http.DefaultClient
	if timeout > 0 {
		client = &http.Client{Timeout: time.Duration(timeout) * time.Millisecond}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	handle := &httpResponseStreamHandle{resp: resp, reader: bufio.NewReader(resp.Body)}
	e.httpResponseStreamMu.Lock()
	id := e.nextResponseStreamID
	e.nextResponseStreamID++
	ensureMap(&e.httpResponseStreams)[id] = handle
	e.httpResponseStreamMu.Unlock()
	return &runtime.Instance{Class: e.responseStreamClass(), Fields: map[string]runtime.Value{"handle": runtime.NewInt64(id)}}, nil
}

func (e *Evaluator) responseStreamClass() *runtime.Class {
	if e.httpStreamRespClass != nil {
		return e.httpStreamRespClass
	}
	get := func(this *runtime.Instance) (*httpResponseStreamHandle, error) {
		id, ok := this.Fields["handle"].(runtime.Int)
		if !ok {
			return nil, fmt.Errorf("invalid stream response handle")
		}
		e.httpResponseStreamMu.Lock()
		defer e.httpResponseStreamMu.Unlock()
		h, ok := e.httpResponseStreams[id.Value.Int64()]
		if !ok {
			return nil, fmt.Errorf("stream response handle not found")
		}
		return h, nil
	}
	cls := &runtime.Class{Name: "StreamResponse", Module: "http", Fields: []runtime.Field{{Name: "handle"}}, Methods: map[string][]runtime.Function{}}
	cls.Methods["read"] = []runtime.Function{{Name: "read", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		h, err := get(this)
		if err != nil {
			return runtime.Null{}, nil
		}
		e.httpResponseStreamMu.Lock()
		defer e.httpResponseStreamMu.Unlock()
		if h.closed {
			return runtime.Null{}, nil
		}
		line, rerr := h.reader.ReadString('\n')
		if len(line) == 0 {
			e.closeResponseStream(this, h)
			if rerr == nil || errors.Is(rerr, io.EOF) {
				return runtime.Null{}, nil
			}
			// A non-EOF read error (connection drop mid-stream) must surface, not look like a clean end.
			return nil, fmt.Errorf("http stream read: %w", rerr)
		}
		return runtime.String{Value: strings.TrimRight(line, "\r\n")}, nil
	}}}
	cls.Methods["status"] = []runtime.Function{{Name: "status", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		h, err := get(this)
		if err != nil {
			return nil, err
		}
		return runtime.NewInt64(int64(h.resp.StatusCode)), nil
	}}}
	cls.Methods["headers"] = []runtime.Function{{Name: "headers", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		h, err := get(this)
		if err != nil {
			return nil, err
		}
		entries := map[string]runtime.DictEntry{}
		for name, values := range h.resp.Header {
			key := runtime.String{Value: name}
			entries[dictKey(key)] = runtime.DictEntry{Key: key, Value: runtime.String{Value: strings.Join(values, ",")}}
		}
		return runtime.DictFromEntries(entries, nil), nil
	}}}
	cls.Methods["done"] = []runtime.Function{{Name: "done", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		h, err := get(this)
		if err != nil {
			return runtime.Bool{Value: true}, nil
		}
		e.httpResponseStreamMu.Lock()
		defer e.httpResponseStreamMu.Unlock()
		return runtime.Bool{Value: h.closed}, nil
	}}}
	cls.Methods["close"] = []runtime.Function{{Name: "close", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		h, err := get(this)
		if err != nil {
			return runtime.Null{}, nil
		}
		e.httpResponseStreamMu.Lock()
		defer e.httpResponseStreamMu.Unlock()
		e.closeResponseStream(this, h)
		return runtime.Null{}, nil
	}}}
	e.httpStreamRespClass = cls
	return cls
}
