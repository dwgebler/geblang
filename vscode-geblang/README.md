# Geblang for Visual Studio Code

Extension version: 1.7.0.

Language support for [Geblang](https://geblang.com), a typed scripting
language with a bytecode VM, a batteries-included standard library and
single-file release builds.

The extension drives the `geblang` toolchain you already have installed: the
language server (`geblang lsp`), the formatter (`geblang fmt`), the test runner
(`geblang test`) and the debug adapter (`geblang dap`). Editor features always
match the Geblang version on your machine.

## Features

- **Syntax highlighting** for `.gb` files.
- **Diagnostics as you type**: parse and type errors, unresolved imports,
  unused imports, and calls to names a module does not export.
- **Completion, hover and signature help** for your code, your workspace
  modules and the whole standard library.
- **Navigation**: Go to Definition, Find References, Rename Symbol (`F2`),
  document outline and workspace symbol search (`Ctrl+T` / `Cmd+T`).
- **Quick fixes**: a mistyped import (`import bytse;`) offers the closest
  stdlib or workspace module.
- **Formatting**: Format Document runs `geblang fmt`. Enable
  `"editor.formatOnSave"` to format every save.
- **Test Explorer**: `@test` methods in classes extending `test.Test` appear in
  the Testing view, with per-test and per-class run buttons and inline failure
  messages.
- **Code lenses**: Run and Debug above `func main()`, and Run all tests above
  every test class.
- **Debugging**: breakpoints, step over / into / out, call stacks and variable
  inspection.
- **Workspaces**: imports in multi-package workspaces resolve through the same
  module resolver as `geblang run`.
- **Snippets** for common declarations (`func`, `class`, `interface`, `enum`,
  `match`, `trycatch`, `testclass`, ...) and stdlib idioms (`asyncall`,
  `httpsession`, `aesencrypt`, `moneysum`, `securerandomints`, `imagedraw`,
  `barcodeqr`, `jobs-worker`, ...).
- **WSL**: on Windows, a Linux `geblang` inside WSL works for both local and
  `\\wsl.localhost\...` workspaces.

## Gebweb

For projects using the [Gebweb](https://geblang.com/web-development) web
framework (a `geblang.yaml` that depends on `gebweb`):

- **Template highlighting**: `.html` files under the project's `templates/`
  folder open as **Gebweb Template**, with HTML highlighting plus
  `{{ value|filter }}` output, `{% if %}` / `{% for %}` / `{% block %}` /
  `{% extends %}` tags and `{# comments #}`, including inside attribute values
  and `<script>` blocks. Other `.html` files, and projects that don't use
  Gebweb, are left alone. Change the folders with `geblang.gebweb.templateDirs`.
- **Snippets**: `gwcontroller`, `gwget`, `gwpost`, `gwview`, `gwdto`,
  `gwresource`, `gwapp` and `gwtest` in `.gb` files; `for`, `if`, `ifelse`,
  `block`, `extends`, `include`, `set`, `out` and `comment` in templates.
- **Commands** (category **Gebweb**): New Project, Start Dev Server, Show
  Routes, Generate (controller, DTO, repository, resource or OpenAPI client),
  Migrate (up, down, status, create), Build Release Binary and Start Worker.
  Each runs the `gebweb` CLI in a terminal at the project root.

## Requirements

Install Geblang 1.41 or later and make sure `geblang` is on your `PATH`, or
point `geblang.executablePath` at it. Download builds from
[geblang.com/downloads](https://geblang.com/downloads) and see
[Getting started](https://geblang.com/getting-started) for setup.

Check the toolchain the extension found with **Geblang: Run Doctor**.

Use Geblang 1.42.0 or later for image drawing, text and fonts, barcode generation
and durable jobs. Their completion, hover and signature help come from the
installed toolchain. After upgrading Geblang, run **Geblang: Restart Language
Server**. The extension version is independent of the toolchain version.

The Gebweb commands also need the `gebweb` CLI on your `PATH` (or set
`geblang.gebweb.executablePath`).

## Getting started

1. Install Geblang and this extension.
2. Open a folder containing `.gb` files (or a project with `geblang.yaml`).
3. Open a `.gb` file. The status bar shows the language server state; click it
   to open the **Geblang Language Server** output channel.
4. Click **Run** above `func main()`, or run **Geblang: Run Current File** from
   the Command Palette.

## Debugging

Press `F5` in a `.gb` file to debug it with no configuration. To customise
arguments or the working directory, add a launch configuration:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "type": "geblang",
      "request": "launch",
      "name": "Run Geblang Script",
      "program": "${file}",
      "cwd": "${workspaceFolder}",
      "args": [],
      "stopOnEntry": false
    }
  ]
}
```

## Commands

| Command | Description |
|---------|-------------|
| Geblang: Run Current File | Run the active file in a terminal. |
| Geblang: Open REPL | Start an interactive `geblang` session. |
| Geblang: Run Doctor | Report the toolchain, version and environment. |
| Geblang: Build Project | Run `geblang build` for the workspace. |
| Geblang: Clean Bytecode Cache | Run `geblang cache clean`. |
| Geblang: Select Geblang Executable | Browse for the `geblang` binary. |
| Geblang: Restart Language Server | Restart `geblang lsp`, for example after upgrading Geblang. |
| Geblang: Show Language Server Output | Open the language server log. |

## Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `geblang.executablePath` | `"geblang"` | Path to the `geblang` executable. |
| `geblang.executionMode` | `"auto"` | `auto`, `native` or `wsl`. `auto` runs natively except for Linux/WSL paths on Windows. |
| `geblang.format.clean` | `false` | Format with `geblang fmt --clean`: drop redundant parentheses and flatten multi-line chains. |
| `geblang.gebweb.executablePath` | `"gebweb"` | Path to the `gebweb` CLI used by the Gebweb commands. |
| `geblang.gebweb.templateDirs` | `["templates"]` | Folders, relative to a Gebweb project's `geblang.yaml`, whose `.html` files open as Gebweb templates. |

## Tasks

To run `geblang check`, `geblang test` and `geblang run` from **Tasks: Run
Task** with results in the Problems panel, copy
[tasks.json](https://github.com/dwgebler/geblang/blob/main/vscode-geblang/templates/tasks.json)
into your workspace's `.vscode/` folder.

## Learn more

- [Geblang manual](https://geblang.com/docs)
- [Source and issues](https://github.com/dwgebler/geblang)

## License

MIT
