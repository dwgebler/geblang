# Headless Browser (`browser`)

> Experimental (1.25.0, extended in 1.38.0). Drives a headless Chrome/Chromium
> over the Chrome DevTools Protocol for functional/E2E testing, visual
> regression checks and scripted browser control. The surface may change.

The `browser` module launches a real headless browser and lets a Geblang program
navigate, interact with the page by selector or by coordinates, wait for the page
to reach a state, read content, capture screenshots, PDFs and screencasts,
compare images, emulate devices, watch the console and the network, and isolate
users in separate contexts. It speaks the DevTools Protocol directly (no external
driver), is gated behind the `--allow-browser` launch flag (it spawns a browser
subprocess and opens sockets), and always terminates the browser on close so a
process is never orphaned.

## Setup

Install Chrome or Chromium yourself; it is not bundled. The module finds it via
`opts.executable`, then `$GEBLANG_CHROME`, then common install locations
(`google-chrome`, `chromium`, ...). Run with `--allow-browser`:

```sh
export GEBLANG_CHROME=/usr/bin/google-chrome   # or pass {"executable": "..."}
geblang --allow-browser script.gb
```

In containers Chrome usually needs `--no-sandbox`; pass it through `opts.args`.
Without `--allow-browser`, `browser.launch` raises a `PermissionError`.

## Conventions

- Optional settings go in a trailing options dict, as with `launch`.
- Coordinates (`mouseClick`, `clip`, `boundingBox`) are CSS pixels relative to
  the top-left of the viewport.
- Every wait takes `timeoutMs` (default 30000) and raises a `TimeoutError` when
  it runs out, so `catch (TimeoutError e)` handles it specifically.
- A navigation option `waitUntil` is one of `load` (the default),
  `domcontentloaded` or `networkidle` (no network activity for half a second).

## Module functions

| Function | Description |
|---|---|
| `launch(opts = {})` | Starts a browser; returns a `Browser`. |
| `compareImages(a, b, opts = {})` | Compares two images pixel by pixel. |

`launch` options: `headless` (default `true`), `executable`, `args` (list of
extra Chrome flags), `timeoutMs` (how long to wait for Chrome to start), and
`viewport` (`{"width", "height", "deviceScaleFactor", "mobile"}`) applied to
every page the browser opens, including pages in contexts that do not set their
own viewport.

`compareImages(a, b, opts)` takes two PNG, JPEG or WebP images, each a file path
or `bytes`, and returns `{"diffPixels", "ratio", "width", "height",
"sameSize"}`. A pixel differs when any channel differs by more than `threshold`
(0 to 1, default `0.1`). Images of different sizes count every pixel as
different, with `sameSize` false. `diffPath` writes a PNG with the differing
pixels in red over a faded copy of the first image.

## Browsers and contexts

| `Browser` method | Description |
|---|---|
| `newPage()` | Opens a new page in the default context; returns a `Page`. |
| `newContext(opts = {})` | Creates an isolated `Context`. |
| `pages()` | All open pages, including ones the app opened. |
| `version()` | The browser product version string. |
| `close()` | Closes the browser and terminates the process. |

A `Context` has its own cookies, storage and cache, so two signed-in users can
run side by side in one browser. Its options apply to each page it opens:
`viewport`, `userAgent`, `locale` and `timezone`.

| `Context` method | Description |
|---|---|
| `newPage()` | Opens a page in this context. |
| `pages()` | This context's open pages. |
| `cookies()` / `setCookie(cookie)` / `clearCookies()` | This context's cookies. |
| `close()` | Closes the context and every page in it. |

## Navigating and reading

```gb
import browser;
import io;

let b = browser.launch({"args": ["--no-sandbox"]});   # geblang --allow-browser
let p = b.newPage();
p.goto("https://example.com", {"waitUntil": "networkidle"});
io.println(p.title());
io.println(p.text("h1"));
io.println(p.count("a"));
p.screenshot("example.png", {"fullPage": true});
b.close();
```

| `Page` method | Description |
|---|---|
| `goto(url, opts = {})` | Navigates; opts `waitUntil`, `timeoutMs`. |
| `reload(opts = {})` | Reloads; same options. |
| `goBack(opts = {})` / `goForward(opts = {})` | Moves through history; `false` when there is nowhere to go. |
| `setContent(html)` | Replaces the document with `html`. |
| `addInitScript(script)` | Runs `script` before any page script on every later navigation. |
| `bringToFront()` | Activates the page's tab. |
| `evaluate(js)` | Runs a JavaScript expression (promises are awaited) and returns the JSON-serializable result. |
| `content()` / `title()` / `url()` | The page HTML / document title / current URL. |
| `text(selector)` / `texts(selector)` | The first match's text (or null) / every match's text. |
| `attribute(selector, name)` / `innerHTML(selector)` | An attribute / the inner HTML (or null). |
| `inputValue(selector)` | A form field's current value. |
| `count(selector)` | How many elements match. |
| `isVisible(selector)` / `isEnabled(selector)` | Whether the element is visible / not disabled. |
| `boundingBox(selector)` | `{"x", "y", "width", "height"}` in viewport coordinates, or null. |
| `close()` | Closes the page. |

`evaluate` returns the JS value marshalled to a Geblang value; a JavaScript
exception surfaces as a catchable error.

## Waiting

| `Page` method | Description |
|---|---|
| `waitFor(selector, timeoutMs or opts)` | Waits for a selector; opts `state` is `attached` (default), `visible`, `hidden` or `detached`. |
| `waitForFunction(js, opts = {})` | Waits until the expression is truthy and returns its value; opts `pollMs` (default 50). |
| `waitForNavigation(opts = {})` | Waits for a navigation (for example after a click) to reach `waitUntil`. |
| `waitForRequest(urlPattern, opts = {})` | Waits for a request whose URL matches. |
| `waitForResponse(urlPattern, opts = {})` | Waits for a matching response to finish; the result includes `body`. |
| `waitForDownload(opts = {})` | Waits for a download to finish (see Downloads). |

Calls run one after another, so a wait can come after the action that triggers
it: the event may already have happened. Each wait therefore returns the
earliest matching event that no earlier wait of the same kind has returned.
Request and response waits only consider requests made since the page last
navigated with `goto`, `reload`, `goBack` or `goForward`. URL patterns use `*` as
the only wildcard.

```gb
import browser;
import io;

let b = browser.launch({"args": ["--no-sandbox"]});
let p = b.newPage();
p.goto("https://app.example.com/");
p.click("#refresh");
let res = p.waitForResponse("*/api/items*");
io.println(res["status"]);
io.println(res["body"]);
let ready = p.waitForFunction("document.querySelectorAll('.item').length");
io.println(ready);
b.close();
```

## Network log

`requests()` returns the page's requests, oldest first (up to the last 1000),
each `{"url", "method", "resourceType", "headers", "status", "statusText",
"mimeType", "responseHeaders", "fromCache", "failed", "finished",
"durationMs"}`. `status` and `durationMs` are null until known; `failed` holds
the browser's error text for a request that failed.

## Console, errors and dialogs

| `Page` method | Description |
|---|---|
| `consoleMessages()` | Console calls: `{"type", "text", "url", "line", "column"}`. |
| `pageErrors()` | Uncaught exceptions: `{"message", "url", "line", "column"}`. |
| `clearConsole()` | Forgets the recorded console messages and page errors. |
| `onDialog(action, promptText = "")` | Answers later dialogs: `"accept"` or `"dismiss"`. |
| `dialogs()` | Dialogs the page opened: `{"type", "message", "defaultPrompt", "accepted"}`. |

Each log keeps the last 1000 entries. Dialogs never block the page: `alert`,
`confirm` and `prompt` are dismissed unless `onDialog("accept")` was called, and
`beforeunload` is always accepted so navigation and closing go ahead. A test can
fail on any page error:

```gb
import browser;
import test;

class NoErrorsTest extends test.Test {
    @test
    func homePageHasNoErrors(): void {
        let b = browser.launch({"args": ["--no-sandbox"]});
        let p = b.newPage();
        p.goto("https://app.example.com/");
        this.assertEmpty(p.pageErrors());
        b.close();
    }
}
```

## Mouse and keyboard

| `Page` method | Description |
|---|---|
| `click(selector, opts = {})` | Scrolls to and clicks with real mouse events; opts `button` (`left`, `right`, `middle`), `clickCount`, `position` (`{"x", "y"}` from the element's top-left). |
| `dblclick(selector)` / `hover(selector)` / `focus(selector)` | Double-clicks / moves over / focuses the element. |
| `check(selector)` / `uncheck(selector)` | Clicks a checkbox or radio until it has that state. |
| `type(selector, text)` | Focuses the element and types text. |
| `fill(selector, value)` | Sets a form field's value and fires input/change. |
| `select(selector, value)` | Selects a dropdown option by value. |
| `setInputFiles(selector, paths)` | Sets a file input's files (a path or a list of paths). |
| `press(key)` | Presses a key or combination: `"Enter"`, `"Shift+Tab"`, `"Control+A"`. |
| `keyDown(key)` / `keyUp(key)` | Holds / releases a key; held modifiers apply to later keys and clicks. |
| `mouseMove(x, y, opts = {})` | Moves the pointer; opts `steps` for intermediate moves. |
| `mouseDown(opts = {})` / `mouseUp(opts = {})` | Presses / releases a button at the pointer; opts `button`, `clickCount`. |
| `mouseClick(x, y, opts = {})` | Clicks at viewport coordinates; opts `button`, `clickCount`. |
| `drag(fromX, fromY, toX, toY, opts = {})` | Drags with the left button; opts `steps` (default 10). |
| `wheel(deltaX, deltaY)` | Scrolls with the mouse wheel at the pointer. |

Key names: single characters, `Enter`, `Tab`, `Escape`, `Backspace`, `Delete`,
`Insert`, `Home`, `End`, `PageUp`, `PageDown`, the `Arrow` keys, `Space`, `F1` to
`F12`, and the modifiers `Shift`, `Control`, `Alt` and `Meta`. `Control` with
`A`, `C`, `V`, `X`, `Z` or `Y` performs select all, copy, paste, cut, undo or
redo. An unknown key name is an error. Coordinate input suits canvas apps and
games, which have no elements to select:

```gb
import browser;

let b = browser.launch({"args": ["--no-sandbox"], "viewport": {"width": 1280, "height": 720}});
let p = b.newPage();
p.goto("https://game.example.com/");
p.waitForFunction("window.game && window.game.ready");
p.mouseClick(1128, 559);
p.drag(200, 300, 600, 300, {"steps": 20});
b.close();
```

## Emulation

| `Page` method | Description |
|---|---|
| `setViewport(width, height, opts = {})` | Sets the viewport; opts `deviceScaleFactor`, `mobile` (also enables touch). |
| `setUserAgent(userAgent)` | Overrides the user agent. |
| `emulateMedia(opts)` | opts `media` (`screen` or `print`), `colorScheme` (`light` or `dark`), `reducedMotion` (`reduce` or `no-preference`). |
| `setTimezone(id)` / `setLocale(locale)` | Overrides the time zone (`"Europe/Paris"`) / locale (`"de-DE"`). |
| `setOffline(bool)` | Takes the page offline or back online. |
| `setExtraHeaders(headers)` | Sends these headers with every request. |
| `emulateFocus(bool)` | Makes the page behave as focused even when it is not. |

A mobile viewport without a `<meta name="viewport">` tag lays the page out at
980 CSS pixels wide, as a real phone does.

## Screenshots, PDFs and screencasts

| `Page` method | Description |
|---|---|
| `screenshot(path, opts = {})` | Writes a screenshot to a file. |
| `screenshotBytes(opts = {})` | Returns the screenshot as `bytes`. |
| `matchScreenshot(baselinePath, opts = {})` | Compares a screenshot with a baseline (see below). |
| `pdf(path, opts = {})` | Writes the page as a PDF. |
| `startScreencast(dir, opts = {})` / `stopScreencast()` | Records frames; `stopScreencast` returns how many were written. |

Screenshot options: `selector` (one element, even outside the viewport), `clip`
(`{"x", "y", "width", "height"}`), or `fullPage` (the whole scrollable page);
use at most one. `format` is `png` (default), `jpeg` or `webp`, with `quality`
0 to 100 for the lossy formats. `omitBackground` makes the default white
background transparent in a PNG.

PDF options: `landscape`, `printBackground`, `preferCSSPageSize`, `scale`,
`paperWidth` and `paperHeight` (inches), `margins` (`{"top", "bottom", "left",
"right"}` in inches) and `pageRanges` (`"1-3, 5"`).

Screencast options: `format` (`jpeg`, the default, or `png`), `quality` and
`everyNthFrame`. Frames are written as `frame-00001.jpg` and so on, each time
the page paints.

### Visual regression

`matchScreenshot(baselinePath, opts)` takes a PNG screenshot (with the same area
options as `screenshot`) and compares it with the baseline. When the baseline
does not exist, or `update` is true, the screenshot becomes the baseline. The
result is the `compareImages` result plus `matched` and `created`. A screenshot
matches when it is the same size and `ratio` is at most `maxDiffRatio` (default
0). `threshold` sets the per-pixel tolerance; on a mismatch `diffPath` and
`actualPath` save the highlighted difference and the new screenshot.

```gb
import browser;
import test;

class LookTest extends test.Test {
    @test
    func headerLooksTheSame(): void {
        let b = browser.launch({"args": ["--no-sandbox"], "viewport": {"width": 1280, "height": 720}});
        let p = b.newPage();
        p.goto("https://app.example.com/");
        let r = p.matchScreenshot("baselines/header.png", {"selector": "header", "maxDiffRatio": 0.001, "diffPath": "out/header-diff.png"});
        this.assertTrue(r["matched"]);
        b.close();
    }
}
```

## Downloads

`setDownloadPath(dir)` saves downloads from the page's context into `dir`.
`waitForDownload(opts)` waits for the next download to finish and returns
`{"path", "suggestedName", "url"}`; the file is named after the server's
suggested name, with ` (1)`, ` (2)` and so on added when the name is taken.

## Cookies

| `Page` method | Description |
|---|---|
| `cookies()` | The page's cookies. |
| `setCookie(cookie)` | Sets a cookie (`name`, `value`, and `url` or `domain`). |
| `clearCookies()` | Clears the browser's cookies. |

## Request interception

`page.route(urlPattern, handler)` intercepts every request whose URL matches the
pattern (`*` is the wildcard). The handler receives a request dict
(`url`, `method`, `headers`, `resourceType`) and returns one of:

- `null` - let the request proceed unchanged;
- `{"abort": true}` - block the request;
- `{"status": ..., "headers": {...}, "body": "..."}` - fulfill it with a mock
  response.

```gb
import browser;

let b = browser.launch({"args": ["--no-sandbox"]});
let p = b.newPage();
p.route("*/api/*", func(dict<string, any> req): ?dict<string, any> {
    if ((req["url"] as string).contains("/api/users")) {
        return {"status": 200, "headers": {"Content-Type": "application/json"},
                "body": "[{\"id\":1,\"name\":\"Ada\"}]"};
    }
    return null;
});
p.goto("https://app.example.com/users");
b.close();
```

This makes it straightforward to stub a backend in a functional test or to block
trackers/assets while scripting a page.

## A functional test

```gb
import browser;
import test;

class LoginTest extends test.Test {
    @test
    func logsIn(): void {
        let b = browser.launch({"args": ["--no-sandbox"]});
        let p = b.newPage();
        p.goto("https://app.example.com/login");
        p.fill("#email", "ada@example.com");
        p.fill("#password", "hunter2");
        p.click("button[type=submit]");
        p.waitForNavigation();
        p.waitFor(".dashboard", {"state": "visible"});
        this.assertTrue((p.url() as string).contains("/dashboard"));
        b.close();
    }
}
```

## Building a binary

A built binary (`geblang build`) carries the capability when baked in: declare
`permissions: { browser: true }` in `geblang.yaml`, or pass
`geblang build --allow-browser`. See [Bundling](../13-bundling.md). The end user
still needs Chrome installed on the machine that runs the binary.
