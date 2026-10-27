# Release Notes

## 1.38.1

### Fixes

- Returning from `finally` overrides a pending return without crashing the
  compiler or leaving extra values on the VM stack. Breaking or continuing
  an inner loop does not run an enclosing finalizer early.
- Nested named functions retain captured locals after their enclosing call
  returns, including recursive functions and captures through another lambda.
  Decorated functions retain their declared argument names, defaults and
  overload types on both runtimes. Closure capture respects block and
  comprehension scope, including a loop binding that shadows its iterable.
- Async calls made through callbacks no longer share instruction storage
  that a reused worker can rewrite concurrently. Socket callbacks do not read
  temporary execution state from their running parent VM.
- Idle VM workers release completed call arguments and stale global
  snapshots instead of keeping them alive between calls.
- List loops observe element replacements, including after a list grows,
  and stop at the original length. Appending does not extend the loop.
- Explicit `null` range starts and steps are rejected on both runtimes.
  Character ranges evaluate an explicit `by` expression once on both.
- Debugger breakpoints in imported functions and inherited methods stop
  in their defining files, and caller frames keep their own source paths.
- Editor rename respects lexical bindings and leaves plain strings,
  comments and unrelated variables unchanged. Navigation, rename,
  completion and diagnostic positions account for UTF-16 text positions.
- The language server exits on an `exit` notification without waiting for
  its input to close, and reports the current toolchain version.
- Sampling deduplicates workers running on the same goroutine. A profile
  report ending in `.folded` keeps a separate folded-stack output file.
  Starting a collection is safe while cancelled workers finish.

### Performance

- HTTP callback contexts construct builtin function tables only when they
  need them, reducing request setup allocations while preserving isolation.

## 1.38.0

### New

- The `browser` module can capture more than the visible viewport.
  `screenshot(path, opts)` takes one element (`selector`), a `clip` region or
  the `fullPage`, in `png`, `jpeg` or `webp` with a `quality`, and can make the
  background transparent. `screenshotBytes(opts)` returns the image as bytes.
  `pdf(path, opts)` takes page size, margins, orientation, scale and page
  ranges. `startScreencast(dir, opts)` and `stopScreencast()` record the page as
  numbered frames.
- Visual regression checks: `browser.compareImages(a, b, opts)` compares two
  PNG, JPEG or WebP images (paths or bytes) pixel by pixel with a per-pixel
  `threshold`, and can write a highlighted difference image.
  `page.matchScreenshot(baseline, opts)` compares a screenshot with a stored
  baseline, creating or updating it as asked.
- Viewport and device emulation: a `viewport` option on `browser.launch`, and
  `setViewport`, `setUserAgent`, `emulateMedia` (print media, dark colour
  scheme, reduced motion), `setTimezone`, `setLocale`, `setOffline`,
  `setExtraHeaders` and `emulateFocus` on pages.
- Waiting: `waitForFunction(js, opts)` waits until a JavaScript expression is
  truthy and returns its value; `waitFor` takes a `state` (`attached`,
  `visible`, `hidden`, `detached`); `goto`, `reload`, `goBack`, `goForward` and
  the new `waitForNavigation` take `waitUntil` (`load`, `domcontentloaded`,
  `networkidle`); `waitForRequest` and `waitForResponse` (with the response
  body) wait for network traffic, and `requests()` lists it. Every wait takes
  `timeoutMs` and raises `TimeoutError`.
- `consoleMessages()`, `pageErrors()` and `clearConsole()` expose what a page
  logged and threw. `onDialog` and `dialogs()` answer and record `alert`,
  `confirm` and `prompt` dialogs.
- Downloads: `setDownloadPath(dir)` and `waitForDownload()`, which names the
  file after the server's suggested name.
- Input by coordinates for canvas apps and games: `mouseMove`, `mouseDown`,
  `mouseUp`, `mouseClick`, `drag` and `wheel`, plus `keyDown`, `keyUp`, key
  combinations such as `press("Control+A")`, `hover`, `dblclick`, `focus`,
  `check`, `uncheck`, `setInputFiles`, and `button`, `clickCount` and
  `position` options on `click`.
- Element queries: `count`, `isVisible`, `isEnabled`, `texts`, `innerHTML`,
  `inputValue` and `boundingBox`.
- `browser.newContext(opts)` returns a `Context` with its own cookies and
  storage, so several users can run side by side in one browser. Its
  `viewport`, `userAgent`, `locale` and `timezone` apply to each page it opens.
- `addInitScript`, `setContent` and `bringToFront` on pages.
- `geblang run --profile <report>` (or `GEBLANG_PROFILE=<report>`, which also
  works for built binaries and servers) samples a program's Geblang call stacks
  about once a millisecond, across modules and `async` tasks, and on exit
  writes the functions and lines it spent its time in, plus folded stacks for
  flame-graph tools. Both backends support it.
- `sys.cpuCount()` returns how many CPUs the process can run code on at once,
  for sizing a pool of CPU-bound `async` tasks.
- `sys.setGcPercent(percent)` sets the garbage collector's target (Go's
  `GOGC`) and returns the previous one, so a CPU-bound batch job can trade
  memory for fewer collections.

### Changes

- `goto` and `reload` now wait for the page's `load` event by default, report
  a navigation the browser could not complete as an error, and raise
  `TimeoutError` when the page does not load in time. They previously returned
  once the document was interactive, and silently after 30 seconds.
- `waitFor` raises `TimeoutError` (a subclass of `Error`) when the selector does
  not appear in time.
- Dialogs no longer block a page: `alert`, `confirm` and `prompt` are dismissed
  unless `onDialog("accept")` was called, and `beforeunload` is accepted.
- `press` sends proper key codes for named keys and rejects unknown key names.
- Less garbage for the collector: cross-module calls reuse their worker VMs
  (they were rebuilt after every collection), a `secureRandom` session keys
  its HMAC once instead of on every draw (outputs and verification are
  unchanged), and a `for` loop over a range literal counts without building a
  range value.
- Calls into another module no longer take one process-wide lock. Calls
  that read a module's globals share a per-module read lock, and a call that
  reassigned none takes no lock on return, so CPU-bound `async` tasks calling
  into the same modules scale better across cores (about 20% more throughput
  with 12 tasks in one benchmark).

### Fixes

- `list.slice()` returns a new list. It shared the original's storage, so
  assigning to an element of the slice changed the original, and pushing onto
  it could overwrite the original's next element.
- A call to a function in another module can name a later argument and leave
  defaulted parameters before it out (`lib.run(1, workers: 4)`). The VM
  reported "missing argument" for the skipped parameter; the evaluator was
  already correct.

Older releases (1.0.0 through 1.37.8) are archived: see the
[release notes archive](https://github.com/dwgebler/geblang/blob/main/docs/user/18-release-notes-archive.md).
