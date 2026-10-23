package evaluator

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"geblang/internal/runtime"
)

func timeoutDB(t *testing.T, dsn string) (*Evaluator, runtime.Value) {
	t.Helper()
	e := New(io.Discard)
	if err := e.ensureBuiltinTypes(); err != nil {
		t.Fatal(err)
	}
	h, err := e.dbOpen(syntheticCall("db.open"), []runtime.Value{runtime.String{Value: "sqlite"}, runtime.String{Value: dsn}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Cleanup() })
	return e, h
}

func TestDBTimeoutPoolWaitAndRecovery(t *testing.T) {
	for _, op := range []string{"begin", "query", "exec"} {
		t.Run(op, func(t *testing.T) {
			e, h := timeoutDB(t, ":memory:")
			db, _ := e.dbHandle(h)
			held, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer held.Rollback()
			result := make(chan error, 1)
			go func() {
				var err error
				if op == "begin" {
					_, err = e.dbBegin(syntheticCall("begin"), []runtime.Value{h, runtime.NewInt64(30)})
				} else {
					_, err = e.dbTimedOperation(h, []runtime.Value{runtime.NewInt64(30), runtime.String{Value: "SELECT 1"}}, op == "query")
				}
				result <- err
			}()
			select {
			case err := <-result:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("want deadline exceeded, got %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("pool wait did not stop")
			}
			stats, err := e.dbStats(syntheticCall("stats"), []runtime.Value{h})
			if err != nil {
				t.Fatal(err)
			}
			count, _ := dictField(stats.(runtime.Dict), "waitCount")
			duration, _ := dictField(stats.(runtime.Dict), "waitDurationMs")
			n, _ := rawInt64(count, "waitCount")
			ms, _ := rawInt64(duration, "waitDurationMs")
			if n != 1 || ms <= 0 {
				t.Fatalf("wait stats count=%d duration=%d", n, ms)
			}
			if err := held.Rollback(); err != nil {
				t.Fatal(err)
			}
			if _, err := e.dbTimedOperation(h, []runtime.Value{runtime.NewInt64(1000), runtime.String{Value: "SELECT 1"}}, false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDBTimeoutTransactionRollsBack(t *testing.T) {
	e, h := timeoutDB(t, filepath.Join(t.TempDir(), "transaction.db"))
	db, _ := e.dbHandle(h)
	if _, err := db.Exec("CREATE TABLE values_test (n INTEGER)"); err != nil {
		t.Fatal(err)
	}
	txID, err := e.dbBegin(syntheticCall("begin"), []runtime.Value{h, runtime.NewInt64(50)})
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := e.txHandle(txID)
	if _, err := tx.tx.Exec("INSERT INTO values_test VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := e.dbCommit(syntheticCall("commit"), []runtime.Value{txID}); err == nil {
		t.Fatal("expired transaction committed")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM values_test").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expired transaction retained %d rows", count)
	}
	if _, err := e.dbTimedOperation(h, []runtime.Value{runtime.NewInt64(1000), runtime.String{Value: "SELECT 1"}}, false); err != nil {
		t.Fatal(err)
	}
}

func TestDBTimeoutSQLiteLock(t *testing.T) {
	e, h := timeoutDB(t, filepath.Join(t.TempDir(), "lock.db"))
	db, _ := e.dbHandle(h)
	if _, err := db.Exec("CREATE TABLE values_test (n INTEGER)"); err != nil {
		t.Fatal(err)
	}
	held, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	if _, err := held.Exec("INSERT INTO values_test VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = held.Rollback()
		close(released)
	}()
	started := time.Now()
	_, err = e.dbTimedOperation(h, []runtime.Value{runtime.NewInt64(30), runtime.String{Value: "INSERT INTO values_test VALUES (2)"}}, false)
	<-released
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded after lock release, got %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("cancellation took %v: %v", time.Since(started), err)
	}
}

func TestDBTimeoutStreamingRowsReleasePool(t *testing.T) {
	e, h := timeoutDB(t, ":memory:")
	rows, err := e.dbTimedOperation(h, []runtime.Value{runtime.NewInt64(30), runtime.String{Value: "SELECT 1 UNION ALL SELECT 2"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := e.dbRowsNext(rows.(*runtime.Instance)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want expired cursor, got %v", err)
	}
	if _, err := e.dbTimedOperation(h, []runtime.Value{runtime.NewInt64(1000), runtime.String{Value: "SELECT 1"}}, false); err != nil {
		t.Fatal(err)
	}
}
