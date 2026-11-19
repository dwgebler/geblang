# File type and MIME detection (`clib.magic`)

`import clib.magic as magic;` provides content-based file type and
MIME type detection backed by libmagic (the library that powers the
`file(1)` command). It fills a gap over signature sniffing done purely
in Geblang: libmagic tests file content, magic numbers, and structural
patterns learned from its compiled database, making identification
accurate even when extensions are missing or wrong.

The module uses the FFI layer so the `ffi` capability must be enabled.

> **Linux / macOS only.** libmagic ships as `libmagic.so.1` on most
> Linux distributions (install `libmagic-dev` / `file` package) and
> as `libmagic.dylib` on macOS via Homebrew.

## Capability

Add to `geblang.yaml`:

```yaml
permissions:
  ffi:
    enabled: true
    libraries:
      - glob: libmagic*
```

For a standalone script:

```sh
geblang --allow-ffi 'libmagic*' script.gb
```

## Module-level functions

These are the easiest entry point. Both functions share a lazily
created, internally guarded `Magic` handle.

### `detect(string path): string`

Returns a human-readable content description for the file at `path`,
for example `"ASCII text"` or `"PNG image data, 640 x 480"`. Uses
libmagic with no extra flags (NONE mode).

### `mime(string path): string`

Returns the MIME type for the file at `path`, for example
`"text/plain"` or `"image/png"`. Uses MIME_TYPE mode.

## Class `Magic`

Use `Magic` directly when you need repeated detection with a specific
set of flags or when you want explicit handle lifetime.

### `Magic(int flags = 0)`

Opens a libmagic handle with `flags`. The two exported constants are:

| Constant | Value | Effect |
|---|---|---|
| `magic.NONE` | `0` | Human-readable content description |
| `magic.MIME_TYPE` | `16` | MIME type string |

Throws `RuntimeError` if libmagic cannot be opened or its database
cannot be loaded.

### `detect(string path): string`

Returns a content description or MIME type for the file at `path`.

### `detectBuffer(bytes data): string`

Returns a content description or MIME type by examining `data`
directly without touching the filesystem.

### `close(): void`

Closes the libmagic handle and releases its resources. Safe to call
more than once.

## Examples

### Detect type and MIME of a file

```gb
import clib.magic as magic;
import io;

let path = "report.pdf";
io.println(magic.detect(path));   /* PDF document, version 1.7 */
io.println(magic.mime(path));     /* application/pdf */
```

### Detect from bytes (no filesystem access needed)

```gb
import clib.magic as magic;
import bytes;
import io;

/* PNG magic bytes */
let pngHeader = bytes.fromList([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
let m = magic.Magic(magic.MIME_TYPE);
io.println(m.detectBuffer(pngHeader));   /* image/png */
m.close();
```

### Multiple files with a single handle

```gb
import clib.magic as magic;
import io;

let files = ["go.mod", "README.md", "data.bin"];
let m = magic.Magic(magic.MIME_TYPE);
for (f in files) {
    io.println("${f}: ${m.detect(f)}");
}
m.close();
```

## Thread safety

Each `Magic` instance serialises its calls internally using a mutex.
Concurrent calls on the same instance from multiple async tasks are
safe. The module-level `detect` and `mime` helpers share one `Magic`
each and are likewise safe to call concurrently.

## Error behaviour

| Failure mode | Surface |
|---|---|
| libmagic not found or FFI not enabled | `RuntimeError` or `PermissionError` |
| `magic_open` or `magic_load` failure | `RuntimeError` with the libmagic error string |
| `magic_file` or `magic_buffer` failure | `RuntimeError` with the libmagic error string |
