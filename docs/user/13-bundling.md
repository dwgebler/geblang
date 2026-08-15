# Bundling And Standalone Executables

`geblang build` produces a self-contained binary from a Geblang package. The
resulting file runs on any machine with the same OS and architecture as the
machine that built it. No Geblang installation, no stdlib directory, and no
`geblang.yaml` manifest are required on the target machine.

## How Bundling Works

A Geblang bundle is a standard executable with a zip archive appended to it.

```
┌──────────────────────────────────────────────────┐
│  geblang interpreter binary (ELF/Mach-O/PE)      │
├──────────────────────────────────────────────────┤
│  zip archive                                      │
│    BUNDLE.json          manifest                  │
│    src/app/main.gb      source files              │
│    src/app/main.gbc     precompiled bytecode      │
│    stdlib/collections.gb  bundled source stdlib   │
│    stdlib/collections.gbc                         │
│    ...                                            │
├──────────────────────────────────────────────────┤
│  8-byte zip size (little-endian uint64)           │
│  4-byte magic: GEBX                               │
└──────────────────────────────────────────────────┘
```

At startup the interpreter checks whether a 12-byte trailer is present and
whether it contains the `GEBX` magic value. If so, it reads the zip, parses
`BUNDLE.json`, and runs the bundled entry module instead of looking for
command-line arguments.

The zip archive is a standard zip file and can be inspected with `unzip -l` or
any zip reader.

## What Gets Bundled

`geblang build` walks the import graph starting from the entry module and
collects every non-native module it can reach:

- **User source modules** are included under `src/` inside the zip.
- **Imported package dependencies** declared in `geblang.yaml` are included
  when they are installed under `vendor/<name>/` and reached by the import
  graph.
- **Source-stdlib modules** (Geblang-written standard library files) are
  included under `stdlib/` inside the zip.
- **Native modules** - Go-backed modules like `io`, `sys`, `collections`,
  `http`, `db`, and so on - are part of the interpreter binary itself and are
  not duplicated in the zip.

Each collected source file is also compiled to bytecode and stored alongside it
as a `.gbc` file. The precompiled bytecode is loaded directly into the
interpreter's bytecode cache on first run, so the bundled program starts at
full speed with no warm-up compilation step.

Only imported modules are bundled. `geblang build` does not copy the entire
package directory, the entire `vendor/` directory, test files, or generated
artifacts. If a package dependency is declared but no module from that package
is imported, it is not included in the bundle. Non-code files (templates, static
assets, data files) are embedded only when listed under `resources:` in the
manifest; see [Embedding Resources](#embedding-resources).

## Embedding Resources

Programs often ship non-code files alongside their source: HTML templates,
static assets, data files. List them under `resources:` in `geblang.yaml` and
`geblang build` embeds them in the bundle ZIP at their project-relative path,
next to `src/` and `stdlib/`.

```yaml
name: myapp
source: src
resources:
  - templates        # a directory: embedded recursively
  - static           # another directory
  - data/*.json      # a glob: matched files only
```

Each entry is either a directory (embedded recursively) or a glob pattern. A
pattern that matches nothing is a build error, so a typo fails loudly rather
than silently shipping an empty bundle. Resource paths may not collide with the
reserved `src/` or `stdlib/` bundle directories.

At run time a program finds its embedded files through `sys.bundleDir()`, which
returns the bundle's extract directory (see [First-Run
Extraction](#first-run-extraction)) or the empty string when the program is not
running from a bundle. Resolve resources against it, falling back to the project
directory in development, so the same code path works in both cases:

```gb
import io;
import sys;

func loadTemplate(string name): string {
    let base = sys.bundleDir();
    if (base == "") { base = "."; }   /* dev: read from the project tree */
    return io.readText(base + "/templates/" + name);
}
```

Because resources keep their project-relative path inside the bundle, the same
relative path (`templates/page.html`) resolves correctly whether `base` is the
project directory in development or the extract directory in a built binary.

For a small file whose content should become part of the program itself
rather than a file extracted at launch, see [Compile-Time File
Embedding](#compile-time-file-embedding) below.

## Compile-Time File Embedding

`embed(path)` reads a file at compile time and inlines its content as a
constant, directly in the compiled program. Unlike `resources:`, there is no
manifest entry, no bundle directory, and nothing to extract at launch: the
value is simply there, the same as a string literal would be.

```gb
import io;

let banner = embed("assets/banner.txt");
io.println(banner);

let icon = embed("assets/icon.png", {binary: true});
io.println("${icon.length()} bytes");
```

`embed(path)` returns a `string`; `embed(path, {binary: true})` returns
`bytes`. Both forms require `path` to be a string literal, not a variable or
an interpolated expression, because the file has to be resolved while the
program compiles. `embed` works the same way under `geblang run`, `geblang
test`, `geblang check`, the REPL, and built binaries, and the experimental
`geblang build --native` path supports it too, including binary embeds.

### Path rules

- The path is resolved relative to the directory of the source file
  containing the `embed(...)` call, not the current working directory or the
  package root.
- Paths use forward slashes on every platform.
- A path may descend into subdirectories (`embed("data/msg.txt")`) but may
  not contain a `..` segment or be absolute; embedding is restricted to the
  subtree under the source file's own directory.
- The path must be a string literal written directly in the call. It cannot
  be a variable, a function result, or a string with interpolation.
- The name `embed` is reserved for this compile-time builtin; using it as a
  variable or value anywhere in a program is a load error.

### Errors happen at load time, not at runtime

A missing file, a path outside the allowed subtree, a directory given where a
file is expected, or a malformed call is reported as a load-time error, the
same class of error as a syntax mistake: the program never starts running.
There is no way to catch an embed failure with `try`/`catch`, because the
file is read before the program's `main` logic exists. `geblang check` also
reports embed problems in place, so an editor or CI run catches a bad path
before the program ever runs.

### Content lives in the program

The embedded content becomes part of the compiled program: it is held in
memory for the life of the process, stored in the cached bytecode (`.gbc`)
alongside the rest of the compiled code, and packed into a `geblang build`
binary just like source code is. Editing the file on disk invalidates the
cached bytecode for the module that embeds it, so the next run picks up the
change automatically. Because the content is duplicated into every place the
program's code already lives, `embed` suits small assets (icons, short
templates, fixed configuration snippets) rather than large files.

### `embed` vs. `resources:` / `sys.bundleDir()`

See [Embedding Resources](#embedding-resources) above for the runtime-extraction
alternative.

| | `embed(path)` | `resources:` + `sys.bundleDir()` |
|---|---|---|
| When the file is read | Compile time; content becomes a constant | Runtime; the bundle extracts its files at first launch and the program reads them from disk |
| Best for | Small, fixed assets baked into the program itself | Larger files, or a whole directory tree, kept as files on disk |

## Package Layout

A bundle is built from a Geblang package. The package needs:

- A `geblang.yaml` manifest that declares the package name and source root.
- At least one source module that exports a `main(list<string> args)` function
  to serve as the entry point.

Minimal package layout:

```
myapp/
  geblang.yaml
  src/
    myapp/
      main.gb
```

`geblang.yaml`:

```yaml
name: myapp
source: src
```

`src/myapp/main.gb`:

```gb
module myapp.main;

import io;
import sys;

export func main(list<string> args): void {
    io.println("Hello from a bundled app!");
}
```

The entry module name must match a `module` declaration that the package
resolver can find. In this layout the canonical name is `myapp.main`.

The same entry file runs directly during development: `geblang <file>`
auto-invokes an exported top-level `main` when the file declares one, forwarding
command-line arguments and using an `int` return value as the exit code. So a
`module` that `export func main(list<string> args)` behaves identically whether
run directly or built. A file with no exported `main` runs as a plain script.

## Running geblang build

```sh
geblang build --entry <module.name> --out <output-path> [<package-dir>]
```

| Argument | Required | Description |
|---|---|---|
| `--entry` | yes | Canonical name of the entry module (must export `main`) |
| `--out` | yes | Path for the output binary |
| `--resource` | no | Extra resource to embed, in addition to the manifest `resources:`. Repeatable. `--resource <path>` keeps the project-relative path; `--resource <path>=<bundlePath>` remaps it (a directory's contents mirror under `<bundlePath>`), so a build step can embed processed copies without altering the source tree |
| `--allow-ffi` | no | Bake an FFI allow-list entry (path or glob) into the binary. Repeatable. Adds to the manifest `permissions.ffi`. See Capabilities below |
| `--allow-onnx` | no | Bake the local ONNX inference capability into the binary |
| `--allow-process-control` | no | Bake the privileged process-control capability into the binary |
| `--allow-browser` | no | Bake the headless-browser automation capability into the binary |
| `--docker` | no | Also write a `Dockerfile` next to the binary (see below) |
| `--docker-port` | no | Add `EXPOSE <port>` to the generated Dockerfile |
| `--force` | no | Overwrite an existing generated Dockerfile |
| `<package-dir>` | no | Package root directory (default: `.`) |

Build the example package above:

```sh
geblang build --entry myapp.main --out ./dist/myapp ./myapp
```

Or from inside the package directory:

```sh
cd myapp
geblang build --entry myapp.main --out ../dist/myapp
```

The output binary is set executable (`chmod 755`). On Unix you can run it
directly:

```sh
./dist/myapp
```

### Capabilities in built binaries

Privileged capabilities - FFI, local ONNX inference, and process control - are
off by default and are normally turned on with a launch flag (`--allow-ffi`,
`--allow-onnx`, `--allow-process-control`, `--allow-browser`). A built binary has no launch-flag
step, so it carries its capabilities baked in at build time and just runs.

Declare them in `geblang.yaml`, the reproducible source of truth:

```yaml
permissions:
  ffi:
    enabled: true
    libraries:
      - glob: /opt/torch/lib/*.so
  onnx: true
  processControl: true
  browser: true
```

This same block also enables those capabilities for `geblang run` / `geblang
test` in the project, so dev and the built binary behave identically.

Alternatively, pass them to `geblang build` for an ad-hoc build; the flags add to
whatever the manifest already declares:

```sh
geblang build --entry app.main --out ./dist/app \
  --allow-ffi '/opt/torch/lib/*.so' --allow-onnx --allow-process-control --allow-browser
```

The resolved capability set is recorded in the bundle, so the end user runs the
binary with no flags. A binary built without any of these stays locked down: a
gated call throws `PermissionError`, exactly as an unflagged `geblang run` would.

### Third-party notices

Alongside the binary, `geblang build` writes a `<output-path>.NOTICES.txt`
file containing the third-party attribution notices for the components the
binary embeds (the Geblang runtime and its dependencies). Ship this file with
the binary to keep the distribution licence-compliant. The same notices are
also available at runtime through the built-in `--notices` flag (below). The
same applies to binaries produced by `gebweb build`, which delegates to
`geblang build`.

### Standard flags and program arguments

Every built binary recognises a few standard flags, but only when one is the
**first argument**:

- `--help` / `-h` prints the binary's usage.
- `--version` prints `<app> <version> (geblang <runtime-version>)`.
- `--notices` prints the embedded third-party notices.

Because these are double-dash flags checked only in first position, they never
clash with a bare-word subcommand (such as a `licenses` or `version` command)
or any argument your own program defines: those are forwarded unchanged. A
literal `--` also forwards everything after it verbatim, so
`./dist/myapp -- --version` passes `--version` to your program instead of
printing the Geblang version.

Otherwise, pass arguments the same way you would to any other binary:

```sh
./dist/myapp --port 8080 --verbose
```

The argument list is forwarded to the entry module's `main` function via
`sys.args()`.

## Docker Output

`geblang build --docker` writes a `Dockerfile` into the output directory
alongside the binary (1.19.0). The image copies the binary and its
`NOTICES` sidecar into `gcr.io/distroless/base-debian12` (glibc included -
the binary links libc dynamically) and runs it as the entrypoint:

```sh
geblang build --entry app.main --out dist/app --docker --docker-port 8085 .
cd dist && docker build -t myapp . && docker run -p 8085:8085 myapp
```

`EXPOSE` is only emitted when `--docker-port` is given - a built binary is
not necessarily a server. An existing `Dockerfile` is left unchanged
unless `--force` is passed, so manual edits survive rebuilds. Arguments
after `docker run <image>` flow to the binary as normal program
arguments; the standard flags below work inside the container too. The
typical reverse-proxy deployment runs the container on an internal port
and fronts it with nginx.

## Cross-Platform Builds

`geblang build` embeds the running runtime into the output, so by default the
binary it writes targets the platform you run it on. To build for another
platform, embed the bundle into a runtime compiled for that target instead:
`geblang build --runtime <path>` reads the runtime at that path rather than the
running one. Because the runtime is pure Go with no cgo it cross-compiles to any
supported target with the Go toolchain, and the bundle itself is platform-
independent, so any host can produce a binary for any target.

The shipped helper `scripts/cross-build.sh` does both steps (cross-compile the
runtime, then embed the bundle) from a source checkout; the Go toolchain is the
only requirement:

```sh
scripts/cross-build.sh --target linux/amd64   --entry app.main --out build/app
scripts/cross-build.sh --target darwin/arm64  --entry app.main --out build/app
scripts/cross-build.sh --target windows/amd64 --entry app.main --out build/app.exe
```

Any host (Linux, macOS, Windows) can build for `linux`, `darwin`, or `windows`
on `amd64` or `arm64`. The package directory is set with `--dir` (default: the
current directory); extra build flags go after `--`:

```sh
scripts/cross-build.sh --target linux/amd64 --entry app.main --out build/app -- --no-assert
```

### Windows limitations

Geblang builds and runs on Windows, but a few unix-oriented capabilities are
unavailable there and report a clear error at runtime; programs that do not use
them run unchanged:

- FFI (`ffi`, `clib.*`) and local ONNX inference (`onnx`) load native shared
  libraries through dlopen, which is unix-only here.
- Advisory file locking (`io.lock` / `io.tryLock`) is unix-only.
- The `hnsw` vector-store backend uses an exact brute-force index on Windows
  rather than the approximate HNSW graph; results are exact and performance
  differs on very large indexes.
- Interactive console widgets (`cli.choose` / `cli.multiChoose`) and the REPL
  line editor fall back to plain line input (no raw-key handling).

## Standard Flags Of Built Binaries

Every built binary answers a small set of standard flags, recognised
only when they are the FIRST argument:

| Flag | Effect |
|---|---|
| `--help`, `-h` | Application name/version, usage, and this flag list |
| `--version` | `<name> <version> (geblang <engine version>)`, from `geblang.yaml` |
| `--notices`, `--licences` | Prints the embedded third-party licence notices |
| `--` | Passes everything after it to the application untouched |

Any other first argument (and all later arguments) flow to the
application's `main(list<string> args)` unchanged, so an application
that defines its own `--help` can still receive it via
`./app -- --help`.

## Full Example: A CLI Tool

Package layout:

```
greet/
  geblang.yaml
  src/
    greet/
      main.gb
      formatter.gb
```

`geblang.yaml`:

```yaml
name: greet
source: src
```

`src/greet/formatter.gb`:

```gb
module greet.formatter;

export func format(string name): string {
    return "Hello, " + name + "!";
}
```

`src/greet/main.gb`:

```gb
module greet.main;

import io;
import sys;
import greet.formatter as fmt;

export func main(list<string> args): void {
    if (args.length() == 0) {
        io.println("Usage: greet <name>");
        sys.exit(1);
    }
    io.println(fmt.format(args[0]));
}
```

Build and run:

```sh
geblang build --entry greet.main --out ./greet-bin ./greet
./greet-bin Ada
# Hello, Ada!
```

Both `greet.main` and `greet.formatter` are discovered automatically by the
import-graph walk and bundled together.

## Bundling Package Dependencies

Geblang's package installer places git dependencies under `vendor/`. Bundling
uses the same package resolver as normal execution, so imported dependency
modules are included automatically once they are installed.

Example application manifest:

```yaml
name: webapp
source: src
dependencies:
  authlib:
    git: https://example.com/authlib.git
    version: main
```

After running `geblang install`, the dependency is available at:

```
webapp/
  geblang.yaml
  src/
    main.gb
  vendor/
    authlib/
      geblang.yaml
      src/
        authlib/
          tokens.gb
```

Application code can import it normally:

```gb
module webapp.main;

import io;
import authlib.tokens as tokens;

export func main(list<string> args): void {
    io.println(tokens.issue("ada"));
}
```

Build the application:

```sh
geblang build --entry webapp.main --out ./webapp-bin ./webapp
```

The resulting executable contains `webapp.main`, `authlib.tokens`, any other
non-native modules reached from those imports, and the source stdlib modules
they need. The target machine does not need `vendor/`, `geblang.yaml`, or a
separate Geblang installation.

`geblang build` does not fetch missing dependencies. If `geblang.yaml` declares
a git dependency and `vendor/<name>/` is absent, run `geblang install` before
building.

## Full Example: A Web Server

```
webapi/
  geblang.yaml
  src/
    webapi/
      main.gb
      routes.gb
```

`geblang.yaml`:

```yaml
name: webapi
source: src
```

`src/webapi/routes.gb`:

```gb
module webapi.routes;

import http;
import web;

export func register(any app): void {
    web.get(app, "/", func(dict<string, any> request): dict<string, any> {
        return http.jsonResponse({"status": "ok"});
    });
}
```

`src/webapi/main.gb`:

```gb
module webapi.main;

import io;
import http;
import web;
import webapi.routes as routes;

export func main(list<string> args): void {
    let port = args.length() > 0 ? args[0] : "8080";
    let app = web.new();
    routes.register(app);
    io.println("Listening on :" + port);
    http.serve("127.0.0.1:" + port, func(dict<string, any> request): dict<string, any> {
        return web.handle(app, request);
    });
}
```

Build a distributable API server:

```sh
geblang build --entry webapi.main --out ./webapi-server ./webapi
./webapi-server 9000
```

## First-Run Extraction

On the first invocation of a bundled binary, the zip is extracted to a
temporary directory under the system temp path:

```
/tmp/geblang-<hash>/
  src/
    ...
  stdlib/
    ...
```

The directory name includes a SHA-256 hash of the zip contents, so different
bundle versions use different directories and never collide. On subsequent runs,
the extracted directory already exists and extraction is skipped - the startup
overhead is paid only once.

Precompiled bytecode is loaded into the interpreter's bytecode cache at
extraction time so recompilation is avoided even after the cache directory is
cleared.

## Inspecting A Bundle

Because the bundle is a valid zip appended to the binary, standard tools work:

```sh
# List bundled files
unzip -l ./dist/myapp

# Extract the source files manually
unzip ./dist/myapp -d ./bundle-contents
```

`BUNDLE.json` inside the zip records the entry module name, the Geblang version
the bundle was built with, and the canonical name, zip path, and source hash
for every bundled module.

## Native Compilation (Experimental)

> **Experimental and unstable.** `geblang build --native` is a preview of an
> ahead-of-time path that compiles a Geblang program to a standalone native
> binary by transpiling it to Go. The set of supported language features and
> stdlib modules below WILL change between releases, and the flag, its output,
> and its diagnostics are not yet covered by any stability guarantee. For
> production builds use plain `geblang build` (the bundled-interpreter binary),
> which supports the whole language.

Instead of bundling the interpreter, `geblang build --native` lowers the entry
program (and the stdlib modules it uses) to self-contained Go source and
compiles it with the local Go toolchain. For dispatch-bound code (tight loops,
recursion, virtual method dispatch) the result runs several times faster than
the bundled VM; gains shrink for allocation- or string-heavy code.

```sh
geblang build --native --entry <module> --out <path> [<package-dir>]
```

It requires a local Go toolchain at least as new as the one that built your
`geblang`, and builds offline (no module downloads). The entry convention is the
same as plain `geblang build`: the entry module must `export func main()` (or
`export func main(list<string> args)`, optionally returning `: int`); a missing
main is a build error.

### Safety: it fails at build time, never silently

The native compiler is a growing subset of the language. When it meets a feature
or stdlib function it does not yet support, it **fails the build with a clear
diagnostic** naming the file, line, and reason, and writes no binary. It never
emits a binary that behaves differently from the interpreter: supported programs
are byte-for-byte identical to `geblang run` / `geblang test`, and unsupported
ones do not build. So it is safe to try `--native` on any program and fall back
to `geblang build` if it reports something unsupported.

### What is supported

- The core language: functions, classes (inheritance and virtual dispatch),
  interfaces, generics, enums (data variants, instance methods, interface
  implementation, and the `values()` / `fromName()` static surface), `match`
  (type / list / enum / guard patterns), exceptions, generators, async/await,
  comprehensions,
  destructuring, closures, optional chaining, spread, named/optional/variadic
  arguments, `with`, string interpolation, slicing, and dynamic navigation of
  `any`-typed values (indexing and `as` casts, e.g. over a `json.parse` result).
- Standard-library modules backed by the Go standard library: `io`, `sys`,
  `collections`, `math`, `json`, `strings` (including the regex methods),
  `encoding`, `crypt` (the hash functions), `time`, `random`, `re` (including
  compiled patterns), `bytes`, `url` (including the `URL` object), `csv`, `xml`,
  `datetime` (functional and object surfaces), `template`, `reflect`.

### What is not supported yet (these diagnose, use `geblang build`)

- Modules backed by third-party libraries: `yaml`, `toml`, `uuid`, `markdown`,
  `pcre`, and `unicode` normalization.
- Stateful / I/O modules: `db`, `http`, `net`, `sockets`, `log`, messaging, and
  similar.
- Calling a method on an `any`-typed value (cast it to a concrete type first);
  assigning into an `any`-typed index.
- Arbitrary-precision integers: native arithmetic uses a fast machine-width path
  that wraps on overflow rather than promoting to big integers.
- Partial-application `_` placeholder arguments: use a typed wrapper function instead.

### Performance note

Repeated string concatenation in a loop (`acc = acc + part`) is O(n^2) in the
generated Go because Go strings are immutable, so a tight concat loop can be
slower than the VM. Build large strings with a list and `join`.

## Limitations

- **OS and architecture**: a built binary runs on the OS and architecture it
  was produced for. Cross-compilation IS supported: `geblang build --runtime
  <path>` embeds the bundle into a runtime compiled for another target, and the
  shipped `scripts/cross-build.sh` helper does both steps (the runtime is pure
  Go and cross-compiles with `CGO_ENABLED=0`). See "Cross-Platform Builds"
  above.
- **No hot-reload**: Bundled source is extracted once and cached. Changes to
  the original source files have no effect on a built bundle.
- **Import-graph walking**: `geblang build` discovers modules by parsing import
  statements statically. Dynamic module loading (using string variables as
  import paths) is not currently traversed. Any module loaded this way must be
  imported explicitly elsewhere in the package so it is included in the bundle.
- **Vendored dependencies**: imported modules from installed package
  dependencies are bundled, but unimported files under `vendor/` are not copied
  wholesale.
- **Native extensions**: Go-backed `ext.*` extensions are not bundled. They must
  be present on the target machine alongside the bundle binary.
