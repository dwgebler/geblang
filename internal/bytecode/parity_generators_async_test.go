package bytecode_test

// Parity tests run each snippet through both the evaluator and the VM and
// assert that the printed output matches.  They pin the intersection of
// features that both execution paths are expected to support identically.

import (
	"testing"
)

// stdlibModuleLoader is a minimal bytecode-side module loader used by
// parity tests for source-distributed stdlib modules (time.scheduler,
// async.rate, etc.). It resolves canonical module paths via the standard
// resolver (which walks ancestors to find stdlib/), compiles each
// imported source module to bytecode on demand, and caches the result
// inside the loader struct.

func TestParityGeneratorFunctionYieldsValues(t *testing.T) {
	runParity(t, `import io;
func numbers(): any {
    yield 1;
    yield 2;
    yield 3;
}
let total = 0;
for (n in numbers()) {
    total = total + n;
}
io.println(total);
	`, "6\n")
}

func TestParityGeneratorFunctionYieldsFromLoop(t *testing.T) {
	runParity(t, `import io;
func upTo(int max): any {
    for (let int i = 1; i <= max; i++) {
        yield i;
    }
}
for (n in upTo(3)) {
    io.println(n);
}
	`, "1\n2\n3\n")
}

func TestParityGeneratorLiteralCapturesValues(t *testing.T) {
	runParity(t, `import io;
func make(int start): callable {
    return func(): any {
        yield start;
        yield start + 1;
    };
}
let gen = make(4);
let out = 0;
for (n in gen()) {
    out = out + n;
}
io.println(out);
	`, "9\n")
}

func TestParityGeneratorTypeHintsAndEarlyBreak(t *testing.T) {
	runParity(t, `import io;
func naturals(): generator<int> {
    int n = 0;
    while (true) {
        yield n;
        n++;
    }
}

iterable<int> values = naturals();
for (n in values) {
    io.println(n);
    break;
}
io.println("done");
	`, "0\ndone\n")
}

func TestParityAsyncFuncAndAwait(t *testing.T) {
	runParity(t, `import io;
async func double(int x): int {
    return x * 2;
}
let task = double(21);
io.println(typeof(task));
io.println(await task);
`, "Task\n42\n")
}

func TestParityAsyncFuncOnReturn(t *testing.T) {
	runParity(t, `import io;
async func compute(): int {
    return 7;
}
let t1 = compute();
let t2 = compute();
io.println(await t1);
io.println(await t2);
`, "7\n7\n")
}

func TestParityAsyncRunAndAwait(t *testing.T) {
	runParity(t, `import io;
import async;
let task = async.run(func(): int {
    return 99;
});
io.println(await task);
`, "99\n")
}

func TestParityAsyncAwaitExpr(t *testing.T) {
	runParity(t, `import io;
async func greet(): string {
    return "hello";
}
io.println(await greet());
`, "hello\n")
}

func TestParityAwaitPreservesThrownErrorClass(t *testing.T) {
	runParity(t, `import io;
async func boom(): int {
    throw ValueError("vb");
}
try {
    io.println(await boom());
} catch (ValueError e) {
    io.println("typed ${e.message}");
}
try {
    io.println(await boom());
} catch (Error e) {
    io.println("base ${e.message}");
}
`, "typed vb\nbase vb\n")
}

func TestParityAwaitThrownErrorInMethodAndAsyncAwait(t *testing.T) {
	runParity(t, `import io;
import async;
async func boom(): int {
    throw ValueError("vm");
}
class Runner {
    func go(): string {
        try {
            await boom();
            return "no";
        } catch (ValueError e) {
            return "method ${e.message}";
        }
    }
}
io.println(Runner().go());
try {
    async.await(boom());
} catch (ValueError e) {
    io.println("native ${e.message}");
}
`, "method vm\nnative vm\n")
}

func TestParityAsyncTaskDoneMethod(t *testing.T) {
	runParity(t, `import io;
async func noop(): void {}
let t = noop();
io.println(await t);
io.println(t.done() as string);
`, "null\ntrue\n")
}

func TestParityAsyncFuncLiteral(t *testing.T) {
	runParity(t, `import io;
import async;
let fn = async func(): int { return 5; };
let t = async.run(fn);
io.println(await t);
`, "5\n")
}

func TestParitySelect(t *testing.T) {
	// Default fires when no case is ready.
	runParityWithStdlib(t, `import io;
import async.channel as ch;
let c = ch.Channel<int>(0);
select {
    case let v = c.recv(): { io.println("recv: " + (v as string)); }
    default: { io.println("default"); }
}
`, "default\n")

	// recv from a buffered channel fires immediately.
	runParityWithStdlib(t, `import io;
import async.channel as ch;
let c = ch.Channel<int>(2);
c.send(7);
select {
    case let v = c.recv(): { io.println("got: " + (v as string)); }
    default: { io.println("default"); }
}
`, "got: 7\n")

	// send to a buffered channel fires immediately when space is available.
	runParityWithStdlib(t, `import io;
import async.channel as ch;
let c = ch.Channel<int>(2);
select {
    case c.send(42): { io.println("sent"); }
    default: { io.println("blocked"); }
}
io.println(c.recv());
`, "sent\n42\n")

	// Multi-case picks the only ready one.
	runParityWithStdlib(t, `import io;
import async.channel as ch;
let a = ch.Channel<int>(1);
let b = ch.Channel<int>(1);
b.send(99);
select {
    case let v = a.recv(): { io.println("a: " + (v as string)); }
    case let v = b.recv(): { io.println("b: " + (v as string)); }
}
`, "b: 99\n")

	// recv without binding works.
	runParityWithStdlib(t, `import io;
import async.channel as ch;
let c = ch.Channel<int>(1);
c.send(5);
select {
    case c.recv(): { io.println("drained"); }
    default: { io.println("nope"); }
}
`, "drained\n")
}

func TestParityAsyncChannel(t *testing.T) {
	// Buffered: send three values, then drain.
	runParityWithStdlib(t, `import io;
import async.channel as ch;
let c = ch.Channel<int>(3);
c.send(1); c.send(2); c.send(3);
io.println(c.recv());
io.println(c.recv());
io.println(c.recv());
`, "1\n2\n3\n")

	// trySend / tryRecv behaviour.
	runParityWithStdlib(t, `import io;
import async.channel as ch;
let c = ch.Channel<int>(2);
io.println(c.trySend(10));
io.println(c.trySend(20));
io.println(c.trySend(30));
io.println(c.tryRecv());
io.println(c.tryRecv());
io.println(c.tryRecv() == null);
`, "true\ntrue\nfalse\n10\n20\ntrue\n")

	// Close + drain + null on closed-empty + send-on-closed throws.
	runParityWithStdlib(t, `import io;
import async.channel as ch;
let c = ch.Channel<int>(2);
c.send(7);
c.close();
io.println(c.isClosed());
io.println(c.recv());
io.println(c.recv() == null);
try { c.send(8); } catch (Error e) { io.println("send on closed throws"); }
try { c.close(); } catch (Error e) { io.println("double close throws"); }
`, "true\n7\ntrue\nsend on closed throws\ndouble close throws\n")

	// Producer + consumer via async.run.
	runParityWithStdlib(t, `import io;
import async;
import async.channel as ch;
let c = ch.Channel<int>(0);
async.run(func(): void {
    for (let int i = 1; i <= 4; i++) { c.send(i * 11); }
    c.close();
});
let total = 0;
for (v in c) { total = total + v; }
io.println(total);
`, "110\n")
}

func TestParityAsyncSync(t *testing.T) {
	// Mutex tryLock semantics + atomics arithmetic round-trip.
	runParityWithStdlib(t, `import io;
import async.sync as sync;
import async.atomic as atomic;

let m = sync.Mutex();
m.lock();
io.println(m.tryLock());
m.unlock();
io.println(m.tryLock());
m.unlock();

let c = atomic.AtomicInt(0);
io.println(c.load());
io.println(c.add(5));
io.println(c.add(-2));
io.println(c.compareAndSwap(3, 42));
io.println(c.compareAndSwap(99, 0));
io.println(c.load());
`, "false\ntrue\n0\n5\n3\ntrue\nfalse\n42\n")

	// Semaphore acquire/tryAcquire/release.
	runParityWithStdlib(t, `import io;
import async.sync as sync;
let s = sync.Semaphore(2);
io.println(s.tryAcquire());
io.println(s.tryAcquire());
io.println(s.tryAcquire());
s.release();
io.println(s.tryAcquire());
`, "true\ntrue\nfalse\ntrue\n")

	// RWMutex: multiple read locks + a write lock.
	runParityWithStdlib(t, `import io;
import async.sync as sync;
let rw = sync.RWMutex();
io.println(rw.tryRLock());
io.println(rw.tryRLock());
io.println(rw.tryLock());
rw.rUnlock();
rw.rUnlock();
io.println(rw.tryLock());
rw.unlock();
`, "true\ntrue\nfalse\ntrue\n")

	// AtomicBool basics.
	runParityWithStdlib(t, `import io;
import async.atomic as atomic;
let f = atomic.AtomicBool();
io.println(f.load());
f.store(true);
io.println(f.load());
io.println(f.compareAndSwap(true, false));
io.println(f.compareAndSwap(true, false));
io.println(f.load());
`, "false\ntrue\ntrue\nfalse\nfalse\n")
}

// TestParityAsyncAll verifies async.all returns results in original
// order once every input task completes.
func TestParityAsyncAll(t *testing.T) {
	runParity(t, `import async;
import io;
let a = async.run(func(): int { async.await(async.sleep(20)); return 1; });
let b = async.run(func(): int { async.await(async.sleep(10)); return 2; });
let c = async.run(func(): int { return 3; });
let results = async.await(async.all([a, b, c]));
io.println(results[0]);
io.println(results[1]);
io.println(results[2]);
`, "1\n2\n3\n")
}

// TestParityAsyncRace verifies async.race resolves with the first
// completing task's value and cancels the others.
func TestParityAsyncRace(t *testing.T) {
	runParity(t, `import async;
import io;
let fast = async.run(func(): string { async.await(async.sleep(10)); return "fast"; });
let slow = async.run(func(): string { async.await(async.sleep(500)); return "slow"; });
io.println(async.await(async.race([fast, slow])));
`, "fast\n")
}

// TestParityAsyncTimeout verifies async.timeout rejects when the
// inner task takes longer than the deadline.
func TestParityAsyncTimeout(t *testing.T) {
	runErrorParity(t, `import async;
import io;
let slow = async.run(func(): string {
    async.await(async.sleep(500));
    return "never";
});
io.println(async.await(async.timeout(slow, 30)));
`, "timeout")
}

// TestParityTaskCancel verifies a cancelled task reports cancelled
// and Await surfaces the cancellation error.
func TestParityTaskCancel(t *testing.T) {
	runParity(t, `import async;
import io;
let t = async.run(func(): int {
    async.await(async.sleep(500));
    return 42;
});
async.cancel(t);
io.println(t.cancelled);
`, "true\n")
}

// TestParityNullCoalesceInAsync verifies the ?? operator works inside
// an async-run callback. Regression test for a bug where
// runWrapper / shiftInstructionOperands omitted OpNullCoalesce from
// the jump-target shift list, so the callback's OpNullCoalesce jumped
// back into the wrapper prologue and looped forever.
func TestParityNullCoalesceInAsync(t *testing.T) {
	runParity(t, `import io;
import async;
let t = async.run(func(): int {
    let v = 5 ?? 10;
    return v;
});
io.println(async.await(t));
`, "5\n")
}

// TestParityOptionalChainInAsync covers the matching OpOptionalChain
// shift fix (?. on a class instance inside an async-run callback).
func TestParityOptionalChainInAsync(t *testing.T) {
	runParity(t, `import io;
import async;
class Box { string name; func Box(string n) { this.name = n; } }
let t = async.run(func(): any {
    Box b = Box("hi");
    return b?.name;
});
io.println(async.await(t));
`, "hi\n")
}

// TestParityParenthesizedSelectorInvokesValue verifies that a
// parenthesized field-access on a method-call target invokes the
// VALUE of the field (a callable) rather than dispatching as a
// method call. Surfaced while caching `appmod.dispatcher(app)` on
// a TestClient field: `(this.dispatch)(request)` previously parsed
// the same as `this.dispatch(request)` so the VM/eval looked up a
// `dispatch` method on TestClient (which doesn't exist).
func TestParityParenthesizedSelectorInvokesValue(t *testing.T) {
	runParity(t, `import io;

class C {
    callable fn;
    func C(callable f) { this.fn = f; }
    func via(int x): int {
        return (this.fn)(x);
    }
}

let c = C(func(int n): int { return n * 3; });
io.println(c.via(7));
`, "21\n")
}

// TestParityUserClassNamedTaskNoCollision guards an evaluator-only
// regression where a user class named `Task` was unconditionally
// rejected by the overload / parameter type-matcher: the evaluator
// short-circuited on `typeName == "Task"` and required the value to
// be a *runtime.Task (the async Task primitive). User-declared Task
// classes now match their own instances. The VM was already correct
// because its type-name dispatch routes through `vmTypeKindForBase`
// and never hard-codes the `Task` string.
// Surfaced building the gebweb Tasks example app, where every CRUD
// handler called `repo.save(Task entity)`.
func TestParityUserClassNamedTaskNoCollision(t *testing.T) {
	runParity(t, `import io;

class Task {
    string id;
    string title;
    func Task() { this.id = ""; this.title = ""; }
}

class Store {
    list<Task> items;
    func Store() { this.items = []; }
    func add(Task t): Task { this.items = this.items.push(t); return t; }
    func adopt(?Task t): bool {
        if (t == null) { return false; }
        this.add(t as Task);
        return true;
    }
}

let s = Store();
let t = Task();
t.id = "t-1";
t.title = "hello";
io.println(s.add(t).title);
io.println(s.adopt(t));
io.println(s.adopt(null));
io.println(s.items.length());
io.println(t instanceof Task);
`, "hello\ntrue\nfalse\n2\ntrue\n")
}

func TestParityGeneratorMethodOnClass(t *testing.T) {
	runParity(t, `import io;

class Box {
    int max;
    func Box(int m) { this.max = m; }
    func nums(): generator<int> {
        for (let int i = 0; i < this.max; i++) {
            yield i;
        }
    }
}

let b = Box(3);
for (n in b.nums()) {
    io.println(n);
}
`, "0\n1\n2\n")
}

// Channel for-in iteration uses a per-consumer iterator (not the shared
// channel), and drains a buffered channel identically on both backends.
func TestParityChannelIteration(t *testing.T) {
	runParityWithStdlib(t, `import io;
import async.channel as ch;
let c = ch.Channel<int>(4);
c.send(10); c.send(20); c.send(30); c.close();
let total = 0;
for (v in c) { total = total + v; }
io.println(total);
`, "60\n")
}

// Manual generator stepping: next()/done()/close() mirror the
// __next/__done iterator contract on both backends (1.19.0).
func TestParityGeneratorManualStepping(t *testing.T) {
	runParity(t, `import io;

func gen(): generator<int> {
    yield 1;
    yield 2;
    yield 3;
}

let g = gen();
io.println(g.done());
io.println(g.next());
io.println(g.next());
io.println(g.done());
io.println(g.next());
io.println(g.done());
io.println(g.next());

let h = gen();
io.println(h.next());
int rest = 0;
for (v in h) {
    rest = rest + v;
}
io.println("rest=${rest}");

let c = gen();
c.next();
c.close();
io.println(c.done());
`, `false
1
2
false
3
true
null
1
rest=5
true
`)
}

func TestParityGeneratorThrowKeepsErrorClass(t *testing.T) {
	runParity(t, `import io;
func g(): generator<int> { yield 1; throw ValueError("boom"); }
try { for (n in g()) { io.println(n); } } catch (ValueError e) { io.println("caught ${e.class}: ${e.message}"); }
class MyErr extends ValueError { func MyErr(string m) { parent(m); } }
func g2(): generator<int> { yield 5; throw MyErr("custom"); }
try { for (n in g2()) { io.println(n); } } catch (ValueError e) { io.println("sub ${e.class}: ${e.message}"); }
func g3(): generator<int> { yield 9; throw TypeError("t"); }
try { let xs = [x * 2 for x in g3()]; io.println(xs); } catch (TypeError e) { io.println("compr ${e.class}"); }
func g4(): generator<int> { yield 2; throw ValueError("nested"); }
try {
    try { for (n in g4()) { io.println(n); } } catch (TypeError e) { io.println("wrong"); }
} catch (ValueError e) { io.println("outer ${e.message}"); }
`, "1\ncaught ValueError: boom\n5\nsub MyErr: custom\ncompr TypeError\n2\nouter nested\n")
}

func TestParityAsyncTasks(t *testing.T) {
	runParityWithStdlib(t, `import io;
import async;
import async.tasks as task;

let doubled = task.map([1, 2, 3, 4], func(int x): int { return x * 2; });
io.println("map=${doubled}");

let bounded = task.map([10, 20, 30], func(int x): int { return x + 1; }, {"concurrency": 2});
io.println("bounded=${bounded}");

let p = task.parallel([func(): int { return 7; }, func(): int { return 8; }]);
io.println("parallel=${p}");

let calls = [0];
let r = task.retry(func(): string {
    calls[0] = calls[0] + 1;
    if (calls[0] < 2) { throw RuntimeError("x"); }
    return "ok";
}, {"attempts": 3, "delayMs": 1});
io.println("retry=${r}");

let t1 = async.run(func(): int { return 1; });
let t2 = async.run(func(): int { throw RuntimeError("e"); });
let outcomes = task.settle([t1, t2]);
io.println("settle=${outcomes[0]["ok"]},${outcomes[1]["ok"]}");
`, "map=[2, 4, 6, 8]\nbounded=[11, 21, 31]\nparallel=[7, 8]\nretry=ok\nsettle=true,false\n")
}
