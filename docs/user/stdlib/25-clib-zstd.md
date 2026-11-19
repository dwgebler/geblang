# Zstd compression (`clib.zstd`)

`import clib.zstd as zstd;` gives Geblang programs fast compression
and decompression backed by libzstd. It fills a gap left by the
native `archive` module, which offers gzip/tar but not the modern
Zstandard format. Zstd typically compresses faster than gzip at
comparable ratios, and much faster again at decompression.

The module uses the FFI layer so you must enable the `ffi` capability
in your project manifest (or pass `--allow-ffi` on the command line).

## Capability

Add to `geblang.yaml`:

```yaml
permissions:
  ffi:
    enabled: true
    libraries:
      - glob: libzstd*
```

For a standalone script:

```sh
geblang --allow-ffi 'libzstd*' script.gb
```

## Functions

### `compress(bytes data, int level = 3): bytes`

Compresses `data` using Zstandard at the given compression level.
Level must be in the range 1..22; the default is 3 (the libzstd
default). Returns a complete zstd frame that carries the original
content size.

### `decompress(bytes frame): bytes`

Decompresses a zstd frame. The frame must carry a known content size
(which `compress` always produces). Throws `RuntimeError` on corrupt
or truncated input, or on frames that do not carry a content size
(for example, frames produced by tools that strip the content-size
header).

## Round-trip example

```gb
import clib.zstd as zstd;
import bytes;
import io;

let original = bytes.fromString("hello, world - this is a test of zstd compression");

let compressed = zstd.compress(original);
io.println("original:   ${original.length()} bytes");
io.println("compressed: ${compressed.length()} bytes");

let recovered = zstd.decompress(compressed);
io.println("round-trip ok: ${recovered.toString() == original.toString()}");
```

Compression level can be raised for better ratios at the cost of
speed:

```gb
let tight = zstd.compress(data, 19);    /* level 19 - high ratio */
let fast  = zstd.compress(data, 1);     /* level 1  - fastest    */
```

## Thread safety

`compress` and `decompress` are stateless one-shot calls. They are
safe to call concurrently from multiple async tasks without any
additional synchronisation.

## Error behaviour

| Failure mode | Surface |
|---|---|
| libzstd not found or FFI not enabled | `RuntimeError` or `PermissionError` |
| Compression failure (libzstd error) | `RuntimeError` |
| Corrupt or truncated frame | `RuntimeError` |
| Frame without a known content size | `RuntimeError` |
