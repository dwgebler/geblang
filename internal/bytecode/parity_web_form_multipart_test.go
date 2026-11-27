package bytecode_test

import "testing"

func TestWebFormMultipartFieldsParity(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import web.http as whttp;

let body = "--B\r\nContent-Disposition: form-data; name=\"title\"\r\n\r\nReport\r\n--B\r\nContent-Disposition: form-data; name=\"doc\"; filename=\"r.txt\"\r\n\r\nhello\r\n--B--\r\n";
let req = whttp.Request({"method": "POST", "path": "/", "headers": {"Content-Type": "multipart/form-data; boundary=B"}, "body": body});
io.println([req.form().keys().sorted(), req.formValue("title"), req.formValue("doc")]);
`)

	want := "[[\"title\"], \"Report\", \"\"]\n"
	if ev != want || vm != want {
		t.Fatalf("multipart form fields mismatch\nevaluator: %q\nvm:        %q\nwant:      %q", ev, vm, want)
	}
}
