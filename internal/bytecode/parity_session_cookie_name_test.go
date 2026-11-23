package bytecode_test

import (
	"path/filepath"
	"strconv"
	"testing"
)

func TestParitySessionStoreCookieName(t *testing.T) {
	dir := strconv.Quote(filepath.Join(t.TempDir(), "s"))
	ev, vm := parityOutputsStdlib(t, `import io;
import web.session as session;
let store = session.fileSessionStore(`+dir+`, 3600, "app_sid");
let saved = store.save({}, {"user": "ada"}, {});
let cookie = saved["headers"]["Set-Cookie"] as string;
let id = cookie.substring(8, cookie.indexOf(";"));
io.println(cookie.startsWith("app_sid="));
io.println(store.load({"headers": {"Cookie": "app_sid=" + id}})["user"]);
io.println(store.load({"headers": {"Cookie": "geb_sid=" + id}}));
io.println(session.fileSessionStore(`+dir+`, 3600).cookieName);
`)
	if ev != vm {
		t.Fatalf("output mismatch:\n  evaluator: %q\n  vm:        %q", ev, vm)
	}
	if want := "true\nada\n{}\ngeb_sid\n"; ev != want {
		t.Fatalf("got %q, want %q", ev, want)
	}
}
