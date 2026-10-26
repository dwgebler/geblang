package evaluator

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"geblang/internal/ast"
	"geblang/internal/cdp"
	"geblang/internal/imagediff"
	"geblang/internal/native"
	"geblang/internal/runtime"
)

type browserRoute struct {
	pattern string
	handler runtime.Function
	page    *cdp.Page
}

type browserViewport struct {
	width, height int
	scale         float64
	mobile        bool
}

// pageSetup is what a browser or context applies to every page it opens.
type pageSetup struct {
	viewport  *browserViewport
	userAgent string
	locale    string
	timezone  string
}

type browserContext struct {
	browser *cdp.Browser
	id      string
	setup   pageSetup
}

const defaultBrowserTimeout = 30 * time.Second

// jsLit JSON-encodes a string into a safe JavaScript string literal.
func jsLit(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func browserErr(err error) error {
	var te *cdp.TimeoutError
	if errors.As(err, &te) {
		return runtime.ClassifiedError{Class: "TimeoutError", Message: te.Error()}
	}
	return err
}

func optsArg(args []runtime.Value, i int, name string) (runtime.Dict, error) {
	if len(args) <= i {
		return runtime.NewDict(), nil
	}
	switch v := args[i].(type) {
	case runtime.Dict:
		return v, nil
	case runtime.Null:
		return runtime.NewDict(), nil
	}
	return runtime.Dict{}, fmt.Errorf("%s: options must be a dict", name)
}

func optFloat(d runtime.Dict, key, name string, def float64) (float64, error) {
	v, ok := dictField(d, key)
	if !ok {
		return def, nil
	}
	f, err := native.FloatLike(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %s must be a number", name, key)
	}
	return f, nil
}

func optInt(d runtime.Dict, key, name string, def int) (int, error) {
	v, ok := dictField(d, key)
	if !ok {
		return def, nil
	}
	n, ok := native.AsInt64(v)
	if !ok {
		return 0, fmt.Errorf("%s: %s must be an int", name, key)
	}
	return int(n), nil
}

func optBool(d runtime.Dict, key, name string, def bool) (bool, error) {
	v, ok := dictField(d, key)
	if !ok {
		return def, nil
	}
	b, ok := v.(runtime.Bool)
	if !ok {
		return false, fmt.Errorf("%s: %s must be a bool", name, key)
	}
	return b.Value, nil
}

func optString(d runtime.Dict, key, name, def string) (string, error) {
	v, ok := dictField(d, key)
	if !ok {
		return def, nil
	}
	s, ok := v.(runtime.String)
	if !ok {
		return "", fmt.Errorf("%s: %s must be a string", name, key)
	}
	return s.Value, nil
}

func optDict(d runtime.Dict, key, name string) (runtime.Dict, bool, error) {
	v, ok := dictField(d, key)
	if !ok {
		return runtime.Dict{}, false, nil
	}
	sub, ok := v.(runtime.Dict)
	if !ok {
		return runtime.Dict{}, false, fmt.Errorf("%s: %s must be a dict", name, key)
	}
	return sub, true, nil
}

func optTimeout(d runtime.Dict, name string) (time.Duration, error) {
	ms, err := optInt(d, "timeoutMs", name, int(defaultBrowserTimeout/time.Millisecond))
	if err != nil {
		return 0, err
	}
	if ms <= 0 {
		return 0, fmt.Errorf("%s: timeoutMs must be positive", name)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

func optWaitUntil(d runtime.Dict, name string) (string, error) {
	v, err := optString(d, "waitUntil", name, "load")
	if err != nil {
		return "", err
	}
	event, ok := cdp.LifecycleEvent(v)
	if !ok {
		return "", fmt.Errorf("%s: waitUntil must be load, domcontentloaded or networkidle", name)
	}
	return event, nil
}

func parseViewport(d runtime.Dict, name string) (*browserViewport, error) {
	w, err := optInt(d, "width", name, 0)
	if err != nil {
		return nil, err
	}
	h, err := optInt(d, "height", name, 0)
	if err != nil {
		return nil, err
	}
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("%s: viewport needs a positive width and height", name)
	}
	scale, err := optFloat(d, "deviceScaleFactor", name, 1)
	if err != nil {
		return nil, err
	}
	mobile, err := optBool(d, "mobile", name, false)
	if err != nil {
		return nil, err
	}
	return &browserViewport{width: w, height: h, scale: scale, mobile: mobile}, nil
}

func parseSetup(d runtime.Dict, name string, withContextFields bool) (pageSetup, error) {
	var s pageSetup
	if vp, ok, err := optDict(d, "viewport", name); err != nil {
		return s, err
	} else if ok {
		if s.viewport, err = parseViewport(vp, name); err != nil {
			return s, err
		}
	}
	if !withContextFields {
		return s, nil
	}
	var err error
	if s.userAgent, err = optString(d, "userAgent", name, ""); err != nil {
		return s, err
	}
	if s.locale, err = optString(d, "locale", name, ""); err != nil {
		return s, err
	}
	if s.timezone, err = optString(d, "timezone", name, ""); err != nil {
		return s, err
	}
	return s, nil
}

func setViewport(p *cdp.Page, v browserViewport) error {
	_, err := p.Send("Emulation.setDeviceMetricsOverride", map[string]any{"width": v.width, "height": v.height, "deviceScaleFactor": v.scale, "mobile": v.mobile})
	if err == nil {
		_, err = p.Send("Emulation.setTouchEmulationEnabled", map[string]any{"enabled": v.mobile})
	}
	return err
}

func (s pageSetup) apply(p *cdp.Page) error {
	if s.viewport != nil {
		if err := setViewport(p, *s.viewport); err != nil {
			return err
		}
	}
	if s.userAgent != "" {
		if _, err := p.Send("Emulation.setUserAgentOverride", map[string]any{"userAgent": s.userAgent}); err != nil {
			return err
		}
	}
	if s.locale != "" {
		if _, err := p.Send("Emulation.setLocaleOverride", map[string]any{"locale": s.locale}); err != nil {
			return err
		}
	}
	if s.timezone != "" {
		if _, err := p.Send("Emulation.setTimezoneOverride", map[string]any{"timezoneId": s.timezone}); err != nil {
			return err
		}
	}
	return nil
}

// orderedDict builds a dict from alternating string keys and values, keeping their order.
func orderedDict(pairs ...any) runtime.Dict {
	d := runtime.NewDict()
	for i := 0; i+1 < len(pairs); i += 2 {
		key := runtime.String{Value: pairs[i].(string)}
		d.PutEntry(dictKey(key), runtime.DictEntry{Key: key, Value: pairs[i+1].(runtime.Value)})
	}
	return d
}

func stringMapDict(m map[string]string) runtime.Dict {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	d := runtime.NewDict()
	for _, k := range keys {
		key := runtime.String{Value: k}
		d.PutEntry(dictKey(key), runtime.DictEntry{Key: key, Value: runtime.String{Value: m[k]}})
	}
	return d
}

func nullableString(s string) runtime.Value {
	if s == "" {
		return runtime.Null{}
	}
	return runtime.String{Value: s}
}

func (e *Evaluator) browserLaunch(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s expects (opts?)", call.Callee.String())
	}
	if err := native.RequireBrowser("browser.launch"); err != nil {
		return nil, err
	}
	o, err := optsArg(args, 0, "browser.launch")
	if err != nil {
		return nil, err
	}
	opts := cdp.LaunchOptions{Headless: true}
	if opts.Executable, err = optString(o, "executable", "browser.launch", ""); err != nil {
		return nil, err
	}
	if opts.Headless, err = optBool(o, "headless", "browser.launch", true); err != nil {
		return nil, err
	}
	if ms, err := optInt(o, "timeoutMs", "browser.launch", 0); err != nil {
		return nil, err
	} else if ms > 0 {
		opts.Timeout = time.Duration(ms) * time.Millisecond
	}
	if v, ok := dictField(o, "args"); ok {
		list, ok := v.(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("browser.launch: args must be a list of strings")
		}
		for _, el := range list.Elements {
			s, ok := el.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("browser.launch: args must be a list of strings")
			}
			opts.Args = append(opts.Args, s.Value)
		}
	}
	setup, err := parseSetup(o, "browser.launch", false)
	if err != nil {
		return nil, err
	}
	b, err := cdp.Launch(opts)
	if err != nil {
		return nil, fmt.Errorf("browser.launch: %w", err)
	}
	e.browserMu.Lock()
	id := e.nextBrowserID
	e.nextBrowserID++
	e.browsers[id] = b
	if e.browserSetups == nil {
		e.browserSetups = map[*cdp.Browser]pageSetup{}
	}
	e.browserSetups[b] = setup
	e.browserMu.Unlock()
	return &runtime.Instance{Class: e.browserClass(), Fields: map[string]runtime.Value{"handle": runtime.NewInt64(id)}}, nil
}

func imageArg(v runtime.Value, name string) ([]byte, error) {
	switch v := v.(type) {
	case runtime.Bytes:
		return v.Value, nil
	case runtime.String:
		return os.ReadFile(v.Value)
	}
	return nil, fmt.Errorf("%s: images must be a file path or bytes", name)
}

func compareResultDict(r imagediff.Result) runtime.Dict {
	return orderedDict(
		"diffPixels", runtime.NewInt64(int64(r.DiffPixels)),
		"ratio", runtime.Float{Value: r.Ratio},
		"width", runtime.NewInt64(int64(r.Width)),
		"height", runtime.NewInt64(int64(r.Height)),
		"sameSize", runtime.Bool{Value: r.SameSize},
	)
}

func optThreshold(o runtime.Dict, name string) (float64, error) {
	t, err := optFloat(o, "threshold", name, 0.1)
	if err != nil {
		return 0, err
	}
	if t < 0 || t > 1 {
		return 0, fmt.Errorf("%s: threshold must be between 0 and 1", name)
	}
	return t, nil
}

func (e *Evaluator) browserCompareImages(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	const name = "browser.compareImages"
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("%s expects (a, b, opts?)", name)
	}
	var imgs [2][]byte
	for i := range imgs {
		data, err := imageArg(args[i], name)
		if err != nil {
			return nil, err
		}
		imgs[i] = data
	}
	o, err := optsArg(args, 2, name)
	if err != nil {
		return nil, err
	}
	threshold, err := optThreshold(o, name)
	if err != nil {
		return nil, err
	}
	diffPath, err := optString(o, "diffPath", name, "")
	if err != nil {
		return nil, err
	}
	a, err := imagediff.Decode(imgs[0])
	if err != nil {
		return nil, fmt.Errorf("%s: first image: %w", name, err)
	}
	b, err := imagediff.Decode(imgs[1])
	if err != nil {
		return nil, fmt.Errorf("%s: second image: %w", name, err)
	}
	r, diff := imagediff.Compare(a, b, threshold)
	if diffPath != "" && diff != nil {
		if err := imagediff.WritePNG(diffPath, diff); err != nil {
			return nil, err
		}
	}
	return compareResultDict(r), nil
}

func handleOf(this *runtime.Instance) (int64, bool) {
	return native.AsInt64(this.Fields["handle"])
}

func nativeMethod(cls *runtime.Class, name string, fn func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error)) {
	cls.Methods[strings.ToLower(name)] = []runtime.Function{{Name: name, Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		v, err := fn(this, args)
		return v, browserErr(err)
	}}}
}

func noArgs(args []runtime.Value, name string) error {
	if len(args) != 0 {
		return fmt.Errorf("%s takes no arguments", name)
	}
	return nil
}

func (e *Evaluator) pageInstance(p *cdp.Page) runtime.Value {
	e.browserMu.Lock()
	defer e.browserMu.Unlock()
	for pid, existing := range e.pages {
		if existing == p {
			return &runtime.Instance{Class: e.pageClass(), Fields: map[string]runtime.Value{"handle": runtime.NewInt64(pid)}}
		}
	}
	pid := e.nextPageID
	e.nextPageID++
	e.pages[pid] = p
	return &runtime.Instance{Class: e.pageClass(), Fields: map[string]runtime.Value{"handle": runtime.NewInt64(pid)}}
}

func (e *Evaluator) openPage(b *cdp.Browser, contextID string, setup pageSetup) (runtime.Value, error) {
	p, err := b.NewPageIn(contextID)
	if err != nil {
		return nil, err
	}
	if err := setup.apply(p); err != nil {
		_ = p.Close()
		return nil, err
	}
	return e.pageInstance(p), nil
}

// listPages wraps the open page targets accepted by keep, attaching to any the program has not seen.
func (e *Evaluator) listPages(b *cdp.Browser, keep func(cdp.TargetInfo) bool) (runtime.Value, error) {
	infos, err := b.PageTargetInfos()
	if err != nil {
		return nil, err
	}
	e.browserMu.Lock()
	byTarget := map[string]*cdp.Page{}
	for _, pg := range e.pages {
		if pg.Browser() == b {
			byTarget[pg.TargetID] = pg
		}
	}
	e.browserMu.Unlock()
	out := make([]runtime.Value, 0, len(infos))
	for _, info := range infos {
		if !keep(info) {
			continue
		}
		pg, ok := byTarget[info.TargetID]
		if !ok {
			if pg, err = b.AttachPage(info.TargetID); err != nil {
				continue
			}
			pg.ContextID = info.ContextID
		}
		out = append(out, e.pageInstance(pg))
	}
	return &runtime.List{Elements: out}, nil
}

func (e *Evaluator) browserClass() *runtime.Class {
	if e.browserClassCache != nil {
		return e.browserClassCache
	}
	get := func(this *runtime.Instance) (*cdp.Browser, error) {
		id, ok := handleOf(this)
		if !ok {
			return nil, fmt.Errorf("invalid browser handle")
		}
		e.browserMu.Lock()
		defer e.browserMu.Unlock()
		b, ok := e.browsers[id]
		if !ok {
			return nil, fmt.Errorf("browser is closed")
		}
		return b, nil
	}
	cls := &runtime.Class{Name: "Browser", Module: "browser", Fields: []runtime.Field{{Name: "handle"}}, Methods: map[string][]runtime.Function{}}
	nativeMethod(cls, "newPage", func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if err := noArgs(args, "Browser.newPage"); err != nil {
			return nil, err
		}
		b, err := get(this)
		if err != nil {
			return nil, err
		}
		e.browserMu.Lock()
		setup := e.browserSetups[b]
		e.browserMu.Unlock()
		return e.openPage(b, "", setup)
	})
	nativeMethod(cls, "newContext", func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		b, err := get(this)
		if err != nil {
			return nil, err
		}
		if len(args) > 1 {
			return nil, fmt.Errorf("Browser.newContext expects (opts?)")
		}
		o, err := optsArg(args, 0, "Browser.newContext")
		if err != nil {
			return nil, err
		}
		setup, err := parseSetup(o, "Browser.newContext", true)
		if err != nil {
			return nil, err
		}
		if setup.viewport == nil {
			e.browserMu.Lock()
			setup.viewport = e.browserSetups[b].viewport
			e.browserMu.Unlock()
		}
		cid, err := b.NewContext()
		if err != nil {
			return nil, err
		}
		e.browserMu.Lock()
		if e.browserContexts == nil {
			e.browserContexts = map[int64]*browserContext{}
		}
		id := e.nextContextID
		e.nextContextID++
		e.browserContexts[id] = &browserContext{browser: b, id: cid, setup: setup}
		e.browserMu.Unlock()
		return &runtime.Instance{Class: e.contextClass(), Fields: map[string]runtime.Value{"handle": runtime.NewInt64(id)}}, nil
	})
	nativeMethod(cls, "version", func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		b, err := get(this)
		if err != nil {
			return nil, err
		}
		v, err := b.Version()
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: v}, nil
	})
	nativeMethod(cls, "pages", func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		b, err := get(this)
		if err != nil {
			return nil, err
		}
		return e.listPages(b, func(cdp.TargetInfo) bool { return true })
	})
	nativeMethod(cls, "close", func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		id, ok := handleOf(this)
		if !ok {
			return runtime.Null{}, nil
		}
		e.browserMu.Lock()
		b := e.browsers[id]
		delete(e.browsers, id)
		if b != nil {
			delete(e.browserSetups, b)
			for pid, pg := range e.pages {
				if pg.Browser() == b {
					delete(e.pages, pid)
				}
			}
			for cid, c := range e.browserContexts {
				if c.browser == b {
					delete(e.browserContexts, cid)
				}
			}
		}
		e.browserMu.Unlock()
		if b != nil {
			b.Close()
		}
		return runtime.Null{}, nil
	})
	e.browserClassCache = cls
	return cls
}

func cookieList(res json.RawMessage) runtime.Value {
	var r struct {
		Cookies json.RawMessage `json:"cookies"`
	}
	if err := json.Unmarshal(res, &r); err != nil || len(r.Cookies) == 0 {
		return &runtime.List{}
	}
	val, perr := native.ParseJSONText(string(r.Cookies))
	if perr != nil {
		return &runtime.List{}
	}
	return val
}

func cookieParams(args []runtime.Value, name string) (map[string]any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects (cookie)", name)
	}
	d, ok := args[0].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s: argument must be a dict", name)
	}
	raw, err := native.ValueToJSON(d)
	if err != nil {
		return nil, err
	}
	params, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: argument must be a dict", name)
	}
	return params, nil
}

func (e *Evaluator) contextClass() *runtime.Class {
	if e.contextClassCache != nil {
		return e.contextClassCache
	}
	get := func(this *runtime.Instance) (*browserContext, error) {
		id, ok := handleOf(this)
		if !ok {
			return nil, fmt.Errorf("invalid context handle")
		}
		e.browserMu.Lock()
		defer e.browserMu.Unlock()
		c, ok := e.browserContexts[id]
		if !ok {
			return nil, fmt.Errorf("context is closed")
		}
		return c, nil
	}
	cls := &runtime.Class{Name: "Context", Module: "browser", Fields: []runtime.Field{{Name: "handle"}}, Methods: map[string][]runtime.Function{}}
	nativeMethod(cls, "newPage", func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if err := noArgs(args, "Context.newPage"); err != nil {
			return nil, err
		}
		c, err := get(this)
		if err != nil {
			return nil, err
		}
		return e.openPage(c.browser, c.id, c.setup)
	})
	nativeMethod(cls, "pages", func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		c, err := get(this)
		if err != nil {
			return nil, err
		}
		return e.listPages(c.browser, func(info cdp.TargetInfo) bool { return info.ContextID == c.id })
	})
	nativeMethod(cls, "cookies", func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		c, err := get(this)
		if err != nil {
			return nil, err
		}
		res, err := c.browser.Send("", "Storage.getCookies", map[string]any{"browserContextId": c.id})
		if err != nil {
			return nil, err
		}
		return cookieList(res), nil
	})
	nativeMethod(cls, "setCookie", func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		c, err := get(this)
		if err != nil {
			return nil, err
		}
		cookie, err := cookieParams(args, "Context.setCookie")
		if err != nil {
			return nil, err
		}
		_, err = c.browser.Send("", "Storage.setCookies", map[string]any{"cookies": []any{cookie}, "browserContextId": c.id})
		return runtime.Null{}, err
	})
	nativeMethod(cls, "clearCookies", func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		c, err := get(this)
		if err != nil {
			return nil, err
		}
		_, err = c.browser.Send("", "Storage.clearCookies", map[string]any{"browserContextId": c.id})
		return runtime.Null{}, err
	})
	nativeMethod(cls, "close", func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		id, ok := handleOf(this)
		if !ok {
			return runtime.Null{}, nil
		}
		e.browserMu.Lock()
		c := e.browserContexts[id]
		delete(e.browserContexts, id)
		if c != nil {
			for pid, pg := range e.pages {
				if pg.Browser() == c.browser && pg.ContextID == c.id {
					delete(e.pages, pid)
				}
			}
		}
		e.browserMu.Unlock()
		if c != nil {
			return runtime.Null{}, c.browser.DisposeContext(c.id)
		}
		return runtime.Null{}, nil
	})
	e.contextClassCache = cls
	return cls
}

// handleFetchPaused runs on the CDP read goroutine; it dispatches the Geblang route handler on its own goroutine so the read loop stays free to receive the resulting Fetch responses.
func (e *Evaluator) handleFetchPaused(sessionID string, params json.RawMessage) {
	var ev struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL     string            `json:"url"`
			Method  string            `json:"method"`
			Headers map[string]string `json:"headers"`
		} `json:"request"`
		ResourceType string `json:"resourceType"`
	}
	if json.Unmarshal(params, &ev) != nil {
		return
	}
	e.browserMu.Lock()
	routes := append([]browserRoute(nil), e.browserRoutes[sessionID]...)
	e.browserMu.Unlock()

	var page *cdp.Page
	var handler runtime.Function
	for _, r := range routes {
		if cdp.MatchURLPattern(r.pattern, ev.Request.URL) {
			page = r.page
			handler = r.handler
			break
		}
	}
	if page == nil {
		if len(routes) > 0 {
			_, _ = routes[0].page.Send("Fetch.continueRequest", map[string]any{"requestId": ev.RequestID})
		}
		return
	}

	go func() {
		reqDict := fetchRequestDict(ev.Request.URL, ev.Request.Method, ev.Request.Headers, ev.ResourceType)
		callbackEval, callbackHandler := e.callbackEvaluator(handler)
		if callbackEval != e {
			defer callbackEval.Cleanup()
			defer callbackEval.startDebugThread("browser fetch")()
		}
		result, err := callbackEval.applyFunction(callbackHandler, []runtime.Value{reqDict})
		if err != nil {
			_, _ = page.Send("Fetch.failRequest", map[string]any{"requestId": ev.RequestID, "errorReason": "Failed"})
			return
		}
		dict, isDict := result.(runtime.Dict)
		if !isDict {
			_, _ = page.Send("Fetch.continueRequest", map[string]any{"requestId": ev.RequestID})
			return
		}
		if v, ok := dictField(dict, "abort"); ok {
			if b, ok := v.(runtime.Bool); ok && b.Value {
				_, _ = page.Send("Fetch.failRequest", map[string]any{"requestId": ev.RequestID, "errorReason": "Aborted"})
				return
			}
		}
		fulfill := map[string]any{"requestId": ev.RequestID, "responseCode": 200}
		if v, ok := dictField(dict, "status"); ok {
			if n, ok := toInt64(v); ok {
				fulfill["responseCode"] = int(n)
			}
		}
		if v, ok := dictField(dict, "body"); ok {
			if s, ok := v.(runtime.String); ok {
				fulfill["body"] = base64.StdEncoding.EncodeToString([]byte(s.Value))
			}
		}
		if v, ok := dictField(dict, "headers"); ok {
			if hd, ok := v.(runtime.Dict); ok {
				var hdrs []map[string]any
				hd.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
					k, _ := entry.Key.(runtime.String)
					val, _ := entry.Value.(runtime.String)
					hdrs = append(hdrs, map[string]any{"name": k.Value, "value": val.Value})
					return true
				})
				fulfill["responseHeaders"] = hdrs
			}
		}
		_, _ = page.Send("Fetch.fulfillRequest", fulfill)
	}()
}

func fetchRequestDict(url, method string, headers map[string]string, resourceType string) runtime.Dict {
	return orderedDict(
		"url", runtime.String{Value: url},
		"method", runtime.String{Value: method},
		"resourceType", runtime.String{Value: resourceType},
		"headers", stringMapDict(headers),
	)
}
