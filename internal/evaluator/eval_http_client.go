package evaluator

import (
	"bytes"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"io"
	mrand "math/rand"
	"net/http"
	"net/http/cookiejar"
	neturl "net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// batch helpers so a Builder can be passed directly to fetchAll.
func httpRequestFromBuilder(this *runtime.Instance) (*http.Request, *http.Client, error) {
	urlVal, ok := this.Fields["_url"].(runtime.String)
	if !ok {
		return nil, nil, fmt.Errorf("Builder: url is not set")
	}
	method := "GET"
	if m, ok := this.Fields["_method"].(runtime.String); ok {
		method = m.Value
	}
	finalURL, err := applyBuilderQuery(urlVal.Value, this.Fields["_query"])
	if err != nil {
		return nil, nil, err
	}
	var req *http.Request
	if pathVal, ok := this.Fields["_bodyFile"].(runtime.String); ok {
		// Stream the file as the body; the http client closes it after Do.
		file, err := os.Open(pathVal.Value)
		if err != nil {
			return nil, nil, fmt.Errorf("withBodyFile: %w", err)
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, nil, fmt.Errorf("withBodyFile: %w", err)
		}
		req, err = http.NewRequest(method, finalURL, file)
		if err != nil {
			file.Close()
			return nil, nil, err
		}
		req.ContentLength = info.Size()
	} else {
		body := ""
		if b, ok := this.Fields["_body"].(runtime.String); ok {
			body = b.Value
		}
		req, err = http.NewRequest(method, finalURL, strings.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
	}
	if hdrs, ok := this.Fields["_headers"].(runtime.HTTPHeaders); ok {
		for key, vals := range hdrs.Values {
			for i, v := range vals {
				if i == 0 {
					req.Header.Set(key, v)
				} else {
					req.Header.Add(key, v)
				}
			}
		}
	}
	client := http.DefaultClient
	if msField, ok := this.Fields["_timeoutMs"]; ok {
		if ms, ok := native.AsInt64(msField); ok {
			client = &http.Client{Timeout: time.Duration(ms) * time.Millisecond}
		}
	}
	return req, client, nil
}

// applyBuilderQuery appends builder query pairs to a URL. The query value
// is a list of [name, value] pairs captured by Builder.withQuery.
func applyBuilderQuery(rawURL string, queryVal runtime.Value) (string, error) {
	list, ok := queryVal.(*runtime.List)
	if !ok || len(list.Elements) == 0 {
		return rawURL, nil
	}
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := parsed.Query()
	for _, el := range list.Elements {
		pair, ok := el.(*runtime.List)
		if !ok || len(pair.Elements) != 2 {
			continue
		}
		name, _ := pair.Elements[0].(runtime.String)
		val, _ := pair.Elements[1].(runtime.String)
		q.Add(name.Value, val.Value)
	}
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

// httpBodyReader converts a Geblang body value into an io.Reader the
// HTTP client can consume. Strings and bytes use in-memory readers
// (Content-Length is set automatically by net/http). An IOStream
// hands back its underlying reader, which produces chunked
// transfer-encoding for unknown-length bodies. Returns (nil, nil) for
// runtime.Null so callers can build requests without a body.
func (e *Evaluator) httpBodyReader(value runtime.Value, label string) (io.Reader, error) {
	switch v := value.(type) {
	case nil, runtime.Null:
		return nil, nil
	case runtime.String:
		return strings.NewReader(v.Value), nil
	case runtime.Bytes:
		return bytes.NewReader(v.Value), nil
	case runtime.NativeObject:
		if v.Kind == "IOStream" || v.Kind == "IOCapture" {
			handle, err := e.ioStreamHandle(v)
			if err != nil {
				return nil, err
			}
			if handle.reader == nil {
				return nil, fmt.Errorf("%s body stream is not readable", label)
			}
			if handle.bufReader != nil {
				return handle.bufReader, nil
			}
			return handle.reader, nil
		}
	case runtime.Int, runtime.SmallInt:
		// File handle from io.open. Read straight from the underlying
		// file - the HTTP client closes its own copy of the reader
		// when the request finishes, but the file handle stays in
		// e.files until the user calls io.close.
		file, err := e.fileHandle(value)
		if err != nil {
			return nil, fmt.Errorf("%s %w", label, err)
		}
		return file, nil
	case *runtime.Instance:
		// streams.IOStream (and subclasses) store the native handle
		// in a `handle` field; recurse on that so the caller can
		// pass the friendly OO wrapper directly.
		if v != nil && v.Class != nil {
			if inner, ok := v.Fields["handle"]; ok {
				return e.httpBodyReader(inner, label)
			}
		}
	}
	return nil, fmt.Errorf("%s body must be string, bytes, or IOStream (got %T)", label, value)
}

func (e *Evaluator) httpRequest(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	// One-arg form starts a fluent request builder; the three/four-arg
	// form issues a request directly.
	if len(args) == 1 {
		return e.httpBuild(call, args)
	}
	if len(args) != 3 && len(args) != 4 {
		return nil, fmt.Errorf("%s expects a url, or method, url, body and optional headers", call.Callee.String())
	}
	method, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s method must be string", call.Callee.String())
	}
	url, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s URL must be string", call.Callee.String())
	}
	bodyReader, err := e.httpBodyReader(args[2], call.Callee.String())
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method.Value, url.Value, bodyReader)
	if err != nil {
		return nil, err
	}
	if len(args) == 4 {
		if err := applyRequestHeaders(call, req, args[3]); err != nil {
			return nil, err
		}
	}
	return doHTTPRequest(http.DefaultClient, req)
}

func httpGet(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("%s expects url and optional headers", call.Callee.String())
	}
	url, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s URL must be string", call.Callee.String())
	}
	req, err := http.NewRequest(http.MethodGet, url.Value, nil)
	if err != nil {
		return nil, err
	}
	if len(args) == 2 {
		if err := applyRequestHeaders(call, req, args[1]); err != nil {
			return nil, err
		}
	}
	return doHTTPRequest(http.DefaultClient, req)
}

func (e *Evaluator) httpPost(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 && len(args) != 3 {
		return nil, fmt.Errorf("%s expects url, body, and optional headers", call.Callee.String())
	}
	url, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s URL must be string", call.Callee.String())
	}
	bodyReader, err := e.httpBodyReader(args[1], call.Callee.String())
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url.Value, bodyReader)
	if err != nil {
		return nil, err
	}
	if len(args) == 3 {
		if err := applyRequestHeaders(call, req, args[2]); err != nil {
			return nil, err
		}
	}
	return doHTTPRequest(http.DefaultClient, req)
}

func (e *Evaluator) httpPostJSON(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 && len(args) != 3 {
		return nil, fmt.Errorf("%s expects url, value, and optional headers", call.Callee.String())
	}
	url, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s URL must be string", call.Callee.String())
	}
	body, err := e.jsonStringFromValue(args[1])
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url.Value, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if len(args) == 3 {
		if err := applyRequestHeaders(call, req, args[2]); err != nil {
			return nil, err
		}
	}
	return doHTTPRequest(http.DefaultClient, req)
}

func httpParseJSON(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects response dict", call.Callee.String())
	}
	var body string
	switch response := args[0].(type) {
	case runtime.Dict:
		text, ok := dictStringField(response, "body")
		if !ok {
			return nil, fmt.Errorf("%s response.body must be string", call.Callee.String())
		}
		body = text
	case *runtime.Instance:
		body = responseBodyText(response)
	default:
		return nil, fmt.Errorf("%s response must be a Response or dict", call.Callee.String())
	}
	value, parseErr := native.ParseJSONText(body)
	if parseErr != nil {
		return nil, fmt.Errorf("%s", parseErr.Message)
	}
	return value, nil
}

func applyRequestHeaders(call *ast.CallExpression, req *http.Request, value runtime.Value) error {
	headers, ok := httpHeaderValue(value)
	if !ok {
		return fmt.Errorf("%s headers must be dict or http.Headers", call.Callee.String())
	}
	for key, values := range headers.Values {
		for i, value := range values {
			if i == 0 {
				req.Header.Set(key, value)
			} else {
				req.Header.Add(key, value)
			}
		}
	}
	return nil
}

func (e *Evaluator) httpRequestWithOptions(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	options, ok := args[0].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s options must be dict", call.Callee.String())
	}
	method := "GET"
	if value, ok := dictStringField(options, "method"); ok {
		method = value
	}
	url, ok := dictStringField(options, "url")
	if !ok {
		return nil, fmt.Errorf("%s options.url is required", call.Callee.String())
	}
	var bodyReader io.Reader
	if value, found := dictField(options, "body"); found {
		var err error
		bodyReader, err = e.httpBodyReader(value, call.Callee.String())
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	if headersValue, ok := dictField(options, "headers"); ok {
		if err := applyRequestHeaders(call, req, headersValue); err != nil {
			return nil, err
		}
	}
	timeout := 0
	if value, ok := dictField(options, "timeoutMs"); ok {
		n, ok := native.AsInt64(value)
		if !ok {
			return nil, fmt.Errorf("%s options.timeoutMs must be int", call.Callee.String())
		}
		timeout = int(n)
	}
	client := http.DefaultClient
	if timeout > 0 {
		client = &http.Client{Timeout: time.Duration(timeout) * time.Millisecond}
	}
	return doHTTPRequest(client, req)
}

// DefaultHTTPUserAgent is the User-Agent header value Geblang sets on
// every outgoing HTTP request that doesn't already specify one. Avoid
// the Go runtime's default "Go-http-client/1.1" so servers can identify
// Geblang traffic and so security/firewall filters can allow-list it.
const DefaultHTTPUserAgent = "Geblang/1.0"

// applyDefaultUserAgent sets the User-Agent header on req to the
// Geblang default if and only if no User-Agent has been set yet. Caller
// supplied headers always win.
func applyDefaultUserAgent(req *http.Request) {
	if req == nil || req.Header == nil {
		return
	}
	if req.Header.Get("User-Agent") != "" {
		return
	}
	req.Header.Set("User-Agent", DefaultHTTPUserAgent)
}

// httpResponseResultClass is the shared Response class used to build
// client results. Captured once at evaluator setup; child evaluators
// share the same pointer so dispatch is identity-safe.
var (
	httpResponseResultClass     *runtime.Class
	httpResponseResultClassOnce sync.Once
)

func doHTTPRequest(client *http.Client, req *http.Request) (runtime.Value, error) {
	applyDefaultUserAgent(req)
	return doHTTPRequestWithRetries(client, req, httpRetryOptions{})
}

// httpRetryOptions describes the retry behaviour of a single request. A
// zero value disables retries entirely (the request runs once). Network
// errors are always retried up to attempts; only the listed status codes
// trigger a retry for HTTP responses.
type httpRetryOptions struct {
	attempts        int   // total attempts (1 means no retry)
	backoffMs       int64 // base backoff between retries
	backoffMaxMs    int64 // upper bound on a single sleep
	retryStatuses   map[int]struct{}
	hasCustomStatus bool
}

func defaultRetryStatuses() map[int]struct{} {
	return map[int]struct{}{502: {}, 503: {}, 504: {}, 429: {}}
}

// doHTTPRequestWithRetries wraps doHTTPRequest with attempt looping and
// exponential backoff plus full jitter. The request body is buffered up
// front so it can be re-sent on each attempt.
func doHTTPRequestWithRetries(client *http.Client, req *http.Request, opts httpRetryOptions) (runtime.Value, error) {
	attempts := opts.attempts
	if attempts < 1 {
		attempts = 1
	}
	statuses := opts.retryStatuses
	if statuses == nil {
		statuses = defaultRetryStatuses()
	}
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	resetBody := func() {
		if bodyBytes == nil {
			return
		}
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))
	}
	resetBody()
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < attempts {
				httpRetrySleep(opts, attempt)
				resetBody()
				continue
			}
			return nil, err
		}
		responseBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt < attempts {
				httpRetrySleep(opts, attempt)
				resetBody()
				continue
			}
			return nil, readErr
		}
		_, shouldRetry := statuses[resp.StatusCode]
		if shouldRetry && attempt < attempts {
			httpRetrySleep(opts, attempt)
			resetBody()
			continue
		}
		headers := map[string]runtime.DictEntry{}
		for name, values := range resp.Header {
			key := runtime.String{Value: name}
			headers[dictKey(key)] = runtime.DictEntry{Key: key, Value: runtime.String{Value: strings.Join(values, ",")}}
		}
		status := runtime.NewInt64(int64(resp.StatusCode))
		body := runtime.String{Value: string(responseBody)}
		headerDict := runtime.Dict{Entries: headers}
		finalURL := req.URL.String()
		if resp.Request != nil && resp.Request.URL != nil {
			finalURL = resp.Request.URL.String()
		}
		urlVal := runtime.String{Value: finalURL}
		if httpResponseResultClass != nil {
			return newResponseInstance(httpResponseResultClass, status, body, headerDict, urlVal), nil
		}
		entries := map[string]runtime.DictEntry{}
		putDict(entries, "status", status)
		putDict(entries, "body", body)
		putDict(entries, "headers", headerDict)
		putDict(entries, "url", urlVal)
		return runtime.Dict{Entries: entries}, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("http request: exhausted %d attempts", attempts)
}

// httpRetrySleep waits before retry N (1-indexed). Sleep is exponential
// in the base backoff with full jitter, capped at backoffMaxMs.
func httpRetrySleep(opts httpRetryOptions, attempt int) {
	base := opts.backoffMs
	if base <= 0 {
		base = 100
	}
	max := opts.backoffMaxMs
	if max <= 0 {
		max = 5000
	}
	// Exponential growth: base, 2*base, 4*base, ...
	delay := base
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay > max {
			delay = max
			break
		}
	}
	// Full jitter: uniform in [0, delay).
	jitter := mrand.Int63n(delay + 1)
	time.Sleep(time.Duration(jitter) * time.Millisecond)
}

// httpRetryOptionsFromDict reads retries / retryBackoffMs /
// retryBackoffMaxMs / retryStatuses from an options dict. Missing keys
// leave the zero-value (which disables retries).
func httpRetryOptionsFromDict(opts runtime.Dict) (httpRetryOptions, error) {
	out := httpRetryOptions{}
	if v, ok := dictField(opts, "retries"); ok {
		if n, ok := toInt64(v); ok {
			out.attempts = int(n)
		}
	}
	if v, ok := dictField(opts, "retryBackoffMs"); ok {
		if n, ok := toInt64(v); ok {
			out.backoffMs = n
		}
	}
	if v, ok := dictField(opts, "retryBackoffMaxMs"); ok {
		if n, ok := toInt64(v); ok {
			out.backoffMaxMs = n
		}
	}
	if v, ok := dictField(opts, "retryStatuses"); ok {
		list, ok := v.(*runtime.List)
		if !ok {
			return out, fmt.Errorf("retryStatuses must be list<int>")
		}
		statuses := map[int]struct{}{}
		for _, elem := range list.Elements {
			n, ok := toInt64(elem)
			if !ok {
				return out, fmt.Errorf("retryStatuses must be list<int>")
			}
			statuses[int(n)] = struct{}{}
		}
		out.retryStatuses = statuses
		out.hasCustomStatus = true
	}
	return out, nil
}

func (e *Evaluator) websocketConnect(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("%s expects url and optional headers", call.Callee.String())
	}
	url, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s URL must be string", call.Callee.String())
	}
	headers := http.Header{}
	if len(args) == 2 {
		headerDict, ok := args[1].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s headers must be dict<string, string>", call.Callee.String())
		}
		for _, __dk := range headerDict.EntryKeys() {
			entry, _ := headerDict.GetEntry(__dk)
			key, keyOK := entry.Key.(runtime.String)
			value, valueOK := entry.Value.(runtime.String)
			if !keyOK || !valueOK {
				return nil, fmt.Errorf("%s headers must be dict<string, string>", call.Callee.String())
			}
			headers.Set(key.Value, value.Value)
		}
	}
	conn, _, err := websocket.DefaultDialer.Dial(url.Value, headers)
	if err != nil {
		return nil, err
	}
	return e.registerWebSocket(conn), nil
}

func websocketUpgradeResponse(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects handler", call.Callee.String())
	}
	handler, ok := args[0].(runtime.Function)
	if !ok {
		return nil, fmt.Errorf("%s handler must be function", call.Callee.String())
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "websocket", handler)
	return runtime.Dict{Entries: entries}, nil
}

func httpStreamResponse(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects handler", call.Callee.String())
	}
	handler, ok := args[0].(runtime.Function)
	if !ok {
		return nil, fmt.Errorf("%s handler must be function", call.Callee.String())
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "stream", handler)
	return runtime.Dict{Entries: entries}, nil
}

type wsHandle struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func (h *wsHandle) writeMessage(messageType int, data []byte) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	return h.conn.WriteMessage(messageType, data)
}

func (e *Evaluator) registerWebSocket(conn *websocket.Conn) runtime.Value {
	e.wsMu.Lock()
	defer e.wsMu.Unlock()
	e.nextWSID++
	ensureMap(&e.websockets)[e.nextWSID] = &wsHandle{conn: conn}
	return runtime.NewInt64(e.nextWSID)
}

func (e *Evaluator) registerHTTPStream(w http.ResponseWriter, flusher http.Flusher) runtime.Value {
	e.httpStreamMu.Lock()
	defer e.httpStreamMu.Unlock()
	e.nextHTTPStreamID++
	ensureMap(&e.httpStreams)[e.nextHTTPStreamID] = &httpStreamHandle{writer: w, flusher: flusher}
	return runtime.NewInt64(e.nextHTTPStreamID)
}

func (e *Evaluator) httpStreamHandle(value runtime.Value) (*httpStreamHandle, error) {
	id, err := httpStreamHandleID(value)
	if err != nil {
		return nil, err
	}
	e.httpStreamMu.Lock()
	handle, ok := e.httpStreams[id]
	e.httpStreamMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.httpStreamHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown http stream handle %d", id)
	}
	return handle, nil
}

func (e *Evaluator) closeHTTPStreamID(id int64) error {
	e.httpStreamMu.Lock()
	handle, ok := e.httpStreams[id]
	if ok {
		handle.closed = true
		delete(e.httpStreams, id)
	}
	e.httpStreamMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.closeHTTPStreamID(id)
	}
	if !ok {
		return fmt.Errorf("unknown http stream handle %d", id)
	}
	return nil
}

func httpStreamHandleID(value runtime.Value) (int64, error) {
	handle, ok := value.(runtime.Int)
	if !ok || !handle.Value.IsInt64() {
		return 0, fmt.Errorf("stream must be http stream handle")
	}
	return handle.Value.Int64(), nil
}

func (e *Evaluator) httpStreamWrite(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects stream and text", call.Callee.String())
	}
	handle, err := e.httpStreamHandle(args[0])
	if err != nil {
		return nil, err
	}
	if handle.closed {
		return nil, fmt.Errorf("http stream is closed")
	}
	text, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s text must be string", call.Callee.String())
	}
	_, err = io.WriteString(handle.writer, text.Value)
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) httpStreamFlush(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects stream", call.Callee.String())
	}
	handle, err := e.httpStreamHandle(args[0])
	if err != nil {
		return nil, err
	}
	if handle.flusher != nil && !handle.closed {
		handle.flusher.Flush()
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) httpStreamClose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects stream", call.Callee.String())
	}
	id, err := httpStreamHandleID(args[0])
	if err != nil {
		return nil, err
	}
	_ = e.closeHTTPStreamID(id)
	return runtime.Null{}, nil
}

func (e *Evaluator) websocketSendText(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects connection and text", call.Callee.String())
	}
	h, err := e.websocketHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	text, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s text must be string", call.Callee.String())
	}
	if err := h.writeMessage(websocket.TextMessage, []byte(text.Value)); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) websocketReadText(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects connection", call.Callee.String())
	}
	h, err := e.websocketHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	messageType, data, err := h.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	if messageType != websocket.TextMessage {
		return nil, fmt.Errorf("%s received non-text message", call.Callee.String())
	}
	return runtime.String{Value: string(data)}, nil
}

func (e *Evaluator) websocketSendBytes(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects connection and bytes", call.Callee.String())
	}
	h, err := e.websocketHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	data, ok := args[1].(runtime.Bytes)
	if !ok {
		return nil, fmt.Errorf("%s data must be bytes", call.Callee.String())
	}
	if err := h.writeMessage(websocket.BinaryMessage, data.Value); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) websocketReadBytes(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects connection", call.Callee.String())
	}
	h, err := e.websocketHandle(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	_, data, err := h.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	return runtime.Bytes{Value: data}, nil
}

func (e *Evaluator) websocketClose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects connection", call.Callee.String())
	}
	id, err := websocketHandleID(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %w", call.Callee.String(), err)
	}
	return runtime.Null{}, e.closeWebSocketID(id)
}

func (e *Evaluator) closeWebSocketID(id int64) error {
	e.wsMu.Lock()
	h, ok := e.websockets[id]
	if ok {
		delete(e.websockets, id)
	}
	e.wsMu.Unlock()
	if !ok {
		return fmt.Errorf("unknown websocket connection %d", id)
	}
	return h.conn.Close()
}

func (e *Evaluator) websocketHandle(value runtime.Value) (*wsHandle, error) {
	id, err := websocketHandleID(value)
	if err != nil {
		return nil, err
	}
	e.wsMu.Lock()
	h, ok := e.websockets[id]
	e.wsMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.websocketHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown websocket connection %d", id)
	}
	return h, nil
}

func websocketHandleID(value runtime.Value) (int64, error) {
	handle, ok := value.(runtime.Int)
	if !ok || !handle.Value.IsInt64() {
		return 0, fmt.Errorf("connection must be websocket handle")
	}
	return handle.Value.Int64(), nil
}

func (e *Evaluator) httpNewClient(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s expects optional options dict", call.Callee.String())
	}
	h := &httpClientHandle{
		client:  &http.Client{},
		headers: http.Header{},
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transportChanged := false
	if len(args) == 1 {
		opts, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s options must be dict", call.Callee.String())
		}
		if v, ok := dictField(opts, "timeoutMs"); ok {
			if ms, ok := toInt64(v); ok {
				h.client.Timeout = time.Duration(ms) * time.Millisecond
			}
		}
		if base, ok := dictStringField(opts, "baseUrl"); ok {
			h.baseURL = base
		}
		if hdrsVal, ok := dictField(opts, "headers"); ok {
			if hdrs, ok := httpHeaderValue(hdrsVal); ok {
				for k, vals := range hdrs.Values {
					for _, v := range vals {
						h.headers.Add(k, v)
					}
				}
			}
		}
		if jarVal, ok := dictField(opts, "cookieJar"); ok {
			jar, err := e.cookieJarFromValue(jarVal, call.Callee.String())
			if err != nil {
				return nil, err
			}
			h.client.Jar = jar
		}
		if keepAlive, ok := dictBoolField(opts, "keepAlive"); ok {
			transport.DisableKeepAlives = !keepAlive
			transportChanged = true
		}
		if maxIdle, ok := dictField(opts, "maxIdleConns"); ok {
			if n, ok := toInt64(maxIdle); ok {
				transport.MaxIdleConns = int(n)
				transport.MaxIdleConnsPerHost = int(n)
				transportChanged = true
			}
		}
		if proxyURL, ok := dictStringField(opts, "proxy"); ok {
			parsed, err := neturl.Parse(proxyURL)
			if err != nil {
				return nil, fmt.Errorf("%s proxy: %v", call.Callee.String(), err)
			}
			transport.Proxy = http.ProxyURL(parsed)
			transportChanged = true
		} else if useEnv, ok := dictBoolField(opts, "proxyFromEnv"); ok && useEnv {
			transport.Proxy = http.ProxyFromEnvironment
			transportChanged = true
		}
		if tlsVal, ok := dictField(opts, "tls"); ok {
			cfg, err := buildHTTPClientTLSConfig(tlsVal, call.Callee.String())
			if err != nil {
				return nil, err
			}
			if cfg != nil {
				transport.TLSClientConfig = cfg
				transportChanged = true
			}
		}
	}
	if transportChanged {
		h.client.Transport = transport
	}
	e.httpClientMu.Lock()
	id := e.nextHTTPClientID
	e.nextHTTPClientID++
	ensureMap(&e.httpClientHandles)[id] = h
	e.httpClientMu.Unlock()
	if e.httpClientClass == nil {
		return nil, fmt.Errorf("Client class is not initialized")
	}
	return &runtime.Instance{Class: e.httpClientClass, Fields: map[string]runtime.Value{"handle": runtime.NewInt64(id)}}, nil
}

// cookieJarFromValue resolves a cookieJar argument: either a CookieJar
// instance (created via http.newCookieJar()) or the bare value `true` to
// auto-create a fresh jar inline.
func (e *Evaluator) cookieJarFromValue(v runtime.Value, label string) (http.CookieJar, error) {
	if b, ok := v.(runtime.Bool); ok {
		if !b.Value {
			return nil, nil
		}
		jar, _ := cookiejar.New(nil)
		return jar, nil
	}
	inst, ok := v.(*runtime.Instance)
	if !ok || inst.Class == nil || inst.Class.Name != "CookieJar" {
		return nil, fmt.Errorf("%s cookieJar must be an http.CookieJar instance or true", label)
	}
	id, ok := inst.Fields["handle"].(runtime.Int)
	if !ok {
		return nil, fmt.Errorf("%s cookieJar handle invalid", label)
	}
	e.httpCookieJarMu.Lock()
	jar, found := e.lookupCookieJar(id.Value.Int64())
	e.httpCookieJarMu.Unlock()
	if !found {
		return nil, fmt.Errorf("%s cookieJar handle not found", label)
	}
	return jar, nil
}

func (e *Evaluator) httpNewCookieJar(call *ast.CallExpression, _ []runtime.Value) (runtime.Value, error) {
	jar, _ := cookiejar.New(nil)
	e.httpCookieJarMu.Lock()
	id := e.nextCookieJarID
	e.nextCookieJarID++
	ensureMap(&e.httpCookieJars)[id] = jar
	e.httpCookieJarMu.Unlock()
	if e.httpCookieJarClass == nil {
		return nil, fmt.Errorf("CookieJar class is not initialized")
	}
	return &runtime.Instance{Class: e.httpCookieJarClass, Fields: map[string]runtime.Value{"handle": runtime.NewInt64(id)}}, nil
}

func (e *Evaluator) httpBuild(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects url argument", call.Callee.String())
	}
	urlStr, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s url must be string", call.Callee.String())
	}
	if e.httpBuilderClass == nil {
		return nil, fmt.Errorf("Builder class is not initialized")
	}
	return &runtime.Instance{
		Class: e.httpBuilderClass,
		Fields: map[string]runtime.Value{
			"_url":       urlStr,
			"_method":    runtime.String{Value: "GET"},
			"_body":      runtime.Null{},
			"_timeoutMs": runtime.Null{},
			"_headers":   runtime.Null{},
			"_query":     runtime.Null{},
		},
	}, nil
}

func httpBuildReqFromSpec(spec runtime.Dict) (*http.Request, string, error) {
	method := "GET"
	if v, ok := dictStringField(spec, "method"); ok {
		method = strings.ToUpper(v)
	}
	urlStr, ok := dictStringField(spec, "url")
	if !ok {
		return nil, "", fmt.Errorf("request spec missing url")
	}
	body := ""
	if v, ok := dictStringField(spec, "body"); ok {
		body = v
	}
	req, err := http.NewRequest(method, urlStr, strings.NewReader(body))
	if err != nil {
		return nil, urlStr, err
	}
	if hdrs, ok := dictField(spec, "headers"); ok {
		if err := applyRequestHeaders(nil, req, hdrs); err != nil {
			return nil, urlStr, err
		}
	}
	return req, urlStr, nil
}

func httpErrorResult(err error) runtime.Value {
	if httpResponseResultClass != nil {
		return newErrorResponseInstance(httpResponseResultClass, err.Error())
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "error", runtime.String{Value: err.Error()})
	return runtime.Dict{Entries: entries}
}

// httpStreamResult annotates a fetchStream result with its input index and
// resolved URL so out-of-order streamed results can be correlated. Exposed
// on a Response via resp["index"] / resp["url"].
func httpStreamResult(result runtime.Value, idx int, url string) runtime.Value {
	switch v := result.(type) {
	case *runtime.Instance:
		v.Fields["_index"] = runtime.NewInt64(int64(idx))
		if url != "" {
			v.Fields["_url"] = runtime.String{Value: url}
		}
	case runtime.Dict:
		putDictV(v, "index", runtime.NewInt64(int64(idx)))
		if url != "" {
			putDictV(v, "url", runtime.String{Value: url})
		}
	}
	return result
}

// httpPrepareElement materialises a request from a batch element, which may
// be a request-spec dict or a Builder instance.
func httpPrepareElement(el runtime.Value) (*http.Request, *http.Client, error) {
	switch v := el.(type) {
	case runtime.Dict:
		req, _, err := httpBuildReqFromSpec(v)
		return req, http.DefaultClient, err
	case *runtime.Instance:
		if v.Class != nil && strings.EqualFold(v.Class.Name, "Builder") {
			return httpRequestFromBuilder(v)
		}
	}
	return nil, nil, fmt.Errorf("request must be a dict spec or a request Builder")
}

// httpBatchLimit reads an optional {limit} concurrency cap from a trailing
// options dict. Zero means unbounded.
func httpBatchLimit(args []runtime.Value, from int) int {
	if len(args) <= from {
		return 0
	}
	if opts, ok := args[from].(runtime.Dict); ok {
		if v, ok := dictField(opts, "limit"); ok {
			if n, ok := toInt64(v); ok && n > 0 {
				return int(n)
			}
		}
	}
	return 0
}

// httpRunBatch runs prepare+request for each element concurrently, capped at
// limit (0 = unbounded), preserving input order. Returns a Task resolving to
// a list where each element is a Response or an {error} dict.
func httpRunBatch(items []runtime.Value, limit int, prepare func(runtime.Value) (*http.Request, *http.Client, error)) *runtime.Task {
	task := runtime.NewTask()
	go func() {
		results := make([]runtime.Value, len(items))
		var sem chan struct{}
		if limit > 0 {
			sem = make(chan struct{}, limit)
		}
		var wg sync.WaitGroup
		for i, item := range items {
			wg.Add(1)
			go func(idx int, it runtime.Value) {
				defer wg.Done()
				if sem != nil {
					sem <- struct{}{}
					defer func() { <-sem }()
				}
				req, client, err := prepare(it)
				if err != nil {
					results[idx] = httpErrorResult(err)
					return
				}
				result, doErr := doHTTPRequest(client, req)
				if doErr != nil {
					results[idx] = httpErrorResult(doErr)
					return
				}
				results[idx] = result
			}(i, item)
		}
		wg.Wait()
		task.Complete(&runtime.List{Elements: results}, nil)
	}()
	return task
}

func (e *Evaluator) httpFetchAll(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("%s expects a list of requests and optional options", call.Callee.String())
	}
	list, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s argument must be a list", call.Callee.String())
	}
	return httpRunBatch(list.Elements, httpBatchLimit(args, 1), httpPrepareElement), nil
}

func (e *Evaluator) httpGetAll(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("%s expects a list of urls and optional options", call.Callee.String())
	}
	list, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s argument must be a list of urls", call.Callee.String())
	}
	prepare := func(el runtime.Value) (*http.Request, *http.Client, error) {
		url, ok := el.(runtime.String)
		if !ok {
			return nil, nil, fmt.Errorf("getAll urls must be strings")
		}
		req, err := http.NewRequest(http.MethodGet, url.Value, nil)
		return req, http.DefaultClient, err
	}
	return httpRunBatch(list.Elements, httpBatchLimit(args, 1), prepare), nil
}

func (e *Evaluator) httpFetchStream(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a list of request specs", call.Callee.String())
	}
	list, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s argument must be a list", call.Callee.String())
	}
	if e.httpFetchStreamClass == nil {
		return nil, fmt.Errorf("FetchStream class is not initialized")
	}
	specs := list.Elements
	ch := make(chan runtime.Value, len(specs))
	sh := &httpFetchStreamHandle{ch: ch, total: len(specs)}
	e.httpFetchStreamMu.Lock()
	id := e.nextFetchStreamID
	e.nextFetchStreamID++
	ensureMap(&e.httpFetchStreams)[id] = sh
	e.httpFetchStreamMu.Unlock()
	for i, specVal := range specs {
		go func(idx int, sv runtime.Value) {
			req, client, prepErr := httpPrepareElement(sv)
			resolvedURL := ""
			if req != nil {
				resolvedURL = req.URL.String()
			}
			if prepErr != nil {
				ch <- httpStreamResult(httpErrorResult(prepErr), idx, resolvedURL)
				return
			}
			result, doErr := doHTTPRequest(client, req)
			if doErr != nil {
				ch <- httpStreamResult(httpErrorResult(doErr), idx, resolvedURL)
				return
			}
			ch <- httpStreamResult(result, idx, resolvedURL)
		}(i, specVal)
	}
	return &runtime.Instance{
		Class:  e.httpFetchStreamClass,
		Fields: map[string]runtime.Value{"handle": runtime.NewInt64(id)},
	}, nil
}

func (e *Evaluator) jsonStringFromValue(value runtime.Value) (string, error) {
	return native.EncodeJSONValueCtx(e.natives.ConversionContext(), value)
}

func buildResponseInstance(class *runtime.Class, label string, args []runtime.Value) (runtime.Value, error) {
	if len(args) == 1 {
		if dict, ok := args[0].(runtime.Dict); ok {
			status, _ := dictField(dict, "status")
			body, _ := dictField(dict, "body")
			headers, _ := dictField(dict, "headers")
			url, _ := dictField(dict, "url")
			return newResponseInstance(class, status, body, headers, url), nil
		}
	}
	if len(args) > 3 {
		return nil, fmt.Errorf("%s expects optional body, status, and headers", label)
	}
	var status runtime.Value = runtime.NewInt64(http.StatusOK)
	var body runtime.Value = runtime.String{}
	var headers runtime.Value = runtime.Dict{Entries: map[string]runtime.DictEntry{}}
	// Positional order is body-first (body, status, headers), matching
	// http.jsonResponse(value, status) / http.redirect(url, status). The
	// single-dict form above stays keyed.
	if len(args) >= 1 {
		switch args[0].(type) {
		case runtime.String, runtime.Bytes, runtime.Int, runtime.NativeObject:
			body = args[0]
		default:
			body = runtime.String{Value: args[0].Inspect()}
		}
	}
	if len(args) >= 2 {
		if _, ok := toInt64(args[1]); !ok {
			return nil, fmt.Errorf("%s status must be int", label)
		}
		status = args[1]
	}
	if len(args) >= 3 {
		if _, ok := httpHeaderValue(args[2]); !ok {
			return nil, fmt.Errorf("%s headers must be dict<string, string>", label)
		}
		headers = args[2]
	}
	return newResponseInstance(class, status, body, headers, runtime.String{}), nil
}
