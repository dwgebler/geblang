package evaluator

// Phase 4 HTTP/server high-concurrency harness (evaluator path). Measures raw
// http.serve throughput and latency, exercises the worker-pool overload path,
// and pins per-request handler-state isolation; the VM now isolates per request too, so both backends are concurrency-safe by default. See docs/http-concurrency-evaluation.md.
//
// Benchmarks: go test -bench BenchmarkHTTPServe -run x ./internal/evaluator

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"geblang/internal/concurrent"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	gruntime "geblang/internal/runtime"
)

// buildServerHandler parses a Geblang function literal and evaluates it in an
// env holding `defines`, returning the resulting closure as the request
// handler. Avoids hand-building AST while letting the handler capture shared
// state (a singleton instance, a native blocker).
func buildServerHandler(tb testing.TB, e *Evaluator, src string, defines map[string]gruntime.Value) gruntime.Function {
	tb.Helper()
	p := parser.New(lexer.New("let __handler = " + src + ";"))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		tb.Fatalf("parse handler: %v", errs)
	}
	env := gruntime.NewEnvironment()
	if err := e.installBuiltinTypes(env); err != nil {
		tb.Fatalf("install builtins: %v", err)
	}
	for name, value := range defines {
		_ = env.Define(name, value, false)
	}
	if _, err := e.evalStatements(program.Statements, env); err != nil {
		tb.Fatalf("eval handler: %v", err)
	}
	value, ok := env.Get("__handler")
	if !ok {
		tb.Fatal("handler binding missing")
	}
	fn, ok := value.(gruntime.Function)
	if !ok {
		tb.Fatalf("handler is %T, want Function", value)
	}
	return fn
}

// startServerTB binds an http.Handler on a loopback tcp4 port (tcp4 dodges the
// IPv6-localhost flakiness seen on some hosts) and returns a started server
// whose client is tuned for concurrent connections.
func startServerTB(tb testing.TB, concurrency int, h http.Handler) *httptest.Server {
	tb.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		tb.Skipf("local sockets unavailable: %v", err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.Listener = listener
	srv.Start()
	if tr, ok := srv.Client().Transport.(*http.Transport); ok {
		tr.MaxIdleConns = 0
		tr.MaxIdleConnsPerHost = concurrency
		tr.MaxConnsPerHost = concurrency
	}
	return srv
}

const okHandlerSrc = `func(any req) { return {"status": 200, "body": "ok"}; }`

// BenchmarkHTTPServeThroughput measures end-to-end request cost (and therefore
// throughput, b.N/elapsed) for a trivial Geblang handler over the real
// http.serve dispatch path, including the per-request child evaluator.
func BenchmarkHTTPServeThroughput(b *testing.B) {
	e := New(io.Discard)
	handler := buildServerHandler(b, e, okHandlerSrc, nil)
	srv := startServerTB(b, 64, e.httpHandler(handler, nil, nil, 0, false, false, handlerLimits{}))
	defer srv.Close()
	client := srv.Client()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get(srv.URL)
			if err != nil {
				b.Error(err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	})
}

// TestHTTPConcurrencyLatency drives a fixed request count at a fixed
// concurrency and logs latency percentiles. It asserts only that every request
// succeeds, so it is not timing-flaky; the numbers are read from the log for
// the evaluation report.
func TestHTTPConcurrencyLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping latency measurement in -short")
	}
	const concurrency = 50
	const perWorker = 200

	e := New(io.Discard)
	handler := buildServerHandler(t, e, okHandlerSrc, nil)
	srv := startServerTB(t, concurrency, e.httpHandler(handler, nil, nil, 0, false, false, handlerLimits{}))
	defer srv.Close()
	client := srv.Client()

	latencies := make([]time.Duration, concurrency*perWorker)
	var errs int64
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				reqStart := time.Now()
				resp, err := client.Get(srv.URL)
				if err != nil {
					atomic.AddInt64(&errs, 1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				latencies[worker*perWorker+i] = time.Since(reqStart)
			}
		}(w)
	}
	wg.Wait()
	elapsed := time.Since(start)

	if errs != 0 {
		t.Fatalf("%d request errors", errs)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	pct := func(q float64) time.Duration { return latencies[int(float64(len(latencies))*q)] }
	rps := float64(len(latencies)) / elapsed.Seconds()
	t.Logf("requests=%d concurrency=%d elapsed=%v throughput=%.0f req/s p50=%v p90=%v p99=%v max=%v",
		len(latencies), concurrency, elapsed.Round(time.Millisecond), rps,
		pct(0.50), pct(0.90), pct(0.99), latencies[len(latencies)-1])
}

// TestHTTPServerWorkerPoolOverload verifies the bounded worker pool: with two
// slots held by blocked handlers and no queue, further concurrent requests are
// rejected with 503 and counted in serverStats. Deterministic: the rejected
// wave is only fired after both slots are confirmed occupied.
func TestHTTPServerWorkerPoolOverload(t *testing.T) {
	e := New(io.Discard)
	const slots = 2
	entered := make(chan struct{}, slots)
	release := make(chan struct{})
	blocker := gruntime.Function{Native: func(this *gruntime.Instance, args []gruntime.Value) (gruntime.Value, error) {
		entered <- struct{}{}
		<-release
		return gruntime.Null{}, nil
	}}
	handler := buildServerHandler(t, e,
		`func(any req) { _block(); return {"status": 200, "body": "ok"}; }`,
		map[string]gruntime.Value{"_block": blocker})

	pool := concurrent.NewPool(slots, 0, concurrent.Reject)
	srv := startServerTB(t, 16, e.httpHandler(handler, pool, nil, 0, false, false, handlerLimits{}))
	defer srv.Close()
	client := srv.Client()

	// Occupy both slots.
	var holders sync.WaitGroup
	for i := 0; i < slots; i++ {
		holders.Add(1)
		go func() {
			defer holders.Done()
			resp, err := client.Get(srv.URL)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}()
	}
	for i := 0; i < slots; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("handlers never occupied the pool slots")
		}
	}

	// With both slots held and no queue, this wave must be rejected.
	const overflow = 8
	var rejected int64
	var wave sync.WaitGroup
	for i := 0; i < overflow; i++ {
		wave.Add(1)
		go func() {
			defer wave.Done()
			resp, err := client.Get(srv.URL)
			if err != nil {
				return
			}
			if resp.StatusCode == http.StatusServiceUnavailable {
				atomic.AddInt64(&rejected, 1)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}()
	}
	wave.Wait()

	if rejected == 0 {
		close(release)
		holders.Wait()
		t.Fatal("expected overflow requests to be rejected with 503")
	}
	stats := pool.Stats()
	if stats.Rejected == 0 {
		close(release)
		holders.Wait()
		t.Fatalf("pool stats reported no rejections: %+v", stats)
	}
	t.Logf("overflow=%d rejected=%d poolStats=%+v", overflow, rejected, stats)

	close(release)
	holders.Wait()
}

// TestServerHandlerStateIsolation documents the evaluator's server-handler
// model: each request runs in a child evaluator with a per-request deep clone
// of the handler closure (callbackEvaluator -> CloneFunction), so a singleton
// captured by the handler is copied per request. State therefore does NOT
// persist across requests on the evaluator (a counter stays at 1), which is
// also why the evaluator does not race on shared Geblang state. The bytecode VM now isolates handler state per request too, so both backends are concurrency-safe by default. See docs/http-concurrency-evaluation.md.
func TestServerHandlerStateIsolation(t *testing.T) {
	e := New(io.Discard)
	storeClass := &gruntime.Class{Name: "Store", Fields: []gruntime.Field{{Name: "n"}}}
	shared := &gruntime.Instance{
		Class:  storeClass,
		Fields: map[string]gruntime.Value{"n": gruntime.NewInt64(0)},
	}
	handler := buildServerHandler(t, e,
		`func(any req) { shared.n = shared.n + 1; return {"status": 200, "body": "${shared.n}"}; }`,
		map[string]gruntime.Value{"shared": shared})
	srv := startServerTB(t, 1, e.httpHandler(handler, nil, nil, 0, false, false, handlerLimits{}))
	defer srv.Close()

	for i := 0; i < 3; i++ {
		resp, err := srv.Client().Get(srv.URL)
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(body) != "1" {
			t.Fatalf("request %d: evaluator isolates handler state per request; got body %q, want \"1\"", i+1, string(body))
		}
	}
}
