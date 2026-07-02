# Paths

Import `path` for portable path string manipulation. Import `pathlib` when you
want object-style path values.

`path` does not touch the filesystem except for `abs`, `real`, and `glob`, which
ask the host to resolve or match paths. Creating directories, reading files,
changing permissions, and deleting paths are `io` responsibilities.

```gb
import path;
import io;

let file = path.join("config", "app.yaml");
if (io.exists(file)) {
    io.println(path.base(file));
}
```

## Native `path` Module

| Function | Returns | Description |
|----------|---------|-------------|
| `join(string ...parts)` | `string` | Join any number of path segments and clean the result |
| `clean(path)` | `string` | Normalize `.` / `..` and duplicate separators |
| `base(path)` | `string` | Last path element |
| `dir(path)` | `string` | Parent directory path |
| `ext(path)` | `string` | Final file extension, including the dot |
| `abs(path)` | `string` | Absolute path resolved from the current working directory |
| `real(path)` | `string` | Canonical absolute path with all symlinks resolved |
| `rel(base, target)` | `string` | Relative path from base to target |
| `glob(pattern)` | `list<string>` | Shell-style path matches |

## Building Paths

Use `path.join` instead of string concatenation. It avoids accidental duplicate
separators and makes intent clear.

```gb
let config = path.join(sys.homedir(), ".config", "app", "settings.yaml");
let cache = path.join(sys.tmpdir(), "geb-cache");
```

`join` accepts any number of segments:

```gb
path.join("config", "environments", "prod.yaml");
# "config/environments/prod.yaml"
```

`clean` normalizes an existing string:

```gb
path.clean("./config/../config/app.yaml");
# "config/app.yaml"
```

## Inspecting Paths

```gb
let p = "src/controllers/user.gb";

io.println(path.base(p)); # "user.gb"
io.println(path.dir(p));  # "src/controllers"
io.println(path.ext(p));  # ".gb"
```

`ext` returns only the final extension:

```gb
io.println(path.ext("archive.tar.gz")); # ".gz"
io.println(path.ext("README"));         # ""
```

`dir("file.txt")` returns `"."` because the file is in the current directory.

## Absolute And Relative Paths

```gb
let abs = path.abs("config/app.yaml");
let rel = path.rel(sys.cwd(), abs);
```

`abs` can raise an error if the host cannot resolve the path. `rel` can raise an
error if the two paths cannot be related, for example across different Windows
volumes.

`real` resolves every symlink along the path and returns the canonical absolute
target. It is the safe way to confirm a file stays inside an intended directory:
resolve both the root and the requested file, then require the file to start
with the root. Because it follows symlinks, it catches escapes that a lexical
`clean` would miss. It raises an error if the path does not exist.

```gb
let root = path.real("public");
let target = path.real(path.join("public", requested));
if (target != root && !target.startsWith(root + "/")) {
    # requested escaped the root; refuse it
}
```

## Glob Matching

`path.glob(pattern)` returns matching paths as `list<string>`.

```gb
let configs = path.glob("config/*.yaml");
let texts = path.glob("*.txt");
let all = path.glob("src/**/*.gb");
```

Supported wildcards:

| Pattern | Meaning |
|---------|---------|
| `*` | Any sequence of non-separator characters |
| `?` | Any single non-separator character |
| `[abc]` | Character class |
| `**` | Zero or more path segments (Python-style recursive match) |

Recursive `**` walks the tree under the static prefix and matches the
trailing segment against the suffix. The result is sorted for
deterministic output. Use `io.walkDir` if you need entry metadata
(size / modtime) rather than just paths:

```gb
for (entry in io.walkDir("src")) {
    if (!(entry["isDir"] as bool) && path.ext(entry["path"]) == ".gb") {
        io.println(entry["path"]);
    }
}
```

## Filesystem Workflows

Create a directory and file:

```gb
let dir = path.join(sys.tmpdir(), "geb-example");
io.mkdir(dir, 0o755);

let file = path.join(dir, "out.txt");
io.writeText(file, "hello");
```

Move a file while preserving clear path intent:

```gb
let src = path.join("build", "app.tmp");
let dst = path.join("build", "app");

io.rename(src, dst);
```

## `pathlib`

`pathlib` is a source-stdlib module that wraps the native `path` functions in a
fluent object-oriented API. It is useful when passing paths through an
application as values with helper methods.

```gb
import pathlib;

let p = pathlib.of("/tmp/data/report.csv");
io.println(p.stem());
io.println(p.ext());
```

### Constructors

| Function | Returns | Description |
|----------|---------|-------------|
| `pathlib.of(raw)` | `Path` | Create a cleaned `Path` from a string |
| `pathlib.join(string ...parts)` | `Path` | Join any number of segments and return a `Path` |

### `Path` Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `base()` | `string` | Last path element |
| `dir()` | `Path` | Parent path |
| `parent()` | `Path` | Alias for `dir()` |
| `ext()` | `string` | File extension including the dot |
| `stem()` | `string` | Base name without extension |
| `withExt(newExt)` | `Path` | Return a path with a different extension |
| `join(string ...parts)` | `Path` | Append any number of segments |
| `abs()` | `Path` | Absolute path |
| `toString()` | `string` | Plain string path |
| `exists()` | `bool` | Whether the path exists |
| `isDir()` | `bool` | Whether the path is a directory |
| `isFile()` | `bool` | Whether the path is a regular file |
| `glob(pattern)` | `list<Path>` | Glob relative to this path |

Methods that return `Path` are immutable-style: they return a new path and do
not modify the original.

```gb
let report = pathlib.of("/tmp/data/report.csv");
let json = report.withExt("json");

io.println(report.toString()); # /tmp/data/report.csv
io.println(json.toString());   # /tmp/data/report.json
```

Glob relative to a directory:

```gb
let src = pathlib.of("./src");
for (file in src.glob("*.gb")) {
    io.println(file.toString());
}
```
