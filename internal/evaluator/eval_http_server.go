package evaluator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/concurrent"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// serverErrorLog builds the *log.Logger assigned to http.Server.ErrorLog.
// net/http logs connection-level failures (TLS handshakes, malformed
// requests) there; these happen before any handler and cannot be turned into
// catchable Geblang errors. Default is quiet (discarded). When opts carries an
// onError callable, each message is forwarded to it instead.
func (e *Evaluator) serverErrorLog(opts runtime.Value, label string) (*log.Logger, error) {
	dict, ok := opts.(runtime.Dict)
	if !ok {
		return log.New(io.Discard, "", 0), nil
	}
	v, ok := dictField(dict, "onError")
	if !ok {
		return log.New(io.Discard, "", 0), nil
	}
	fn, ok := v.(runtime.Function)
	if !ok {
		return nil, fmt.Errorf("%s opts.onError must be a function", label)
	}
	return log.New(&serverErrorWriter{e: e, fn: fn}, "", 0), nil
}

// serverErrorWriter forwards each net/http ErrorLog line to a Geblang
// callback. Callback failures are swallowed: this is a logging sink, not a
// request path, and a panic here would otherwise take down the server.
type serverErrorWriter struct {
	e  *Evaluator
	fn runtime.Function
}

func (w *serverErrorWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	defer func() { _ = recover() }()
	ev, fn := w.e.callbackEvaluator(w.fn)
	if ev != w.e {
		defer ev.Cleanup()
		defer ev.startDebugThread("server error log")()
	}
	_, _ = ev.applyFunction(fn, []runtime.Value{runtime.String{Value: msg}})
	return len(p), nil
}

func (e *Evaluator) httpServe(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 && len(args) != 3 {
		return nil, fmt.Errorf("%s expects address, handler, and optional opts", call.Callee.String())
	}
	addr, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s address must be string", call.Callee.String())
	}
	handler, ok := args[1].(runtime.Function)
	if !ok {
		return nil, fmt.Errorf("%s handler must be a function", call.Callee.String())
	}
	pool, err := serverPoolFromArgs(args, 2, call.Callee.String())
	if err != nil {
		return nil, err
	}
	var serverOpts runtime.Value = runtime.Null{}
	if len(args) >= 3 {
		serverOpts = args[2]
	}
	tlsCfg, _, err := buildHTTPServerTLSConfig(serverOpts, addr.Value, call.Callee.String())
	if err != nil {
		return nil, err
	}
	tp, err := parseTrustedProxies(serverOpts, call.Callee.String())
	if err != nil {
		return nil, err
	}
	errLog, err := e.serverErrorLog(serverOpts, call.Callee.String())
	if err != nil {
		return nil, err
	}
	maxBody, err := parseMaxBodyBytes(serverOpts, call.Callee.String())
	if err != nil {
		return nil, err
	}
	debug, err := parseDebugFlag(serverOpts, call.Callee.String())
	if err != nil {
		return nil, err
	}
	share, err := parseShareHandler(serverOpts, call.Callee.String())
	if err != nil {
		return nil, err
	}
	server := &http.Server{
		Addr:     addr.Value,
		Handler:  e.httpHandler(handler, pool, tp, maxBody, serveDebugEnabled(debug), share),
		ErrorLog: errLog,
	}
	if tlsCfg != nil {
		server.TLSConfig = tlsCfg
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			return nil, err
		}
		return runtime.Null{}, nil
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return nil, err
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) httpListen(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 && len(args) != 3 {
		return nil, fmt.Errorf("%s expects address, handler, and optional opts", call.Callee.String())
	}
	addr, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s address must be string", call.Callee.String())
	}
	handler, ok := args[1].(runtime.Function)
	if !ok {
		return nil, fmt.Errorf("%s handler must be a function", call.Callee.String())
	}
	pool, err := serverPoolFromArgs(args, 2, call.Callee.String())
	if err != nil {
		return nil, err
	}
	var serverOpts runtime.Value = runtime.Null{}
	if len(args) >= 3 {
		serverOpts = args[2]
	}
	tlsCfg, certPEM, err := buildHTTPServerTLSConfig(serverOpts, addr.Value, call.Callee.String())
	if err != nil {
		return nil, err
	}
	tp, err := parseTrustedProxies(serverOpts, call.Callee.String())
	if err != nil {
		return nil, err
	}
	errLog, err := e.serverErrorLog(serverOpts, call.Callee.String())
	if err != nil {
		return nil, err
	}
	maxBody, err := parseMaxBodyBytes(serverOpts, call.Callee.String())
	if err != nil {
		return nil, err
	}
	debug, err := parseDebugFlag(serverOpts, call.Callee.String())
	if err != nil {
		return nil, err
	}
	share, err := parseShareHandler(serverOpts, call.Callee.String())
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", addr.Value)
	if err != nil {
		return nil, err
	}
	server := &http.Server{Handler: e.httpHandler(handler, pool, tp, maxBody, serveDebugEnabled(debug), share), ErrorLog: errLog}
	handle := &httpServerHandle{server: server, listener: listener, done: make(chan error, 1), pool: pool, certPEM: certPEM}
	e.httpServerMu.Lock()
	e.nextHTTPServerID++
	id := e.nextHTTPServerID
	e.httpServers[id] = handle
	e.httpServerMu.Unlock()
	go func() {
		var err error
		if tlsCfg != nil {
			server.TLSConfig = tlsCfg
			err = server.ServeTLS(listener, "", "")
		} else {
			err = server.Serve(listener)
		}
		if err == http.ErrServerClosed {
			err = nil
		}
		handle.done <- err
		close(handle.done)
	}()
	return runtime.NewInt64(id), nil
}

// serverPoolFromArgs pulls the maxConcurrent/queueSize/onOverload
// dict at args[optsIndex] (if present) and returns a configured
// pool. Nil pool means unbounded.
func serverPoolFromArgs(args []runtime.Value, optsIndex int, label string) (*concurrent.Pool, error) {
	if len(args) <= optsIndex {
		return nil, nil
	}
	opts, ok := args[optsIndex].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s opts must be dict", label)
	}
	maxConcurrent := 0
	if v, ok := dictField(opts, "maxConcurrent"); ok {
		n, ok := toInt64(v)
		if !ok || n < 0 {
			return nil, fmt.Errorf("%s opts.maxConcurrent must be a non-negative int", label)
		}
		maxConcurrent = int(n)
	}
	queueSize := 0
	if v, ok := dictField(opts, "queueSize"); ok {
		n, ok := toInt64(v)
		if !ok || n < 0 {
			return nil, fmt.Errorf("%s opts.queueSize must be a non-negative int", label)
		}
		queueSize = int(n)
	}
	policy := concurrent.Reject
	if v, ok := dictField(opts, "onOverload"); ok {
		s, ok := v.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s opts.onOverload must be string", label)
		}
		switch s.Value {
		case "reject", "wait", "drop":
			policy = concurrent.ParsePolicy(s.Value)
		default:
			return nil, fmt.Errorf("%s opts.onOverload must be \"reject\", \"wait\", or \"drop\"", label)
		}
	}
	if maxConcurrent == 0 && queueSize == 0 {
		return nil, nil
	}
	return concurrent.NewPool(maxConcurrent, queueSize, policy), nil
}

func (e *Evaluator) httpServerStats(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a server handle", call.Callee.String())
	}
	handle, err := e.httpServerHandle(args[0])
	if err != nil {
		return nil, err
	}
	return poolStatsDict(handle.pool), nil
}

func (e *Evaluator) netServerStats(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a server handle", call.Callee.String())
	}
	id, ok := native.AsInt64(args[0])
	if !ok {
		return nil, fmt.Errorf("%s server handle must be int", call.Callee.String())
	}
	e.netServerMu.Lock()
	server, ok := e.netServers[id]
	e.netServerMu.Unlock()
	if !ok {
		if e.parent != nil {
			return e.parent.netServerStats(call, args)
		}
		return nil, fmt.Errorf("unknown net server handle %d", id)
	}
	return poolStatsDict(server.pool), nil
}

func poolStatsDict(pool *concurrent.Pool) runtime.Value {
	stats := pool.Stats()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "active", runtime.NewInt64(stats.Active))
	putDict(entries, "queued", runtime.NewInt64(stats.Queued))
	putDict(entries, "rejected", runtime.NewInt64(stats.Rejected))
	putDict(entries, "maxConcurrent", runtime.NewInt64(stats.MaxConcurrent))
	return runtime.Dict{Entries: entries}
}

func (e *Evaluator) httpServerAddr(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a server handle", call.Callee.String())
	}
	handle, err := e.httpServerHandle(args[0])
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: handle.listener.Addr().String()}, nil
}

func (e *Evaluator) httpServerCert(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a server handle", call.Callee.String())
	}
	handle, err := e.httpServerHandle(args[0])
	if err != nil {
		return nil, err
	}
	if len(handle.certPEM) == 0 {
		return runtime.Null{}, nil
	}
	return runtime.String{Value: string(handle.certPEM)}, nil
}

func (e *Evaluator) httpClose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a server handle", call.Callee.String())
	}
	id, err := rawInt64(args[0], "http server handle")
	if err != nil {
		return nil, err
	}
	handle, ok := e.takeHTTPServerHandle(id)
	if !ok {
		return runtime.Null{}, nil
	}
	return runtime.Null{}, closeHTTPServerHandle(handle)
}

func (e *Evaluator) httpShutdown(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("%s expects server handle and optional timeoutMs", call.Callee.String())
	}
	id, err := rawInt64(args[0], "http server handle")
	if err != nil {
		return nil, err
	}
	timeout := int64(5000)
	if len(args) == 2 {
		timeout, err = rawInt64(args[1], "timeoutMs")
		if err != nil {
			return nil, err
		}
		if timeout < 0 {
			return nil, fmt.Errorf("%s timeoutMs must be non-negative", call.Callee.String())
		}
	}
	handle, ok := e.takeHTTPServerHandle(id)
	if !ok {
		return runtime.Null{}, nil
	}
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
		defer cancel()
	}
	if err := handle.server.Shutdown(ctx); err != nil {
		return nil, err
	}
	if err := waitHTTPServerDone(handle); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

// httpWait blocks until the server behind the handle stops serving
// (close or shutdown from anywhere, including a signal handler).
// Unknown handles return immediately: the server is already gone.
func (e *Evaluator) httpWait(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects a server handle", call.Callee.String())
	}
	id, err := rawInt64(args[0], "http server handle")
	if err != nil {
		return nil, err
	}
	e.httpServerMu.Lock()
	handle, ok := e.httpServers[id]
	e.httpServerMu.Unlock()
	for !ok && e.parent != nil {
		e = e.parent
		e.httpServerMu.Lock()
		handle, ok = e.httpServers[id]
		e.httpServerMu.Unlock()
	}
	if !ok {
		return runtime.Null{}, nil
	}
	if err := waitHTTPServerDone(handle); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) httpServerHandle(value runtime.Value) (*httpServerHandle, error) {
	id, err := rawInt64(value, "http server handle")
	if err != nil {
		return nil, err
	}
	e.httpServerMu.Lock()
	handle, ok := e.httpServers[id]
	e.httpServerMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.httpServerHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown http server handle %d", id)
	}
	return handle, nil
}

func (e *Evaluator) takeHTTPServerHandle(id int64) (*httpServerHandle, bool) {
	e.httpServerMu.Lock()
	handle, ok := e.httpServers[id]
	if ok {
		delete(e.httpServers, id)
	}
	e.httpServerMu.Unlock()
	if ok {
		return handle, true
	}
	if e.parent != nil {
		return e.parent.takeHTTPServerHandle(id)
	}
	return nil, false
}

func closeHTTPServerHandle(handle *httpServerHandle) error {
	if handle == nil || handle.server == nil {
		return nil
	}
	if err := handle.server.Close(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return waitHTTPServerDone(handle)
}

func waitHTTPServerDone(handle *httpServerHandle) error {
	if handle == nil || handle.done == nil {
		return nil
	}
	err, ok := <-handle.done
	if !ok {
		return nil
	}
	return err
}

func (e *Evaluator) httpHandler(handler runtime.Function, pool *concurrent.Pool, tp *trustedProxies, maxBody int64, debug bool, shareHandler bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pool != nil && !pool.IsUnbounded() {
			if err := pool.Acquire(r.Context()); err != nil {
				switch pool.Policy() {
				case concurrent.Drop:
					if hijacker, ok := w.(http.Hijacker); ok {
						if conn, _, hErr := hijacker.Hijack(); hErr == nil {
							_ = conn.Close()
							return
						}
					}
					w.WriteHeader(http.StatusServiceUnavailable)
				default:
					http.Error(w, "server at capacity", http.StatusServiceUnavailable)
				}
				return
			}
			defer pool.Release()
		}
		if maxBody > 0 {
			if r.ContentLength > maxBody {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		response, err := e.callHTTPHandler(handler, r, body, tp, shareHandler)
		if err != nil {
			headline, rendered := serveErrorParts(err)
			if debug {
				fmt.Fprintln(e.stderr, rendered)
				http.Error(w, rendered, http.StatusInternalServerError)
			} else {
				fmt.Fprintf(e.stderr, "http.serve: handler error: %s\n", headline)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
			return
		}
		e.writeHTTPResponse(w, r, response)
	})
}

func (e *Evaluator) callHTTPHandler(handler runtime.Function, request *http.Request, body []byte, tp *trustedProxies, shareHandler bool) (runtime.Value, error) {
	// Per-request isolation: a child evaluator isolates eval-side dispatch state; an eval handler is also deep-cloned (shareHandler opts out; a VM handler is a trampoline that isolates itself).
	child := e.childForCallback()
	defer child.Cleanup()
	if child.debug != nil {
		defer child.startDebugThread("request " + request.Method + " " + request.URL.Path)()
	}
	callbackHandler := handler
	if handler.Native == nil && !shareHandler {
		callbackHandler = runtime.CloneFunction(handler)
	}
	requestArg, err := child.httpRequestArgument(callbackHandler, request, body, tp)
	if err != nil {
		return nil, err
	}
	return child.applyFunction(callbackHandler, []runtime.Value{requestArg})
}

func (e *Evaluator) callbackEvaluator(handler runtime.Function) (*Evaluator, runtime.Function) {
	if handler.Native != nil {
		return e, handler
	}
	child := e.childForCallback()
	return child, runtime.CloneFunction(handler)
}

// httpRequestDict builds the dict-form server request. It mirrors the rich
// Request instance: proxy-aware scheme/host/clientIp (honouring trustedProxies)
// and _clientCert when a verified peer certificate is present.
func httpRequestDict(request *http.Request, body []byte, tp *trustedProxies) runtime.Value {
	entries := httpRequestEntries(request, body)
	scheme, host, clientIP := resolveRequestMeta(request, tp)
	put := func(key string, value runtime.Value) {
		kv := runtime.String{Value: key}
		entries[dictKey(kv)] = runtime.DictEntry{Key: kv, Value: value}
	}
	put("scheme", runtime.String{Value: scheme})
	put("host", runtime.String{Value: host})
	put("clientIp", runtime.String{Value: clientIP})
	if request.TLS != nil && len(request.TLS.PeerCertificates) > 0 {
		put("_clientCert", clientCertDict(request.TLS.PeerCertificates[0]))
	}
	return runtime.Dict{Entries: entries}
}

// Canonicalize the request path so a non-canonical form (leading //, dot segments) cannot slip past a path-prefix check that a trimmed route still matches.
func cleanRequestPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	cleaned := path.Clean(p)
	if p[len(p)-1] == '/' && cleaned != "/" {
		cleaned += "/" // path.Clean drops a trailing slash; keep it for route parity.
	}
	return cleaned
}

func httpRequestEntries(request *http.Request, body []byte) map[string]runtime.DictEntry {
	headers := map[string]runtime.DictEntry{}
	for name, values := range request.Header {
		key := runtime.String{Value: name}
		headers[dictKey(key)] = runtime.DictEntry{Key: key, Value: runtime.String{Value: strings.Join(values, ",")}}
	}
	entries := map[string]runtime.DictEntry{}
	put := func(key string, value runtime.Value) {
		keyValue := runtime.String{Value: key}
		entries[dictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: value}
	}
	put("method", runtime.String{Value: request.Method})
	put("path", runtime.String{Value: cleanRequestPath(request.URL.Path)})
	put("query", runtime.String{Value: request.URL.RawQuery})
	put("remoteAddr", runtime.String{Value: request.RemoteAddr})
	put("body", runtime.String{Value: string(body)})
	put("headers", runtime.Dict{Entries: headers})
	return entries
}

// parseMaxBodyBytes reads opts.maxBodyBytes for http.serve/listen.
// 0 (or absent) means unlimited.
func parseMaxBodyBytes(opts runtime.Value, label string) (int64, error) {
	dict, ok := opts.(runtime.Dict)
	if !ok {
		return 0, nil
	}
	v, ok := dictField(dict, "maxBodyBytes")
	if !ok {
		return 0, nil
	}
	n, ok := toInt64(v)
	if !ok || n < 0 {
		return 0, fmt.Errorf("%s opts.maxBodyBytes must be a non-negative int", label)
	}
	return n, nil
}

// parseDebugFlag reads opts.debug for http.serve/listen.
func parseDebugFlag(opts runtime.Value, label string) (bool, error) {
	dict, ok := opts.(runtime.Dict)
	if !ok {
		return false, nil
	}
	v, ok := dictField(dict, "debug")
	if !ok {
		return false, nil
	}
	b, ok := v.(runtime.Bool)
	if !ok {
		return false, fmt.Errorf("%s opts.debug must be a bool", label)
	}
	return b.Value, nil
}

// parseShareHandler reads opts.shareHandler: when true the handler is invoked shared (not cloned per request), for frameworks that manage their own per-request isolation.
func parseShareHandler(opts runtime.Value, label string) (bool, error) {
	dict, ok := opts.(runtime.Dict)
	if !ok {
		return false, nil
	}
	v, ok := dictField(dict, "shareHandler")
	if !ok {
		return false, nil
	}
	b, ok := v.(runtime.Bool)
	if !ok {
		return false, fmt.Errorf("%s opts.shareHandler must be a bool", label)
	}
	return b.Value, nil
}

// serveDebugEnabled folds the opts.debug flag with the GEBLANG_DEBUG env switch.
func serveDebugEnabled(optDebug bool) bool {
	if optDebug {
		return true
	}
	v := os.Getenv("GEBLANG_DEBUG")
	return v != "" && v != "0"
}

// serveErrorParts extracts a one-line headline and the full canonical render from a handler error.
func serveErrorParts(err error) (string, string) {
	var thrown thrownError
	if errors.As(err, &thrown) {
		v := thrown.value
		return v.Inspect(), uncaughtFromThrown(v).Render()
	}
	var uncaught *runtime.UncaughtError
	if errors.As(err, &uncaught) {
		headline := uncaught.Class
		if uncaught.Message != "" {
			headline += ": " + uncaught.Message
		}
		return headline, uncaught.Render()
	}
	rerr := runtime.NewRecoverableError(err)
	return rerr.Inspect(), uncaughtFromThrown(rerr).Render()
}

// trustedProxies matches peer IPs allowed to set X-Forwarded-* headers.
type trustedProxies struct {
	nets []*net.IPNet
}

func (tp *trustedProxies) trusts(ip net.IP) bool {
	if tp == nil || ip == nil {
		return false
	}
	for _, n := range tp.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// privateProxyCIDRs backs the "private" trustedProxies keyword: loopback,
// link-local, and RFC 1918 / ULA ranges.
var privateProxyCIDRs = []string{
	"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12",
	"192.168.0.0/16", "169.254.0.0/16", "fc00::/7", "fe80::/10",
}

func parseTrustedProxies(opts runtime.Value, label string) (*trustedProxies, error) {
	dict, ok := opts.(runtime.Dict)
	if !ok {
		return nil, nil
	}
	v, ok := dictField(dict, "trustedProxies")
	if !ok {
		return nil, nil
	}
	list, ok := v.(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s opts.trustedProxies must be a list of strings", label)
	}
	tp := &trustedProxies{}
	for _, el := range list.Elements {
		s, ok := el.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s opts.trustedProxies entries must be strings", label)
		}
		entry := strings.TrimSpace(s.Value)
		if strings.EqualFold(entry, "private") {
			for _, c := range privateProxyCIDRs {
				if _, n, err := net.ParseCIDR(c); err == nil {
					tp.nets = append(tp.nets, n)
				}
			}
			continue
		}
		if strings.Contains(entry, "/") {
			_, n, err := net.ParseCIDR(entry)
			if err != nil {
				return nil, fmt.Errorf("%s invalid trustedProxies CIDR %q", label, entry)
			}
			tp.nets = append(tp.nets, n)
			continue
		}
		ip := net.ParseIP(entry)
		if ip == nil {
			return nil, fmt.Errorf("%s invalid trustedProxies IP %q", label, entry)
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		tp.nets = append(tp.nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return tp, nil
}

func ipFromRemoteAddr(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func firstForwardedValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		return strings.TrimSpace(v[:i])
	}
	return strings.TrimSpace(v)
}

// resolveRequestMeta returns scheme, host, and client IP, honouring
// X-Forwarded-{Proto,Host,For} only when the immediate peer is a trusted
// proxy. Otherwise the socket values are authoritative (anti-spoofing).
func resolveRequestMeta(request *http.Request, tp *trustedProxies) (scheme, host, clientIP string) {
	scheme = "http"
	if request.TLS != nil {
		scheme = "https"
	}
	host = request.Host
	clientIP = ipFromRemoteAddr(request.RemoteAddr)
	if !tp.trusts(net.ParseIP(clientIP)) {
		return scheme, host, clientIP
	}
	if xfp := firstForwardedValue(request.Header.Get("X-Forwarded-Proto")); xfp != "" {
		scheme = strings.ToLower(xfp)
	}
	if xfh := firstForwardedValue(request.Header.Get("X-Forwarded-Host")); xfh != "" {
		host = xfh
	}
	if xff := request.Header.Get("X-Forwarded-For"); xff != "" {
		clientIP = clientIPFromForwarded(xff, clientIP, tp)
	}
	return scheme, host, clientIP
}

// clientIPFromForwarded walks X-Forwarded-For from the right (closest proxy
// first) and returns the first address that is not itself a trusted proxy.
func clientIPFromForwarded(xff, fallback string, tp *trustedProxies) string {
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		ip := net.ParseIP(candidate)
		if ip == nil {
			continue
		}
		if !tp.trusts(ip) {
			return candidate
		}
	}
	if first := strings.TrimSpace(parts[0]); first != "" {
		return first
	}
	return fallback
}

func requestStringField(this *runtime.Instance, name string) string {
	if v, ok := this.Fields[name].(runtime.String); ok {
		return v.Value
	}
	return ""
}

func requestHeaderValue(this *runtime.Instance, name string) string {
	if hv, ok := httpHeaderValue(this.Fields["headers"]); ok {
		if vals := hv.Values[http.CanonicalHeaderKey(name)]; len(vals) > 0 {
			return vals[0]
		}
		return ""
	}
	if dict, ok := this.Fields["headers"].(runtime.Dict); ok {
		canonical := http.CanonicalHeaderKey(name)
		result := ""
		dict.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			if k, ok := entry.Key.(runtime.String); ok && http.CanonicalHeaderKey(k.Value) == canonical {
				if val, ok := entry.Value.(runtime.String); ok {
					result = val.Value
					return false
				}
			}
			return true
		})
		if result != "" {
			return result
		}
	}
	return ""
}

func requestQueryValues(this *runtime.Instance) neturl.Values {
	values, _ := neturl.ParseQuery(requestStringField(this, "query"))
	return values
}

func nativeRequestScheme(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.scheme expects no arguments")
	}
	scheme := requestStringField(this, "scheme")
	if scheme == "" {
		scheme = "http"
	}
	return runtime.String{Value: scheme}, nil
}

func nativeRequestIsSecure(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.isSecure expects no arguments")
	}
	return runtime.Bool{Value: requestStringField(this, "scheme") == "https"}, nil
}

func nativeRequestHost(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.host expects no arguments")
	}
	return runtime.String{Value: requestStringField(this, "host")}, nil
}

func nativeRequestClientIP(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.clientIp expects no arguments")
	}
	if ip := requestStringField(this, "clientIp"); ip != "" {
		return runtime.String{Value: ip}, nil
	}
	return runtime.String{Value: ipFromRemoteAddr(requestStringField(this, "remoteAddr"))}, nil
}

func nativeRequestClientCert(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.clientCert expects no arguments")
	}
	if v := this.Fields["_clientCert"]; v != nil {
		return v, nil
	}
	return runtime.Null{}, nil
}

func nativeRequestText(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.text expects no arguments")
	}
	body, err := requestBodyText(this)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: body}, nil
}

func nativeRequestIsMethod(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Request.isMethod expects one argument")
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("Request.isMethod argument must be string")
	}
	return runtime.Bool{Value: strings.EqualFold(requestStringField(this, "method"), name.Value)}, nil
}

func nativeRequestIsJSON(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.isJson expects no arguments")
	}
	ct := strings.ToLower(requestHeaderValue(this, "Content-Type"))
	return runtime.Bool{Value: strings.Contains(ct, "json")}, nil
}

func nativeRequestCookie(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Request.cookie expects one argument")
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("Request.cookie name must be string")
	}
	cookieHeader := requestHeaderValue(this, "Cookie")
	if cookieHeader == "" {
		return runtime.Null{}, nil
	}
	header := http.Header{}
	header.Set("Cookie", cookieHeader)
	dummy := &http.Request{Header: header}
	c, err := dummy.Cookie(name.Value)
	if err != nil {
		return runtime.Null{}, nil
	}
	return runtime.String{Value: c.Value}, nil
}

func nativeRequestQuery(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Request.query expects one argument")
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("Request.query name must be string")
	}
	values := requestQueryValues(this)
	if !values.Has(name.Value) {
		return runtime.Null{}, nil
	}
	return runtime.String{Value: values.Get(name.Value)}, nil
}

func nativeRequestQueryInt(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Request.queryInt expects one argument")
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("Request.queryInt name must be string")
	}
	values := requestQueryValues(this)
	if !values.Has(name.Value) {
		return runtime.Null{}, nil
	}
	n, ok := new(big.Int).SetString(strings.TrimSpace(values.Get(name.Value)), 10)
	if !ok {
		return runtime.Null{}, nil
	}
	return runtime.Int{Value: n}, nil
}

func nativeRequestQueryBool(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Request.queryBool expects one argument")
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("Request.queryBool name must be string")
	}
	values := requestQueryValues(this)
	if !values.Has(name.Value) {
		return runtime.Null{}, nil
	}
	switch strings.ToLower(strings.TrimSpace(values.Get(name.Value))) {
	case "1", "true", "yes", "on":
		return runtime.Bool{Value: true}, nil
	case "0", "false", "no", "off", "":
		return runtime.Bool{Value: false}, nil
	}
	return runtime.Null{}, nil
}

func nativeRequestQueryAll(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Request.queryAll expects one argument")
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("Request.queryAll name must be string")
	}
	values := requestQueryValues(this)[name.Value]
	elements := make([]runtime.Value, len(values))
	for i, v := range values {
		elements[i] = runtime.String{Value: v}
	}
	return &runtime.List{Elements: elements}, nil
}

func nativeRequestRouteParam(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Request.routeParam expects one argument")
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("Request.routeParam name must be string")
	}
	params, ok := this.Fields["params"].(runtime.Dict)
	if !ok {
		return runtime.Null{}, nil
	}
	value, ok := dictField(params, name.Value)
	if !ok {
		return runtime.Null{}, nil
	}
	return value, nil
}

func nativeRequestRouteParams(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("Request.routeParams expects no arguments")
	}
	if params, ok := this.Fields["params"].(runtime.Dict); ok {
		return params, nil
	}
	return runtime.Dict{Entries: map[string]runtime.DictEntry{}}, nil
}

func (e *Evaluator) httpRequestArgument(handler runtime.Function, request *http.Request, body []byte, tp *trustedProxies) (runtime.Value, error) {
	if !handlerWantsRequestObject(handler) {
		return httpRequestDict(request, body, tp), nil
	}
	class := e.httpRequestClass
	if class == nil && e.parent != nil {
		class = e.parent.httpRequestClass
	}
	if class == nil {
		return nil, fmt.Errorf("Request class is not available")
	}
	fields := fieldsFromEntries(httpRequestEntries(request, body))
	scheme, host, clientIP := resolveRequestMeta(request, tp)
	fields["scheme"] = runtime.String{Value: scheme}
	fields["host"] = runtime.String{Value: host}
	fields["clientIp"] = runtime.String{Value: clientIP}
	if request.TLS != nil && len(request.TLS.PeerCertificates) > 0 {
		fields["_clientCert"] = clientCertDict(request.TLS.PeerCertificates[0])
	}
	return &runtime.Instance{Class: class, Fields: fields}, nil
}

func handlerWantsRequestObject(handler runtime.Function) bool {
	if len(handler.Parameters) == 0 || handler.Parameters[0].Type == nil {
		return false
	}
	typ := handler.Parameters[0].Type
	return typ.Operator == "" && !typ.ListAlias && typeNamesEqual(typ.Name, "Request")
}

func (e *Evaluator) writeHTTPResponse(w http.ResponseWriter, r *http.Request, response runtime.Value) {
	if handler, ok := streamResponseHandler(response); ok {
		e.writeHTTPStreamResponse(w, response, handler)
		return
	}
	if handler, ok := websocketResponseHandler(response); ok {
		e.writeWebSocketResponse(w, r, response, handler)
		return
	}
	if outer, desc, ok := serveFileDescriptor(response); ok {
		writeHTTPServeFile(w, r, outer, desc)
		return
	}
	e.writeHTTPResponseValue(w, response)
}

// Response-dict key carrying the file marker.
const serveFileMarkerKey = "__serveFile"

// serveFileMarkerKind tags the native sentinel only web.serveFileMarker produces, so echoed JSON cannot forge a file response.
const serveFileMarkerKind = "serveFile"

func serveFileDescriptor(response runtime.Value) (runtime.Dict, runtime.Dict, bool) {
	dict, ok := response.(runtime.Dict)
	if !ok {
		return runtime.Dict{}, runtime.Dict{}, false
	}
	value, ok := dictField(dict, serveFileMarkerKey)
	if !ok {
		return runtime.Dict{}, runtime.Dict{}, false
	}
	marker, ok := value.(runtime.NativeObject)
	if !ok || marker.Kind != serveFileMarkerKind {
		return runtime.Dict{}, runtime.Dict{}, false
	}
	desc, ok := marker.Payload.(runtime.Dict)
	if !ok {
		return runtime.Dict{}, runtime.Dict{}, false
	}
	return dict, desc, true
}

// Serves the marker file via http.ServeContent (HEAD/Range/conditional/Content-Length); any open or stat failure is a 404 that never leaks the path.
func writeHTTPServeFile(w http.ResponseWriter, r *http.Request, outer runtime.Dict, desc runtime.Dict) {
	path, _ := dictStringField(desc, "path")
	if path == "" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if headers, ok := dictField(outer, "headers"); ok {
		writeHTTPHeaders(w.Header(), headers)
	}
	if headers, ok := dictField(desc, "headers"); ok {
		writeHTTPHeaders(w.Header(), headers)
	}
	name, ok := dictStringField(desc, "name")
	if !ok || name == "" {
		name = filepath.Base(path)
	}
	if ct, ok := dictStringField(desc, "contentType"); ok && ct != "" && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", ct)
	}
	// Default validator so If-None-Match yields a 304 for callers that set no ETag.
	if w.Header().Get("Etag") == "" {
		w.Header().Set("Etag", fmt.Sprintf("\"%x-%x\"", info.Size(), info.ModTime().UnixNano()))
	}
	if status, ok := dictField(outer, "status"); ok {
		if code, ok := toInt64(status); ok && code != 0 && int(code) != http.StatusOK {
			w = &statusOverrideWriter{ResponseWriter: w, override: int(code)}
		}
	}
	http.ServeContent(w, r, name, info.ModTime(), file)
}

// statusOverrideWriter substitutes a caller status for the 200 ServeContent writes on a full body, leaving 206/304/416 intact so range and conditional semantics hold.
type statusOverrideWriter struct {
	http.ResponseWriter
	override int
	wrote    bool
}

func (w *statusOverrideWriter) WriteHeader(code int) {
	if !w.wrote {
		w.wrote = true
		if code == http.StatusOK {
			code = w.override
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func streamResponseHandler(response runtime.Value) (runtime.Function, bool) {
	if instance, ok := response.(*runtime.Instance); ok && strings.EqualFold(instance.Class.Name, "Response") {
		response = responseInstanceDict(instance)
	}
	dict, ok := response.(runtime.Dict)
	if !ok {
		return runtime.Function{}, false
	}
	value, ok := dictField(dict, "stream")
	if !ok {
		return runtime.Function{}, false
	}
	handler, ok := value.(runtime.Function)
	return handler, ok
}

func websocketResponseHandler(response runtime.Value) (runtime.Function, bool) {
	if instance, ok := response.(*runtime.Instance); ok && strings.EqualFold(instance.Class.Name, "Response") {
		response = responseInstanceDict(instance)
	}
	dict, ok := response.(runtime.Dict)
	if !ok {
		return runtime.Function{}, false
	}
	value, ok := dictField(dict, "websocket")
	if !ok {
		return runtime.Function{}, false
	}
	handler, ok := value.(runtime.Function)
	return handler, ok
}

func (e *Evaluator) writeWebSocketResponse(w http.ResponseWriter, r *http.Request, response runtime.Value, handler runtime.Function) {
	if dict, ok := response.(runtime.Dict); ok {
		if value, ok := dictField(dict, "headers"); ok {
			writeHTTPHeaders(w.Header(), value)
		}
	}
	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	callbackEval, callbackHandler := e.callbackEvaluator(handler)
	if callbackEval != e {
		defer callbackEval.Cleanup()
		defer callbackEval.startDebugThread("websocket")()
	}
	handleValue := callbackEval.registerWebSocket(conn)
	handleID, _ := websocketHandleID(handleValue)
	defer func() {
		if err := callbackEval.closeWebSocketID(handleID); err != nil && !strings.Contains(err.Error(), "unknown websocket connection") {
			_ = conn.Close()
		}
	}()
	_, err = callbackEval.applyFunction(callbackHandler, []runtime.Value{handleValue})
	if err != nil {
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error()))
	}
}

func (e *Evaluator) writeHTTPStreamResponse(w http.ResponseWriter, response runtime.Value, handler runtime.Function) {
	status := http.StatusOK
	if dict, ok := response.(runtime.Dict); ok {
		if value, ok := dictField(dict, "status"); ok {
			if n, ok := toInt64(value); ok {
				status = int(n)
			}
		}
		if value, ok := dictField(dict, "headers"); ok {
			writeHTTPHeaders(w.Header(), value)
		}
	}
	flusher, _ := w.(http.Flusher)
	w.WriteHeader(status)
	if flusher != nil {
		flusher.Flush()
	}
	callbackEval, callbackHandler := e.callbackEvaluator(handler)
	if callbackEval != e {
		defer callbackEval.Cleanup()
		defer callbackEval.startDebugThread("stream")()
	}
	handleValue := callbackEval.registerHTTPStream(w, flusher)
	handleID, _ := httpStreamHandleID(handleValue)
	defer callbackEval.closeHTTPStreamID(handleID)
	_, _ = callbackEval.applyFunction(callbackHandler, []runtime.Value{handleValue})
}

func writeHTTPResponse(w http.ResponseWriter, response runtime.Value) {
	(&Evaluator{}).writeHTTPResponseValue(w, response)
}

func writeHTTPHeaders(target http.Header, value runtime.Value) {
	headers, ok := httpHeaderValue(value)
	if !ok {
		return
	}
	for key, values := range headers.Values {
		for i, value := range values {
			if i == 0 {
				target.Set(key, value)
			} else {
				target.Add(key, value)
			}
		}
	}
}

func (e *Evaluator) writeHTTPResponseValue(w http.ResponseWriter, response runtime.Value) {
	if instance, ok := response.(*runtime.Instance); ok && strings.EqualFold(instance.Class.Name, "Response") {
		response = responseInstanceDict(instance)
	}
	status := http.StatusOK
	var body runtime.Value = runtime.Bytes{}
	if text, ok := response.(runtime.String); ok {
		body = text
	} else if dict, ok := response.(runtime.Dict); ok {
		if value, ok := dict.GetEntry(dictKey(runtime.String{Value: "status"})); ok {
			if n, ok := toInt64(value.Value); ok {
				status = int(n)
			}
		}
		if value, ok := dict.GetEntry(dictKey(runtime.String{Value: "headers"})); ok {
			writeHTTPHeaders(w.Header(), value.Value)
		}
		if value, ok := dict.GetEntry(dictKey(runtime.String{Value: "body"})); ok {
			body = value.Value
		}
	} else if _, ok := response.(runtime.Null); !ok {
		body = runtime.String{Value: response.Inspect()}
	}
	// A known-length body gets an explicit Content-Length so the response is not chunked and the connection can be kept alive (the cached-page and static-asset hot paths).
	if n := httpBodyByteLen(body); n >= 0 && w.Header().Get("Content-Length") == "" {
		w.Header().Set("Content-Length", strconv.Itoa(n))
	}
	w.WriteHeader(status)
	_ = e.writeHTTPBody(w, body)
}

// httpBodyByteLen returns the byte length of a fully-buffered body, or -1 when the length is not known up front (e.g. a streamed file handle).
func httpBodyByteLen(body runtime.Value) int {
	switch b := body.(type) {
	case runtime.String:
		return len(b.Value)
	case runtime.Bytes:
		return len(b.Value)
	}
	return -1
}

func (e *Evaluator) writeHTTPBody(w http.ResponseWriter, body runtime.Value) error {
	switch body := body.(type) {
	case runtime.String:
		_, err := io.WriteString(w, body.Value)
		return err
	case runtime.Bytes:
		_, err := w.Write(body.Value)
		return err
	case runtime.Int:
		file, err := e.fileHandle(body)
		if err != nil {
			_, _ = io.WriteString(w, body.Inspect())
			return err
		}
		_, err = io.Copy(w, file)
		return err
	case runtime.NativeObject:
		if body.Kind == "IOBuffer" {
			buffer, err := e.bufferHandle(body.ID)
			if err != nil {
				_, _ = io.WriteString(w, body.Inspect())
				return err
			}
			_, err = io.Copy(w, bytes.NewReader(buffer.Bytes()))
			return err
		}
		_, err := io.WriteString(w, body.Inspect())
		return err
	case runtime.Null:
		return nil
	default:
		_, err := io.WriteString(w, body.Inspect())
		return err
	}
}
