package bytecode_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/cdp"
	"geblang/internal/native"
)

// TestParityBrowserLive drives the extended browser surface on both backends against a real headless Chrome.
func TestParityBrowserLive(t *testing.T) {
	if cdp.FindChrome() == "" {
		t.Skip("no Chrome available (set GEBLANG_CHROME)")
	}
	native.SetBrowserEnabled(true)
	defer native.SetBrowserEnabled(false)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "up.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `import browser;
import io;
let b = browser.launch({"args": ["--no-sandbox"], "viewport": {"width": 400, "height": 300}});
let ctx = b.newContext({"timezone": "UTC"});
let p = ctx.newPage();
p.setContent("<body style='margin:0'><input id='t' value='abc'><input id='c' type='checkbox'><input id='f' type='file'><div id='d' style='width:40px;height:20px;background:blue'></div><span class='s'>a</span><span class='s'>b</span></body>");
p.evaluate("window.keys = []; document.addEventListener('keydown', e => keys.push(e.key)); console.log('ready'); null");
p.focus("#t");
p.press("End");
p.press("Shift+ArrowLeft");
p.press("Backspace");
p.check("#c");
p.setInputFiles("#f", "DIR/up.txt");
p.mouseClick(5, 5);
io.println(p.inputValue("#t") as string);
io.println(p.evaluate("keys.join(',')") as string);
io.println(p.evaluate("document.getElementById('c').checked") as string);
io.println(p.evaluate("document.getElementById('f').files[0].name") as string);
io.println(p.count(".s") as string);
io.println(p.texts(".s") as string);
io.println(p.isVisible("#d") as string);
let box = p.boundingBox("#d") as dict<string, any>;
io.println((box["width"] as string) + "x" + (box["height"] as string));
io.println(p.waitForFunction("innerWidth") as string);
io.println((p.consoleMessages()[0] as dict<string, any>)["text"] as string);
io.println(p.evaluate("Intl.DateTimeFormat().resolvedOptions().timeZone") as string);
p.screenshot("DIR/d.png", {"selector": "#d"});
let r = browser.compareImages("DIR/d.png", p.screenshotBytes({"selector": "#d"}));
io.println((r["diffPixels"] as string) + " " + (r["sameSize"] as string));
let m = p.matchScreenshot("DIR/base.png", {"selector": "#d", "update": true});
io.println((m["created"] as string) + " " + (m["matched"] as string));
try {
    p.waitFor("#none", {"state": "visible", "timeoutMs": 100});
} catch (TimeoutError e) {
    io.println("timeout");
}
ctx.close();
b.close();
`
	src = strings.ReplaceAll(src, "DIR", dir)
	runParityStateful(t, src, "ab\nEnd,Shift,ArrowLeft,Backspace\ntrue\nup.txt\n2\n[\"a\", \"b\"]\ntrue\n40x20\n400\nready\nUTC\n0 true\ntrue true\ntimeout\n")
}
