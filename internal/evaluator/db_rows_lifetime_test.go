package evaluator

import (
	goruntime "runtime"
	"testing"
	"time"

	"geblang/internal/runtime"
)

func openDBRowsCount(e *Evaluator) int {
	e.dbMu.Lock()
	defer e.dbMu.Unlock()
	return len(e.dbRows)
}

func rowsQuery(t *testing.T, e *Evaluator, h runtime.Value, sql string) *runtime.Instance {
	t.Helper()
	rows, err := e.dbQueryRows(syntheticCall("db.query"), []runtime.Value{h, runtime.String{Value: sql}})
	if err != nil {
		t.Fatal(err)
	}
	return rows.(*runtime.Instance)
}

// A long-running server reads through Rows without closing them (all(),
// first()); each one used to stay in the evaluator's registry, with its row
// cache, until shutdown, so memory grew with every query.
func TestDBRowsLeaveNoRegistryEntryOnceDone(t *testing.T) {
	e, h := timeoutDB(t, ":memory:")
	const sql = "SELECT 1 AS a UNION ALL SELECT 2 UNION ALL SELECT 3"
	for i := 0; i < 50; i++ {
		rows := rowsQuery(t, e, h, sql)
		all, err := e.dbRowsAll(rows)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(all.(*runtime.List).Elements); n != 3 {
			t.Fatalf("all() returned %d rows, want 3", n)
		}
		// The cache still serves random access after exhaustion.
		if _, err := e.dbRowsGet(rows, 2); err != nil {
			t.Fatal(err)
		}
	}
	if n := openDBRowsCount(e); n != 0 {
		t.Fatalf("%d exhausted Rows still registered", n)
	}

	closed := rowsQuery(t, e, h, sql)
	if _, err := e.dbRowsNext(closed); err != nil {
		t.Fatal(err)
	}
	if _, err := e.dbRowsClose(closed); err != nil {
		t.Fatal(err)
	}
	if n := openDBRowsCount(e); n != 0 {
		t.Fatalf("%d closed Rows still registered", n)
	}
}

// Rows dropped part-way through are closed when collected, which also
// returns their connection to the pool.
func TestDBRowsAbandonedOpenAreReleased(t *testing.T) {
	e, h := timeoutDB(t, ":memory:")
	db, err := e.dbHandle(h)
	if err != nil {
		t.Fatal(err)
	}
	func() {
		rows := rowsQuery(t, e, h, "SELECT 1 UNION ALL SELECT 2")
		if _, err := e.dbRowsNext(rows); err != nil {
			t.Fatal(err)
		}
	}()
	if n := openDBRowsCount(e); n != 1 {
		t.Fatalf("open Rows registered = %d, want 1", n)
	}
	deadline := time.Now().Add(5 * time.Second)
	for openDBRowsCount(e) != 0 || db.Stats().InUse != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("abandoned Rows not released: registered=%d inUse=%d", openDBRowsCount(e), db.Stats().InUse)
		}
		goruntime.GC()
		time.Sleep(10 * time.Millisecond)
	}
}
