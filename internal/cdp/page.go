package cdp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// logLimit caps each per-page log so a long-lived page cannot grow without bound.
const logLimit = 1000

// Page is an attached page target; commands carry its session id.
type Page struct {
	browser   *Browser
	TargetID  string
	SessionID string
	ContextID string

	mu           sync.Mutex
	console      []ConsoleMessage
	errors       []PageError
	dialogs      []Dialog
	dialogAccept bool
	promptText   string
	seq          int64
	requests     map[string]*Request
	order        []*Request
	navs         []navigation
	lifecycle    map[string]map[string]bool
	loaders      []string
	cast         *screencast
	downloadDir  string
	reqFloor     int64
	navMark      int64
	dlMark       int64

	mouseX, mouseY float64
	buttons        int
	modifiers      int
}

// ConsoleMessage is one console API call made by the page.
type ConsoleMessage struct {
	Type   string
	Text   string
	URL    string
	Line   int
	Column int
}

// PageError is an uncaught exception thrown in the page.
type PageError struct {
	Message string
	URL     string
	Line    int
	Column  int
}

// Dialog is an alert, confirm, prompt or beforeunload dialog the page opened.
type Dialog struct {
	Type          string
	Message       string
	DefaultPrompt string
	Accepted      bool
}

// Request is one network request made by the page.
type Request struct {
	Seq          int64
	ID           string
	URL          string
	Method       string
	ResourceType string
	Headers      map[string]string
	Status       int
	StatusText   string
	MimeType     string
	RespHeaders  map[string]string
	FromCache    bool
	Failed       string
	Finished     bool
	Start, End   float64

	returned [2]bool
}

type navigation struct {
	seq      int64
	loaderID string
	url      string
	sameDoc  bool
}

type screencast struct {
	dir    string
	ext    string
	frames int
}

func newPage(b *Browser, targetID, sessionID string) *Page {
	return &Page{browser: b, TargetID: targetID, SessionID: sessionID, requests: map[string]*Request{}, lifecycle: map[string]map[string]bool{}}
}

// Close shuts down the page session and removes its target.
func (p *Page) Close() error {
	p.browser.pagesMu.Lock()
	delete(p.browser.pages, p.SessionID)
	p.browser.pagesMu.Unlock()
	_, err := p.browser.Send("", "Target.closeTarget", map[string]any{"targetId": p.TargetID})
	return err
}

// Send issues a CDP command on this page's session.
func (p *Page) Send(method string, params any) (json.RawMessage, error) {
	return p.browser.Send(p.SessionID, method, params)
}

// Browser returns the owning browser (for event registration on its connection).
func (p *Page) Browser() *Browser { return p.browser }

func (p *Page) handleEvent(method string, params json.RawMessage) {
	switch method {
	case "Runtime.consoleAPICalled":
		p.onConsole(params)
	case "Runtime.exceptionThrown":
		p.onException(params)
	case "Page.javascriptDialogOpening":
		p.onDialog(params)
	case "Network.requestWillBeSent":
		p.onRequest(params)
	case "Network.responseReceived":
		p.onResponse(params)
	case "Network.loadingFinished", "Network.loadingFailed":
		p.onLoadingDone(params)
	case "Page.frameNavigated":
		p.onFrameNavigated(params)
	case "Page.navigatedWithinDocument":
		p.onSameDocument(params)
	case "Page.lifecycleEvent":
		p.onLifecycle(params)
	case "Page.screencastFrame":
		p.onScreencastFrame(params)
	}
}

type remoteObject struct {
	Type        string          `json:"type"`
	Value       json.RawMessage `json:"value"`
	Description string          `json:"description"`
}

func (o remoteObject) text() string {
	if len(o.Value) > 0 {
		var s string
		if json.Unmarshal(o.Value, &s) == nil {
			return s
		}
		return string(o.Value)
	}
	if o.Description != "" {
		return o.Description
	}
	return o.Type
}

type callFrame struct {
	URL    string `json:"url"`
	Line   int    `json:"lineNumber"`
	Column int    `json:"columnNumber"`
}

func (p *Page) onConsole(params json.RawMessage) {
	var ev struct {
		Type       string         `json:"type"`
		Args       []remoteObject `json:"args"`
		StackTrace struct {
			CallFrames []callFrame `json:"callFrames"`
		} `json:"stackTrace"`
	}
	if json.Unmarshal(params, &ev) != nil {
		return
	}
	parts := make([]string, len(ev.Args))
	for i, a := range ev.Args {
		parts[i] = a.text()
	}
	msg := ConsoleMessage{Type: ev.Type, Text: strings.Join(parts, " ")}
	if len(ev.StackTrace.CallFrames) > 0 {
		f := ev.StackTrace.CallFrames[0]
		msg.URL, msg.Line, msg.Column = f.URL, f.Line+1, f.Column+1
	}
	p.mu.Lock()
	p.console = appendCapped(p.console, msg)
	p.mu.Unlock()
}

func (p *Page) onException(params json.RawMessage) {
	var ev struct {
		Details struct {
			Text      string       `json:"text"`
			URL       string       `json:"url"`
			Line      int          `json:"lineNumber"`
			Column    int          `json:"columnNumber"`
			Exception remoteObject `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if json.Unmarshal(params, &ev) != nil {
		return
	}
	d := ev.Details
	message := d.Exception.Description
	if message == "" {
		message = d.Text
	}
	p.mu.Lock()
	p.errors = appendCapped(p.errors, PageError{Message: message, URL: d.URL, Line: d.Line + 1, Column: d.Column + 1})
	p.mu.Unlock()
}

func (p *Page) onDialog(params json.RawMessage) {
	var ev struct {
		Type          string `json:"type"`
		Message       string `json:"message"`
		DefaultPrompt string `json:"defaultPrompt"`
	}
	if json.Unmarshal(params, &ev) != nil {
		return
	}
	p.mu.Lock()
	accept := p.dialogAccept || ev.Type == "beforeunload"
	prompt := p.promptText
	p.dialogs = appendCapped(p.dialogs, Dialog{Type: ev.Type, Message: ev.Message, DefaultPrompt: ev.DefaultPrompt, Accepted: accept})
	p.mu.Unlock()
	reply := map[string]any{"accept": accept}
	if accept && ev.Type == "prompt" {
		reply["promptText"] = prompt
	}
	// The reply's response arrives on the read loop this handler runs on.
	go func() { _, _ = p.Send("Page.handleJavaScriptDialog", reply) }()
}

func (p *Page) onRequest(params json.RawMessage) {
	var ev struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL     string            `json:"url"`
			Method  string            `json:"method"`
			Headers map[string]string `json:"headers"`
		} `json:"request"`
		Type             string  `json:"type"`
		Timestamp        float64 `json:"timestamp"`
		RedirectResponse *struct {
			Status     int    `json:"status"`
			StatusText string `json:"statusText"`
		} `json:"redirectResponse"`
	}
	if json.Unmarshal(params, &ev) != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if prev := p.requests[ev.RequestID]; prev != nil && ev.RedirectResponse != nil {
		prev.Status, prev.StatusText = ev.RedirectResponse.Status, ev.RedirectResponse.StatusText
		prev.Finished, prev.End = true, ev.Timestamp
	}
	p.seq++
	r := &Request{Seq: p.seq, ID: ev.RequestID, URL: ev.Request.URL, Method: ev.Request.Method, ResourceType: ev.Type, Headers: ev.Request.Headers, Start: ev.Timestamp}
	p.requests[ev.RequestID] = r
	p.order = append(p.order, r)
	if len(p.order) > logLimit {
		drop := p.order[0]
		p.order = p.order[1:]
		if p.requests[drop.ID] == drop {
			delete(p.requests, drop.ID)
		}
	}
}

func (p *Page) onResponse(params json.RawMessage) {
	var ev struct {
		RequestID string `json:"requestId"`
		Response  struct {
			Status        int               `json:"status"`
			StatusText    string            `json:"statusText"`
			MimeType      string            `json:"mimeType"`
			Headers       map[string]string `json:"headers"`
			FromDiskCache bool              `json:"fromDiskCache"`
		} `json:"response"`
	}
	if json.Unmarshal(params, &ev) != nil {
		return
	}
	p.mu.Lock()
	if r := p.requests[ev.RequestID]; r != nil {
		r.Status, r.StatusText, r.MimeType = ev.Response.Status, ev.Response.StatusText, ev.Response.MimeType
		r.RespHeaders, r.FromCache = ev.Response.Headers, ev.Response.FromDiskCache
	}
	p.mu.Unlock()
}

func (p *Page) onLoadingDone(params json.RawMessage) {
	var ev struct {
		RequestID string  `json:"requestId"`
		Timestamp float64 `json:"timestamp"`
		ErrorText string  `json:"errorText"`
	}
	if json.Unmarshal(params, &ev) != nil {
		return
	}
	p.mu.Lock()
	if r := p.requests[ev.RequestID]; r != nil {
		r.Finished, r.End, r.Failed = true, ev.Timestamp, ev.ErrorText
	}
	p.mu.Unlock()
}

func (p *Page) onFrameNavigated(params json.RawMessage) {
	var ev struct {
		Frame struct {
			ID       string `json:"id"`
			ParentID string `json:"parentId"`
			LoaderID string `json:"loaderId"`
			URL      string `json:"url"`
		} `json:"frame"`
	}
	if json.Unmarshal(params, &ev) != nil || ev.Frame.ParentID != "" {
		return
	}
	p.mu.Lock()
	p.seq++
	p.navs = appendCapped(p.navs, navigation{seq: p.seq, loaderID: ev.Frame.LoaderID, url: ev.Frame.URL})
	p.mu.Unlock()
}

func (p *Page) onSameDocument(params json.RawMessage) {
	var ev struct {
		FrameID string `json:"frameId"`
		URL     string `json:"url"`
	}
	if json.Unmarshal(params, &ev) != nil || ev.FrameID != p.TargetID {
		return
	}
	p.mu.Lock()
	p.seq++
	p.navs = appendCapped(p.navs, navigation{seq: p.seq, url: ev.URL, sameDoc: true})
	p.mu.Unlock()
}

func (p *Page) onLifecycle(params json.RawMessage) {
	var ev struct {
		FrameID  string `json:"frameId"`
		LoaderID string `json:"loaderId"`
		Name     string `json:"name"`
	}
	if json.Unmarshal(params, &ev) != nil || ev.FrameID != p.TargetID {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	events := p.lifecycle[ev.LoaderID]
	if events == nil {
		events = map[string]bool{}
		p.lifecycle[ev.LoaderID] = events
		p.loaders = append(p.loaders, ev.LoaderID)
		if len(p.loaders) > 32 {
			delete(p.lifecycle, p.loaders[0])
			p.loaders = p.loaders[1:]
		}
	}
	events[ev.Name] = true
}

func (p *Page) onScreencastFrame(params json.RawMessage) {
	var ev struct {
		Data      string `json:"data"`
		SessionID int    `json:"sessionId"`
	}
	if json.Unmarshal(params, &ev) != nil {
		return
	}
	p.mu.Lock()
	cast := p.cast
	if cast != nil {
		cast.frames++
		name := filepath.Join(cast.dir, fmt.Sprintf("frame-%05d.%s", cast.frames, cast.ext))
		if data, err := base64.StdEncoding.DecodeString(ev.Data); err == nil {
			_ = os.WriteFile(name, data, 0o644)
		}
	}
	p.mu.Unlock()
	go func() { _, _ = p.Send("Page.screencastFrameAck", map[string]any{"sessionId": ev.SessionID}) }()
}

func appendCapped[T any](list []T, v T) []T {
	list = append(list, v)
	if len(list) > logLimit {
		list = list[len(list)-logLimit:]
	}
	return list
}

// ConsoleMessages returns the console calls recorded since the page opened or was last cleared.
func (p *Page) ConsoleMessages() []ConsoleMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ConsoleMessage(nil), p.console...)
}

// PageErrors returns the uncaught exceptions recorded since the page opened or was last cleared.
func (p *Page) PageErrors() []PageError {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]PageError(nil), p.errors...)
}

// ClearConsole forgets the recorded console messages and page errors.
func (p *Page) ClearConsole() {
	p.mu.Lock()
	p.console, p.errors = nil, nil
	p.mu.Unlock()
}

// Dialogs returns the dialogs the page has opened.
func (p *Page) Dialogs() []Dialog {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Dialog(nil), p.dialogs...)
}

// SetDialogPolicy sets how later dialogs are answered.
func (p *Page) SetDialogPolicy(accept bool, promptText string) {
	p.mu.Lock()
	p.dialogAccept, p.promptText = accept, promptText
	p.mu.Unlock()
}

// Requests returns the network requests the page has made, oldest first.
func (p *Page) Requests() []Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Request, len(p.order))
	for i, r := range p.order {
		out[i] = *r
	}
	return out
}

// Seq is the latest event sequence number.
func (p *Page) Seq() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seq
}

// WaitRequest waits for the earliest request since the last navigation, not returned by an earlier wait of the same kind, whose URL matches pattern; response also waits for it to finish.
func (p *Page) WaitRequest(pattern string, response bool, timeout time.Duration) (Request, error) {
	re := compileURLPattern(pattern)
	kind := 0
	if response {
		kind = 1
	}
	deadline := time.Now().Add(timeout)
	for {
		p.mu.Lock()
		for _, r := range p.order {
			if r.Seq <= p.reqFloor || r.returned[kind] || !re.MatchString(r.URL) {
				continue
			}
			if !response || r.Finished {
				r.returned[kind] = true
				found := *r
				p.mu.Unlock()
				return found, nil
			}
			break
		}
		p.mu.Unlock()
		if time.Now().After(deadline) {
			what := "request"
			if response {
				what = "response"
			}
			return Request{}, timeoutf("no %s matching %q within %v", what, pattern, timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ResponseBody returns a finished request's body, or ok=false when the browser no longer has it.
func (p *Page) ResponseBody(requestID string) (string, bool) {
	res, err := p.Send("Network.getResponseBody", map[string]any{"requestId": requestID})
	if err != nil {
		return "", false
	}
	var r struct {
		Body          string `json:"body"`
		Base64Encoded bool   `json:"base64Encoded"`
	}
	if json.Unmarshal(res, &r) != nil {
		return "", false
	}
	if r.Base64Encoded {
		data, err := base64.StdEncoding.DecodeString(r.Body)
		if err != nil {
			return "", false
		}
		return string(data), true
	}
	return r.Body, true
}

// LifecycleEvent maps a waitUntil option to the Chrome lifecycle event it waits for.
func LifecycleEvent(waitUntil string) (string, bool) {
	switch waitUntil {
	case "load":
		return "load", true
	case "domcontentloaded":
		return "DOMContentLoaded", true
	case "networkidle":
		return "networkIdle", true
	}
	return "", false
}

// Navigate loads url and waits until the main frame reaches the lifecycle event.
func (p *Page) Navigate(url, event string, timeout time.Duration) error {
	p.startNavigation()
	res, err := p.Send("Page.navigate", map[string]any{"url": url})
	if err != nil {
		return err
	}
	var r struct {
		LoaderID  string `json:"loaderId"`
		ErrorText string `json:"errorText"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return err
	}
	if r.ErrorText != "" {
		return fmt.Errorf("navigating to %s: %s", url, r.ErrorText)
	}
	if r.LoaderID != "" {
		if err := p.waitLifecycle(r.LoaderID, event, time.Now().Add(timeout)); err != nil {
			return err
		}
	}
	p.markNavigated()
	return nil
}

// startNavigation stops request waits matching anything the previous document loaded.
func (p *Page) startNavigation() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reqFloor = p.seq
	return p.seq
}

func (p *Page) markNavigated() {
	p.mu.Lock()
	p.navMark = p.seq
	p.mu.Unlock()
}

// WaitNavigation waits for the earliest main-frame navigation no earlier wait returned to reach the lifecycle event.
func (p *Page) WaitNavigation(event string, timeout time.Duration) error {
	p.mu.Lock()
	since := p.navMark
	p.mu.Unlock()
	return p.waitNavigationAfter(since, event, timeout)
}

func (p *Page) waitNavigationAfter(since int64, event string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		p.mu.Lock()
		var found navigation
		ok := false
		for _, nav := range p.navs {
			if nav.seq > since {
				found, ok = nav, true
				break
			}
		}
		p.mu.Unlock()
		if ok {
			if !found.sameDoc {
				if err := p.waitLifecycle(found.loaderID, event, deadline); err != nil {
					return err
				}
			}
			p.markNavigated()
			return nil
		}
		if time.Now().After(deadline) {
			return timeoutf("no navigation within %v", timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (p *Page) waitLifecycle(loaderID, event string, deadline time.Time) error {
	for {
		p.mu.Lock()
		done := p.lifecycle[loaderID][event]
		p.mu.Unlock()
		if done {
			return nil
		}
		if time.Now().After(deadline) {
			return timeoutf("page did not reach %s in time", event)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Reload reloads the page and waits for the lifecycle event.
func (p *Page) Reload(event string, timeout time.Duration) error {
	since := p.startNavigation()
	if _, err := p.Send("Page.reload", nil); err != nil {
		return err
	}
	return p.waitNavigationAfter(since, event, timeout)
}

// History moves delta entries through the session history; false when there is no such entry.
func (p *Page) History(delta int, event string, timeout time.Duration) (bool, error) {
	res, err := p.Send("Page.getNavigationHistory", nil)
	if err != nil {
		return false, err
	}
	var h struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID int `json:"id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(res, &h); err != nil {
		return false, err
	}
	idx := h.CurrentIndex + delta
	if idx < 0 || idx >= len(h.Entries) {
		return false, nil
	}
	since := p.startNavigation()
	if _, err := p.Send("Page.navigateToHistoryEntry", map[string]any{"entryId": h.Entries[idx].ID}); err != nil {
		return false, err
	}
	return true, p.waitNavigationAfter(since, event, timeout)
}

// StartScreencast writes the page's frames into dir until StopScreencast.
func (p *Page) StartScreencast(dir, format string, quality, everyNth int) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ext := format
	if ext == "jpeg" {
		ext = "jpg"
	}
	p.mu.Lock()
	p.cast = &screencast{dir: dir, ext: ext}
	p.mu.Unlock()
	params := map[string]any{"format": format, "everyNthFrame": everyNth}
	if format == "jpeg" {
		params["quality"] = quality
	}
	_, err := p.Send("Page.startScreencast", params)
	return err
}

// StopScreencast stops recording and returns how many frames were written.
func (p *Page) StopScreencast() (int, error) {
	_, err := p.Send("Page.stopScreencast", nil)
	p.mu.Lock()
	n := 0
	if p.cast != nil {
		n = p.cast.frames
	}
	p.cast = nil
	p.mu.Unlock()
	return n, err
}

// TimeoutError reports a wait that ran out of time.
type TimeoutError struct{ msg string }

func (e *TimeoutError) Error() string { return e.msg }

func timeoutf(format string, args ...any) error {
	return &TimeoutError{msg: fmt.Sprintf(format, args...)}
}

// MatchURLPattern matches a CDP-style url pattern (only * is a wildcard) against a URL.
func MatchURLPattern(pattern, url string) bool {
	return compileURLPattern(pattern).MatchString(url)
}

func compileURLPattern(pattern string) *regexp.Regexp {
	var sb strings.Builder
	sb.WriteString("^")
	for _, ch := range pattern {
		if ch == '*' {
			sb.WriteString(".*")
		} else {
			sb.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	sb.WriteString("$")
	return regexp.MustCompile(sb.String())
}
