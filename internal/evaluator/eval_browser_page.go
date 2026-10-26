package evaluator

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"time"

	"geblang/internal/cdp"
	"geblang/internal/imagediff"
	"geblang/internal/native"
	"geblang/internal/runtime"
)

func strArg(args []runtime.Value, i int, name, what string) (string, error) {
	if len(args) <= i {
		return "", fmt.Errorf("%s: missing %s", name, what)
	}
	s, ok := args[i].(runtime.String)
	if !ok {
		return "", fmt.Errorf("%s: %s must be a string", name, what)
	}
	return s.Value, nil
}

func numArg(args []runtime.Value, i int, name, what string) (float64, error) {
	if len(args) <= i {
		return 0, fmt.Errorf("%s: missing %s", name, what)
	}
	f, err := native.FloatLike(args[i])
	if err != nil {
		return 0, fmt.Errorf("%s: %s must be a number", name, what)
	}
	return f, nil
}

func maxArgs(args []runtime.Value, n int, name, shape string) error {
	if len(args) > n {
		return fmt.Errorf("%s expects %s", name, shape)
	}
	return nil
}

// pageEvalRaw runs JS in the page and returns the raw JSON result value, surfacing JS exceptions as an error.
func pageEvalRaw(p *cdp.Page, js string) (json.RawMessage, error) {
	res, err := p.Send("Runtime.evaluate", map[string]any{"expression": js, "returnByValue": true, "awaitPromise": true})
	if err != nil {
		return nil, err
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	if r.ExceptionDetails != nil {
		msg := r.ExceptionDetails.Exception.Description
		if msg == "" {
			msg = r.ExceptionDetails.Text
		}
		return nil, fmt.Errorf("page.evaluate: %s", msg)
	}
	return r.Result.Value, nil
}

func rawToValue(raw json.RawMessage) runtime.Value {
	if len(raw) == 0 || string(raw) == "null" {
		return runtime.Null{}
	}
	val, perr := native.ParseJSONText(string(raw))
	if perr != nil {
		return runtime.Null{}
	}
	return val
}

func pageEvaluate(p *cdp.Page, js string) (runtime.Value, error) {
	raw, err := pageEvalRaw(p, js)
	if err != nil {
		return nil, err
	}
	return rawToValue(raw), nil
}

// onElement runs body with `el` bound to the first match of selector, failing when nothing matches.
func onElement(p *cdp.Page, sel, body string) (runtime.Value, error) {
	return pageEvaluate(p, fmt.Sprintf("(() => { const el = document.querySelector(%s); if (!el) throw new Error('no element matches ' + %s); %s })()", jsLit(sel), jsLit(sel), body))
}

// queryElement runs body with `el` bound to the first match of selector, or null when nothing matches.
func queryElement(p *cdp.Page, sel, body string) (runtime.Value, error) {
	return pageEvaluate(p, fmt.Sprintf("(() => { const el = document.querySelector(%s); %s })()", jsLit(sel), body))
}

const jsVisible = "!!el && el.isConnected && el.checkVisibility({visibilityProperty: true}) && el.getBoundingClientRect().width > 0 && el.getBoundingClientRect().height > 0"

func elementPoint(p *cdp.Page, sel string, offset *[2]float64) (float64, float64, error) {
	pos := "r.left + r.width / 2, r.top + r.height / 2"
	if offset != nil {
		pos = fmt.Sprintf("r.left + %v, r.top + %v", offset[0], offset[1])
	}
	raw, err := pageEvalRaw(p, fmt.Sprintf("(() => { const el = document.querySelector(%s); if (!el) throw new Error('no element matches ' + %s); el.scrollIntoView({block: 'center', inline: 'center'}); const r = el.getBoundingClientRect(); return [%s]; })()", jsLit(sel), jsLit(sel), pos))
	if err != nil {
		return 0, 0, err
	}
	var xy []float64
	if json.Unmarshal(raw, &xy) != nil || len(xy) != 2 {
		return 0, 0, fmt.Errorf("%q is not clickable", sel)
	}
	return xy[0], xy[1], nil
}

func optButton(o runtime.Dict, name string) (string, int, error) {
	button, err := optString(o, "button", name, "left")
	if err != nil {
		return "", 0, err
	}
	if !cdp.ValidButton(button) {
		return "", 0, fmt.Errorf("%s: button must be left, right or middle", name)
	}
	count, err := optInt(o, "clickCount", name, 1)
	if err != nil {
		return "", 0, err
	}
	if count < 1 {
		return "", 0, fmt.Errorf("%s: clickCount must be at least 1", name)
	}
	return button, count, nil
}

func clickElement(p *cdp.Page, sel string, o runtime.Dict, name string) error {
	button, count, err := optButton(o, name)
	if err != nil {
		return err
	}
	var offset *[2]float64
	if pos, ok, err := optDict(o, "position", name); err != nil {
		return err
	} else if ok {
		x, err := optFloat(pos, "x", name, 0)
		if err != nil {
			return err
		}
		y, err := optFloat(pos, "y", name, 0)
		if err != nil {
			return err
		}
		offset = &[2]float64{x, y}
	}
	x, y, err := elementPoint(p, sel, offset)
	if err != nil {
		return err
	}
	return p.MouseClick(x, y, button, count)
}

func layoutSize(p *cdp.Page) (float64, float64, error) {
	res, err := p.Send("Page.getLayoutMetrics", nil)
	if err != nil {
		return 0, 0, err
	}
	var m struct {
		Content struct {
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		} `json:"cssContentSize"`
	}
	if err := json.Unmarshal(res, &m); err != nil {
		return 0, 0, err
	}
	return m.Content.Width, m.Content.Height, nil
}

// captureScreenshot takes a screenshot as the options describe; forcePNG ignores format and quality.
func captureScreenshot(p *cdp.Page, o runtime.Dict, name string, forcePNG bool) ([]byte, error) {
	params := map[string]any{"format": "png"}
	if !forcePNG {
		format, err := optString(o, "format", name, "png")
		if err != nil {
			return nil, err
		}
		switch format {
		case "png":
		case "jpeg", "webp":
			params["format"] = format
			quality, err := optInt(o, "quality", name, 80)
			if err != nil {
				return nil, err
			}
			if quality < 0 || quality > 100 {
				return nil, fmt.Errorf("%s: quality must be between 0 and 100", name)
			}
			params["quality"] = quality
		default:
			return nil, fmt.Errorf("%s: format must be png, jpeg or webp", name)
		}
	}
	sel, err := optString(o, "selector", name, "")
	if err != nil {
		return nil, err
	}
	clip, hasClip, err := optDict(o, "clip", name)
	if err != nil {
		return nil, err
	}
	full, err := optBool(o, "fullPage", name, false)
	if err != nil {
		return nil, err
	}
	areas := 0
	for _, set := range []bool{sel != "", hasClip, full} {
		if set {
			areas++
		}
	}
	if areas > 1 {
		return nil, fmt.Errorf("%s: use only one of selector, clip and fullPage", name)
	}
	switch {
	case sel != "":
		raw, err := pageEvalRaw(p, fmt.Sprintf("(() => { const el = document.querySelector(%s); if (!el) throw new Error('no element matches ' + %s); const r = el.getBoundingClientRect(); return [r.left + scrollX, r.top + scrollY, r.width, r.height]; })()", jsLit(sel), jsLit(sel)))
		if err != nil {
			return nil, err
		}
		var box []float64
		if json.Unmarshal(raw, &box) != nil || len(box) != 4 || box[2] <= 0 || box[3] <= 0 {
			return nil, fmt.Errorf("%s: %q has no visible area", name, sel)
		}
		params["clip"] = map[string]any{"x": box[0], "y": box[1], "width": box[2], "height": box[3], "scale": 1}
		params["captureBeyondViewport"] = true
	case hasClip:
		var vals [4]float64
		for i, k := range []string{"x", "y", "width", "height"} {
			if vals[i], err = optFloat(clip, k, name, 0); err != nil {
				return nil, err
			}
		}
		if vals[2] <= 0 || vals[3] <= 0 {
			return nil, fmt.Errorf("%s: clip needs a positive width and height", name)
		}
		raw, err := pageEvalRaw(p, "[scrollX, scrollY]")
		if err != nil {
			return nil, err
		}
		var scroll []float64
		if json.Unmarshal(raw, &scroll) != nil || len(scroll) != 2 {
			scroll = []float64{0, 0}
		}
		params["clip"] = map[string]any{"x": vals[0] + scroll[0], "y": vals[1] + scroll[1], "width": vals[2], "height": vals[3], "scale": 1}
	case full:
		w, h, err := layoutSize(p)
		if err != nil {
			return nil, err
		}
		params["clip"] = map[string]any{"x": 0, "y": 0, "width": w, "height": h, "scale": 1}
		params["captureBeyondViewport"] = true
	}
	omit, err := optBool(o, "omitBackground", name, false)
	if err != nil {
		return nil, err
	}
	if omit {
		if _, err := p.Send("Emulation.setDefaultBackgroundColorOverride", map[string]any{"color": map[string]any{"r": 0, "g": 0, "b": 0, "a": 0}}); err != nil {
			return nil, err
		}
		defer func() { _, _ = p.Send("Emulation.setDefaultBackgroundColorOverride", nil) }()
	}
	return sendForData(p, "Page.captureScreenshot", params)
}

func sendForData(p *cdp.Page, method string, params map[string]any) ([]byte, error) {
	res, err := p.Send(method, params)
	if err != nil {
		return nil, err
	}
	var r struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(r.Data)
}

func pdfParams(o runtime.Dict, name string) (map[string]any, error) {
	params := map[string]any{}
	for _, k := range []string{"landscape", "printBackground", "preferCSSPageSize"} {
		if _, ok := dictField(o, k); ok {
			v, err := optBool(o, k, name, false)
			if err != nil {
				return nil, err
			}
			params[k] = v
		}
	}
	for _, k := range []string{"scale", "paperWidth", "paperHeight"} {
		if _, ok := dictField(o, k); ok {
			v, err := optFloat(o, k, name, 0)
			if err != nil {
				return nil, err
			}
			params[k] = v
		}
	}
	if margins, ok, err := optDict(o, "margins", name); err != nil {
		return nil, err
	} else if ok {
		for side, param := range map[string]string{"top": "marginTop", "bottom": "marginBottom", "left": "marginLeft", "right": "marginRight"} {
			if _, ok := dictField(margins, side); ok {
				v, err := optFloat(margins, side, name, 0)
				if err != nil {
					return nil, err
				}
				params[param] = v
			}
		}
	}
	if ranges, err := optString(o, "pageRanges", name, ""); err != nil {
		return nil, err
	} else if ranges != "" {
		params["pageRanges"] = ranges
	}
	return params, nil
}

func requestDict(r cdp.Request) runtime.Dict {
	var status, duration runtime.Value = runtime.Null{}, runtime.Null{}
	if r.Status > 0 {
		status = runtime.NewInt64(int64(r.Status))
	}
	if r.Finished && r.End >= r.Start {
		duration = runtime.Float{Value: (r.End - r.Start) * 1000}
	}
	return orderedDict(
		"url", runtime.String{Value: r.URL},
		"method", runtime.String{Value: r.Method},
		"resourceType", runtime.String{Value: r.ResourceType},
		"headers", stringMapDict(r.Headers),
		"status", status,
		"statusText", runtime.String{Value: r.StatusText},
		"mimeType", runtime.String{Value: r.MimeType},
		"responseHeaders", stringMapDict(r.RespHeaders),
		"fromCache", runtime.Bool{Value: r.FromCache},
		"failed", nullableString(r.Failed),
		"finished", runtime.Bool{Value: r.Finished},
		"durationMs", duration,
	)
}

func (e *Evaluator) pageClass() *runtime.Class {
	if e.pageClassCache != nil {
		return e.pageClassCache
	}
	get := func(this *runtime.Instance) (*cdp.Page, error) {
		id, ok := handleOf(this)
		if !ok {
			return nil, fmt.Errorf("invalid page handle")
		}
		e.browserMu.Lock()
		defer e.browserMu.Unlock()
		p, ok := e.pages[id]
		if !ok {
			return nil, fmt.Errorf("page is closed")
		}
		return p, nil
	}
	cls := &runtime.Class{Name: "Page", Module: "browser", Fields: []runtime.Field{{Name: "handle"}}, Methods: map[string][]runtime.Function{}}
	method := func(name string, fn func(p *cdp.Page, args []runtime.Value) (runtime.Value, error)) {
		nativeMethod(cls, name, func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			p, err := get(this)
			if err != nil {
				return nil, err
			}
			return fn(p, args)
		})
	}
	simple := func(name, what string, fn func(p *cdp.Page, s string) (runtime.Value, error)) {
		method(name, func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
			if err := maxArgs(args, 1, "Page."+name, "("+what+")"); err != nil {
				return nil, err
			}
			s, err := strArg(args, 0, "Page."+name, what)
			if err != nil {
				return nil, err
			}
			return fn(p, s)
		})
	}
	null := func(err error) (runtime.Value, error) { return runtime.Null{}, err }

	method("goto", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.goto"
		if err := maxArgs(args, 2, name, "(url, opts?)"); err != nil {
			return nil, err
		}
		url, err := strArg(args, 0, name, "url")
		if err != nil {
			return nil, err
		}
		o, err := optsArg(args, 1, name)
		if err != nil {
			return nil, err
		}
		event, err := optWaitUntil(o, name)
		if err != nil {
			return nil, err
		}
		timeout, err := optTimeout(o, name)
		if err != nil {
			return nil, err
		}
		return null(p.Navigate(url, event, timeout))
	})
	navOpts := func(args []runtime.Value, n int, name string) (string, time.Duration, error) {
		o, err := optsArg(args, n, name)
		if err != nil {
			return "", 0, err
		}
		event, err := optWaitUntil(o, name)
		if err != nil {
			return "", 0, err
		}
		timeout, err := optTimeout(o, name)
		return event, timeout, err
	}
	method("reload", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := maxArgs(args, 1, "Page.reload", "(opts?)"); err != nil {
			return nil, err
		}
		event, timeout, err := navOpts(args, 0, "Page.reload")
		if err != nil {
			return nil, err
		}
		return null(p.Reload(event, timeout))
	})
	history := func(name string, delta int) {
		method(name, func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
			if err := maxArgs(args, 1, "Page."+name, "(opts?)"); err != nil {
				return nil, err
			}
			event, timeout, err := navOpts(args, 0, "Page."+name)
			if err != nil {
				return nil, err
			}
			moved, err := p.History(delta, event, timeout)
			return runtime.Bool{Value: moved}, err
		})
	}
	history("goBack", -1)
	history("goForward", 1)
	method("waitForNavigation", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := maxArgs(args, 1, "Page.waitForNavigation", "(opts?)"); err != nil {
			return nil, err
		}
		event, timeout, err := navOpts(args, 0, "Page.waitForNavigation")
		if err != nil {
			return nil, err
		}
		return null(p.WaitNavigation(event, timeout))
	})
	simple("setContent", "html", func(p *cdp.Page, html string) (runtime.Value, error) {
		_, err := p.Send("Page.setDocumentContent", map[string]any{"frameId": p.TargetID, "html": html})
		return null(err)
	})
	simple("addInitScript", "script", func(p *cdp.Page, js string) (runtime.Value, error) {
		_, err := p.Send("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": js})
		return null(err)
	})
	method("bringToFront", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := noArgs(args, "Page.bringToFront"); err != nil {
			return nil, err
		}
		_, err := p.Send("Page.bringToFront", nil)
		return null(err)
	})

	simple("evaluate", "jsExpression", func(p *cdp.Page, js string) (runtime.Value, error) {
		return pageEvaluate(p, js)
	})
	method("content", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		return pageEvaluate(p, "document.documentElement.outerHTML")
	})
	method("title", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		return pageEvaluate(p, "document.title")
	})
	method("url", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		return pageEvaluate(p, "location.href")
	})
	simple("text", "selector", func(p *cdp.Page, sel string) (runtime.Value, error) {
		return queryElement(p, sel, "return el ? el.textContent : null;")
	})
	method("attribute", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		sel, name, err := twoStrings(args, "Page.attribute", "name")
		if err != nil {
			return nil, err
		}
		return queryElement(p, sel, fmt.Sprintf("return el ? el.getAttribute(%s) : null;", jsLit(name)))
	})
	simple("count", "selector", func(p *cdp.Page, sel string) (runtime.Value, error) {
		return pageEvaluate(p, fmt.Sprintf("document.querySelectorAll(%s).length", jsLit(sel)))
	})
	simple("isVisible", "selector", func(p *cdp.Page, sel string) (runtime.Value, error) {
		return queryElement(p, sel, "return "+jsVisible+";")
	})
	simple("isEnabled", "selector", func(p *cdp.Page, sel string) (runtime.Value, error) {
		return onElement(p, sel, "return !el.disabled && !el.closest('fieldset:disabled');")
	})
	simple("texts", "selector", func(p *cdp.Page, sel string) (runtime.Value, error) {
		return pageEvaluate(p, fmt.Sprintf("Array.from(document.querySelectorAll(%s), el => el.textContent)", jsLit(sel)))
	})
	simple("innerHTML", "selector", func(p *cdp.Page, sel string) (runtime.Value, error) {
		return queryElement(p, sel, "return el ? el.innerHTML : null;")
	})
	simple("inputValue", "selector", func(p *cdp.Page, sel string) (runtime.Value, error) {
		return onElement(p, sel, "if (!('value' in el)) throw new Error('element has no value'); return el.value;")
	})
	simple("boundingBox", "selector", func(p *cdp.Page, sel string) (runtime.Value, error) {
		return queryElement(p, sel, "if (!el) return null; const r = el.getBoundingClientRect(); return {x: r.x, y: r.y, width: r.width, height: r.height};")
	})

	method("waitFor", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.waitFor"
		if err := maxArgs(args, 2, name, "(selector, timeoutMs | opts?)"); err != nil {
			return nil, err
		}
		sel, err := strArg(args, 0, name, "selector")
		if err != nil {
			return nil, err
		}
		o := runtime.NewDict()
		timeout := defaultBrowserTimeout
		if len(args) == 2 {
			if n, ok := native.AsInt64(args[1]); ok {
				timeout = time.Duration(n) * time.Millisecond
			} else if o, err = optsArg(args, 1, name); err != nil {
				return nil, err
			} else if timeout, err = optTimeout(o, name); err != nil {
				return nil, err
			}
		}
		state, err := optString(o, "state", name, "attached")
		if err != nil {
			return nil, err
		}
		var cond string
		switch state {
		case "attached":
			cond = "!!el"
		case "detached":
			cond = "!el"
		case "visible":
			cond = jsVisible
		case "hidden":
			cond = "!(" + jsVisible + ")"
		default:
			return nil, fmt.Errorf("%s: state must be attached, detached, visible or hidden", name)
		}
		js := fmt.Sprintf("(() => { const el = document.querySelector(%s); return %s; })()", jsLit(sel), cond)
		deadline := time.Now().Add(timeout)
		for {
			if raw, err := pageEvalRaw(p, js); err == nil && string(raw) == "true" {
				return runtime.Null{}, nil
			}
			if time.Now().After(deadline) {
				return nil, runtime.ClassifiedError{Class: "TimeoutError", Message: fmt.Sprintf("Page.waitFor: %q not %s within %v", sel, state, timeout)}
			}
			time.Sleep(25 * time.Millisecond)
		}
	})
	method("waitForFunction", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.waitForFunction"
		if err := maxArgs(args, 2, name, "(jsExpression, opts?)"); err != nil {
			return nil, err
		}
		js, err := strArg(args, 0, name, "jsExpression")
		if err != nil {
			return nil, err
		}
		o, err := optsArg(args, 1, name)
		if err != nil {
			return nil, err
		}
		timeout, err := optTimeout(o, name)
		if err != nil {
			return nil, err
		}
		poll, err := optInt(o, "pollMs", name, 50)
		if err != nil {
			return nil, err
		}
		if poll <= 0 {
			return nil, fmt.Errorf("%s: pollMs must be positive", name)
		}
		wrapped := fmt.Sprintf("(async () => { const v = await (%s); return v ? {ok: true, v} : {ok: false}; })()", js)
		deadline := time.Now().Add(timeout)
		for {
			raw, err := pageEvalRaw(p, wrapped)
			if err != nil {
				return nil, err
			}
			var r struct {
				OK bool            `json:"ok"`
				V  json.RawMessage `json:"v"`
			}
			if json.Unmarshal(raw, &r) == nil && r.OK {
				return rawToValue(r.V), nil
			}
			if time.Now().After(deadline) {
				return nil, runtime.ClassifiedError{Class: "TimeoutError", Message: fmt.Sprintf("%s: condition not met within %v", name, timeout)}
			}
			time.Sleep(time.Duration(poll) * time.Millisecond)
		}
	})
	waitNet := func(name string, response bool) {
		method(name, func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
			full := "Page." + name
			if err := maxArgs(args, 2, full, "(urlPattern, opts?)"); err != nil {
				return nil, err
			}
			pattern, err := strArg(args, 0, full, "urlPattern")
			if err != nil {
				return nil, err
			}
			o, err := optsArg(args, 1, full)
			if err != nil {
				return nil, err
			}
			timeout, err := optTimeout(o, full)
			if err != nil {
				return nil, err
			}
			r, err := p.WaitRequest(pattern, response, timeout)
			if err != nil {
				return nil, err
			}
			d := requestDict(r)
			if response {
				var body runtime.Value = runtime.Null{}
				if r.Failed == "" {
					if text, ok := p.ResponseBody(r.ID); ok {
						body = runtime.String{Value: text}
					}
				}
				key := runtime.String{Value: "body"}
				d.PutEntry(dictKey(key), runtime.DictEntry{Key: key, Value: body})
			}
			return d, nil
		})
	}
	waitNet("waitForRequest", false)
	waitNet("waitForResponse", true)
	method("requests", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := noArgs(args, "Page.requests"); err != nil {
			return nil, err
		}
		reqs := p.Requests()
		out := make([]runtime.Value, len(reqs))
		for i, r := range reqs {
			out[i] = requestDict(r)
		}
		return &runtime.List{Elements: out}, nil
	})

	method("consoleMessages", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := noArgs(args, "Page.consoleMessages"); err != nil {
			return nil, err
		}
		msgs := p.ConsoleMessages()
		out := make([]runtime.Value, len(msgs))
		for i, m := range msgs {
			out[i] = orderedDict("type", runtime.String{Value: m.Type}, "text", runtime.String{Value: m.Text},
				"url", runtime.String{Value: m.URL}, "line", runtime.NewInt64(int64(m.Line)), "column", runtime.NewInt64(int64(m.Column)))
		}
		return &runtime.List{Elements: out}, nil
	})
	method("pageErrors", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := noArgs(args, "Page.pageErrors"); err != nil {
			return nil, err
		}
		errs := p.PageErrors()
		out := make([]runtime.Value, len(errs))
		for i, pe := range errs {
			out[i] = orderedDict("message", runtime.String{Value: pe.Message}, "url", runtime.String{Value: pe.URL},
				"line", runtime.NewInt64(int64(pe.Line)), "column", runtime.NewInt64(int64(pe.Column)))
		}
		return &runtime.List{Elements: out}, nil
	})
	method("clearConsole", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := noArgs(args, "Page.clearConsole"); err != nil {
			return nil, err
		}
		p.ClearConsole()
		return runtime.Null{}, nil
	})
	method("onDialog", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.onDialog"
		if err := maxArgs(args, 2, name, "(action, promptText?)"); err != nil {
			return nil, err
		}
		action, err := strArg(args, 0, name, "action")
		if err != nil {
			return nil, err
		}
		if action != "accept" && action != "dismiss" {
			return nil, fmt.Errorf("%s: action must be accept or dismiss", name)
		}
		prompt := ""
		if len(args) == 2 {
			if prompt, err = strArg(args, 1, name, "promptText"); err != nil {
				return nil, err
			}
		}
		p.SetDialogPolicy(action == "accept", prompt)
		return runtime.Null{}, nil
	})
	method("dialogs", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := noArgs(args, "Page.dialogs"); err != nil {
			return nil, err
		}
		ds := p.Dialogs()
		out := make([]runtime.Value, len(ds))
		for i, d := range ds {
			out[i] = orderedDict("type", runtime.String{Value: d.Type}, "message", runtime.String{Value: d.Message},
				"defaultPrompt", runtime.String{Value: d.DefaultPrompt}, "accepted", runtime.Bool{Value: d.Accepted})
		}
		return &runtime.List{Elements: out}, nil
	})

	simple("setDownloadPath", "dir", func(p *cdp.Page, dir string) (runtime.Value, error) {
		return null(p.SetDownloadPath(dir))
	})
	method("waitForDownload", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.waitForDownload"
		if err := maxArgs(args, 1, name, "(opts?)"); err != nil {
			return nil, err
		}
		o, err := optsArg(args, 0, name)
		if err != nil {
			return nil, err
		}
		timeout, err := optTimeout(o, name)
		if err != nil {
			return nil, err
		}
		d, path, err := p.WaitDownload(timeout)
		if err != nil {
			return nil, err
		}
		return orderedDict("path", runtime.String{Value: path}, "suggestedName", runtime.String{Value: d.SuggestedName}, "url", runtime.String{Value: d.URL}), nil
	})

	method("click", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := maxArgs(args, 2, "Page.click", "(selector, opts?)"); err != nil {
			return nil, err
		}
		sel, err := strArg(args, 0, "Page.click", "selector")
		if err != nil {
			return nil, err
		}
		o, err := optsArg(args, 1, "Page.click")
		if err != nil {
			return nil, err
		}
		return null(clickElement(p, sel, o, "Page.click"))
	})
	simple("dblclick", "selector", func(p *cdp.Page, sel string) (runtime.Value, error) {
		x, y, err := elementPoint(p, sel, nil)
		if err != nil {
			return nil, err
		}
		return null(p.MouseClick(x, y, "left", 2))
	})
	simple("hover", "selector", func(p *cdp.Page, sel string) (runtime.Value, error) {
		x, y, err := elementPoint(p, sel, nil)
		if err != nil {
			return nil, err
		}
		return null(p.MouseMove(x, y, 1))
	})
	simple("focus", "selector", func(p *cdp.Page, sel string) (runtime.Value, error) {
		_, err := onElement(p, sel, "el.focus();")
		return null(err)
	})
	setChecked := func(name string, want bool) {
		simple(name, "selector", func(p *cdp.Page, sel string) (runtime.Value, error) {
			read := "if (!('checked' in el)) throw new Error('element is not a checkbox or radio'); return el.checked;"
			v, err := onElement(p, sel, read)
			if err != nil {
				return nil, err
			}
			if b, ok := v.(runtime.Bool); ok && b.Value == want {
				return runtime.Null{}, nil
			}
			if err := clickElement(p, sel, runtime.NewDict(), "Page."+name); err != nil {
				return nil, err
			}
			v, err = onElement(p, sel, read)
			if err != nil {
				return nil, err
			}
			if b, ok := v.(runtime.Bool); !ok || b.Value != want {
				return nil, fmt.Errorf("Page.%s: %q did not change state", name, sel)
			}
			return runtime.Null{}, nil
		})
	}
	setChecked("check", true)
	setChecked("uncheck", false)
	method("type", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		sel, text, err := twoStrings(args, "Page.type", "text")
		if err != nil {
			return nil, err
		}
		if _, err := onElement(p, sel, "el.focus();"); err != nil {
			return nil, err
		}
		_, err = p.Send("Input.insertText", map[string]any{"text": text})
		return null(err)
	})
	method("fill", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		sel, val, err := twoStrings(args, "Page.fill", "value")
		if err != nil {
			return nil, err
		}
		_, err = onElement(p, sel, fmt.Sprintf("el.value = %s; el.dispatchEvent(new Event('input', {bubbles: true})); el.dispatchEvent(new Event('change', {bubbles: true}));", jsLit(val)))
		return null(err)
	})
	method("select", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		sel, val, err := twoStrings(args, "Page.select", "value")
		if err != nil {
			return nil, err
		}
		_, err = onElement(p, sel, fmt.Sprintf("el.value = %s; el.dispatchEvent(new Event('change', {bubbles: true}));", jsLit(val)))
		return null(err)
	})
	method("setInputFiles", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.setInputFiles"
		if len(args) != 2 {
			return nil, fmt.Errorf("%s expects (selector, paths)", name)
		}
		sel, err := strArg(args, 0, name, "selector")
		if err != nil {
			return nil, err
		}
		var paths []string
		switch v := args[1].(type) {
		case runtime.String:
			paths = []string{v.Value}
		case *runtime.List:
			for _, el := range v.Elements {
				s, ok := el.(runtime.String)
				if !ok {
					return nil, fmt.Errorf("%s: paths must be strings", name)
				}
				paths = append(paths, s.Value)
			}
		default:
			return nil, fmt.Errorf("%s: paths must be a string or list of strings", name)
		}
		files := make([]string, len(paths))
		for i, path := range paths {
			abs, err := filepath.Abs(path)
			if err != nil {
				return nil, err
			}
			if _, err := os.Stat(abs); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			files[i] = abs
		}
		doc, err := p.Send("DOM.getDocument", map[string]any{"depth": 0})
		if err != nil {
			return nil, err
		}
		var root struct {
			Root struct {
				NodeID int `json:"nodeId"`
			} `json:"root"`
		}
		if err := json.Unmarshal(doc, &root); err != nil {
			return nil, err
		}
		found, err := p.Send("DOM.querySelector", map[string]any{"nodeId": root.Root.NodeID, "selector": sel})
		if err != nil {
			return nil, err
		}
		var node struct {
			NodeID int `json:"nodeId"`
		}
		if err := json.Unmarshal(found, &node); err != nil || node.NodeID == 0 {
			return nil, fmt.Errorf("%s: no element matches %q", name, sel)
		}
		_, err = p.Send("DOM.setFileInputFiles", map[string]any{"files": files, "nodeId": node.NodeID})
		return null(err)
	})
	simple("press", "key", func(p *cdp.Page, key string) (runtime.Value, error) {
		return null(p.Press(key))
	})
	simple("keyDown", "key", func(p *cdp.Page, key string) (runtime.Value, error) {
		return null(p.KeyDown(key))
	})
	simple("keyUp", "key", func(p *cdp.Page, key string) (runtime.Value, error) {
		return null(p.KeyUp(key))
	})
	method("mouseMove", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.mouseMove"
		if err := maxArgs(args, 3, name, "(x, y, opts?)"); err != nil {
			return nil, err
		}
		x, err := numArg(args, 0, name, "x")
		if err != nil {
			return nil, err
		}
		y, err := numArg(args, 1, name, "y")
		if err != nil {
			return nil, err
		}
		o, err := optsArg(args, 2, name)
		if err != nil {
			return nil, err
		}
		steps, err := optInt(o, "steps", name, 1)
		if err != nil {
			return nil, err
		}
		return null(p.MouseMove(x, y, steps))
	})
	button := func(name string, down bool) {
		method(name, func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
			full := "Page." + name
			if err := maxArgs(args, 1, full, "(opts?)"); err != nil {
				return nil, err
			}
			o, err := optsArg(args, 0, full)
			if err != nil {
				return nil, err
			}
			b, count, err := optButton(o, full)
			if err != nil {
				return nil, err
			}
			if down {
				return null(p.MouseDown(b, count))
			}
			return null(p.MouseUp(b, count))
		})
	}
	button("mouseDown", true)
	button("mouseUp", false)
	method("mouseClick", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.mouseClick"
		if err := maxArgs(args, 3, name, "(x, y, opts?)"); err != nil {
			return nil, err
		}
		x, err := numArg(args, 0, name, "x")
		if err != nil {
			return nil, err
		}
		y, err := numArg(args, 1, name, "y")
		if err != nil {
			return nil, err
		}
		o, err := optsArg(args, 2, name)
		if err != nil {
			return nil, err
		}
		b, count, err := optButton(o, name)
		if err != nil {
			return nil, err
		}
		return null(p.MouseClick(x, y, b, count))
	})
	method("drag", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.drag"
		if err := maxArgs(args, 5, name, "(fromX, fromY, toX, toY, opts?)"); err != nil {
			return nil, err
		}
		var pts [4]float64
		for i, what := range []string{"fromX", "fromY", "toX", "toY"} {
			v, err := numArg(args, i, name, what)
			if err != nil {
				return nil, err
			}
			pts[i] = v
		}
		o, err := optsArg(args, 4, name)
		if err != nil {
			return nil, err
		}
		steps, err := optInt(o, "steps", name, 10)
		if err != nil {
			return nil, err
		}
		if err := p.MouseMove(pts[0], pts[1], 1); err != nil {
			return nil, err
		}
		if err := p.MouseDown("left", 1); err != nil {
			return nil, err
		}
		if err := p.MouseMove(pts[2], pts[3], steps); err != nil {
			return nil, err
		}
		return null(p.MouseUp("left", 1))
	})
	method("wheel", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.wheel"
		if len(args) != 2 {
			return nil, fmt.Errorf("%s expects (deltaX, deltaY)", name)
		}
		dx, err := numArg(args, 0, name, "deltaX")
		if err != nil {
			return nil, err
		}
		dy, err := numArg(args, 1, name, "deltaY")
		if err != nil {
			return nil, err
		}
		return null(p.Wheel(dx, dy))
	})

	method("setViewport", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.setViewport"
		if err := maxArgs(args, 3, name, "(width, height, opts?)"); err != nil {
			return nil, err
		}
		if len(args) < 2 {
			return nil, fmt.Errorf("%s expects (width, height, opts?)", name)
		}
		o, err := optsArg(args, 2, name)
		if err != nil {
			return nil, err
		}
		merged := orderedDict("width", args[0], "height", args[1])
		for _, k := range []string{"deviceScaleFactor", "mobile"} {
			if v, ok := dictField(o, k); ok {
				key := runtime.String{Value: k}
				merged.PutEntry(dictKey(key), runtime.DictEntry{Key: key, Value: v})
			}
		}
		vp, err := parseViewport(merged, name)
		if err != nil {
			return nil, err
		}
		return null(setViewport(p, *vp))
	})
	simple("setUserAgent", "userAgent", func(p *cdp.Page, ua string) (runtime.Value, error) {
		_, err := p.Send("Emulation.setUserAgentOverride", map[string]any{"userAgent": ua})
		return null(err)
	})
	simple("setTimezone", "timezoneId", func(p *cdp.Page, tz string) (runtime.Value, error) {
		_, err := p.Send("Emulation.setTimezoneOverride", map[string]any{"timezoneId": tz})
		return null(err)
	})
	simple("setLocale", "locale", func(p *cdp.Page, locale string) (runtime.Value, error) {
		_, err := p.Send("Emulation.setLocaleOverride", map[string]any{"locale": locale})
		return null(err)
	})
	method("emulateMedia", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.emulateMedia"
		if len(args) != 1 {
			return nil, fmt.Errorf("%s expects (opts)", name)
		}
		o, err := optsArg(args, 0, name)
		if err != nil {
			return nil, err
		}
		media, err := optString(o, "media", name, "")
		if err != nil {
			return nil, err
		}
		if media != "" && media != "screen" && media != "print" {
			return nil, fmt.Errorf("%s: media must be screen or print", name)
		}
		var features []map[string]any
		for _, f := range []struct{ key, feature string }{{"colorScheme", "prefers-color-scheme"}, {"reducedMotion", "prefers-reduced-motion"}} {
			v, err := optString(o, f.key, name, "")
			if err != nil {
				return nil, err
			}
			if v != "" {
				features = append(features, map[string]any{"name": f.feature, "value": v})
			}
		}
		_, err = p.Send("Emulation.setEmulatedMedia", map[string]any{"media": media, "features": features})
		return null(err)
	})
	boolSetter := func(name string, send func(p *cdp.Page, on bool) error) {
		method(name, func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("Page.%s expects (bool)", name)
			}
			b, ok := args[0].(runtime.Bool)
			if !ok {
				return nil, fmt.Errorf("Page.%s: argument must be a bool", name)
			}
			return null(send(p, b.Value))
		})
	}
	boolSetter("setOffline", func(p *cdp.Page, on bool) error {
		_, err := p.Send("Network.emulateNetworkConditions", map[string]any{"offline": on, "latency": 0, "downloadThroughput": -1, "uploadThroughput": -1})
		return err
	})
	boolSetter("emulateFocus", func(p *cdp.Page, on bool) error {
		_, err := p.Send("Emulation.setFocusEmulationEnabled", map[string]any{"enabled": on})
		return err
	})
	method("setExtraHeaders", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Page.setExtraHeaders expects (headers)")
		}
		d, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("Page.setExtraHeaders: headers must be a dict of strings")
		}
		headers := map[string]any{}
		var bad error
		d.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			k, ok1 := entry.Key.(runtime.String)
			v, ok2 := entry.Value.(runtime.String)
			if !ok1 || !ok2 {
				bad = fmt.Errorf("Page.setExtraHeaders: headers must be a dict of strings")
				return false
			}
			headers[k.Value] = v.Value
			return true
		})
		if bad != nil {
			return nil, bad
		}
		_, err := p.Send("Network.setExtraHTTPHeaders", map[string]any{"headers": headers})
		return null(err)
	})

	method("screenshot", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := maxArgs(args, 2, "Page.screenshot", "(path, opts?)"); err != nil {
			return nil, err
		}
		path, err := strArg(args, 0, "Page.screenshot", "path")
		if err != nil {
			return nil, err
		}
		o, err := optsArg(args, 1, "Page.screenshot")
		if err != nil {
			return nil, err
		}
		data, err := captureScreenshot(p, o, "Page.screenshot", false)
		if err != nil {
			return nil, err
		}
		return null(os.WriteFile(path, data, 0o644))
	})
	method("screenshotBytes", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := maxArgs(args, 1, "Page.screenshotBytes", "(opts?)"); err != nil {
			return nil, err
		}
		o, err := optsArg(args, 0, "Page.screenshotBytes")
		if err != nil {
			return nil, err
		}
		data, err := captureScreenshot(p, o, "Page.screenshotBytes", false)
		if err != nil {
			return nil, err
		}
		return runtime.Bytes{Value: data}, nil
	})
	method("pdf", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := maxArgs(args, 2, "Page.pdf", "(path, opts?)"); err != nil {
			return nil, err
		}
		path, err := strArg(args, 0, "Page.pdf", "path")
		if err != nil {
			return nil, err
		}
		o, err := optsArg(args, 1, "Page.pdf")
		if err != nil {
			return nil, err
		}
		params, err := pdfParams(o, "Page.pdf")
		if err != nil {
			return nil, err
		}
		data, err := sendForData(p, "Page.printToPDF", params)
		if err != nil {
			return nil, err
		}
		return null(os.WriteFile(path, data, 0o644))
	})
	method("matchScreenshot", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.matchScreenshot"
		if err := maxArgs(args, 2, name, "(baselinePath, opts?)"); err != nil {
			return nil, err
		}
		baseline, err := strArg(args, 0, name, "baselinePath")
		if err != nil {
			return nil, err
		}
		o, err := optsArg(args, 1, name)
		if err != nil {
			return nil, err
		}
		threshold, err := optThreshold(o, name)
		if err != nil {
			return nil, err
		}
		maxRatio, err := optFloat(o, "maxDiffRatio", name, 0)
		if err != nil {
			return nil, err
		}
		update, err := optBool(o, "update", name, false)
		if err != nil {
			return nil, err
		}
		diffPath, err := optString(o, "diffPath", name, "")
		if err != nil {
			return nil, err
		}
		actualPath, err := optString(o, "actualPath", name, "")
		if err != nil {
			return nil, err
		}
		shot, err := captureScreenshot(p, o, name, true)
		if err != nil {
			return nil, err
		}
		actual, err := imagediff.Decode(shot)
		if err != nil {
			return nil, err
		}
		existing, readErr := os.ReadFile(baseline)
		if update || os.IsNotExist(readErr) {
			if dir := filepath.Dir(baseline); dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return nil, err
				}
			}
			if err := os.WriteFile(baseline, shot, 0o644); err != nil {
				return nil, err
			}
			b := actual.Bounds()
			d := compareResultDict(imagediff.Result{Width: b.Dx(), Height: b.Dy(), SameSize: true})
			return withMatch(d, true, true), nil
		}
		if readErr != nil {
			return nil, readErr
		}
		var expected image.Image
		if expected, err = imagediff.Decode(existing); err != nil {
			return nil, fmt.Errorf("%s: baseline: %w", name, err)
		}
		r, diff := imagediff.Compare(expected, actual, threshold)
		matched := r.SameSize && r.Ratio <= maxRatio
		if !matched {
			if diffPath != "" && diff != nil {
				if err := imagediff.WritePNG(diffPath, diff); err != nil {
					return nil, err
				}
			}
			if actualPath != "" {
				if err := os.WriteFile(actualPath, shot, 0o644); err != nil {
					return nil, err
				}
			}
		}
		return withMatch(compareResultDict(r), matched, false), nil
	})
	method("startScreencast", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		const name = "Page.startScreencast"
		if err := maxArgs(args, 2, name, "(dir, opts?)"); err != nil {
			return nil, err
		}
		dir, err := strArg(args, 0, name, "dir")
		if err != nil {
			return nil, err
		}
		o, err := optsArg(args, 1, name)
		if err != nil {
			return nil, err
		}
		format, err := optString(o, "format", name, "jpeg")
		if err != nil {
			return nil, err
		}
		if format != "jpeg" && format != "png" {
			return nil, fmt.Errorf("%s: format must be jpeg or png", name)
		}
		quality, err := optInt(o, "quality", name, 80)
		if err != nil {
			return nil, err
		}
		every, err := optInt(o, "everyNthFrame", name, 1)
		if err != nil {
			return nil, err
		}
		if every < 1 {
			return nil, fmt.Errorf("%s: everyNthFrame must be at least 1", name)
		}
		return null(p.StartScreencast(dir, format, quality, every))
	})
	method("stopScreencast", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if err := noArgs(args, "Page.stopScreencast"); err != nil {
			return nil, err
		}
		n, err := p.StopScreencast()
		return runtime.NewInt64(int64(n)), err
	})

	method("cookies", func(p *cdp.Page, _ []runtime.Value) (runtime.Value, error) {
		res, err := p.Send("Network.getCookies", nil)
		if err != nil {
			return nil, err
		}
		return cookieList(res), nil
	})
	method("setCookie", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		params, err := cookieParams(args, "Page.setCookie")
		if err != nil {
			return nil, err
		}
		_, err = p.Send("Network.setCookie", params)
		return null(err)
	})
	method("clearCookies", func(p *cdp.Page, _ []runtime.Value) (runtime.Value, error) {
		_, err := p.Send("Network.clearBrowserCookies", nil)
		return null(err)
	})
	method("route", func(p *cdp.Page, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("Page.route expects (urlPattern, handler)")
		}
		pattern, ok := args[0].(runtime.String)
		handler, ok2 := args[1].(runtime.Function)
		if !ok || !ok2 {
			return nil, fmt.Errorf("Page.route: (urlPattern, handler) expected")
		}
		if _, err := p.Send("Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": pattern.Value}}}); err != nil {
			return nil, err
		}
		e.browserMu.Lock()
		if e.browserRoutes == nil {
			e.browserRoutes = map[string][]browserRoute{}
		}
		e.browserRoutes[p.SessionID] = append(e.browserRoutes[p.SessionID], browserRoute{pattern: pattern.Value, handler: handler, page: p})
		e.browserMu.Unlock()
		p.Browser().OnEvent("Fetch.requestPaused", e.handleFetchPaused)
		return runtime.Null{}, nil
	})
	nativeMethod(cls, "close", func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		id, ok := handleOf(this)
		if !ok {
			return runtime.Null{}, nil
		}
		e.browserMu.Lock()
		p := e.pages[id]
		delete(e.pages, id)
		if p != nil {
			delete(e.browserRoutes, p.SessionID)
		}
		e.browserMu.Unlock()
		if p != nil {
			p.Close()
		}
		return runtime.Null{}, nil
	})
	e.pageClassCache = cls
	return cls
}

func withMatch(d runtime.Dict, matched, created bool) runtime.Dict {
	out := orderedDict("matched", runtime.Bool{Value: matched}, "created", runtime.Bool{Value: created})
	d.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
		out.PutEntry(key, entry)
		return true
	})
	return out
}

func twoStrings(args []runtime.Value, name, second string) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("%s expects (selector, %s)", name, second)
	}
	a, ok := args[0].(runtime.String)
	b, ok2 := args[1].(runtime.String)
	if !ok || !ok2 {
		return "", "", fmt.Errorf("%s: (selector, %s) must be strings", name, second)
	}
	return a.Value, b.Value, nil
}
