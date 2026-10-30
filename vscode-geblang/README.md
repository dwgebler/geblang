# vscode-geblang

Visual Studio Code extension for [Geblang](https://github.com/dgebler/geblang).

## Features

Completion, hover and signature help use the selected Geblang binary's LSP
catalog. Use Geblang 1.40.1 for `string.byteLength`, `cli.promptDecimal`,
`money.sum`, `json.write` and bounded `web.cache.memoryCacheStore` signatures.
The `clipromptdecimal`, `moneysum`, `jsonwrite` and `boundedcache` snippets cover
these APIs.

- Syntax highlighting for `.gb` files
- Real-time diagnostics via LSP, debounced at 200 ms:
  parse and semantic errors, bytecode type errors, unresolved imports,
  unused imports (warning), and cross-module symbol checks
  (`foo.bar()` is flagged when `bar` isn't exported by `foo`).
- **Quick-fix** for unresolved imports: when you type
  `import bytse;`, the lightbulb offers `bytes` as a replacement
  (and ranks workspace modules alongside the stdlib).
- **Go to Definition**, **Find References** (whole-file, whole-word
  matches), and **Rename Symbol** (`F2`, whole-file) for the
  identifier under the cursor.
- **Workspace Symbol Search** (`Ctrl-T` / `Cmd-T`) across every
  `.gb` file in the open folder.
- **Language Server output channel**: View -> Output ->
  "Geblang Language Server". A status-bar item shows the LSP state
  (starting / running / error) and clicks through to the channel.
- Step debugging via DAP: breakpoints, step over/into/out, variable inspection
- **Test Explorer**: `@test`-decorated methods inside `class X extends test.Test`
  appear in the Testing view; run individual tests or whole classes with the
  inline play button. Failures surface the error message inline.
- **Code lenses**: `Run` / `Debug` lenses appear above `func main()` and
  every test class declaration so you can launch a script without opening
  the command palette.
- **Format Document**: pipes the current buffer through `geblang fmt --stdin`.
  Add `"editor.formatOnSave": true` (optionally scoped to
  `"[geblang]"`) in your settings to format every save through the
  same path. No extra extension setting required.
- **Commands** (from the palette): Run Current File, Open REPL, Run Doctor,
  Build Project, Clean Bytecode Cache, Select Geblang Executable.
- Snippets for common scaffolding: `func`, `asyncfunc`, `genfunc`,
  `genericfunc`, `genericinterface`, `genericlambda`, `class`, `classex`,
  `interface`, `enum`, `match`, `try`, `trycatch`, `forin`, `for`, `while`,
  `testclass`, `test`, `immutable`, `defer`, `import`, `export`, `module`,
  `init`, `functools`, `asyncrate`, `stopwatch`, `color`. New 1.0 snippets: `asyncall`,
  `asyncrace`, `asynctimeout`, `asynccancel`, `aesencrypt`,
  `aesdecrypt`, `chacha20encrypt`, `chacha20decrypt`, `rematch`,
  `rematchall`, `base32`, `base58`, `httpsession`, `httpproxy`,
  `schedtimer`, `schedticker`.
- Workspace tasks template at `vscode-geblang/templates/tasks.json` that
  wires `geblang check`, `geblang test`, and `geblang run` into VS Code's
  Problems panel and Tasks: Run Task menu - copy it into your workspace's
  `.vscode/tasks.json` to enable

## Requirements

The `geblang` binary must be on your `PATH`, or you can set the full path in settings.

## Setup

### Install dependencies and compile

```sh
cd vscode-geblang
npm install
npm run compile
```

### Install in VS Code (development)

Open the `vscode-geblang` folder in VS Code and press **F5** to launch a new Extension Development Host window.

### Install from VSIX

```sh
npm install -g @vscode/vsce
vsce package
code --install-extension vscode-geblang-1.2.0.vsix
```

## Debugging a Geblang Script

Add a `launch.json` to your workspace's `.vscode/` directory:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "type": "geblang",
      "request": "launch",
      "name": "Run Script",
      "program": "${file}",
      "stopOnEntry": false
    }
  ]
}
```

Then open a `.gb` file, set breakpoints, and press **F5**.

## Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `geblang.executablePath` | `"geblang"` | Path to the `geblang` binary |

## Architecture

- **LSP**: `geblang lsp` runs as a language server subprocess. On open/change it runs the parser and semantic analyzer and pushes errors as diagnostics.
- **DAP**: `geblang dap` runs as a debug adapter subprocess. It executes scripts through the evaluator with a step-mode debug hook, implementing the Debug Adapter Protocol over stdin/stdout.
