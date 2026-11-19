package bytecode_test

import "testing"

func TestWebAfterMiddlewareRunsOnUnmatchedRoutesParity(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import http;
import web.router as router;

let app = router.newRouter();
router.get(app, "/ok", func(dict<string, any> req): dict<string, any> {
    return {"status": 200, "body": "ok"};
});
router.after(app, func(Request req, Response resp): Response {
    return resp.withHeader("X-Seen", req.path);
});
for (path in ["/ok", "/missing"]) {
    let r = router.handle(app, {"method": "GET", "path": path, "headers": {}, "body": ""});
    io.println("${r["status"]} ${(r["headers"] as dict<string, any>)["X-Seen"]}");
}
`)
	want := "200 /ok\n404 /missing\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator %q, vm %q, want %q", ev, vm, want)
	}
}
