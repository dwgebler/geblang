package bytecode_test

import "testing"

func TestParitySortAcceptsNullComparator(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import collections;
let xs = [3, 1, 2];
xs.sort(null);
io.println(xs);
io.println([9, 7, 8].sorted(null));
io.println([9, 7, 8].sorted(compare: null));
io.println(collections.sorted(["b", "a"], null));
any dyn = [2, 1];
io.println(dyn.sorted(null));
`)
	want := "[1, 2, 3]\n[7, 8, 9]\n[7, 8, 9]\n[\"a\", \"b\"]\n[1, 2]\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityDbBeginNullTimeout(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import db;
let conn = db.open("sqlite", ":memory:");
db.exec(conn, "create table t (n int)");
let tx = db.begin(conn, opts: {"isolation": "default"});
db.txExec(tx, "insert into t values (1)");
db.commit(tx);
let tx2 = db.begin(conn, null);
db.rollback(tx2);
let c = db.connect("sqlite", ":memory:");
let tx3 = c.begin(null, {"isolation": "serializable"});
tx3.rollback();
io.println(db.query(conn, "select count(*) as n from t")[0]["n"]);
try { db.begin(conn, "soon"); } catch (Error e) { io.println(e.message); }
`)
	want := "1\ntimeoutMs must be int\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityChiSquareEmptyExpectedIsUniform(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import stats;
let base = stats.chiSquareTest([10, 20, 30]);
io.println(stats.chiSquareTest([10, 20, 30], []) == base);
io.println(stats.chiSquareTest([10, 20, 30], opts: {"ddof": 0}) == base);
io.println(stats.chiSquareTest([10, 20, 30], [20, 20, 20]) == base);
try { stats.chiSquareTest([10, 20], [1, 2, 3]); } catch (Error e) { io.println(e.message); }
`)
	want := "true\ntrue\ntrue\nstats.chiSquareTest: expected length must match observed\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}

func TestParityProcessRunOptions(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import process;
let r = process.run("sh", ["-c", "pwd; printf %s \"$GEB_X\""], {"cwd": "/", "env": {"GEB_X": "hi", "PATH": "/usr/bin:/bin"}});
io.println(r.stdout());
io.println(process.run("pwd", opts: {"cwd": "/"}).stdout());
io.println(process.run("sleep", ["5"], {"timeoutMs": 50}).timedOut());
try { process.run("pwd", [], {"args": ["x"]}); } catch (Error e) { io.println(e.message); }
try { process.run("pwd", [], {"timeoutMs": "x"}); } catch (Error e) { io.println(e.message); }
`)
	want := "/\nhi\n/\n\ntrue\nprocess.run options.args is not allowed; pass it as an argument\nprocess.run options.timeoutMs must be int\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
