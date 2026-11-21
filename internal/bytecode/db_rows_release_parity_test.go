package bytecode_test

import "testing"

func TestParityDBRowsRandomAccessReleasesConnection(t *testing.T) {
	source := `import io;
import db;
import async;

class Repo {
    db.Connection conn;
    func Repo(db.Connection conn) { this.conn = conn; }
    func name(string id): ?string {
        let row = this.conn.queryOne("SELECT name FROM t WHERE id = ?", id);
        return row == null ? null : row["name"] as string;
    }
}

let conn = db.connect("sqlite", ":memory:");
conn.exec("CREATE TABLE t (id TEXT, name TEXT)");
conn.exec("INSERT INTO t VALUES ('a', 'x'), ('b', 'y'), ('c', 'z')");

let rows = conn.query("SELECT * FROM t ORDER BY id");
io.println(rows.first()["id"]);
io.println(rows.row()["id"]);
io.println(rows.get(2)["id"]);
io.println(rows.get(5) == null);
conn.exec("UPDATE t SET name = 'w' WHERE id = 'a'");

io.println(conn.query("SELECT * FROM t WHERE id = 'none'").isEmpty());
io.println(conn.query("SELECT * FROM t").isEmpty());
conn.exec("UPDATE t SET name = 'v' WHERE id = 'b'");

io.println(conn.queryOne("SELECT name FROM t WHERE id = ?", "a")["name"]);
io.println(conn.queryOne("SELECT name FROM t WHERE id = ?", "none") == null);
conn.exec("UPDATE t SET name = 'u' WHERE id = 'c'");

let one = conn.queryOne;
io.println(one("SELECT name FROM t WHERE id = 'b'")["name"]);
io.println(Repo(conn).name("c"));
io.println(async.await(async.run(func(): any { return conn.queryOne("SELECT count(*) AS n FROM t")["n"]; })));

let stream = conn.query("SELECT id FROM t ORDER BY id");
stream.next();
io.println(stream.row()["id"]);
stream.next();
io.println(stream.row()["id"]);
stream.close();
conn.exec("DELETE FROM t");
io.println(conn.queryOne("SELECT count(*) AS n FROM t")["n"]);
`
	ev, vm := parityOutputsStdlib(t, source)
	want := "a\na\nc\ntrue\ntrue\nfalse\nw\ntrue\nv\nu\n3\na\nb\n0\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
