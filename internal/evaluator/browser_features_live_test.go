package evaluator_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"geblang/internal/cdp"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/native"
	"geblang/internal/parser"
)

// runLiveBrowser evaluates a Geblang program with the browser gate open, skipping without Chrome.
func runLiveBrowser(t *testing.T, prog string) string {
	t.Helper()
	if cdp.FindChrome() == "" {
		t.Skip("no Chrome available (set GEBLANG_CHROME)")
	}
	native.SetBrowserEnabled(true)
	defer native.SetBrowserEnabled(false)
	pr := parser.New(lexer.New(prog))
	program := pr.ParseProgram()
	if len(pr.Errors()) != 0 {
		t.Fatalf("parse: %v", pr.Errors())
	}
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(program); err != nil {
		t.Fatalf("eval: %v\n%s", err, out.String())
	}
	return out.String()
}

func expectLines(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w+"\n") {
			t.Errorf("missing %q in output:\n%s", w, got)
		}
	}
}

// fixtureServer serves the pages the live tests drive.
func fixtureServer(t *testing.T) (*httptest.Server, *sync.Map) {
	seen := &sync.Map{}
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		seen.Store("x-test", r.Header.Get("X-Test"))
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<title>fixture</title><a id="next" href="/second">next</a><a id="dl" href="/file">file</a><div id="box" style="width:50px;height:40px;background:red"></div>`)
	})
	mux.HandleFunc("/second", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<title>second</title><script>fetch('/api/data').then(r => r.text()).then(t => { document.title = 'got ' + t; });</script>`)
	})
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"n":7}`)
	})
	mux.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="report.txt"`)
		fmt.Fprint(w, "downloaded body")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, seen
}

const launchLine = `let b = browser.launch({"args": ["--no-sandbox"]});
`

func TestBrowserCaptureLive(t *testing.T) {
	dir := t.TempDir()
	prog := `import browser;
import io;
` + launchLine + `let p = b.newPage();
p.setViewport(400, 300);
p.setContent("<body style='margin:0;background:white'><div id='box' style='margin:10px;width:60px;height:30px;background:blue'></div><div style='height:900px'></div></body>");
p.screenshot("DIR/el.png", {"selector": "#box"});
p.screenshot("DIR/clip.jpg", {"clip": {"x": 0, "y": 0, "width": 100, "height": 50}, "format": "jpeg", "quality": 50});
p.screenshot("DIR/full.png", {"fullPage": true});
let raw = p.screenshotBytes({"format": "webp"});
io.println("bytes=" + ((raw.length() > 0) as string));
p.pdf("DIR/doc.pdf", {"landscape": true, "printBackground": true, "margins": {"top": 0.5}});
try {
    p.screenshot("DIR/x.png", {"fullPage": true, "selector": "#box"});
} catch (Error e) {
    io.println("conflict=" + ((e.message as string).contains("only one") as string));
}
try {
    p.screenshot("DIR/x.png", {"format": "gif"});
} catch (Error e) {
    io.println("format=" + ((e.message as string).contains("png, jpeg or webp") as string));
}
let cmp = browser.compareImages("DIR/el.png", "DIR/el.png");
io.println("same=" + (cmp["diffPixels"] as string) + " size=" + (cmp["width"] as string) + "x" + (cmp["height"] as string));
let other = browser.compareImages("DIR/el.png", "DIR/full.png");
io.println("sameSize=" + (other["sameSize"] as string));
b.close();
`
	got := runLiveBrowser(t, strings.ReplaceAll(prog, "DIR", dir))
	expectLines(t, got, "bytes=true", "conflict=true", "format=true", "same=0 size=60x30", "sameSize=false")
	for name, magic := range map[string]string{"el.png": "\x89PNG", "clip.jpg": "\xff\xd8", "full.png": "\x89PNG", "doc.pdf": "%PDF"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !strings.HasPrefix(string(data), magic) {
			t.Errorf("%s: not written as expected (err %v)", name, err)
		}
	}
}

func TestBrowserMatchScreenshotLive(t *testing.T) {
	dir := t.TempDir()
	prog := `import browser;
import io;
` + launchLine + `let p = b.newPage();
p.setViewport(200, 100);
p.setContent("<body style='margin:0;background:white'><div id='s' style='width:80px;height:40px;background:green'></div></body>");
let first = p.matchScreenshot("DIR/base/shot.png", {"selector": "#s"});
io.println("created=" + (first["created"] as string) + " matched=" + (first["matched"] as string));
let again = p.matchScreenshot("DIR/base/shot.png", {"selector": "#s"});
io.println("again created=" + (again["created"] as string) + " matched=" + (again["matched"] as string) + " diff=" + (again["diffPixels"] as string));
p.evaluate("document.getElementById('s').style.background = 'red'");
let changed = p.matchScreenshot("DIR/base/shot.png", {"selector": "#s", "diffPath": "DIR/diff.png", "actualPath": "DIR/actual.png"});
io.println("changed matched=" + (changed["matched"] as string) + " ratio=" + (changed["ratio"] as string));
let tolerated = p.matchScreenshot("DIR/base/shot.png", {"selector": "#s", "maxDiffRatio": 1.0});
io.println("tolerated=" + (tolerated["matched"] as string));
let updated = p.matchScreenshot("DIR/base/shot.png", {"selector": "#s", "update": true});
io.println("updated created=" + (updated["created"] as string));
io.println("now=" + (p.matchScreenshot("DIR/base/shot.png", {"selector": "#s"})["matched"] as string));
b.close();
`
	got := runLiveBrowser(t, strings.ReplaceAll(prog, "DIR", dir))
	expectLines(t, got, "created=true matched=true", "again created=false matched=true diff=0",
		"changed matched=false ratio=1", "tolerated=true", "updated created=true", "now=true")
	for _, name := range []string{"diff.png", "actual.png"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not written: %v", name, err)
		}
	}
}

func TestBrowserEmulationLive(t *testing.T) {
	srv, seen := fixtureServer(t)
	prog := `import browser;
import io;
let b = browser.launch({"args": ["--no-sandbox"], "viewport": {"width": 500, "height": 400}});
let p = b.newPage();
p.goto("about:blank");
io.println("launchViewport=" + (p.evaluate("innerWidth + 'x' + innerHeight") as string));
p.setViewport(320, 640, {"deviceScaleFactor": 2});
io.println("viewport=" + (p.evaluate("innerWidth + 'x' + innerHeight + '@' + devicePixelRatio") as string));
p.setViewport(320, 640, {"mobile": true});
io.println("touch=" + (p.evaluate("navigator.maxTouchPoints > 0") as string));
p.setUserAgent("GeblangBot/1.0");
io.println("ua=" + (p.evaluate("navigator.userAgent") as string));
p.emulateMedia({"colorScheme": "dark", "reducedMotion": "reduce", "media": "print"});
io.println("dark=" + (p.evaluate("matchMedia('(prefers-color-scheme: dark)').matches") as string));
io.println("reduced=" + (p.evaluate("matchMedia('(prefers-reduced-motion: reduce)').matches") as string));
io.println("print=" + (p.evaluate("matchMedia('print').matches") as string));
p.setTimezone("Asia/Tokyo");
io.println("tz=" + (p.evaluate("Intl.DateTimeFormat().resolvedOptions().timeZone") as string));
p.setLocale("de-DE");
io.println("locale=" + (p.evaluate("Intl.DateTimeFormat().resolvedOptions().locale") as string));
p.emulateFocus(true);
io.println("focus=" + (p.evaluate("document.hasFocus()") as string));
p.setExtraHeaders({"X-Test": "yes"});
p.goto("SRV/page");
p.setOffline(true);
io.println("offline=" + (p.evaluate("fetch('SRV/api/data').then(() => 'ok', () => 'failed')") as string));
p.setOffline(false);
try {
    p.emulateMedia({"media": "tv"});
} catch (Error e) {
    io.println("badMedia=" + ((e.message as string).contains("screen or print") as string));
}
b.close();
`
	got := runLiveBrowser(t, strings.ReplaceAll(prog, "SRV", srv.URL))
	expectLines(t, got, "launchViewport=500x400", "viewport=320x640@2", "touch=true", "ua=GeblangBot/1.0", "dark=true", "reduced=true",
		"print=true", "tz=Asia/Tokyo", "locale=de-DE", "focus=true", "offline=failed", "badMedia=true")
	if v, _ := seen.Load("x-test"); v != "yes" {
		t.Errorf("extra header not sent: %v", v)
	}
}

func TestBrowserWaitsAndNetworkLive(t *testing.T) {
	srv, _ := fixtureServer(t)
	prog := `import browser;
import io;
` + launchLine + `let p = b.newPage();
p.goto("SRV/page", {"waitUntil": "networkidle"});
io.println("title=" + (p.title() as string));
p.click("#next");
p.waitForNavigation();
let res = p.waitForResponse("*/api/data");
io.println("status=" + (res["status"] as string) + " body=" + (res["body"] as string) + " type=" + (res["mimeType"] as string));
let v = p.waitForFunction("document.title.startsWith('got') && document.title");
io.println("waited=" + (v as string));
let req = p.waitForRequest("*/second");
io.println("request=" + (req["method"] as string));
bool listed = false;
for (r in p.requests()) {
    if ((r["url"] as string).endsWith("/api/data") && r["finished"] as bool) {
        listed = true;
    }
}
io.println("listed=" + (listed as string));
p.evaluate("setTimeout(() => { const d = document.createElement('div'); d.id = 'late'; document.body.appendChild(d); }, 150); null");
p.waitFor("#late");
p.evaluate("document.getElementById('late').style.display = 'none'; null");
p.waitFor("#late", {"state": "hidden"});
p.evaluate("setTimeout(() => document.getElementById('late').remove(), 100); null");
p.waitFor("#late", {"state": "detached", "timeoutMs": 2000});
io.println("states=ok");
try {
    p.waitFor("#never", 200);
} catch (TimeoutError e) {
    io.println("timeout=TimeoutError");
}
try {
    p.waitForFunction("false", {"timeoutMs": 150, "pollMs": 20});
} catch (TimeoutError e) {
    io.println("fnTimeout=TimeoutError");
}
try {
    p.waitForResponse("*/nothing", {"timeoutMs": 150});
} catch (TimeoutError e) {
    io.println("netTimeout=TimeoutError");
}
try {
    p.goto("SRV/page", {"waitUntil": "never"});
} catch (Error e) {
    io.println("badWait=" + ((e.message as string).contains("waitUntil") as string));
}
b.close();
`
	got := runLiveBrowser(t, strings.ReplaceAll(prog, "SRV", srv.URL))
	expectLines(t, got, "title=fixture", `status=200 body={"n":7} type=application/json`, "waited=got {\"n\":7}",
		"request=GET", "listed=true", "states=ok", "timeout=TimeoutError", "fnTimeout=TimeoutError", "netTimeout=TimeoutError", "badWait=true")
}

func TestBrowserConsoleDialogsDownloadsLive(t *testing.T) {
	srv, _ := fixtureServer(t)
	dir := t.TempDir()
	prog := `import browser;
import io;
` + launchLine + `let p = b.newPage();
p.goto("SRV/page");
p.evaluate("console.log('hello', 42); console.warn('careful'); setTimeout(() => { throw new Error('boom'); }, 0); null");
p.waitForFunction("new Promise(r => setTimeout(() => r(true), 100))");
for (m in p.consoleMessages()) {
    io.println("console " + (m["type"] as string) + ": " + (m["text"] as string));
}
let errs = p.pageErrors();
io.println("error=" + ((errs[0]["message"] as string).contains("boom") as string));
p.clearConsole();
io.println("cleared=" + (p.consoleMessages().length() as string) + "," + (p.pageErrors().length() as string));
io.println("confirm=" + (p.evaluate("confirm('sure?')") as string));
p.onDialog("accept", "Ada");
io.println("prompt=" + (p.evaluate("prompt('name?')") as string));
let ds = p.dialogs();
io.println("dialogs=" + (ds.length() as string) + " first=" + (ds[0]["message"] as string) + " accepted=" + (ds[1]["accepted"] as string));
p.setDownloadPath("DIR");
p.click("#dl");
let d = p.waitForDownload();
io.println("download=" + (d["suggestedName"] as string) + " named=" + ((d["path"] as string).endsWith("report.txt") as string));
p.click("#dl");
let d2 = p.waitForDownload();
io.println("second=" + ((d2["path"] as string).endsWith("report (1).txt") as string));
b.close();
`
	got := runLiveBrowser(t, strings.ReplaceAll(strings.ReplaceAll(prog, "SRV", srv.URL), "DIR", dir))
	expectLines(t, got, "console log: hello 42", "console warning: careful", "error=true", "cleared=0,0",
		"confirm=false", "prompt=Ada", "dialogs=2 first=sure? accepted=true", "download=report.txt named=true", "second=true")
	data, err := os.ReadFile(filepath.Join(dir, "report.txt"))
	if err != nil || string(data) != "downloaded body" {
		t.Errorf("download content: %q %v", data, err)
	}
}

func TestBrowserInputLive(t *testing.T) {
	dir := t.TempDir()
	upload := filepath.Join(dir, "upload.txt")
	if err := os.WriteFile(upload, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	prog := `import browser;
import io;
` + launchLine + `let p = b.newPage();
p.setViewport(600, 400);
p.setContent("""
<body style='margin:0'>
<div id='pad' style='width:300px;height:200px;background:#eee'></div>
<input id='t' value='hello'><input id='c' type='checkbox'><input id='f' type='file'>
<button id='b' disabled>no</button><span class='i'>one</span><span class='i'>two</span>
<script>
window.log = [];
const pad = document.getElementById('pad');
for (const t of ['mousedown', 'mouseup', 'click', 'dblclick', 'mouseover', 'contextmenu']) pad.addEventListener(t, e => log.push(t + '@' + e.offsetX + ',' + e.offsetY + ':' + e.button));
pad.addEventListener('wheel', e => log.push('wheel ' + e.deltaY));
document.addEventListener('keydown', e => log.push('key ' + e.key + (e.shiftKey ? '+shift' : '')));
</script>
</body>
""");
p.mouseClick(10, 20);
p.mouseClick(30, 40, {"button": "right"});
p.drag(5, 5, 100, 50, {"steps": 4});
p.wheel(0, 120);
p.dblclick("#pad");
p.hover("#pad");
io.println("log=" + (p.evaluate("log.filter(x => !x.startsWith('mouseover')).join('|')") as string));
p.evaluate("log = []; null");
p.focus("#t");
p.press("Control+A");
p.press("Backspace");
p.press("Shift+Tab");
p.keyDown("Shift");
p.press("q");
p.keyUp("Shift");
io.println("text=[" + (p.inputValue("#t") as string) + "] keys=" + (p.evaluate("log.join('|')") as string));
p.check("#c");
io.println("checked=" + (p.evaluate("document.getElementById('c').checked") as string));
p.check("#c");
p.uncheck("#c");
io.println("unchecked=" + (p.evaluate("!document.getElementById('c').checked") as string));
p.setInputFiles("#f", "UPLOAD");
io.println("file=" + (p.evaluate("document.getElementById('f').files[0].name") as string));
io.println("count=" + (p.count(".i") as string) + " texts=" + (p.texts(".i") as string));
io.println("visible=" + (p.isVisible("#pad") as string) + "," + (p.isVisible("#missing") as string));
io.println("enabled=" + (p.isEnabled("#b") as string) + "," + (p.isEnabled("#t") as string));
let box = p.boundingBox("#pad");
io.println("box=" + (box["width"] as string) + "x" + (box["height"] as string) + " missing=" + ((p.boundingBox("#nope") == null) as string));
io.println("html=" + (p.innerHTML(".i") as string));
try {
    p.press("NoSuchKey");
} catch (Error e) {
    io.println("badKey=" + ((e.message as string).contains("unknown key") as string));
}
try {
    p.mouseClick(1, 1, {"button": "side"});
} catch (Error e) {
    io.println("badButton=" + ((e.message as string).contains("left, right or middle") as string));
}
b.close();
`
	got := runLiveBrowser(t, strings.ReplaceAll(prog, "UPLOAD", upload))
	expectLines(t, got,
		"log=mousedown@10,20:0|mouseup@10,20:0|click@10,20:0|mousedown@30,40:2|contextmenu@30,40:2|mouseup@30,40:2|mousedown@5,5:0|mouseup@100,50:0|click@100,50:0|wheel 120|mousedown@150,100:0|mouseup@150,100:0|click@150,100:0|mousedown@150,100:0|mouseup@150,100:0|click@150,100:0|dblclick@150,100:0",
		"text=[] keys=key Control|key A|key Backspace|key Shift+shift|key Tab+shift|key Shift+shift|key q+shift",
		"checked=true", "unchecked=true", "file=upload.txt", `count=2 texts=["one", "two"]`, "visible=true,false",
		"enabled=false,true", "box=300x200 missing=true", "html=one", "badKey=true", "badButton=true")
}

func TestBrowserContextsAndScriptingLive(t *testing.T) {
	srv, _ := fixtureServer(t)
	prog := `import browser;
import io;
` + launchLine + `let alice = b.newContext({"viewport": {"width": 360, "height": 640}, "userAgent": "Alice", "timezone": "Europe/Paris"});
let bob = b.newContext();
let a = alice.newPage();
let c = bob.newPage();
a.goto("SRV/page");
c.goto("SRV/page");
alice.setCookie({"name": "who", "value": "alice", "url": "SRV"});
io.println("alice=" + (a.evaluate("document.cookie") as string) + " bob=[" + (c.evaluate("document.cookie") as string) + "]");
io.println("aliceCookies=" + (alice.cookies().length() as string) + " bobCookies=" + (bob.cookies().length() as string));
io.println("setup=" + (a.evaluate("innerWidth + ' ' + navigator.userAgent + ' ' + Intl.DateTimeFormat().resolvedOptions().timeZone") as string));
io.println("pages=" + (alice.pages().length() as string));
alice.clearCookies();
io.println("clearedAlice=" + (alice.cookies().length() as string));
alice.close();
try {
    a.title();
} catch (Error e) {
    io.println("closedPage=" + ((e.message as string).contains("closed") as string));
}
let p = b.newPage();
p.addInitScript("window.early = typeof document.body;");
p.goto("SRV/page");
io.println("init=" + (p.evaluate("String(window.early)") as string));
p.goto("SRV/second");
io.println("back=" + (p.goBack() as string) + " title=" + (p.title() as string));
io.println("forward=" + (p.goForward() as string) + " url=" + ((p.url() as string).endsWith("/second") as string));
io.println("noForward=" + (p.goForward() as string));
p.setContent("<h1>set</h1>");
io.println("content=" + (p.text("h1") as string));
p.bringToFront();
b.close();
`
	got := runLiveBrowser(t, strings.ReplaceAll(prog, "SRV", srv.URL))
	expectLines(t, got, "alice=who=alice bob=[]", "aliceCookies=1 bobCookies=0", "setup=360 Alice Europe/Paris", "pages=1",
		"clearedAlice=0", "closedPage=true", "init=object", "back=true title=fixture", "forward=true url=true", "noForward=false", "content=set")
}

func TestBrowserScreencastLive(t *testing.T) {
	dir := t.TempDir()
	prog := `import browser;
import io;
` + launchLine + `let p = b.newPage();
p.setViewport(200, 100);
p.setContent("<div id='d' style='width:50px;height:50px;background:red'></div>");
p.startScreencast("DIR/frames", {"format": "png", "everyNthFrame": 1});
p.waitForFunction("new Promise(r => { let n = 0; const tick = () => { document.getElementById('d').style.marginLeft = (n * 5) + 'px'; if (++n < 20) requestAnimationFrame(tick); else r(true); }; tick(); })");
let frames = p.stopScreencast();
io.println("frames>0=" + ((frames > 0) as string));
b.close();
`
	got := runLiveBrowser(t, strings.ReplaceAll(prog, "DIR", dir))
	expectLines(t, got, "frames>0=true")
	matches, _ := filepath.Glob(filepath.Join(dir, "frames", "frame-*.png"))
	if len(matches) == 0 {
		t.Errorf("no frames written")
	}
}
