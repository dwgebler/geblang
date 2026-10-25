# Getting Started

## Installation

There are two ways to get the `geblang` toolchain. The official Docker image is
the fastest start and needs nothing but Docker. Building from source gives you
the fastest development loop, a binary on your `PATH`, and the option to build
the VS Code extension.

### Docker (official image)

The official image ships the CLI and the standard library, so you can run
Geblang without installing Go or building anything. The `latest` tag always
tracks the newest release.

```sh
docker pull dwgebler/geblang        # same as dwgebler/geblang:latest

# Run a script from the current directory (mounted at /app):
docker run --rm -v "$PWD":/app dwgebler/geblang hello.gb

# Start the REPL:
docker run --rm -it dwgebler/geblang repl

# Run tests:
docker run --rm -v "$PWD":/app dwgebler/geblang test tests/

# Pin a specific release instead of latest:
docker run --rm -v "$PWD":/app dwgebler/geblang:1.27.0 hello.gb
```

The entrypoint is `geblang`, so anything after the image name is passed straight
to the CLI (`run`, `test`, `build`, `fmt`, ...). The working directory is
`/app`; mount your project there. The image is built on a glibc base, so FFI
(`dlopen`) works for shared libraries you install or mount in.

To ship a containerised application, base your image on it:

```dockerfile
FROM dwgebler/geblang:latest
WORKDIR /app
COPY . /app
CMD ["main.gb"]
```

### Build from source

Building from source gives you a `geblang` binary on your `PATH` and the option
to build the VS Code extension.

#### Prerequisites

Choose a build path before you start:

| Path | Tools required |
|---|---|
| Local Go build | Go 1.26.3 or newer (matching `go.mod`), `make`, Git |
| Docker build | Docker, `make`, Git |

You only need one of these, not both. The Go path gives the fastest build and
development loop. The Docker path avoids installing Go on the host.

To build and install the VS Code extension you also need one of:

| Extension path | Tools required |
|---|---|
| Docker (recommended) | Docker (already required for Docker build) |
| Local Node.js | Node.js 18 or newer, npm |

The Docker extension path requires no Node.js. If you already chose the Docker
build path for Geblang itself, no additional tools are needed.

#### Get the source

```sh
git clone https://github.com/dwgebler/geblang.git
cd geblang
```

#### Build the binary

**With Go:**

```sh
make build
```

This produces a `geblang` binary in the current directory.

**With Docker:**

```sh
make docker-build
```

This writes the binary and bundled stdlib to:

```
build/geblang
build/stdlib/
```

Use the Docker path when you do not want to install Go locally.

#### Add Geblang to your PATH

The `geblang` binary must be on your `PATH` for the VS Code extension, the
REPL, and all other tooling to work.

The binary is self-contained: it embeds the standard library, so copying just
the `geblang` file anywhere is enough - no extra files and no environment
variables. (To develop against a stdlib checkout instead, point `GEBLANG_STDLIB`
at it; see [Modules and Packages](07-modules-packages.md).)

**Linux and macOS:**

```sh
sudo cp geblang /usr/local/bin/
```

Or, without `sudo`, into a user-local directory:

```sh
mkdir -p ~/.local/bin
cp geblang ~/.local/bin/
# Add to your shell profile if ~/.local/bin is not already on PATH:
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc  # or ~/.zshrc
source ~/.bashrc
```

Verify:

```sh
geblang --version
```

**WSL (Windows Subsystem for Linux):**

The same Linux instructions apply. Run them inside your WSL terminal.
`/usr/local/bin/geblang` is a Linux path  -  VS Code's Remote WSL extension will
call it from Linux automatically.

**Windows (native):**

With Go installed, build from PowerShell or Command Prompt:

```sh
go build -o geblang.exe ./cmd/geblang
```

Copy `geblang.exe` to a directory on your `PATH` (for example, a `bin\`
folder under your user profile that you have already added to `%PATH%`).

Alternatively, `go install` places the binary directly in `%GOPATH%\bin`:

```sh
go install ./cmd/geblang
```

If `%GOPATH%\bin` is on your `PATH` this is the simplest option on Windows.

## VS Code IDE Setup

The VS Code extension lives in `vscode-geblang/` inside the repository. It must
be built and installed from source. It provides syntax highlighting, live error
diagnostics, completion, and step debugging for `.gb` files.

### Step 1  -  Build the extension

**Option A: Docker (no Node.js needed)**

```sh
make vscode-build
```

This builds the extension inside a container and writes the output to:

```
build/vscode/out/extension.js   compiled extension
build/vscode/vsix/geblang.vsix  installable package
```

**Option B: Local Node.js**

```sh
cd vscode-geblang
npm install
npm run compile
npx @vscode/vsce package --no-dependencies --out geblang.vsix
cd ..
```

The `geblang.vsix` file is produced inside `vscode-geblang/`.

### Step 2  -  Install the extension

**Linux and macOS:**

```sh
make vscode-install
```

Or equivalently:

```sh
code --install-extension build/vscode/vsix/geblang.vsix
```

**WSL:**

Always install via `make vscode-install` from the WSL terminal:

```sh
make vscode-install
```

This copies the VSIX to `C:\Windows\Temp\` and passes that path to `code`,
avoiding the *"UNC host 'wsl.localhost' access is not allowed"* error that
occurs when VS Code tries to open a VSIX from a `\\wsl.localhost\` path
directly. Never use the VS Code GUI's *Extensions to Install from VSIX...* dialog
to navigate to a file inside WSL  -  use the command above instead.

**Windows (native):**

From PowerShell or Command Prompt, after building with Option B above:

```sh
code --install-extension vscode-geblang\geblang.vsix
```

Or use the VS Code GUI: open the Extensions panel, click the `...` menu, choose
*Install from VSIX...*, and browse to the file.

### Step 3  -  Configure VS Code

Open VS Code Settings (`Ctrl+,` / `Cmd+,`) and search for `geblang`.

| Setting | Default | When to change |
|---|---|---|
| `geblang.executablePath` | `"geblang"` | Set to the full binary path if `geblang` is not on `PATH` |
| `geblang.executionMode` | `"auto"` | Leave as `auto`; change to `wsl` only if auto-detection fails |

**WSL-specific:** leave `geblang.executablePath` as a Linux path such as
`/usr/local/bin/geblang`. The extension knows to call it through `wsl.exe` when
needed. Do not point it at a Windows path.

Example `settings.json` snippet:

```json
{
  "geblang.executablePath": "/usr/local/bin/geblang"
}
```

### Step 4  -  Verify the setup

1. Open a `.gb` file. The syntax should be highlighted immediately.
2. Introduce a deliberate error (delete a semicolon, reference an unknown
   identifier). A red underline should appear within a second or two.
3. Fix the error. Press **F5** to start a debug session. Set a breakpoint and
   confirm execution pauses there.

If diagnostics are not appearing, open the Output panel (`Ctrl+Shift+U` /
`Cmd+Shift+U`), select **Geblang Language Server** from the dropdown, and check
for startup errors. The most common cause is `geblang` not being found  -  set
`geblang.executablePath` to the full path.

### Debugging features

Once the extension is active:

- **Breakpoints**: click in the editor gutter to set them, press **F5** to
  start debugging.
- **Variable inspection**: the Variables panel shows local values when paused.
- **Expression evaluation**: open the Debug Console (`Ctrl+Shift+Y`) and type
  any Geblang expression while paused at a breakpoint. Hover over a variable in
  the editor to see its value inline.
- **Watch expressions**: add expressions in the Watch panel to re-evaluate them
  at each pause.

| Action | Shortcut |
|---|---|
| Start / Continue | F5 |
| Step Over | F10 |
| Step Into | F11 |
| Step Out | Shift+F11 |
| Stop | Shift+F5 |

## Claude Code skill

Geblang ships a Claude Code skill that equips Claude to write, run, test, check,
format, build, and bundle Geblang correctly: the idioms, the cross-language
gotchas (`//` is integer division, `parent()` not `super`, decimal-default
literals), the standard-library surface, and the `geblang` CLI workflow.

It lives at `.claude/skills/geblang/` in the source tree. Claude Code loads it
automatically as a project skill when you work inside a checkout of the
repository. To use it in any project, copy it into your user skills:

```sh
cp -r .claude/skills/geblang ~/.claude/skills/geblang
```

## Running Scripts

```sh
geblang script.gb
geblang --disable-vm script.gb
geblang --vm-strict script.gb
geblang --trace-exec script.gb
```

By default Geblang compiles to bytecode and runs on the VM. `--trace-exec`
prints which execution path was used:

```sh
geblang --trace-exec examples/core.gb
```

Run an executable module with `-m`. The module is resolved like any import and
its exported `main(args)` function receives the remaining arguments:

```sh
geblang -m http.server 8080
```

## REPL

```sh
geblang
geblang repl
```

The REPL supports declarations, imports, multi-line input, history, and
completion. REPL commands:

```text
:help       show available commands
:quit       exit
:reset      clear all bindings and imports
:load file  execute a .gb file into the session
:vars       list current bindings
:imports    list imported modules
:stdlib     list all standard-library modules
:mode       show current execution mode
:history    show command history
```

Expression results print automatically. Imported module names are constants for
the session lifetime; use `:reset` to start fresh.

## Checking Code

```sh
geblang check script.gb
geblang check src/
geblang check --json src/
geblang check --no-lint src/
geblang check --strict src/
```

`check` parses, semantically analyses, and lints without executing. It validates
imports using the same path rules used at runtime. Lint warnings (unused imports,
unreachable statements) are reported but do not fail the command unless
`--strict` is used.

As of 1.13.0, `check` also performs type-level checks:

- Unknown type names in any annotation position (parameter, return, field,
  variable, generic argument, catch clause, or `as` cast) are errors:
  `error[semantic]: unknown type "FakeType" in parameter x of function f`
- Type checks run inside class method bodies: mismatched return types and
  argument types against declared annotations are caught statically.
- A module-qualified type name whose module does not export that name is
  flagged: `error[type]: shapes has no exported type NonExistent`

Static-analysis errors are also enforced by `geblang run`: a script that fails
`check` because of a no-matching-overload call, a type mismatch, or any other
hard error aborts before the first statement runs instead of executing partway
through and crashing. Warnings print to stderr but do not block execution.
`check` remains the place to discover all findings up front (CI / pre-commit
hook).

Generate API documentation from docblocks:

```sh
geblang doc src/
geblang doc --format json src/
geblang doc --out build/api.md src/
```

## Testing

```sh
geblang test examples/sample_test.gb
geblang test --tag fast examples
```

Test discovery scans for `_test.gb` files when the path is a directory.

```gb
import test;

class SampleTest extends test.Test {
    @test
    func addition(): void {
        this.assertEquals(4, 2 + 2);
    }
}
```

## Cache And Diagnostics

Bytecode cache files are written under `.geblang-cache/`.

```sh
geblang cache stats
geblang cache clean
geblang doctor
```

`doctor` reports version, working directory, Go toolchain lookup, package
manifest discovery, and cache status. Run it first when diagnosing an
environment problem.
