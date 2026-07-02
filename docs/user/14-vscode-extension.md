# VS Code Extension

Geblang ships a VS Code extension in the `vscode-geblang/` directory of the
repository. It provides:

- **Syntax highlighting** for `.gb` files.
- **Live diagnostics** - parse and semantic errors appear as you type, via the
  built-in language server (`geblang lsp`).
- **Completion and signature help** for standard-library modules, functions,
  classes, and common language keywords.
- **Step debugging** - breakpoints, step over/into/out, call stack, and
  variable inspection, via the built-in debug adapter (`geblang dap`).

## Building The Extension

You need the `geblang` binary on your `PATH` before installing the extension.
See [Getting Started](01-getting-started.md) for build instructions.

### Option A - Docker (no Node.js required)

```sh
make vscode-build
```

This builds the extension inside a Docker container and writes the compiled
output to:

```
build/vscode/out/extension.js   compiled extension entry point
build/vscode/vsix/geblang.vsix  installable package (if vsce succeeded)
```

Install the VSIX from the terminal:

```sh
make vscode-install
# equivalent to:
code --install-extension build/vscode/vsix/geblang.vsix
```

> **WSL users:** do not use the VS Code GUI's *Install from VSIX…* dialog to
> install from a WSL path. VS Code on Windows cannot open VSIX files via
> `\\wsl.localhost\` UNC paths and will error with *"UNC host 'wsl.localhost'
> access is not allowed"*. Always install via `make vscode-install`, which
> copies the VSIX to the Windows temp directory before invoking `code`, so the
> path translation is handled automatically.

### Option B - Local Node.js

```sh
cd vscode-geblang
npm install
npm run compile
```

To also produce a VSIX package:

```sh
npx @vscode/vsce package --no-dependencies --out geblang.vsix
code --install-extension geblang.vsix
```

### Option C - Extension Development Host (no install needed)

Open the `vscode-geblang/` folder in VS Code, then press **F5**. VS Code
launches a second window with the extension already loaded. This is the fastest
path for iterating on the extension itself.

Prerequisite: run `npm install && npm run compile` first (or `make vscode-build`
to ensure `out/extension.js` exists).

## Requirements

| Requirement | Purpose |
|---|---|
| `geblang` on `PATH` | Language server and debug adapter |
| VS Code 1.80 or later | Extension host API |

If `geblang` is installed to a non-standard location, set the path in VS Code
settings (see [Settings](#settings) below).

## Features

### Syntax Highlighting

`.gb` files are highlighted automatically once the extension is active.
Keywords, types, string interpolation, operators, and comments are all
distinguished.

### Diagnostics

Open any `.gb` file and errors appear as red underlines immediately. Hover
over an underline to read the message. The Problems panel
(Ctrl+Shift+M / Cmd+Shift+M) lists all diagnostics across open files.

The language server runs `geblang lsp` as a subprocess. It re-analyses the
file on every change and sends results back to VS Code without touching the
file on disk.

### Completion And Signature Help

The language server exposes the bundled standard library to VS Code:

- Type `import ` to complete module names such as `io`, `http`, `db`, `web`,
  `collections`, `reflect`, and `websocket`.
- Type a module member access such as `io.` or `db.` to complete exported
  functions and classes.
- Type a function call such as `http.listen(` or `db.query(` to see the
  expected parameters and return shape.

Completion metadata is provided by the `geblang lsp` process, so it works with
the same executable configured for diagnostics and debugging.

### Step Debugging

#### Quick start

1. Open a `.gb` file.
2. Click in the gutter to set a breakpoint (red dot).
3. Press **F5** - VS Code picks up the default launch configuration and starts
   the debug adapter.
4. Execution pauses at the breakpoint. Use the debug toolbar or keyboard
   shortcuts to step:

| Action | Shortcut |
|---|---|
| Continue | F5 |
| Step Over | F10 |
| Step Into | F11 |
| Step Out | Shift+F11 |
| Stop | Shift+F5 |

The **Variables** panel shows all local variables at the current scope. The
**Call Stack** panel shows the active call frames.

#### launch.json

For more control, add a `.vscode/launch.json` to your workspace:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "type": "geblang",
      "request": "launch",
      "name": "Run current file",
      "program": "${file}",
      "cwd": "${workspaceFolder}",
      "stopOnEntry": false
    },
    {
      "type": "geblang",
      "request": "launch",
      "name": "Run with args",
      "program": "${workspaceFolder}/src/main.gb",
      "cwd": "${workspaceFolder}",
      "args": ["--verbose"],
      "stopOnEntry": true
    }
  ]
}
```

| Property | Type | Default | Description |
|---|---|---|---|
| `program` | string | (required) | Path to the `.gb` script to run |
| `cwd` | string | `${workspaceFolder}` | Working directory used when running and resolving local modules |
| `args` | string[] | `[]` | Arguments passed to the script |
| `stopOnEntry` | boolean | `false` | Pause at the first statement |
| `geblangPath` | string | `"geblang"` | Override the `geblang` binary path |

#### Debugging concurrent code

Breakpoints fire inside concurrent worker bodies, not just the main script:
`async.run` / `async.all` / `async.race` worker functions, lazy generator
bodies, and network request handlers.

Each running worker appears as its own entry in the **Call Stack** pane's
thread list. The main script is the first thread; workers are added as they
start and removed as they finish.

Hitting a breakpoint establishes a focused stop and asks every other evaluator
worker to park at its next statement boundary. This pause is cooperative:
workers currently inside a native or blocking operation may not park
immediately, so the adapter reports `allThreadsStopped: false`. **Continue**
(F5) resumes the focused thread and every worker that has parked. A step action
(Step Over / Into / Out) advances the selected thread in step mode and resumes
other parked threads in continue mode. This lets those threads satisfy channel,
lock, and task dependencies needed by the selected thread. They park again
when the next debugger stop is established.

The adapter also honours single-thread execution requests. When the client
issues Continue or a step with the single-thread flag set, only the selected
thread runs; the other parked workers stay suspended rather than being resumed.
The selected thread runs (or steps) until it stops again, at which point the
world is once more fully stopped. Single-thread execution is useful for
advancing one worker without letting the others make progress, but a thread
that depends on a suspended worker (for example, awaiting a channel it does not
feed itself) will not make progress until you resume the others with a normal
Continue. A normal (all-thread) Continue or step is unchanged and remains the
default.

Only the focused (stopped) thread is fully inspectable. Other worker threads
appear in the thread list but their call stack and variables are not
expandable while a different thread holds the stop.

Variable inspection and expression evaluation resolve against the selected
call frame. Selecting an outer frame in the Call Stack pane runs
evaluate/watch expressions in that frame's scope. Outer-frame inspection
covers parameters and enclosing scope; locals declared inside an outer
frame's body are not currently visible.

#### Known limitations

- The debug adapter runs scripts on the evaluator path. Scripts that require
  `--vm-strict` mode may behave differently under the debugger.
- Only the main script file has source-mapped breakpoints. Breakpoints set in
  imported modules are not yet supported.
- Outer-frame inspection covers a frame's parameters and enclosing scope;
  locals declared inside an outer frame's body are not currently visible.
  Module-level variables are visible in the outermost frame.
- Disconnect and Terminate cancel debugger evaluations at Geblang statement
  boundaries, including work spawned with `async.run`. A native call that
  blocks inside an evaluation cannot be interrupted until that call returns.

These limitations are expected to be lifted in a post-1.0.0 release.

## Settings

Open **File > Preferences > Settings** (or Cmd+, on macOS) and search for
`geblang`.

| Setting | Default | Description |
|---|---|---|
| `geblang.executablePath` | `"geblang"` | Absolute path to the `geblang` binary. Useful when `geblang` is not on the system `PATH`. |
| `geblang.executionMode` | `"auto"` | Launch mode: `auto`, `native`, or `wsl`. `auto` runs native executables directly and uses `wsl.exe` only for Linux/WSL paths on Windows. |

Example `settings.json` entry:

```json
{
  "geblang.executablePath": "/usr/local/bin/geblang",
  "geblang.executionMode": "auto"
}
```

On native Windows, point `geblang.executablePath` at `geblang.exe` or leave it
as `geblang` if the executable is on `PATH`. On Windows with WSL, set the path
to a Linux path such as `/home/me/bin/geblang` and leave
`geblang.executionMode` as `auto`, or set it explicitly to `wsl`.

## Troubleshooting

**"UNC host 'wsl.localhost' access is not allowed" when installing VSIX**
You are on Windows with WSL and used the GUI's *Install from VSIX…* dialog to
navigate to a file under `\\wsl.localhost\`. VS Code cannot install VSIXs via
UNC paths. Run `make vscode-install` from your WSL terminal instead - it copies
the VSIX to `C:\Windows\Temp\` before calling `code`, so the Windows process
gets a native path it can read.

**Syntax highlighting not working**
Confirm the extension is active: open the Extensions panel and check that
*Geblang* is listed and enabled.

**No diagnostics appearing**
Check the Output panel (Ctrl+Shift+U / Cmd+Shift+U), select **Geblang
Language Server** from the dropdown, and look for startup errors. The most
common cause is `geblang` not being found on `PATH`. Set
`geblang.executablePath` to the full binary path.

**Breakpoints not binding**
Ensure the file is saved before starting a debug session. Breakpoints in
unsaved buffers are shown as grey (unverified) and will not pause execution.

**Debug session exits immediately**
The script may have a parse error. Check the Debug Console for the error
message, fix it, and re-launch.
