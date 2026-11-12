package main

import (
	"bufio"
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestJobsWorkerAcrossModulesCacheBuildAndProcesses(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	stdlib, err := filepath.Abs("../../stdlib")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"geblang.yaml": "name: jobs_fixture\nsource: .\n",
		"handlers.gb": `module handlers;
import db;
import jobs;
export class Base {
    db.Connection conn;
    func Base(db.Connection conn) { this.conn = conn; }
    func inherited(jobs.Job job): void {
        this.conn.exec("INSERT INTO effects VALUES (?, ?) ON CONFLICT DO NOTHING", job.id, job.payload["n"]);
    }
}
export class Handler extends Base {
    func Handler(db.Connection conn) { parent(conn); }
    func own(jobs.Job job): void { this.inherited(job); }
}
export class NativeConnection extends db.Connection {
    func NativeConnection() { parent("sqlite", ":memory:"); }
    func ownDriver(): string { return this.driver(); }
}
export class DomainError extends RuntimeError {
    func DomainError(string message) { parent(message); }
}
export class ConflictError extends DomainError {
    func ConflictError(string message) { parent(message); }
}
`,
		"app.gb": `module app;
import db;
import jobs;
import sys;
import io;
import handlers;
import reflect;
import async;
import errors;
class Child extends handlers.Handler {
    func Child(db.Connection conn) { parent(conn); }
    func child(jobs.Job job): void { this.own(job); }
}
export func main(list<string> args): int {
    let conn = db.Connection("sqlite", sys.getenv("JOBS_FIXTURE_DB") ?? ":memory:");
    defer conn.close();
    conn.exec("CREATE TABLE IF NOT EXISTS effects (id TEXT PRIMARY KEY, n INTEGER)");
    conn.exec("CREATE TABLE IF NOT EXISTS control (name TEXT PRIMARY KEY)");
    let queue = jobs.Queue(conn, {"namespace": "fixture", "operationTimeoutMs": 100});
    let handler = Child(conn);
    queue.register("inherited", handler.inherited);
    queue.register("own", handler.own);
    queue.register("child", handler.child);
    queue.register("reflected", reflect.method(handler, "inherited"));
    queue.register("blocking", func(jobs.Job job): void {
        if (job.attempt == 1) {
            io.println("claimed");
            while (conn.query("SELECT name FROM control WHERE name = 'release'").all().length() == 0) { async.await(async.sleep(5)); }
        }
        handler.own(job);
    }, {"maxAttempts": 2, "backoffMs": [0]});
    if (args.length() > 0 && args[0] == "seed-crash") {
        io.println(queue.enqueue("blocking", {"n": 99}));
        return 0;
    }
    if (args.length() > 0 && args[0] == "lease-worker") {
        return jobs.cli(jobs.Worker(queue, {"pollIntervalMs": 10, "leaseMs": 2000, "heartbeatMs": 100, "shutdownTimeoutMs": 5000}), null, args[1:]);
    }
    if (args.length() > 0 && args[0] == "produce") {
        for (i in 0..<24) { queue.enqueue(["inherited", "own", "child"][i % 3], {"n": i}); }
        return 0;
    }
    if (args.length() > 0 && args[0] == "inspect") {
        io.println(conn.driver());
        io.println(queue.stats().completed);
        io.println(conn.query("SELECT SUM(n) AS n FROM effects").all()[0]["n"]);
        return 0;
    }
    if (args.length() > 0 && args[0] == "selftest") {
        let bound = conn.driver;
        io.println(bound());
        let native = handlers.NativeConnection();
        let beginNative = native.begin;
        let nativeTx = beginNative(1000, {"isolation": "default"});
        nativeTx.rollback();
        io.println(native instanceof db.Connection);
        io.println(native.ownDriver());
        native.close();
        bool caught = false;
        try { throw jobs.ConflictError("job conflict"); }
        catch (jobs.JobError e) { caught = true; }
        io.println(caught);
        bool nativeCaught = false;
        try { async.await(async.timeout(async.token(), 0)); }
        catch (IOError e) { nativeCaught = true; }
        catch (Error e) {}
        io.println(nativeCaught);
        io.println(errors.is(jobs.TimeoutError("job"), "IOError"));
        let tx = conn.begin(1000, {"isolation": "default"});
        let id = queue.enqueueIn(tx, "child", {"n": 7});
        tx.rollback();
        io.println(queue.get(id) == null);
        for (name in ["inherited", "own", "child", "reflected"]) { queue.enqueue(name, {"n": 7}); }
        let result = jobs.Worker(queue, {"drainOnce": true, "maxConcurrency": 2}).run();
        io.println(result.completed);
        io.println(conn.query("SELECT SUM(n) AS n FROM effects").all()[0]["n"]);
        return 0;
    }
    return jobs.cli(jobs.Worker(queue), null, args);
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(), "GEBLANG_STDLIB="+stdlib)
	command := func(path string, args ...string) *exec.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		t.Cleanup(cancel)
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Dir, cmd.Env = dir, env
		return cmd
	}
	run := func(cmd *exec.Cmd) string {
		t.Helper()
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", cmd.Args, err, out)
		}
		return string(out)
	}
	want := "sqlite\ntrue\nsqlite\ntrue\ntrue\nfalse\ntrue\n4\n28\n"
	for _, args := range [][]string{
		{"worker", "--entry", "app", "selftest"},
		{"worker", "--entry", "app", "selftest"},
		{"worker", "--disable-vm", "--entry", "app", "selftest"},
	} {
		if got := run(command(bin, args...)); got != want {
			t.Fatalf("%v: got %q, want %q", args, got, want)
		}
	}
	out := filepath.Join(dir, "worker")
	run(command(bin, "build", "--entry", "app", "--out", out, "."))
	built := command(out, "selftest")
	built.Dir = t.TempDir()
	built.Env = nil
	for _, entry := range env {
		if !strings.HasPrefix(entry, "GEBLANG_STDLIB=") {
			built.Env = append(built.Env, entry)
		}
	}
	if got := run(built); got != want {
		t.Fatalf("standalone built worker: got %q, want %q", got, want)
	}
	for _, runtime := range []string{"--vm-strict", "--disable-vm"} {
		env = append(env, "JOBS_FIXTURE_DB="+filepath.Join(t.TempDir(), "jobs.db"))
		run(command(bin, "worker", runtime, "--entry", "app", "produce"))
		a := command(bin, "worker", runtime, "--entry", "app", "--once")
		b := command(bin, "worker", runtime, "--entry", "app", "--once")
		var aOut, bOut strings.Builder
		a.Stdout, a.Stderr = &aOut, &aOut
		b.Stdout, b.Stderr = &bOut, &bOut
		if err := a.Start(); err != nil {
			t.Fatal(err)
		}
		if err := b.Start(); err != nil {
			t.Fatal(err)
		}
		if err := a.Wait(); err != nil {
			t.Fatalf("worker A: %v\n%s", err, aOut.String())
		}
		if err := b.Wait(); err != nil {
			t.Fatalf("worker B: %v\n%s", err, bOut.String())
		}
		if got := run(command(bin, "worker", runtime, "--entry", "app", "inspect")); got != "sqlite\n24\n276\n" {
			t.Fatalf("separate producer and workers: %q", got)
		}
	}
	for _, mode := range []string{"--vm-strict", "--disable-vm"} {
		for _, crash := range []bool{false, true} {
			database := filepath.Join(t.TempDir(), "signals.db")
			env = append(env, "JOBS_FIXTURE_DB="+database)
			id := strings.TrimSpace(run(command(bin, "worker", mode, "--entry", "app", "seed-crash")))
			worker := command(bin, "worker", mode, "--entry", "app", "lease-worker")
			stdout, err := worker.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var errors strings.Builder
			worker.Stderr = &errors
			if err := worker.Start(); err != nil {
				t.Fatal(err)
			}
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				ready <- scanner.Scan() && scanner.Text() == "claimed"
				for scanner.Scan() {
				}
			}()
			select {
			case ok := <-ready:
				if !ok {
					_ = worker.Process.Kill()
					_ = worker.Wait()
					t.Fatalf("worker did not claim: %s", errors.String())
				}
			case <-time.After(15 * time.Second):
				_ = worker.Process.Kill()
				_ = worker.Wait()
				t.Fatal("worker claim deadline")
			}
			signal := syscall.SIGTERM
			if crash {
				signal = syscall.SIGKILL
			}
			if err := worker.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			if !crash {
				connection, err := sql.Open("sqlite", database)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := connection.Exec("INSERT INTO control VALUES ('release')"); err != nil {
					t.Fatal(err)
				}
				if err := connection.Close(); err != nil {
					t.Fatal(err)
				}
			}
			err = worker.Wait()
			if !crash && err != nil {
				t.Fatalf("signal drain %s: %v %s", mode, err, errors.String())
			}
			if crash && err == nil {
				t.Fatal("killed worker exited successfully")
			}
			connection, err := sql.Open("sqlite", database)
			if err != nil {
				t.Fatal(err)
			}
			if crash {
				var expiry int64
				if err := connection.QueryRow("SELECT lease_until_ms FROM geblang_jobs_items WHERE id = ?", id).Scan(&expiry); err != nil {
					t.Fatal(err)
				}
				for time.Now().UnixMilli() <= expiry {
					time.Sleep(time.Millisecond)
				}
			} else {
				var status string
				if err := connection.QueryRow("SELECT status FROM geblang_jobs_items WHERE id = ?", id).Scan(&status); err != nil {
					t.Fatal(err)
				}
				if status != "completed" {
					t.Fatalf("drained job status %s", status)
				}
			}
			if err := connection.Close(); err != nil {
				t.Fatal(err)
			}
			run(command(bin, "worker", mode, "--entry", "app", "lease-worker", "--once"))
			if got := run(command(bin, "worker", mode, "--entry", "app", "inspect")); got != "sqlite\n1\n99\n" {
				t.Fatalf("recovered job %s crash=%t: %q", mode, crash, got)
			}
		}
	}

}
