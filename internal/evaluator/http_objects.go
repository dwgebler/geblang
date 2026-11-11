package evaluator

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	neturl "net/url"
	"strings"
	"time"

	"geblang/internal/native"
	"geblang/internal/runtime"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func (e *Evaluator) httpClientObjectClasses() []*runtime.Class {
	resolveURL := func(base, rel string) string {
		if base == "" || strings.HasPrefix(rel, "http://") || strings.HasPrefix(rel, "https://") {
			return rel
		}
		return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(rel, "/")
	}

	applyDefaultHeaders := func(h *httpClientHandle, req *http.Request) {
		applyDefaultUserAgent(req)
		for key, vals := range h.headers {
			for i, v := range vals {
				if i == 0 {
					req.Header.Set(key, v)
				} else {
					req.Header.Add(key, v)
				}
			}
		}
	}

	getClientHandle := func(this *runtime.Instance) (*httpClientHandle, error) {
		id, ok := this.Fields["handle"].(runtime.Int)
		if !ok {
			return nil, fmt.Errorf("invalid client handle")
		}
		e.httpClientMu.Lock()
		defer e.httpClientMu.Unlock()
		h, ok := e.lookupHTTPClient(id.Value.Int64())
		if !ok {
			return nil, fmt.Errorf("client handle not found")
		}
		return h, nil
	}

	newJarInst := func(id int64) *runtime.Instance {
		return &runtime.Instance{
			Class:  e.httpCookieJarClass,
			Fields: map[string]runtime.Value{"handle": runtime.NewInt64(id)},
		}
	}

	// CookieJar class
	cookieJarClass := &runtime.Class{
		Name:    "CookieJar",
		Fields:  []runtime.Field{{Name: "handle"}},
		Methods: map[string][]runtime.Function{},
	}
	cookieJarClass.Methods["cookies"] = []runtime.Function{{Name: "cookies", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("CookieJar.cookies expects url argument")
		}
		urlStr, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("CookieJar.cookies url must be string")
		}
		id, ok := this.Fields["handle"].(runtime.Int)
		if !ok {
			return nil, fmt.Errorf("invalid jar handle")
		}
		e.httpCookieJarMu.Lock()
		jar, ok := e.lookupCookieJar(id.Value.Int64())
		e.httpCookieJarMu.Unlock()
		if !ok {
			return nil, fmt.Errorf("jar handle not found")
		}
		parsedURL, err := neturl.Parse(urlStr.Value)
		if err != nil {
			return nil, fmt.Errorf("CookieJar.cookies invalid url: %v", err)
		}
		cookies := jar.Cookies(parsedURL)
		elems := make([]runtime.Value, 0, len(cookies))
		for _, c := range cookies {
			entries := map[string]runtime.DictEntry{}
			putDict(entries, "name", runtime.String{Value: c.Name})
			putDict(entries, "value", runtime.String{Value: c.Value})
			putDict(entries, "domain", runtime.String{Value: c.Domain})
			putDict(entries, "path", runtime.String{Value: c.Path})
			putDict(entries, "secure", runtime.Bool{Value: c.Secure})
			putDict(entries, "httpOnly", runtime.Bool{Value: c.HttpOnly})
			elems = append(elems, runtime.DictFromEntries(entries, nil))
		}
		return &runtime.List{Elements: elems}, nil
	}}}
	cookieJarClass.Methods["setcookies"] = []runtime.Function{{Name: "setCookies", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("CookieJar.setCookies expects (url, list<dict>)")
		}
		urlStr, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("CookieJar.setCookies url must be string")
		}
		list, ok := args[1].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("CookieJar.setCookies expects a list of cookies")
		}
		id, ok := this.Fields["handle"].(runtime.Int)
		if !ok {
			return nil, fmt.Errorf("invalid jar handle")
		}
		e.httpCookieJarMu.Lock()
		jar, found := e.lookupCookieJar(id.Value.Int64())
		e.httpCookieJarMu.Unlock()
		if !found {
			return nil, fmt.Errorf("jar handle not found")
		}
		parsedURL, err := neturl.Parse(urlStr.Value)
		if err != nil {
			return nil, fmt.Errorf("CookieJar.setCookies invalid url: %v", err)
		}
		cookies := make([]*http.Cookie, 0, len(list.Elements))
		for i, el := range list.Elements {
			d, ok := el.(runtime.Dict)
			if !ok {
				return nil, fmt.Errorf("CookieJar.setCookies element %d is not a dict", i)
			}
			name, _ := dictStringField(d, "name")
			value, _ := dictStringField(d, "value")
			if name == "" {
				return nil, fmt.Errorf("CookieJar.setCookies element %d missing name", i)
			}
			c := &http.Cookie{Name: name, Value: value}
			if domain, ok := dictStringField(d, "domain"); ok {
				c.Domain = domain
			}
			if path, ok := dictStringField(d, "path"); ok {
				c.Path = path
			}
			if secure, ok := dictBoolField(d, "secure"); ok {
				c.Secure = secure
			}
			if httpOnly, ok := dictBoolField(d, "httpOnly"); ok {
				c.HttpOnly = httpOnly
			}
			cookies = append(cookies, c)
		}
		jar.SetCookies(parsedURL, cookies)
		return runtime.Null{}, nil
	}}}
	cookieJarClass.Methods["clear"] = []runtime.Function{{Name: "clear", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		id, ok := this.Fields["handle"].(runtime.Int)
		if !ok {
			return nil, fmt.Errorf("invalid jar handle")
		}
		newJar, _ := cookiejar.New(nil)
		e.httpCookieJarMu.Lock()
		ensureMap(&e.httpCookieJars)[id.Value.Int64()] = newJar
		e.httpCookieJarMu.Unlock()
		return runtime.Null{}, nil
	}}}

	// Client class
	clientClass := &runtime.Class{
		Name:    "Client",
		Fields:  []runtime.Field{{Name: "handle"}},
		Methods: map[string][]runtime.Function{},
	}
	clientClass.Methods["get"] = []runtime.Function{{Name: "get", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("Client.get expects url and optional headers")
		}
		urlStr, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Client.get url must be string")
		}
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest(http.MethodGet, resolveURL(h.baseURL, urlStr.Value), nil)
		if err != nil {
			return nil, err
		}
		applyDefaultHeaders(h, req)
		if len(args) == 2 {
			if err := applyRequestHeaders(nil, req, args[1]); err != nil {
				return nil, err
			}
		}
		return doHTTPRequest(h.client, req)
	}}}
	clientClass.Methods["post"] = []runtime.Function{{Name: "post", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("Client.post expects url, body, and optional headers")
		}
		urlStr, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Client.post url must be string")
		}
		body, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Client.post body must be string")
		}
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest(http.MethodPost, resolveURL(h.baseURL, urlStr.Value), strings.NewReader(body.Value))
		if err != nil {
			return nil, err
		}
		applyDefaultHeaders(h, req)
		if len(args) == 3 {
			if err := applyRequestHeaders(nil, req, args[2]); err != nil {
				return nil, err
			}
		}
		return doHTTPRequest(h.client, req)
	}}}
	clientClass.Methods["request"] = []runtime.Function{{Name: "request", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Client.request expects one options dict")
		}
		opts, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("Client.request options must be dict")
		}
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		method := "GET"
		if v, ok := dictStringField(opts, "method"); ok {
			method = v
		}
		urlStr, ok := dictStringField(opts, "url")
		if !ok {
			return nil, fmt.Errorf("Client.request options.url is required")
		}
		body := ""
		if v, ok := dictStringField(opts, "body"); ok {
			body = v
		}
		req, err := http.NewRequest(method, resolveURL(h.baseURL, urlStr), strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		applyDefaultHeaders(h, req)
		if hdrs, ok := dictField(opts, "headers"); ok {
			if err := applyRequestHeaders(nil, req, hdrs); err != nil {
				return nil, err
			}
		}
		client := h.client
		if v, ok := dictField(opts, "timeoutMs"); ok {
			if ms, ok := toInt64(v); ok {
				client = &http.Client{
					Jar:       h.client.Jar,
					Transport: h.client.Transport,
					Timeout:   time.Duration(ms) * time.Millisecond,
				}
			}
		}
		retry, err := httpRetryOptionsFromDict(opts)
		if err != nil {
			return nil, err
		}
		return doHTTPRequestWithRetries(client, req, retry)
	}}}
	clientClass.Methods["settimeout"] = []runtime.Function{{Name: "setTimeout", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Client.setTimeout expects milliseconds argument")
		}
		ms, ok := toInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("Client.setTimeout argument must be int")
		}
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		h.client.Timeout = time.Duration(ms) * time.Millisecond
		return runtime.Null{}, nil
	}}}
	clientClass.Methods["setbaseurl"] = []runtime.Function{{Name: "setBaseUrl", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Client.setBaseUrl expects url argument")
		}
		urlStr, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Client.setBaseUrl argument must be string")
		}
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		h.baseURL = urlStr.Value
		return runtime.Null{}, nil
	}}}
	clientClass.Methods["setdefaultheader"] = []runtime.Function{{Name: "setDefaultHeader", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("Client.setDefaultHeader expects name and value arguments")
		}
		name, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Client.setDefaultHeader name must be string")
		}
		val, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Client.setDefaultHeader value must be string")
		}
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		h.headers.Set(name.Value, val.Value)
		return runtime.Null{}, nil
	}}}
	clientClass.Methods["attachcookiejar"] = []runtime.Function{{Name: "attachCookieJar", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Client.attachCookieJar expects a CookieJar argument")
		}
		jarInst, ok := args[0].(*runtime.Instance)
		if !ok {
			return nil, fmt.Errorf("Client.attachCookieJar argument must be a CookieJar instance")
		}
		jarIDVal, ok := jarInst.Fields["handle"].(runtime.Int)
		if !ok {
			return nil, fmt.Errorf("Client.attachCookieJar invalid jar instance")
		}
		e.httpCookieJarMu.Lock()
		jar, ok := e.lookupCookieJar(jarIDVal.Value.Int64())
		e.httpCookieJarMu.Unlock()
		if !ok {
			return nil, fmt.Errorf("Client.attachCookieJar jar not found")
		}
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		h.client.Jar = jar
		return runtime.Null{}, nil
	}}}
	clientClass.Methods["cookiejar"] = []runtime.Function{{Name: "cookieJar", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		e.httpCookieJarMu.Lock()
		defer e.httpCookieJarMu.Unlock()
		for id, jar := range e.httpCookieJars {
			if jar == h.client.Jar {
				return newJarInst(id), nil
			}
		}
		jar, _ := cookiejar.New(nil)
		h.client.Jar = jar
		id := e.nextCookieJarID
		e.nextCookieJarID++
		ensureMap(&e.httpCookieJars)[id] = jar
		return newJarInst(id), nil
	}}}

	// Helpers for async and batch operations
	doHTTPRequestAsync := func(client *http.Client, req *http.Request) *runtime.Task {
		task := runtime.NewTask()
		go func() {
			result, err := doHTTPRequest(client, req)
			task.Complete(result, err)
		}()
		return task
	}

	buildRequestFromSpec := func(h *httpClientHandle, spec runtime.Dict) (*http.Request, string, error) {
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
		resolvedURL := urlStr
		if h != nil {
			resolvedURL = resolveURL(h.baseURL, urlStr)
		}
		req, err := http.NewRequest(method, resolvedURL, strings.NewReader(body))
		if err != nil {
			return nil, resolvedURL, err
		}
		if h != nil {
			applyDefaultHeaders(h, req)
		}
		if hdrs, ok := dictField(spec, "headers"); ok {
			if err := applyRequestHeaders(nil, req, hdrs); err != nil {
				return nil, resolvedURL, err
			}
		}
		return req, resolvedURL, nil
	}

	clientClass.Methods["getasync"] = []runtime.Function{{Name: "getAsync", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("Client.getAsync expects url and optional headers")
		}
		urlStr, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Client.getAsync url must be string")
		}
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest(http.MethodGet, resolveURL(h.baseURL, urlStr.Value), nil)
		if err != nil {
			return nil, err
		}
		applyDefaultHeaders(h, req)
		if len(args) == 2 {
			if err := applyRequestHeaders(nil, req, args[1]); err != nil {
				return nil, err
			}
		}
		return doHTTPRequestAsync(h.client, req), nil
	}}}
	clientClass.Methods["postasync"] = []runtime.Function{{Name: "postAsync", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("Client.postAsync expects url, body, and optional headers")
		}
		urlStr, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Client.postAsync url must be string")
		}
		body, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Client.postAsync body must be string")
		}
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest(http.MethodPost, resolveURL(h.baseURL, urlStr.Value), strings.NewReader(body.Value))
		if err != nil {
			return nil, err
		}
		applyDefaultHeaders(h, req)
		if len(args) == 3 {
			if err := applyRequestHeaders(nil, req, args[2]); err != nil {
				return nil, err
			}
		}
		return doHTTPRequestAsync(h.client, req), nil
	}}}
	clientClass.Methods["requestasync"] = []runtime.Function{{Name: "requestAsync", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Client.requestAsync expects one options dict")
		}
		opts, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("Client.requestAsync options must be dict")
		}
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		req, _, err := buildRequestFromSpec(h, opts)
		if err != nil {
			return nil, err
		}
		client := h.client
		if v, ok := dictField(opts, "timeoutMs"); ok {
			if ms, ok := toInt64(v); ok {
				client = &http.Client{
					Jar:       h.client.Jar,
					Transport: h.client.Transport,
					Timeout:   time.Duration(ms) * time.Millisecond,
				}
			}
		}
		return doHTTPRequestAsync(client, req), nil
	}}}

	// FetchStream class - completion-ordered streaming parallel fetch
	fetchStreamClass := &runtime.Class{
		Name:    "FetchStream",
		Fields:  []runtime.Field{{Name: "handle"}},
		Methods: map[string][]runtime.Function{},
	}

	getFetchStreamHandle := func(this *runtime.Instance) (*httpFetchStreamHandle, error) {
		id, ok := this.Fields["handle"].(runtime.Int)
		if !ok {
			return nil, fmt.Errorf("invalid fetch stream handle")
		}
		e.httpFetchStreamMu.Lock()
		defer e.httpFetchStreamMu.Unlock()
		sh, ok := e.httpFetchStreams[id.Value.Int64()]
		if !ok {
			return nil, fmt.Errorf("fetch stream handle not found")
		}
		return sh, nil
	}

	releaseFetchStream := func(this *runtime.Instance) {
		if id, ok := this.Fields["handle"].(runtime.Int); ok {
			e.httpFetchStreamMu.Lock()
			delete(e.httpFetchStreams, id.Value.Int64())
			e.httpFetchStreamMu.Unlock()
		}
	}
	nextFn := func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		sh, err := getFetchStreamHandle(this)
		if err != nil {
			return runtime.Null{}, nil
		}
		sh.mu.Lock()
		if sh.read >= sh.total {
			sh.mu.Unlock()
			return runtime.Null{}, nil
		}
		sh.mu.Unlock()
		result := <-sh.ch
		sh.mu.Lock()
		sh.read++
		done := sh.read >= sh.total
		sh.mu.Unlock()
		if done {
			releaseFetchStream(this)
		}
		return result, nil
	}
	fetchStreamClass.Methods["next"] = []runtime.Function{{Name: "next", Native: nextFn}}
	fetchStreamClass.Methods["nextasync"] = []runtime.Function{{Name: "nextAsync", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		task := runtime.NewTask()
		go func() {
			result, err := nextFn(this, args)
			task.Complete(result, err)
		}()
		return task, nil
	}}}
	fetchStreamClass.Methods["remaining"] = []runtime.Function{{Name: "remaining", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		sh, err := getFetchStreamHandle(this)
		if err != nil {
			return runtime.NewInt64(0), nil
		}
		sh.mu.Lock()
		defer sh.mu.Unlock()
		return runtime.NewInt64(int64(sh.total - sh.read)), nil
	}}}
	fetchStreamClass.Methods["done"] = []runtime.Function{{Name: "done", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		sh, err := getFetchStreamHandle(this)
		if err != nil {
			return runtime.Bool{Value: true}, nil
		}
		sh.mu.Lock()
		defer sh.mu.Unlock()
		return runtime.Bool{Value: sh.read >= sh.total}, nil
	}}}

	buildClientStreamRequest := func(h *httpClientHandle, el runtime.Value) (*http.Request, error) {
		switch v := el.(type) {
		case runtime.Dict:
			req, _, err := buildRequestFromSpec(h, v)
			return req, err
		case *runtime.Instance:
			if v.Class != nil && strings.EqualFold(v.Class.Name, "Builder") {
				req, _, err := httpRequestFromBuilder(v)
				return req, err
			}
		}
		return nil, fmt.Errorf("request must be a dict spec or a request Builder")
	}

	spawnFetchStream := func(h *httpClientHandle, specs []runtime.Value) (*runtime.Instance, error) {
		ch := make(chan runtime.Value, len(specs))
		sh := &httpFetchStreamHandle{ch: ch, total: len(specs)}
		e.httpFetchStreamMu.Lock()
		id := e.nextFetchStreamID
		e.nextFetchStreamID++
		ensureMap(&e.httpFetchStreams)[id] = sh
		e.httpFetchStreamMu.Unlock()
		for i, specVal := range specs {
			go func(idx int, sv runtime.Value) {
				req, reqErr := buildClientStreamRequest(h, sv)
				resolvedURL := ""
				if req != nil {
					resolvedURL = req.URL.String()
				}
				if reqErr != nil {
					ch <- httpStreamResult(httpErrorResult(reqErr), idx, resolvedURL)
					return
				}
				var client *http.Client
				if h != nil {
					client = h.client
				} else {
					client = http.DefaultClient
				}
				result, doErr := doHTTPRequest(client, req)
				if doErr != nil {
					ch <- httpStreamResult(httpErrorResult(doErr), idx, resolvedURL)
					return
				}
				ch <- httpStreamResult(result, idx, resolvedURL)
			}(i, specVal)
		}
		return &runtime.Instance{Class: fetchStreamClass, Fields: map[string]runtime.Value{"handle": runtime.NewInt64(id)}}, nil
	}

	clientClass.Methods["fetchall"] = []runtime.Function{{Name: "fetchAll", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("Client.fetchAll expects a list of requests and optional options")
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("Client.fetchAll argument must be a list")
		}
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		prepare := func(el runtime.Value) (*http.Request, *http.Client, error) {
			switch v := el.(type) {
			case runtime.Dict:
				req, _, reqErr := buildRequestFromSpec(h, v)
				return req, h.client, reqErr
			case *runtime.Instance:
				if v.Class != nil && strings.EqualFold(v.Class.Name, "Builder") {
					req, _, reqErr := httpRequestFromBuilder(v)
					return req, h.client, reqErr
				}
			}
			return nil, nil, fmt.Errorf("request must be a dict spec or a request Builder")
		}
		return httpRunBatch(list.Elements, httpBatchLimit(args, 1), prepare), nil
	}}}
	clientClass.Methods["fetchstream"] = []runtime.Function{{Name: "fetchStream", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Client.fetchStream expects a list of request specs")
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("Client.fetchStream argument must be a list")
		}
		h, err := getClientHandle(this)
		if err != nil {
			return nil, err
		}
		return spawnFetchStream(h, list.Elements)
	}}}

	// RequestBuilder class
	builderClass := &runtime.Class{
		Name: "Builder",
		Fields: []runtime.Field{
			{Name: "_url"}, {Name: "_method"}, {Name: "_body"},
			{Name: "_timeoutMs"}, {Name: "_headers"}, {Name: "_query"},
		},
		Methods: map[string][]runtime.Function{},
	}
	builderClass.Methods["method"] = []runtime.Function{{Name: "method", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Builder.method expects one argument")
		}
		m, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.method argument must be string")
		}
		this.Fields["_method"] = m
		return this, nil
	}}}
	builderClass.Methods["header"] = []runtime.Function{{Name: "header", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("Builder.header expects name and value arguments")
		}
		name, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.header name must be string")
		}
		val, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.header value must be string")
		}
		var hdrs runtime.HTTPHeaders
		if existing, ok := this.Fields["_headers"].(runtime.HTTPHeaders); ok {
			hdrs = existing
		} else {
			hdrs = runtime.HTTPHeaders{Values: map[string][]string{}}
		}
		hdrs.Values[http.CanonicalHeaderKey(name.Value)] = []string{val.Value}
		this.Fields["_headers"] = hdrs
		return this, nil
	}}}
	builderClass.Methods["body"] = []runtime.Function{{Name: "body", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Builder.body expects one argument")
		}
		b, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.body argument must be string")
		}
		this.Fields["_body"] = b
		return this, nil
	}}}
	builderClass.Methods["timeout"] = []runtime.Function{{Name: "timeout", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Builder.timeout expects milliseconds argument")
		}
		if !native.IsInt(args[0]) {
			return nil, fmt.Errorf("Builder.timeout argument must be int")
		}
		this.Fields["_timeoutMs"] = args[0]
		return this, nil
	}}}
	builderClass.Methods["send"] = []runtime.Function{{Name: "send", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		req, client, err := httpRequestFromBuilder(this)
		if err != nil {
			return nil, err
		}
		return doHTTPRequest(client, req)
	}}}

	cloneBuilder := func(this *runtime.Instance) *runtime.Instance {
		fields := make(map[string]runtime.Value, len(this.Fields))
		for k, v := range this.Fields {
			fields[k] = v
		}
		if h, ok := this.Fields["_headers"].(runtime.HTTPHeaders); ok {
			nh := runtime.HTTPHeaders{Values: map[string][]string{}}
			for k, vs := range h.Values {
				nh.Values[k] = append([]string(nil), vs...)
			}
			fields["_headers"] = nh
		}
		if q, ok := this.Fields["_query"].(*runtime.List); ok {
			fields["_query"] = &runtime.List{Elements: append([]runtime.Value(nil), q.Elements...)}
		}
		return &runtime.Instance{Class: this.Class, Fields: fields}
	}
	setBuilderHeader := func(this *runtime.Instance, name, value string) {
		hdrs, ok := this.Fields["_headers"].(runtime.HTTPHeaders)
		if !ok {
			hdrs = runtime.HTTPHeaders{Values: map[string][]string{}}
			this.Fields["_headers"] = hdrs
		}
		hdrs.Values[http.CanonicalHeaderKey(name)] = []string{value}
	}

	builderClass.Methods["withmethod"] = []runtime.Function{{Name: "withMethod", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Builder.withMethod expects one argument")
		}
		m, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.withMethod argument must be string")
		}
		next := cloneBuilder(this)
		next.Fields["_method"] = m
		return next, nil
	}}}
	builderClass.Methods["withheader"] = []runtime.Function{{Name: "withHeader", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("Builder.withHeader expects name and value arguments")
		}
		name, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.withHeader name must be string")
		}
		val, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.withHeader value must be string")
		}
		next := cloneBuilder(this)
		setBuilderHeader(next, name.Value, val.Value)
		return next, nil
	}}}
	builderClass.Methods["withheaders"] = []runtime.Function{{Name: "withHeaders", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Builder.withHeaders expects a dict argument")
		}
		dict, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("Builder.withHeaders argument must be a dict")
		}
		next := cloneBuilder(this)
		for _, __dk := range dict.EntryKeys() {
			entry, _ := dict.GetEntry(__dk)
			name, ok := entry.Key.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("Builder.withHeaders keys must be strings")
			}
			val, ok := entry.Value.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("Builder.withHeaders values must be strings")
			}
			setBuilderHeader(next, name.Value, val.Value)
		}
		return next, nil
	}}}
	builderClass.Methods["withquery"] = []runtime.Function{{Name: "withQuery", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("Builder.withQuery expects name and value arguments")
		}
		name, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.withQuery name must be string")
		}
		val, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.withQuery value must be string")
		}
		next := cloneBuilder(this)
		list, ok := next.Fields["_query"].(*runtime.List)
		if !ok {
			list = &runtime.List{}
		}
		pair := &runtime.List{Elements: []runtime.Value{name, val}}
		next.Fields["_query"] = &runtime.List{Elements: append(append([]runtime.Value(nil), list.Elements...), pair)}
		return next, nil
	}}}
	builderClass.Methods["withbody"] = []runtime.Function{{Name: "withBody", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Builder.withBody expects one argument")
		}
		b, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.withBody argument must be string")
		}
		next := cloneBuilder(this)
		next.Fields["_body"] = b
		delete(next.Fields, "_bodyFile")
		return next, nil
	}}}
	builderClass.Methods["withbodyfile"] = []runtime.Function{{Name: "withBodyFile", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Builder.withBodyFile expects one argument (path)")
		}
		p, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.withBodyFile argument must be a path string")
		}
		next := cloneBuilder(this)
		next.Fields["_bodyFile"] = p
		delete(next.Fields, "_body")
		return next, nil
	}}}
	builderClass.Methods["withjson"] = []runtime.Function{{Name: "withJson", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Builder.withJson expects one argument")
		}
		encoded, err := native.EncodeJSONValue(args[0])
		if err != nil {
			return nil, err
		}
		next := cloneBuilder(this)
		next.Fields["_body"] = runtime.String{Value: encoded}
		delete(next.Fields, "_bodyFile")
		setBuilderHeader(next, "Content-Type", "application/json")
		return next, nil
	}}}
	builderClass.Methods["withbearer"] = []runtime.Function{{Name: "withBearer", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Builder.withBearer expects a token argument")
		}
		token, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.withBearer token must be string")
		}
		next := cloneBuilder(this)
		setBuilderHeader(next, "Authorization", "Bearer "+token.Value)
		return next, nil
	}}}
	builderClass.Methods["withbasicauth"] = []runtime.Function{{Name: "withBasicAuth", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("Builder.withBasicAuth expects user and password arguments")
		}
		user, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.withBasicAuth user must be string")
		}
		pass, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Builder.withBasicAuth password must be string")
		}
		next := cloneBuilder(this)
		creds := base64.StdEncoding.EncodeToString([]byte(user.Value + ":" + pass.Value))
		setBuilderHeader(next, "Authorization", "Basic "+creds)
		return next, nil
	}}}
	builderClass.Methods["withtimeout"] = []runtime.Function{{Name: "withTimeout", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Builder.withTimeout expects milliseconds argument")
		}
		if !native.IsInt(args[0]) {
			return nil, fmt.Errorf("Builder.withTimeout argument must be int")
		}
		next := cloneBuilder(this)
		next.Fields["_timeoutMs"] = args[0]
		return next, nil
	}}}

	return []*runtime.Class{cookieJarClass, clientClass, builderClass, fetchStreamClass}
}

// httpRequestFromBuilder materialises an *http.Request and the client to
// run it from a Builder instance's fields. Shared by Builder.send and the
