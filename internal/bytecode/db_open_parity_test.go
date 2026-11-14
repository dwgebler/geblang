package bytecode_test

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestParitySqliteConcurrentFirstOpensSucceed(t *testing.T) {
	source := fmt.Sprintf(`import io;
import db;
import async;
string file = %q;
list<Task<any>> opens = [];
for (i in 0..<8) {
    let n = i;
    opens.push(async.run(func(): string {
        let conn = db.Connection("sqlite", file + (n %% 2 as string));
        defer conn.close();
        return conn.query("PRAGMA journal_mode").toList()[0]["journal_mode"] as string;
    }));
}
int ok = 0;
for (open in opens) { if (async.await(open) == "wal") { ok += 1; } }
io.println(ok);
`, filepath.Join(t.TempDir(), "fresh.sqlite"))
	ev, vm := parityOutputsStdlib(t, source)
	want := "8\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
