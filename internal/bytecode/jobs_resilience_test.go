package bytecode_test

import "testing"

func TestParityJobsIsolateInvalidRetryPolicyAndShutdownSuccess(t *testing.T) {
	source := `import io;
import db;
import jobs;
import store;
import async;
let conn = db.Connection("sqlite", ":memory:");
let queue = jobs.Queue(conn, {"namespace": "parity"});
let events = store.Store();
queue.register("bad", func(jobs.Job job): void { events.incr("bad"); }, {"backoffMs": func(int attempt): int { throw ValueError("unknown retry backoff"); }});
queue.register("good", func(jobs.Job job): void { events.incr("good"); });
let bad = queue.enqueue("bad", {}, {"queue": "high"});
let good = queue.enqueue("good", {});
let worker = jobs.Worker(queue, {"queues": ["high", "default", "low"], "queueConcurrency": {"high": 1}});
io.println(jobs.cli(worker, null, ["--queue", "high", "--queue", "default", "--once"]));
io.println(queue.get(bad).status + " " + queue.get(bad).lastError);
io.println(queue.get(good).status + " " + (events.get("good") as string));
io.println(events.get("bad") == null);
let draining = jobs.Queue(conn, {"namespace": "draining"});
draining.register("slow", func(jobs.Job job): void {
    events.set("running", true);
    while (!job.isCancellationRequested()) { async.await(async.sleep(1)); }
}, {"backoffMs": [0]});
let slow = draining.enqueue("slow", {});
let handle = jobs.Worker(draining, {"shutdownTimeoutMs": 1, "pollIntervalMs": 1}).start();
while (events.get("running") != true) { async.await(async.sleep(1)); }
handle.requestStop();
let result = handle.wait(10000);
io.println((result.completed as string) + " " + (result.retry as string) + " " + draining.get(slow).status);
io.println(jobs.Scheduler(queue).revision("absent") == null);
`
	ev, vm := parityOutputsStdlib(t, source)
	want := "1\nfailed jobs: invalid retry policy: unknown retry backoff\ncompleted 1\ntrue\n1 0 completed\ntrue\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
