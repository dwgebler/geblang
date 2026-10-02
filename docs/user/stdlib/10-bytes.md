# Bytes, Encoding, And Compression

## Bytes

Import `bytes`:

- `fromString(text)`, `toString(bytes)`
- `rawString(bytes)` returns a byte-preserving string without UTF-8 validation
- `fromList(list<int>)` - builds bytes from a list of byte values
  (0-255); rejects out-of-range values
- `fromHex(text)`, `toHex(bytes)`
- `fromBase64(text)`, `toBase64(bytes)`
- `fromBase64Url(text)`, `toBase64Url(bytes)` - unpadded URL-safe
  base64 (RFC 4648 section 5; the variant JWT/JOSE uses). The
  decoder accepts both padded and unpadded input.
- `concat(left, right)` - concatenates two `bytes` values; call repeatedly or
  use `list.reduce` to join many buffers

Bytes values also expose `b.toHex()`, `b.toBase64()`, and
`b.toBase64Url()` as methods, equivalent to the module helpers.
`b.toList()` returns the byte values as a `list<int>` (the inverse of
`bytes.fromList`); `b.get(i)` / `b[i]` reads a single byte value.
`bytes.toString(data)` and `data.toString()` decode valid UTF-8 and throw a
`RuntimeError` when the bytes are not valid UTF-8.

`b.contains(n)` tests for a single BYTE VALUE (an int 0-255), not a
sub-sequence: `bytes.fromString("hi").contains(104)` is `true`
because `0x68` (`h`) appears. To search for a byte sub-sequence,
convert with `toString` and use string `contains`, or compare
slices.

```gb
import bytes;

let data = bytes.fromString("hello");
io.println(bytes.toHex(data));
io.println(bytes.toString(data));
```

## Encoding

Import `encoding` for transport encodings and HTML/URL escaping. The guiding
rule: `encoding.*` is **text-oriented** (works in `string`s), while the
`bytes.*` methods are **binary-oriented** (work in `bytes`).

Base64:

- `base64Encode(value)`, `base64Decode(text)` - standard Base64. Encode accepts
  a `string` or `bytes`; decode returns a `string`.
- `base64UrlEncode(value)`, `base64UrlDecode(text)` - unpadded URL-safe Base64
  (RFC 4648 section 5; matches JWT/JOSE). Encode accepts a `string` or `bytes`;
  decode returns a `string` and accepts padded or unpadded input.

```gb
import encoding;

let token = encoding.base64UrlEncode("user:42");      # string -> string
let back  = encoding.base64UrlDecode(token);          # back == "user:42"
```

For binary payloads, decode through the `bytes` module so the result stays
binary: `bytes.fromBase64(s)` and `bytes.fromBase64Url(s)` (the inverses of
`b.toBase64()` / `b.toBase64Url()`).

Other base encodings (binary-oriented - decoders return `bytes`):

- `base32Encode(value)`, `base32Decode(text)` - RFC 4648 standard alphabet,
  accepts padded or unpadded input on decode.
- `base58Encode(value)`, `base58Decode(text)` - Bitcoin/IPFS alphabet (no `0`,
  `O`, `I`, `l`); preserves leading zero bytes by emitting leading `1`s.

`base32Encode` / `base58Encode` (like the base64 encoders) accept either a
`string` or `bytes`, so they compose with random material from
`secrets.randomBytes`:

```gb
import secrets;

let totpSecret = encoding.base32Encode(secrets.randomBytes(20));
let walletId   = encoding.base58Encode(secrets.randomBytes(16));
```

URL and HTML escaping (all string -> string):

- `urlEncode(text)`, `urlDecode(text)` - percent-encoding for query components.
- `htmlEscape(text)`, `htmlUnescape(text)` - escape/unescape HTML entities.
  Use `htmlEscape` to make untrusted text safe to drop into HTML: it turns
  `<b>` into `&lt;b&gt;` so nothing renders as markup.
- `sanitizeHtml(html)` - when you must render untrusted *HTML* (a rich-text
  comment, say) rather than show it as text, this strips dangerous content
  against a safe allow-list (keeps common formatting tags like `<b>`, `<a>`,
  `<p>`; removes `<script>`/`<style>`, `on*` event handlers, and unsafe URL
  schemes). Escaping neutralizes all markup; sanitizing keeps a safe subset.

```gb
encoding.htmlEscape("<b>hi</b>");                       # "&lt;b&gt;hi&lt;/b&gt;"
encoding.sanitizeHtml("<b>hi</b><script>x()</script>"); # "<b>hi</b>"
```

Use this module for transport encodings and escaping, not password hashing or
cryptographic operations.

## Binary

Import `binary` for Python `struct`-style packing of typed values
into a byte buffer:

- `binary.pack(format, ...values)` returns `bytes`.
- `binary.unpack(format, data)` returns a `list<any>` of values.
- `binary.unpackNamed(spec, data)` returns a `dict<string, any>`;
  `spec` is a `list` of `{"name": string, "type": string}` dicts.
- `binary.size(format)` returns the number of bytes the format
  consumes, useful for buffer sizing.

The first character of the format may set endianness: `>` big,
`<` little, `!` network (= big), `=` host native. The default
is big-endian. Per-field codes:

| Code | Type             | Bytes |
|------|------------------|-------|
| `b`  | int8 (signed)    | 1     |
| `B`  | uint8            | 1     |
| `h`  | int16            | 2     |
| `H`  | uint16           | 2     |
| `i`  | int32            | 4     |
| `I`  | uint32           | 4     |
| `q`  | int64            | 8     |
| `Q`  | uint64           | 8     |
| `f`  | float32          | 4     |
| `d`  | float64          | 8     |
| `Ns` | N-byte string    | N     |
| `Nx` | N pad bytes      | N     |

A leading digit before a non-`s`/`x` code repeats it (`4I` is
shorthand for `IIII`); each repeat takes its own positional
argument.

```gb
import binary;
import bytes;

let header = binary.pack(">IHB", 0xDEADBEEF, 1024, 7);
io.println(bytes.toHex(header));        /* deadbeef040007 */

let parts = binary.unpack(">IHB", header);
io.println(parts);                      /* [3735928559, 1024, 7] */

let labelled = binary.unpackNamed([
    {"name": "magic",   "type": ">I"},
    {"name": "size",    "type": "H"},
    {"name": "version", "type": "B"}
], header);
io.println(labelled["magic"]);          /* 3735928559 */
```

Unsigned 64-bit values whose high bit is set are returned as a
big-int (`Int`) on unpack so the value round-trips losslessly;
pack accepts either `int` form.

## Compression

Import `compress`:

- `gzip(value)`
- `gunzip(bytes)`

```gb
import bytes;
import compress;

let packed = compress.gzip(bytes.fromString("payload"));
io.println(bytes.toString(compress.gunzip(packed)));
```

## Archives

Import `archive` for zip and tar archive reading and writing.
The original eager functions remain available for small archives. Use the
file-backed API below when archive entries may be large or when extracting
untrusted archives.

- `archive.zipRead(bytes)` and `archive.zipWrite(entries)`.
- `archive.tarRead(bytes)` and `archive.tarWrite(entries)`.
- `archive.tarGzRead(bytes)` and `archive.tarGzWrite(entries)`
  for the common `.tar.gz` / `.tgz` shape.

Each reader returns a `list<dict<string, any>>` whose dicts
carry `name` (string), `data` (bytes), `isDir` (bool), and
`size` (int).  Each writer accepts the same shape; the `data`
field may be a `string` or `bytes`. Tar writers sort entries by
name so output is deterministic.

```gb
import archive;
import bytes;

let raw = archive.zipWrite([
    {"name": "hello.txt", "data": "hello world"},
    {"name": "nested/inside.txt", "data": "nested"}
]);

let entries = archive.zipRead(raw);
for (e in entries) {
    io.println(e["name"] as string);
    io.println(bytes.toString(e["data"] as bytes));
}

let tgz = archive.tarGzWrite([
    {"name": "config.toml", "data": "key = \"value\""}
]);
let configEntries = archive.tarGzRead(tgz);
```

### Streaming archives

`archive.open(path, format = "auto")` opens a zip, tar, or tar.gz file. The
`ArchiveReader.next()` cursor returns an `ArchiveEntry` or `null` at EOF. An
entry has `name`, `size`, and `isDir` fields and `readBytes(n)` and `close()`
methods. Only one entry is active: `next()` closes the previous one, including
any unread data. Close the reader when finished. Zip input must be a seekable
file. Tar and tar.gz advance sequentially. Auto detection reads the file
header.

`archive.create(path, format)` creates a streaming writer.
`addFile(name, source)` accepts bytes, a file path, or a binary stream with `__readBytes(n)`
or `readBytes(n)`. Streams are spooled to a temporary file so tar can record
the entry size without retaining the entire input in memory. `addDir(name)`
adds a directory. Call `close()` to finalize the archive; repeated close is
safe. Entry names must be relative forward-slash paths without traversal.

```gb
import archive;
import bytes;

let writer = archive.create("backup.tar.gz", "tar.gz");
writer.addDir("data");
writer.addFile("data/settings.bin", bytes.fromHex("00ff"));
writer.close();

let reader = archive.open("backup.tar.gz");
while (true) {
    let entry = reader.next();
    if (entry == null) { break; }
    io.println(entry.name);
    entry.close();
}
reader.close();
```

`archive.extract(path, destination, opts = {})` returns the extracted relative
paths. Options are `maxEntries` (default 10000), `maxEntryBytes` (64 MiB),
`maxTotalBytes` (512 MiB), `overwrite` (default false), and `filter` (a
callable receiving `{"name", "size", "isDir"}` metadata). Limits are positive.
The extractor rejects absolute and traversal names, symlink and hardlink
entries, and paths through existing symlinks. It checks actual decompressed
bytes. New files use owner-only permissions and newly created directories use
owner-only traversal permissions; archive modes and timestamps are ignored. If an entry
fails, its partial file is removed. Earlier completed entries remain.

Reader errors (corrupt or non-archive bytes) and writer errors
(missing `name` / `data` field) throw catchable runtime
exceptions, so callers can wrap untrusted input in `try` / `catch`.
