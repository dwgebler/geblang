package evaluator

import (
	"bytes"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/runtime"
	"io"
	"mime"
	"mime/multipart"
	"strings"
)

func (e *Evaluator) webNew(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	e.webMu.Lock()
	defer e.webMu.Unlock()
	e.nextWebID++
	e.webApps[e.nextWebID] = &webApp{routes: []webRoute{}, beforeMiddlewares: []runtime.Value{}, middlewares: []runtime.Value{}}
	return runtime.NewInt64(e.nextWebID), nil
}

func (e *Evaluator) webUse(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects app and middleware", call.Callee.String())
	}
	app, err := e.webApp(args[0])
	if err != nil {
		return nil, err
	}
	if !runtime.IsCallableValue(args[1]) {
		return nil, fmt.Errorf("%s middleware must be function", call.Callee.String())
	}
	e.webMu.Lock()
	defer e.webMu.Unlock()
	app.middlewares = append(app.middlewares, args[1])
	return runtime.Null{}, nil
}

func (e *Evaluator) webBefore(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects app and middleware", call.Callee.String())
	}
	app, err := e.webApp(args[0])
	if err != nil {
		return nil, err
	}
	if !runtime.IsCallableValue(args[1]) {
		return nil, fmt.Errorf("%s middleware must be function", call.Callee.String())
	}
	e.webMu.Lock()
	defer e.webMu.Unlock()
	app.beforeMiddlewares = append(app.beforeMiddlewares, args[1])
	return runtime.Null{}, nil
}

func (e *Evaluator) webGet(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	return e.webRouteWithMethod(call, args, "GET")
}

func (e *Evaluator) webPost(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	return e.webRouteWithMethod(call, args, "POST")
}

func (e *Evaluator) webRoute(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 4 {
		return nil, fmt.Errorf("%s expects app, method, path, handler", call.Callee.String())
	}
	method, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s method must be string", call.Callee.String())
	}
	return e.registerWebRoute(call, args[0], method.Value, args[2], args[3])
}

func (e *Evaluator) webRouteWithMethod(call *ast.CallExpression, args []runtime.Value, method string) (runtime.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("%s expects app, path, handler", call.Callee.String())
	}
	return e.registerWebRoute(call, args[0], method, args[1], args[2])
}

func (e *Evaluator) registerWebRoute(call *ast.CallExpression, appValue runtime.Value, method string, pathValue runtime.Value, handlerValue runtime.Value) (runtime.Value, error) {
	app, err := e.webApp(appValue)
	if err != nil {
		return nil, err
	}
	path, ok := pathValue.(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s path must be string", call.Callee.String())
	}
	if !runtime.IsCallableValue(handlerValue) {
		return nil, fmt.Errorf("%s handler must be function", call.Callee.String())
	}
	e.webMu.Lock()
	defer e.webMu.Unlock()
	app.routes = append(app.routes, webRoute{method: strings.ToUpper(method), path: path.Value, handler: handlerValue})
	return runtime.Null{}, nil
}

// webCallableParamIsType reports whether the callable's param at index declares
// the given bare type name (e.g. "Request", "Response"). Native functions carry
// no parameter metadata and so default to false (dict).
func webCallableParamIsType(fn runtime.Value, index int, name string) bool {
	function, ok := fn.(runtime.Function)
	if !ok {
		return false
	}
	if len(function.Parameters) <= index || function.Parameters[index].Type == nil {
		return false
	}
	typ := function.Parameters[index].Type
	return typ.Operator == "" && !typ.ListAlias && typeNamesEqual(typ.Name, name)
}

// webRequestArg returns a rich Request instance when the callable opts in by
// typing its first parameter `Request`; otherwise the request dict unchanged.
func (e *Evaluator) webRequestArg(fn runtime.Value, request runtime.Dict) runtime.Value {
	if !webCallableParamIsType(fn, 0, "Request") {
		return request
	}
	class := e.httpRequestClass
	if class == nil && e.parent != nil {
		class = e.parent.httpRequestClass
	}
	if class == nil {
		return request
	}
	return &runtime.Instance{Class: class, Fields: fieldsFromDict(request)}
}

// webResponseArg returns a rich Response instance when the callable's parameter
// at index is typed `Response`; otherwise the response dict unchanged.
func (e *Evaluator) webResponseArg(fn runtime.Value, index int, response runtime.Value) runtime.Value {
	if !webCallableParamIsType(fn, index, "Response") {
		return response
	}
	if instance, ok := response.(*runtime.Instance); ok && strings.EqualFold(instance.Class.Name, "Response") {
		return instance
	}
	dict, ok := response.(runtime.Dict)
	if !ok {
		return response
	}
	class := e.httpResponseClass
	if class == nil && e.parent != nil {
		class = e.parent.httpResponseClass
	}
	if class == nil {
		return response
	}
	status, _ := dictField(dict, "status")
	if status == nil {
		status = runtime.NewInt64(200)
	}
	body, _ := dictField(dict, "body")
	if body == nil {
		body = runtime.String{}
	}
	headers, _ := dictField(dict, "headers")
	if headers == nil {
		headers = runtime.Dict{Entries: map[string]runtime.DictEntry{}}
	}
	return newResponseInstance(class, status, body, headers, runtime.String{})
}

// runWebAfterMiddlewares runs the response-phase chain (last-registered first).
// Each middleware receives request + response as objects or dicts per its own
// parameter types; the result is normalized back to a dict between hops so the
// chain stays uniform and the final value is always a response dict.
func (e *Evaluator) runWebAfterMiddlewares(middlewares []runtime.Value, request runtime.Dict, response runtime.Value) (runtime.Value, error) {
	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i]
		result, err := e.callValue(mw, []runtime.Value{e.webRequestArg(mw, request), e.webResponseArg(mw, 1, response)})
		if err != nil {
			return nil, err
		}
		response = normalizeWebResponse(result)
	}
	return response, nil
}

func (e *Evaluator) webHandle(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects app and request", call.Callee.String())
	}
	app, err := e.webApp(args[0])
	if err != nil {
		return nil, err
	}
	request, ok := args[1].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s request must be dict", call.Callee.String())
	}
	e.webMu.Lock()
	routes := append([]webRoute(nil), app.routes...)
	beforeMiddlewares := append([]runtime.Value(nil), app.beforeMiddlewares...)
	middlewares := append([]runtime.Value(nil), app.middlewares...)
	e.webMu.Unlock()
	for _, middleware := range beforeMiddlewares {
		result, err := e.callValue(middleware, []runtime.Value{e.webRequestArg(middleware, request)})
		if err != nil {
			return nil, err
		}
		if _, ok := result.(runtime.Null); !ok {
			response := normalizeWebResponse(result)
			return e.runWebAfterMiddlewares(middlewares, request, response)
		}
	}
	method, _ := dictStringField(request, "method")
	path, _ := dictStringField(request, "path")
	for _, route := range routes {
		params, ok := matchWebRoute(route, method, path)
		if !ok {
			continue
		}
		requestWithParams := copyDict(request)
		putDictV(requestWithParams, "params", params)
		response, err := e.callValue(route.handler, []runtime.Value{e.webRequestArg(route.handler, requestWithParams)})
		if err != nil {
			return nil, err
		}
		response = normalizeWebResponse(response)
		return e.runWebAfterMiddlewares(middlewares, requestWithParams, response)
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "status", runtime.NewInt64(404))
	putDict(entries, "body", runtime.String{Value: "not found"})
	return runtime.Dict{Entries: entries}, nil
}

func normalizeWebResponse(response runtime.Value) runtime.Value {
	if instance, ok := response.(*runtime.Instance); ok && strings.EqualFold(instance.Class.Name, "Response") {
		return responseInstanceDict(instance)
	}
	if _, ok := response.(runtime.Dict); ok {
		return response
	}
	if _, ok := response.(runtime.Null); ok {
		entries := map[string]runtime.DictEntry{}
		putDict(entries, "status", runtime.NewInt64(204))
		putDict(entries, "body", runtime.String{Value: ""})
		return runtime.Dict{Entries: entries}
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "status", runtime.NewInt64(200))
	putDict(entries, "body", runtime.String{Value: response.Inspect()})
	return runtime.Dict{Entries: entries}
}

func webWithHeader(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("%s expects response, name, and value", call.Callee.String())
	}
	response, ok := args[0].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s response must be dict", call.Callee.String())
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s header name must be string", call.Callee.String())
	}
	value, ok := args[2].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s header value must be string", call.Callee.String())
	}
	out := copyDict(response)
	headersValue, ok := dictField(out, "headers")
	headers := runtime.Dict{Entries: map[string]runtime.DictEntry{}}
	if ok {
		existing, ok := headersValue.(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s response.headers must be dict", call.Callee.String())
		}
		headers = copyDict(existing)
	}
	putDictV(headers, name.Value, value)
	putDictV(out, "headers", headers)
	return out, nil
}

// webServeFileMarker wraps a descriptor dict in an unforgeable native sentinel; the HTTP writer serves a file only when the __serveFile value is this type.
func webServeFileMarker(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a descriptor dict", call.Callee.String())
	}
	desc, ok := args[0].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s descriptor must be dict", call.Callee.String())
	}
	if _, ok := dictStringField(desc, "path"); !ok {
		return nil, fmt.Errorf("%s descriptor requires a path", call.Callee.String())
	}
	return runtime.NativeObject{Kind: serveFileMarkerKind, Payload: copyDict(desc)}, nil
}

// webServeFilePath reads the path from a serve-file marker so the in-process TestClient can materialize the body; empty for anything else.
func webServeFilePath(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a marker value", call.Callee.String())
	}
	marker, ok := args[0].(runtime.NativeObject)
	if !ok || marker.Kind != serveFileMarkerKind {
		return runtime.String{}, nil
	}
	desc, ok := marker.Payload.(runtime.Dict)
	if !ok {
		return runtime.String{}, nil
	}
	path, _ := dictStringField(desc, "path")
	return runtime.String{Value: path}, nil
}

// webParseMultipart parses a `multipart/form-data` request body and
// returns a dict of the form
//
//	{"fields": dict<string, string>, "files": dict<string, dict>}
//
// where each file entry is `{filename, contentType, bytes}`. The
// argument is the same request dict the framework dispatches to
// route handlers: it must carry a `body` (string or bytes) and a
// `headers` dict with `Content-Type: multipart/form-data; boundary=...`.
//
// Returns an error if the body isn't multipart or the boundary is
// missing/malformed; callers can wrap that as a 400.
func webParseMultipart(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects request dict", call.Callee.String())
	}
	request, ok := args[0].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s request must be dict", call.Callee.String())
	}

	headersValue, _ := dictField(request, "headers")
	contentType := ""
	if headers, ok := headersValue.(runtime.Dict); ok {
		headers.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			if key, ok := entry.Key.(runtime.String); ok && strings.EqualFold(key.Value, "Content-Type") {
				if v, ok := entry.Value.(runtime.String); ok {
					contentType = v.Value
					return false
				}
			}
			return true
		})
	}
	if contentType == "" {
		return nil, fmt.Errorf("%s request has no Content-Type header", call.Callee.String())
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("%s parse Content-Type: %v", call.Callee.String(), err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil, fmt.Errorf("%s Content-Type is not multipart (got %q)", call.Callee.String(), mediaType)
	}
	boundary, ok := params["boundary"]
	if !ok || boundary == "" {
		return nil, fmt.Errorf("%s Content-Type missing boundary", call.Callee.String())
	}

	bodyValue, _ := dictField(request, "body")
	var bodyReader io.Reader
	switch v := bodyValue.(type) {
	case runtime.String:
		bodyReader = strings.NewReader(v.Value)
	case runtime.Bytes:
		bodyReader = bytes.NewReader(v.Value)
	case nil, runtime.Null:
		bodyReader = strings.NewReader("")
	default:
		return nil, fmt.Errorf("%s request body must be string or bytes (got %s)", call.Callee.String(), bodyValue.TypeName())
	}

	reader := multipart.NewReader(bodyReader, boundary)
	fieldsEntries := map[string]runtime.DictEntry{}
	filesEntries := map[string]runtime.DictEntry{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s read part: %v", call.Callee.String(), err)
		}
		name := part.FormName()
		filename := part.FileName()
		data, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			return nil, fmt.Errorf("%s read part body: %v", call.Callee.String(), err)
		}
		if filename == "" {
			putDict(fieldsEntries, name, runtime.String{Value: string(data)})
			continue
		}
		partContentType := part.Header.Get("Content-Type")
		if partContentType == "" {
			partContentType = "application/octet-stream"
		}
		fileEntries := map[string]runtime.DictEntry{}
		putDict(fileEntries, "filename", runtime.String{Value: filename})
		putDict(fileEntries, "contentType", runtime.String{Value: partContentType})
		putDict(fileEntries, "bytes", runtime.Bytes{Value: data})
		putDict(filesEntries, name, runtime.Dict{Entries: fileEntries})
	}

	out := map[string]runtime.DictEntry{}
	putDict(out, "fields", runtime.Dict{Entries: fieldsEntries})
	putDict(out, "files", runtime.Dict{Entries: filesEntries})
	return runtime.Dict{Entries: out}, nil
}

func (e *Evaluator) webApp(value runtime.Value) (*webApp, error) {
	id, ok := value.(runtime.Int)
	if !ok || !id.Value.IsInt64() {
		return nil, fmt.Errorf("web app handle must be int")
	}
	e.webMu.Lock()
	defer e.webMu.Unlock()
	app, ok := e.lookupWebApp(id.Value.Int64())
	if !ok {
		return nil, fmt.Errorf("unknown web app handle %d", id.Value.Int64())
	}
	return app, nil
}

func matchWebRoute(route webRoute, method string, path string) (runtime.Dict, bool) {
	if route.method != strings.ToUpper(method) {
		return runtime.Dict{}, false
	}
	routeParts := splitWebPath(route.path)
	pathParts := splitWebPath(path)
	if len(routeParts) != len(pathParts) {
		return runtime.Dict{}, false
	}
	params := runtime.Dict{Entries: map[string]runtime.DictEntry{}}
	for i, routePart := range routeParts {
		if strings.HasPrefix(routePart, ":") && len(routePart) > 1 {
			putDictV(params, routePart[1:], runtime.String{Value: pathParts[i]})
			continue
		}
		if routePart != pathParts[i] {
			return runtime.Dict{}, false
		}
	}
	return params, true
}

func splitWebPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return []string{}
	}
	return strings.Split(path, "/")
}
