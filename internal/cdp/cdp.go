// Package cdp is a minimal hand-rolled Chrome DevTools Protocol client over gorilla/websocket: launch a headless Chrome, attach to page targets, and round-trip JSON commands. No external CDP library.
package cdp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const defaultCommandTimeout = 30 * time.Second

// Browser is a connection to a launched headless Chrome.
type Browser struct {
	cmd  *exec.Cmd
	conn *websocket.Conn
	udd  string

	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcResult
	closed  bool

	writeMu sync.Mutex

	eventMu  sync.Mutex
	handlers map[string]func(sessionID string, params json.RawMessage)

	pagesMu   sync.Mutex
	pages     map[string]*Page
	downloads downloadLog
}

type rpcResult struct {
	result json.RawMessage
	err    *RPCError
}

// RPCError is a CDP-reported command failure.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e.Data != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.Data)
	}
	return e.Message
}

// LaunchOptions configures Launch; an empty Executable triggers discovery.
type LaunchOptions struct {
	Executable string
	Headless   bool
	Args       []string
	Timeout    time.Duration
}

var wsURLRe = regexp.MustCompile(`ws://\S+`)

// Launch starts a headless Chrome and connects to its DevTools endpoint.
func Launch(opts LaunchOptions) (*Browser, error) {
	exe := opts.Executable
	if exe == "" {
		exe = FindChrome()
	}
	if exe == "" {
		return nil, fmt.Errorf("no Chrome/Chromium found; set the executable option or $GEBLANG_CHROME")
	}
	udd, err := os.MkdirTemp("", "geblang-chrome-")
	if err != nil {
		return nil, err
	}
	args := []string{
		"--remote-debugging-port=0",
		"--user-data-dir=" + udd,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-gpu",
		"--disable-background-networking",
	}
	if opts.Headless {
		args = append(args, "--headless=new")
	}
	args = append(args, opts.Args...)
	args = append(args, "about:blank")

	cmd := exec.Command(exe, args...)
	setProcessGroup(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		os.RemoveAll(udd)
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		os.RemoveAll(udd)
		return nil, err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	wsURL, err := readWSURL(stderr, timeout)
	if err != nil {
		killProcessGroup(cmd)
		_ = cmd.Wait()
		os.RemoveAll(udd)
		return nil, fmt.Errorf("waiting for Chrome devtools endpoint: %w", err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		killProcessGroup(cmd)
		_ = cmd.Wait()
		os.RemoveAll(udd)
		return nil, fmt.Errorf("connect to devtools: %w", err)
	}
	b := &Browser{cmd: cmd, conn: conn, udd: udd, pending: map[int]chan rpcResult{}, handlers: map[string]func(string, json.RawMessage){}, pages: map[string]*Page{}}
	go b.readLoop()
	return b, nil
}

// readWSURL scans Chrome's stderr for the devtools URL, then keeps draining it so the pipe never blocks Chrome.
func readWSURL(stderr io.Reader, timeout time.Duration) (string, error) {
	found := make(chan string, 1)
	failed := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		sent := false
		for sc.Scan() {
			if !sent && strings.Contains(sc.Text(), "DevTools listening on") {
				if m := wsURLRe.FindString(sc.Text()); m != "" {
					found <- m
					sent = true
				}
			}
		}
		if !sent {
			failed <- fmt.Errorf("Chrome exited before announcing a devtools endpoint")
		}
	}()
	select {
	case u := <-found:
		return u, nil
	case err := <-failed:
		return "", err
	case <-time.After(timeout):
		return "", fmt.Errorf("timed out after %v", timeout)
	}
}

func (b *Browser) readLoop() {
	for {
		_, data, err := b.conn.ReadMessage()
		if err != nil {
			b.failAll(err)
			return
		}
		var msg struct {
			ID        int             `json:"id"`
			Result    json.RawMessage `json:"result"`
			Error     *RPCError       `json:"error"`
			Method    string          `json:"method"`
			Params    json.RawMessage `json:"params"`
			SessionID string          `json:"sessionId"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.ID != 0 {
			b.mu.Lock()
			ch := b.pending[msg.ID]
			delete(b.pending, msg.ID)
			b.mu.Unlock()
			if ch != nil {
				ch <- rpcResult{result: msg.Result, err: msg.Error}
			}
			continue
		}
		if msg.Method != "" {
			b.eventMu.Lock()
			h := b.handlers[msg.Method]
			b.eventMu.Unlock()
			if h != nil {
				h(msg.SessionID, msg.Params)
			}
			b.dispatch(msg.SessionID, msg.Method, msg.Params)
		}
	}
}

func (b *Browser) failAll(err error) {
	b.mu.Lock()
	b.closed = true
	pending := b.pending
	b.pending = map[int]chan rpcResult{}
	b.mu.Unlock()
	for _, ch := range pending {
		ch <- rpcResult{err: &RPCError{Message: "browser connection closed: " + err.Error()}}
	}
}

// Send issues a CDP command (sessionID empty = browser-level) and returns its result.
func (b *Browser) Send(sessionID, method string, params any) (json.RawMessage, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, fmt.Errorf("browser is closed")
	}
	b.nextID++
	id := b.nextID
	ch := make(chan rpcResult, 1)
	b.pending[id] = ch
	b.mu.Unlock()

	envelope := map[string]any{"id": id, "method": method}
	if params != nil {
		envelope["params"] = params
	}
	if sessionID != "" {
		envelope["sessionId"] = sessionID
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	b.writeMu.Lock()
	err = b.conn.WriteMessage(websocket.TextMessage, data)
	b.writeMu.Unlock()
	if err != nil {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return nil, err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return r.result, nil
	case <-time.After(defaultCommandTimeout):
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return nil, fmt.Errorf("cdp %s timed out", method)
	}
}

// OnEvent registers a handler for a CDP event method (used for interception/page lifecycle).
func (b *Browser) OnEvent(method string, h func(sessionID string, params json.RawMessage)) {
	b.eventMu.Lock()
	b.handlers[method] = h
	b.eventMu.Unlock()
}

// NewPage creates and attaches to a fresh page target in the default context.
func (b *Browser) NewPage() (*Page, error) {
	return b.NewPageIn("")
}

// NewPageIn creates a page in a browser context ("" is the default context).
func (b *Browser) NewPageIn(contextID string) (*Page, error) {
	params := map[string]any{"url": "about:blank"}
	if contextID != "" {
		params["browserContextId"] = contextID
	}
	res, err := b.Send("", "Target.createTarget", params)
	if err != nil {
		return nil, err
	}
	var ct struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(res, &ct); err != nil {
		return nil, err
	}
	p, err := b.AttachPage(ct.TargetID)
	if p != nil {
		p.ContextID = contextID
	}
	return p, err
}

// AttachPage attaches to an existing page target and enables its domains.
func (b *Browser) AttachPage(targetID string) (*Page, error) {
	res, err := b.Send("", "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true})
	if err != nil {
		return nil, err
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(res, &at); err != nil {
		return nil, err
	}
	p := newPage(b, targetID, at.SessionID)
	b.pagesMu.Lock()
	b.pages[at.SessionID] = p
	b.pagesMu.Unlock()
	for _, d := range []string{"Page.enable", "Runtime.enable", "DOM.enable", "Network.enable"} {
		_, _ = p.Send(d, nil)
	}
	_, _ = p.Send("Page.setLifecycleEventsEnabled", map[string]any{"enabled": true})
	return p, nil
}

// TargetInfo describes an open page target.
type TargetInfo struct {
	TargetID  string `json:"targetId"`
	Type      string `json:"type"`
	ContextID string `json:"browserContextId"`
}

// PageTargets returns the targetIds of all open page targets.
func (b *Browser) PageTargets() ([]string, error) {
	infos, err := b.PageTargetInfos()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(infos))
	for _, t := range infos {
		ids = append(ids, t.TargetID)
	}
	return ids, nil
}

// PageTargetInfos returns every open page target with its browser context.
func (b *Browser) PageTargetInfos() ([]TargetInfo, error) {
	res, err := b.Send("", "Target.getTargets", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		TargetInfos []TargetInfo `json:"targetInfos"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	var out []TargetInfo
	for _, t := range r.TargetInfos {
		if t.Type == "page" {
			out = append(out, t)
		}
	}
	return out, nil
}

// NewContext creates an isolated browser context with its own cookies and storage.
func (b *Browser) NewContext() (string, error) {
	res, err := b.Send("", "Target.createBrowserContext", map[string]any{})
	if err != nil {
		return "", err
	}
	var r struct {
		ContextID string `json:"browserContextId"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return "", err
	}
	return r.ContextID, nil
}

// DisposeContext closes a browser context and every page in it.
func (b *Browser) DisposeContext(contextID string) error {
	_, err := b.Send("", "Target.disposeBrowserContext", map[string]any{"browserContextId": contextID})
	return err
}

func (b *Browser) dispatch(sessionID, method string, params json.RawMessage) {
	if sessionID == "" {
		b.downloads.handle(method, params)
		return
	}
	b.pagesMu.Lock()
	p := b.pages[sessionID]
	b.pagesMu.Unlock()
	if p != nil {
		p.handleEvent(method, params)
	}
}

// Version returns the "Browser" product string from Browser.getVersion.
func (b *Browser) Version() (string, error) {
	res, err := b.Send("", "Browser.getVersion", nil)
	if err != nil {
		return "", err
	}
	var v struct {
		Product string `json:"product"`
	}
	_ = json.Unmarshal(res, &v)
	return v.Product, nil
}

// Close terminates Chrome and cleans up its profile dir.
func (b *Browser) Close() error {
	b.mu.Lock()
	already := b.closed
	b.closed = true
	b.mu.Unlock()
	if !already {
		_, _ = b.Send("", "Browser.close", nil)
	}
	_ = b.conn.Close()
	killProcessGroup(b.cmd)
	_ = b.cmd.Wait()
	os.RemoveAll(b.udd)
	return nil
}

// FindChrome locates a browser binary: $GEBLANG_CHROME, then PATH names, then common install paths.
func FindChrome() string {
	if p := os.Getenv("GEBLANG_CHROME"); p != "" {
		return p
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	for _, p := range []string{
		"/usr/bin/google-chrome",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
		"/opt/google/chrome/chrome",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}
