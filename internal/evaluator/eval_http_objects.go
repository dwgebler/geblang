package evaluator

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"net/http"
	"sort"
)

func httpHeadersObject(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s expects optional dict or http.Headers", call.Callee.String())
	}
	if len(args) == 0 {
		return runtime.HTTPHeaders{Values: map[string][]string{}}, nil
	}
	headers, err := httpHeadersFromValue(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %v", call.Callee.String(), err)
	}
	return headers, nil
}

func httpCookieObject(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects dict, Set-Cookie string, or http.Cookie", call.Callee.String())
	}
	cookie, err := native.HTTPCookieFromValue(args[0])
	if err != nil {
		return nil, fmt.Errorf("%s %v", call.Callee.String(), err)
	}
	return cookie, nil
}

func httpHeadersFromValue(value runtime.Value) (runtime.HTTPHeaders, error) {
	switch value := value.(type) {
	case runtime.HTTPHeaders:
		return copyHTTPHeaders(value), nil
	case runtime.Dict:
		out := runtime.HTTPHeaders{Values: map[string][]string{}}
		for _, __dk := range value.EntryKeys() {
			entry, _ := value.GetEntry(__dk)
			key, ok := entry.Key.(runtime.String)
			if !ok {
				return out, fmt.Errorf("headers keys must be strings")
			}
			switch headerValue := entry.Value.(type) {
			case runtime.String:
				out.Values[http.CanonicalHeaderKey(key.Value)] = []string{headerValue.Value}
			case *runtime.List:
				values := make([]string, 0, len(headerValue.Elements))
				for _, element := range headerValue.Elements {
					text, ok := element.(runtime.String)
					if !ok {
						return out, fmt.Errorf("headers list values must be strings")
					}
					values = append(values, text.Value)
				}
				out.Values[http.CanonicalHeaderKey(key.Value)] = values
			default:
				return out, fmt.Errorf("headers values must be strings or list<string>")
			}
		}
		return out, nil
	default:
		return runtime.HTTPHeaders{}, fmt.Errorf("headers must be dict or http.Headers")
	}
}

func copyHTTPHeaders(headers runtime.HTTPHeaders) runtime.HTTPHeaders {
	out := runtime.HTTPHeaders{Values: map[string][]string{}}
	for key, values := range headers.Values {
		out.Values[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
	return out
}

func httpHeadersToDict(headers runtime.HTTPHeaders) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	for key, values := range headers.Values {
		keyValue := runtime.String{Value: http.CanonicalHeaderKey(key)}
		var value runtime.Value
		if len(values) == 1 {
			value = runtime.String{Value: values[0]}
		} else {
			elements := make([]runtime.Value, len(values))
			for i, item := range values {
				elements[i] = runtime.String{Value: item}
			}
			value = &runtime.List{Elements: elements}
		}
		entries[dictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: value}
	}
	return runtime.Dict{Entries: entries}
}

func httpHeaderValue(value runtime.Value) (runtime.HTTPHeaders, bool) {
	headers, ok := value.(runtime.HTTPHeaders)
	if ok {
		return headers, true
	}
	dict, ok := value.(runtime.Dict)
	if !ok {
		return runtime.HTTPHeaders{}, false
	}
	headers, err := httpHeadersFromValue(dict)
	return headers, err == nil
}

func httpHeadersMethod(receiver runtime.HTTPHeaders, name string, args []runtime.Value) (runtime.Value, error) {
	headers := copyHTTPHeaders(receiver)
	switch name {
	case "get":
		key, err := singleHeaderName(name, args)
		if err != nil {
			return nil, err
		}
		values := headers.Values[key]
		if len(values) == 0 {
			return runtime.Null{}, nil
		}
		return runtime.String{Value: values[0]}, nil
	case "getAll":
		key, err := singleHeaderName(name, args)
		if err != nil {
			return nil, err
		}
		values := headers.Values[key]
		elements := make([]runtime.Value, len(values))
		for i, value := range values {
			elements[i] = runtime.String{Value: value}
		}
		return &runtime.List{Elements: elements}, nil
	case "has":
		key, err := singleHeaderName(name, args)
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: len(headers.Values[key]) > 0}, nil
	case "set":
		if len(args) != 2 {
			return nil, fmt.Errorf("http.Headers.set expects name and value")
		}
		key, value, err := headerNameValue("set", args)
		if err != nil {
			return nil, err
		}
		headers.Values[key] = []string{value}
		return headers, nil
	case "add":
		if len(args) != 2 {
			return nil, fmt.Errorf("http.Headers.add expects name and value")
		}
		key, value, err := headerNameValue("add", args)
		if err != nil {
			return nil, err
		}
		headers.Values[key] = append(headers.Values[key], value)
		return headers, nil
	case "delete":
		key, err := singleHeaderName(name, args)
		if err != nil {
			return nil, err
		}
		delete(headers.Values, key)
		return headers, nil
	case "keys":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Headers.keys expects no arguments")
		}
		keys := make([]string, 0, len(headers.Values))
		for key := range headers.Values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		elements := make([]runtime.Value, len(keys))
		for i, key := range keys {
			elements[i] = runtime.String{Value: key}
		}
		return &runtime.List{Elements: elements}, nil
	case "toDict":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Headers.toDict expects no arguments")
		}
		return httpHeadersToDict(headers), nil
	default:
		return nil, fmt.Errorf("http.Headers has no method %s", name)
	}
}

func singleHeaderName(method string, args []runtime.Value) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("http.Headers.%s expects name", method)
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return "", fmt.Errorf("http.Headers.%s name must be string", method)
	}
	return http.CanonicalHeaderKey(name.Value), nil
}

func headerNameValue(method string, args []runtime.Value) (string, string, error) {
	name, ok := args[0].(runtime.String)
	if !ok {
		return "", "", fmt.Errorf("http.Headers.%s name must be string", method)
	}
	value, ok := args[1].(runtime.String)
	if !ok {
		return "", "", fmt.Errorf("http.Headers.%s value must be string", method)
	}
	return http.CanonicalHeaderKey(name.Value), value.Value, nil
}

func httpObjectClasses(env *runtime.Environment) []*runtime.Class {
	requestClass := &runtime.Class{
		Name: "Request",
		Fields: []runtime.Field{
			{Name: "method"},
			{Name: "path"},
			{Name: "query"},
			{Name: "remoteAddr"},
			{Name: "body"},
			{Name: "headers"},
		},
		Methods: map[string][]runtime.Function{
			"header":      []runtime.Function{{Name: "header", Native: nativeRequestHeader}},
			"json":        []runtime.Function{{Name: "json", Native: nativeRequestJSON}},
			"bodytext":    []runtime.Function{{Name: "bodyText", Native: nativeRequestBodyText}},
			"bodybytes":   []runtime.Function{{Name: "bodyBytes", Native: nativeRequestBodyBytes}},
			"todict":      []runtime.Function{{Name: "toDict", Native: nativeRequestToDict}},
			"inspect":     []runtime.Function{{Name: "inspect", Native: nativeRequestInspect}},
			"scheme":      []runtime.Function{{Name: "scheme", Native: nativeRequestScheme}},
			"issecure":    []runtime.Function{{Name: "isSecure", Native: nativeRequestIsSecure}},
			"host":        []runtime.Function{{Name: "host", Native: nativeRequestHost}},
			"clientip":    []runtime.Function{{Name: "clientIp", Native: nativeRequestClientIP}},
			"clientcert":  []runtime.Function{{Name: "clientCert", Native: nativeRequestClientCert}},
			"text":        []runtime.Function{{Name: "text", Native: nativeRequestText}},
			"ismethod":    []runtime.Function{{Name: "isMethod", Native: nativeRequestIsMethod}},
			"isjson":      []runtime.Function{{Name: "isJson", Native: nativeRequestIsJSON}},
			"cookie":      []runtime.Function{{Name: "cookie", Native: nativeRequestCookie}},
			"query":       []runtime.Function{{Name: "query", Native: nativeRequestQuery}},
			"queryint":    []runtime.Function{{Name: "queryInt", Native: nativeRequestQueryInt}},
			"querybool":   []runtime.Function{{Name: "queryBool", Native: nativeRequestQueryBool}},
			"queryall":    []runtime.Function{{Name: "queryAll", Native: nativeRequestQueryAll}},
			"routeparam":  []runtime.Function{{Name: "routeParam", Native: nativeRequestRouteParam}},
			"routeparams": []runtime.Function{{Name: "routeParams", Native: nativeRequestRouteParams}},
		},
		Constructors: []runtime.Function{{Name: "Request", Native: nativeRequestConstructor}},
		Env:          env,
	}
	responseClass := &runtime.Class{
		Name: "Response",
		Fields: []runtime.Field{
			{Name: "status"},
			{Name: "body"},
			{Name: "headers"},
		},
		Methods: map[string][]runtime.Function{
			"withheader":    []runtime.Function{{Name: "withHeader", Native: nativeResponseWithHeader}},
			"withbody":      []runtime.Function{{Name: "withBody", Native: nativeResponseWithBody}},
			"withstatus":    []runtime.Function{{Name: "withStatus", Native: nativeResponseWithStatus}},
			"todict":        []runtime.Function{{Name: "toDict", Native: nativeResponseToDict}},
			"inspect":       []runtime.Function{{Name: "inspect", Native: nativeResponseInspect}},
			"status":        []runtime.Function{{Name: "status", Native: nativeResponseStatus}},
			"ok":            []runtime.Function{{Name: "ok", Native: responseStatusPredicate("ok", 200, 299)}},
			"issuccessful":  []runtime.Function{{Name: "isSuccessful", Native: responseStatusPredicate("isSuccessful", 200, 299)}},
			"isredirect":    []runtime.Function{{Name: "isRedirect", Native: responseStatusPredicate("isRedirect", 300, 399)}},
			"isclienterror": []runtime.Function{{Name: "isClientError", Native: responseStatusPredicate("isClientError", 400, 499)}},
			"isservererror": []runtime.Function{{Name: "isServerError", Native: responseStatusPredicate("isServerError", 500, 599)}},
			"isnotfound":    []runtime.Function{{Name: "isNotFound", Native: nativeResponseIsNotFound}},
			"iserror":       []runtime.Function{{Name: "isError", Native: nativeResponseIsError}},
			"error":         []runtime.Function{{Name: "error", Native: nativeResponseError}},
			"body":          []runtime.Function{{Name: "body", Native: nativeResponseBody}},
			"text":          []runtime.Function{{Name: "text", Native: nativeResponseText}},
			"bytes":         []runtime.Function{{Name: "bytes", Native: nativeResponseBytes}},
			"json":          []runtime.Function{{Name: "json", Native: nativeResponseJSON}},
			"header":        []runtime.Function{{Name: "header", Native: nativeResponseHeader}},
			"headers":       []runtime.Function{{Name: "headers", Native: nativeResponseHeaders}},
			"url":           []runtime.Function{{Name: "url", Native: nativeResponseURL}},
			"__index":       []runtime.Function{{Name: "__index", Native: nativeResponseIndex}},
		},
		Constructors: []runtime.Function{{Name: "Response", Native: nativeResponseConstructor}},
		Env:          env,
	}
	return []*runtime.Class{requestClass, responseClass}
}

func nativeRequestConstructor(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("Request constructor expects optional dict")
	}
	if len(args) == 0 {
		return runtime.Null{}, nil
	}
	dict, ok := args[0].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("Request constructor expects dict")
	}
	for key, value := range fieldsFromDict(dict) {
		this.Fields[key] = value
	}
	return runtime.Null{}, nil
}

func nativeRequestHeader(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Request.header expects one argument")
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("Request.header name must be string")
	}
	headers, ok := httpHeaderValue(this.Fields["headers"])
	if !ok {
		return runtime.Null{}, nil
	}
	values := headers.Values[http.CanonicalHeaderKey(name.Value)]
	if len(values) == 0 {
		return runtime.Null{}, nil
	}
	return runtime.String{Value: values[0]}, nil
}

func nativeRequestJSON(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.json expects no arguments")
	}
	body, err := requestBodyText(this)
	if err != nil {
		return nil, err
	}
	value, parseErr := native.ParseJSONText(body)
	if parseErr != nil {
		return nil, fmt.Errorf("%s", parseErr.Message)
	}
	return value, nil
}

func nativeRequestBodyText(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.bodyText expects no arguments")
	}
	body, err := requestBodyText(this)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: body}, nil
}

func nativeRequestBodyBytes(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.bodyBytes expects no arguments")
	}
	switch body := this.Fields["body"].(type) {
	case runtime.String:
		return runtime.Bytes{Value: []byte(body.Value)}, nil
	case runtime.Bytes:
		return body, nil
	case runtime.Null:
		return runtime.Bytes{}, nil
	default:
		return nil, fmt.Errorf("Request.body must be string or bytes")
	}
}

func requestBodyText(this *runtime.Instance) (string, error) {
	switch body := this.Fields["body"].(type) {
	case runtime.String:
		return body.Value, nil
	case runtime.Bytes:
		return string(body.Value), nil
	case runtime.Null:
		return "", nil
	default:
		return "", fmt.Errorf("Request.body must be string or bytes")
	}
}

func nativeRequestToDict(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.toDict expects no arguments")
	}
	entries := map[string]runtime.DictEntry{}
	for _, name := range []string{"method", "path", "query", "remoteAddr", "body", "headers"} {
		value, ok := this.Fields[name]
		if !ok {
			value = runtime.Null{}
		}
		if name == "headers" {
			if headers, ok := httpHeaderValue(value); ok {
				value = httpHeadersToDict(headers)
			}
		}
		putDict(entries, name, value)
	}
	if params, ok := this.Fields["params"].(runtime.Dict); ok {
		putDict(entries, "params", params)
	}
	return runtime.Dict{Entries: entries}, nil
}

func nativeRequestInspect(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.inspect expects no arguments")
	}
	method, _ := this.Fields["method"].(runtime.String)
	path, _ := this.Fields["path"].(runtime.String)
	return runtime.String{Value: method.Value + " " + path.Value}, nil
}

func nativeResponseConstructor(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	value, err := buildResponseInstance(this.Class, "Response constructor", args)
	if err != nil {
		return nil, err
	}
	instance := value.(*runtime.Instance)
	this.Fields = instance.Fields
	return runtime.Null{}, nil
}

func nativeResponseWithHeader(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("Response.withHeader expects name and value")
	}
	name, nameOK := args[0].(runtime.String)
	value, valueOK := args[1].(runtime.String)
	if !nameOK || !valueOK {
		return nil, fmt.Errorf("Response.withHeader expects string name and value")
	}
	headers := map[string]runtime.DictEntry{}
	if existing, ok := httpHeaderValue(this.Fields["headers"]); ok {
		httpHeadersToDict(existing).ForEachEntry(func(k string, e runtime.DictEntry) bool {
			headers[k] = e
			return true
		})
	}
	putDict(headers, name.Value, value)
	return newResponseInstance(this.Class, this.Fields["status"], this.Fields["body"], runtime.Dict{Entries: headers}, this.Fields["url"]), nil
}

func nativeResponseWithBody(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Response.withBody expects one argument")
	}
	return newResponseInstance(this.Class, this.Fields["status"], args[0], this.Fields["headers"], this.Fields["url"]), nil
}

func nativeResponseWithStatus(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Response.withStatus expects one argument")
	}
	if _, ok := toInt64(args[0]); !ok {
		return nil, fmt.Errorf("Response.withStatus status must be int")
	}
	return newResponseInstance(this.Class, args[0], this.Fields["body"], this.Fields["headers"], this.Fields["url"]), nil
}

func nativeResponseToDict(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Response.toDict expects no arguments")
	}
	return responseInstanceDict(this), nil
}

func responseStatusInt64(this *runtime.Instance) int64 {
	if v, ok := toInt64(this.Fields["status"]); ok {
		return v
	}
	return 0
}

func nativeResponseStatus(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Response.status expects no arguments")
	}
	return runtime.NewInt64(responseStatusInt64(this)), nil
}

// nativeResponseURL returns the final URL after redirects (empty for non-request responses).
func nativeResponseURL(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Response.url expects no arguments")
	}
	if v, ok := this.Fields["url"].(runtime.String); ok {
		return v, nil
	}
	return runtime.String{}, nil
}

func responseStatusPredicate(name string, lo, hi int64) func(*runtime.Instance, []runtime.Value) (runtime.Value, error) {
	return func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Response.%s expects no arguments", name)
		}
		s := responseStatusInt64(this)
		return runtime.Bool{Value: s >= lo && s <= hi}, nil
	}
}

func nativeResponseIsNotFound(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Response.isNotFound expects no arguments")
	}
	return runtime.Bool{Value: responseStatusInt64(this) == http.StatusNotFound}, nil
}

func responseBodyText(this *runtime.Instance) string {
	switch v := this.Fields["body"].(type) {
	case runtime.String:
		return v.Value
	case *runtime.Bytes:
		return string(v.Value)
	}
	return ""
}

func nativeResponseText(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Response.text expects no arguments")
	}
	return runtime.String{Value: responseBodyText(this)}, nil
}

// nativeResponseBody returns the raw stored body value (the method form of
// resp["body"]); text()/bytes()/json() give typed access instead.
func nativeResponseBody(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Response.body expects no arguments")
	}
	if v := this.Fields["body"]; v != nil {
		return v, nil
	}
	return runtime.Null{}, nil
}

func nativeResponseBytes(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Response.bytes expects no arguments")
	}
	switch body := this.Fields["body"].(type) {
	case runtime.Bytes:
		return body, nil
	case *runtime.Bytes:
		return *body, nil
	}
	return runtime.Bytes{Value: []byte(responseBodyText(this))}, nil
}

func nativeResponseJSON(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Response.json expects no arguments")
	}
	value, parseErr := native.ParseJSONText(responseBodyText(this))
	if parseErr != nil {
		return nil, fmt.Errorf("%s", parseErr.Message)
	}
	return value, nil
}

func nativeResponseHeaders(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Response.headers expects no arguments")
	}
	if headers := this.Fields["headers"]; headers != nil {
		return headers, nil
	}
	return runtime.Dict{Entries: map[string]runtime.DictEntry{}}, nil
}

func nativeResponseHeader(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Response.header expects one argument")
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("Response.header name must be string")
	}
	if headers, ok := httpHeaderValue(this.Fields["headers"]); ok {
		values := headers.Values[http.CanonicalHeaderKey(name.Value)]
		if len(values) == 0 {
			return runtime.Null{}, nil
		}
		return runtime.String{Value: values[0]}, nil
	}
	if dict, ok := this.Fields["headers"].(runtime.Dict); ok {
		canonical := http.CanonicalHeaderKey(name.Value)
		var found runtime.Value
		dict.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			if key, ok := entry.Key.(runtime.String); ok && http.CanonicalHeaderKey(key.Value) == canonical {
				found = entry.Value
				return false
			}
			return true
		})
		if found != nil {
			return found, nil
		}
	}
	return runtime.Null{}, nil
}

func nativeResponseIndex(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Response.__index expects one argument")
	}
	key, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("Response index key must be string")
	}
	switch key.Value {
	case "status", "body", "headers":
		if v := this.Fields[key.Value]; v != nil {
			return v, nil
		}
		return runtime.Null{}, nil
	case "error":
		if v := this.Fields["_error"]; v != nil {
			return v, nil
		}
		return runtime.Null{}, nil
	case "url":
		// Real client responses carry the post-redirect URL; fetchStream sets _url for input correlation.
		if v := this.Fields["url"]; v != nil {
			return v, nil
		}
		if v := this.Fields["_url"]; v != nil {
			return v, nil
		}
		return runtime.Null{}, nil
	case "index":
		if v := this.Fields["_index"]; v != nil {
			return v, nil
		}
		return runtime.Null{}, nil
	}
	return runtime.Null{}, nil
}

func nativeResponseIsError(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Response.isError expects no arguments")
	}
	_, ok := this.Fields["_error"].(runtime.String)
	return runtime.Bool{Value: ok}, nil
}

func nativeResponseError(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Response.error expects no arguments")
	}
	if v, ok := this.Fields["_error"].(runtime.String); ok {
		return v, nil
	}
	return runtime.Null{}, nil
}

// newErrorResponseInstance builds a Response that represents a request that
// never completed a round trip. isError() is true and error() carries the
// message; status() is 0 and ok() is false.
func newErrorResponseInstance(class *runtime.Class, message string) *runtime.Instance {
	return &runtime.Instance{Class: class, Fields: map[string]runtime.Value{
		"status":  runtime.NewInt64(0),
		"body":    runtime.String{},
		"headers": runtime.Dict{Entries: map[string]runtime.DictEntry{}},
		"url":     runtime.String{},
		"_error":  runtime.String{Value: message},
	}}
}

func nativeResponseInspect(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Response.inspect expects no arguments")
	}
	status := "200"
	if value, ok := native.IntValueToBigInt(this.Fields["status"]); ok {
		status = value.String()
	}
	return runtime.String{Value: "HTTP " + status}, nil
}

func builtinClass(env *runtime.Environment, name string) (*runtime.Class, error) {
	if env == nil {
		return nil, fmt.Errorf("%s class is not available", name)
	}
	value, ok := env.Get(name)
	if !ok {
		return nil, fmt.Errorf("%s class is not available", name)
	}
	class, ok := value.(*runtime.Class)
	if !ok {
		return nil, fmt.Errorf("%s is not a class", name)
	}
	return class, nil
}

func fieldsFromEntries(entries map[string]runtime.DictEntry) map[string]runtime.Value {
	fields := map[string]runtime.Value{}
	for _, entry := range entries {
		if key, ok := entry.Key.(runtime.String); ok {
			fields[key.Value] = entry.Value
		}
	}
	return fields
}

func responseInstanceDict(instance *runtime.Instance) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	status, ok := instance.Fields["status"]
	if !ok {
		status = runtime.NewInt64(http.StatusOK)
	}
	body, ok := instance.Fields["body"]
	if !ok {
		body = runtime.String{}
	}
	headers, ok := instance.Fields["headers"]
	if !ok {
		headers = runtime.Dict{Entries: map[string]runtime.DictEntry{}}
	}
	if headerValue, ok := httpHeaderValue(headers); ok {
		headers = httpHeadersToDict(headerValue)
	}
	putDict(entries, "status", status)
	putDict(entries, "body", body)
	putDict(entries, "headers", headers)
	if url, ok := instance.Fields["url"].(runtime.String); ok && url.Value != "" {
		putDict(entries, "url", url)
	}
	return runtime.Dict{Entries: entries}
}

func newResponseInstance(class *runtime.Class, status runtime.Value, body runtime.Value, headers runtime.Value, url runtime.Value) *runtime.Instance {
	if status == nil {
		status = runtime.NewInt64(http.StatusOK)
	}
	if s, ok := status.(runtime.SmallInt); ok {
		status = runtime.NewInt64(s.Value)
	}
	if body == nil {
		body = runtime.String{}
	}
	if headers == nil {
		headers = runtime.Dict{Entries: map[string]runtime.DictEntry{}}
	}
	if url == nil {
		url = runtime.String{}
	}
	return &runtime.Instance{Class: class, Fields: map[string]runtime.Value{
		"status":  status,
		"body":    body,
		"headers": headers,
		"url":     url,
	}}
}

func (e *Evaluator) httpResponseObject(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if e.httpResponseClass == nil {
		return nil, fmt.Errorf("Response class is not available")
	}
	return buildResponseInstance(e.httpResponseClass, call.Callee.String(), args)
}

func (e *Evaluator) httpJSONResponseObject(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("%s expects value and optional status", call.Callee.String())
	}
	if e.httpResponseClass == nil {
		return nil, fmt.Errorf("Response class is not available")
	}
	status := runtime.Value(runtime.NewInt64(http.StatusOK))
	if len(args) == 2 {
		if _, ok := toInt64(args[1]); !ok {
			return nil, fmt.Errorf("%s status must be int", call.Callee.String())
		}
		status = args[1]
	}
	body, err := e.jsonStringFromValue(args[0])
	if err != nil {
		return nil, err
	}
	headers := map[string]runtime.DictEntry{}
	putDict(headers, "Content-Type", runtime.String{Value: "application/json"})
	return newResponseInstance(e.httpResponseClass, status, runtime.String{Value: body}, runtime.Dict{Entries: headers}, runtime.String{}), nil
}

func (e *Evaluator) httpRedirectObject(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("%s expects url and optional status", call.Callee.String())
	}
	url, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s url must be string", call.Callee.String())
	}
	status := runtime.Value(runtime.NewInt64(http.StatusFound))
	if len(args) == 2 {
		if _, ok := toInt64(args[1]); !ok {
			return nil, fmt.Errorf("%s status must be int", call.Callee.String())
		}
		status = args[1]
	}
	if e.httpResponseClass == nil {
		return nil, fmt.Errorf("Response class is not available")
	}
	headers := map[string]runtime.DictEntry{}
	putDict(headers, "Location", runtime.String{Value: url.Value})
	return newResponseInstance(e.httpResponseClass, status, runtime.String{}, runtime.Dict{Entries: headers}, runtime.String{}), nil
}
