# Data Formats

Geblang includes native parsers and serializers for common data formats.

## JSON

Import `json`:

- `parse(text)` - parse JSON string into Geblang value
- `parseAs(text, ClassRef)` - parse and reconstruct a class instance
  (calls static `__deserialize(dict)` when defined, else the
  constructor matched on parameter names)
- `tryParse(text)` - parse without throwing; returns a result dict
  `{"ok": bool, "value": any, "error": any}` (on failure `ok` is
  `false`, `value` is `null`, and `error` describes the problem)
- `stringify(value)` - serialize to JSON string (accepts dicts, lists,
  scalars, and user-defined class instances; classes can override with
  `__serialize()`)
- `validate(text)` - returns `bool` (`true` when valid)
- `validateDetailed(text)` - returns `{"valid": bool, "error": string|null}` with detail
- `reader(source)` - streaming reader (see below)
- `stream(source, handler)` - streaming with a callback

```gb
import json;

let data = json.parse('{"name":"Ada","roles":["admin"]}');
io.println(data["name"]);                    # Ada
io.println(json.stringify({"ok": true}));    # {"ok":true}

let result = json.tryParse("not json");
if (!(result["ok"] as bool)) {
    io.println("invalid JSON: ${result["error"]}");
} else {
    io.println("parsed: ${result["value"]}");
}
```

`tryParse` never throws: it always returns a result dict. Check
`result["ok"]` before reading `result["value"]`. A `== null` guard
would never fire (the dict itself is never `null`), so failures would
pass silently.

## JSON Lines

Import `jsonl` for one complete JSON value per physical line. This format is
also called NDJSON. Unlike `json.reader`, it reads records rather than JSON
syntax events. Newline-delimited records work well for logs and exports.

| Function | Returns | Description |
|----------|---------|-------------|
| `reader(source, opts = {})` | `Reader` | Lazily reads records from text, bytes, or a readable stream. |
| `readAll(source, opts = {})` | `list<any>` | Reads every record into memory. |
| `writer(sink)` | `Writer` | Writes compact JSON records to a writable stream. |
| `stringify(values)` | `string` | Converts an iterable to newline-terminated JSON Lines text. |

`Reader.hasNext()` must be checked before `next()`: JSON `null` is a valid
record, so `null` cannot signal end of input. `next()` raises `ValueError` at
EOF. A `Reader` buffers at most one line beyond its current input chunk. The
default `maxLineBytes` is 1 MiB; set a positive limit in `opts` if needed.
Empty lines raise `ParseError` unless `skipBlank` is true. Errors include the
1-based physical line number. LF and CRLF are accepted, including a final
record without a newline. Invalid UTF-8 is rejected.

```gb
import jsonl;
import streams;
import io;

let source = streams.open("events.jsonl", "r");
defer source.close();
let records = jsonl.reader(source);
defer records.close();
while (records.hasNext()) {
    let event = records.next();
    io.println(event);
}
```

Pass text content directly for small inputs: `jsonl.readAll("1\nnull\n")`.
To read a path, open it explicitly with `streams.open(path)`; string arguments
are interpreted as content. The writer retries partial sink writes and emits
one `\n` after each value. `Reader.close()` and `Writer.close()` do not close
caller-provided streams. `Writer.close()` flushes its sink. The caller owns and
closes that sink.

For converting a CSV reader one row at a time:

```gb
import csv;
import jsonl;
import streams;

let input = streams.open("events.csv", "r");
let output = streams.open("events.jsonl", "w");
defer input.close();
defer output.close();
let rows = csv.reader(input);
let records = jsonl.writer(output);
defer rows.close();
defer records.close();
while (rows.hasNext()) {
    records.write(rows.next());
}
```

## YAML

Import `yaml`:

- `parse(text)`, `parseAs(text, ClassRef)`, `tryParse(text)` (returns a
  `{"ok", "value", "error"}` result dict; see JSON above), `stringify(value)`
- `validate(text)` - returns `bool`; `validateDetailed(text)` - returns detail dict
- `reader(source)`, `stream(source, handler)`

```gb
import yaml;

let cfg = yaml.parse("server:\n  port: 8080\n");
io.println(cfg["server"]["port"] as string);   # 8080
```

## TOML

Import `toml`:

- `parse(text)`, `parseAs(text, ClassRef)`, `tryParse(text)` (returns a
  `{"ok", "value", "error"}` result dict; see JSON above), `stringify(value)`
- `validate(text)` - returns `bool`; `validateDetailed(text)` - returns detail dict

TOML is typically configuration-sized and does not expose a streaming reader.

```gb
import toml;

let cfg = toml.parse('[database]\nurl = "postgres://localhost/app"\n');
io.println(cfg["database"]["url"]);
```

## XML

Import `xml`:

- `parse(text)`, `parseAs(text, ClassRef)`, `tryParse(text)` (returns a
  `{"ok", "value", "error"}` result dict; see JSON above), `stringify(value)`
- `validate(text)` - returns `bool`; `validateDetailed(text)` - returns detail dict
- `reader(source)`, `stream(source, handler)`

## HTML (1.28.0)

For parsing real-world HTML with the lenient HTML5 parser and querying it with
CSS selectors, use the `html` module. It has its own chapter: see
[HTML](36-html.md).

## Class instances: stringify and parseAs

`json.stringify`, `yaml.stringify`, and `toml.stringify` accept
user-defined class instances. By default the call emits the
**public** fields - any field whose name does not start with `_` or
`__`. Classes can replace the default by implementing
`__serialize()`; the return value is recursively serialised.

```gb
import json;

class Point {
    int x;
    int y;
    int _secret;
    func Point(int x, int y) { this.x = x; this.y = y; this._secret = 99; }
}

io.println(json.stringify(Point(3, 4)));
/* {"x":3,"y":4} - _secret is omitted. */
```

The companion `parseAs(text, ClassRef)` reconstructs an instance.
It calls a static `__deserialize(dict)` factory on the target
class when one is defined; otherwise it matches dict keys against
the constructor's parameter names and calls the constructor
positionally.

```gb
let p = json.parseAs("{\"x\":3,\"y\":4}", Point);
io.println(p.x);
```

Reconstruction is recursive: a field whose declared type is another
class (or a `list` / `dict` of one) is itself deserialized into an
instance, so a whole object tree comes back fully typed.

```gb
class Address { string postcode; }
class Order {
    string customer;
    Address shipTo;            # nested object
    list<Address> stops;       # list of objects
}

let o = json.parseAs(
    "{\"customer\":\"Ada\",\"shipTo\":{\"postcode\":\"EC1\"},\"stops\":[{\"postcode\":\"W1\"}]}",
    Order);
io.println(o.shipTo.postcode);   # EC1
io.println(o.stops[0].postcode); # W1
```

Fields typed `any` (or a primitive) keep their raw parsed value, a
class with `__deserialize` controls its own nested handling, and a
value whose shape does not match the field (for example a string where
an object is declared) is left as-is.

The same conventions apply to `yaml.parseAs`, `toml.parseAs`, and
`xml.parseAs`. See chapter 6's *Serialisation* section for details.

## CSV

Import `csv` for both in-memory parsing and streaming.

| Function | Returns | Description |
|----------|---------|-------------|
| `csv.parse(text, options?)` | `list<list<string>>` | Parses CSV text into a list of rows; each row is a list of cell strings. Options: `delimiter` (single char), `trimSpace` (bool). |
| `csv.parseDict(text, options?)` | `list<dict<string, string>>` | Parses CSV text using the first row as headers; returns a list of dicts keyed by header name. Same options. |
| `csv.stringify(rows, options?)` | `string` | Serialises a list-of-lists OR a list-of-dicts into CSV text. For a list of dicts, the first dict's keys become the header row; missing keys in later dicts produce empty cells. Options: `delimiter`. |
| `csv.reader(source)` | `CsvReader` | Returns an incremental pull reader over a file / bytes / string source. Use `.hasNext()`, `.next()`, `.close()`. |
| `csv.stream(source, handler)` | `void` | Push-based streaming with one callback per row. |

```gb
import csv;
import io;

# In-memory parse / stringify.
let rows = csv.parse("name,age\nAda,37\nGrace,55");
io.println(rows[1][0]); # Ada

let dicts = csv.parseDict("name,age\nAda,37\nGrace,55");
io.println(dicts[0]["age"]); # 37

# Custom delimiter + trimSpace.
let semi = csv.parse("a; b; c\n1; 2; 3", {"delimiter": ";", "trimSpace": true});

# Large file - streaming.
csv.stream(io.open("large.csv", "r"), func(list<string> row): void {
    io.println(row[0]);
});
```

## MessagePack (1.6.0)

The `msgpack` module encodes Geblang values to MessagePack 5 bytes
and back. The codec is hand-rolled (no Go dependency) and covers
the common cases: nil, bool, signed integers, float, str, bin,
array, and map. Ext types and the timestamp extension are not
supported in 1.6.0.

```gb
import msgpack;

let bytes = msgpack.encode({"items": [1, 2, 3], "ok": true});
let value = msgpack.decode(bytes);
```

### Functions

| Function | Returns | Description |
|----------|---------|-------------|
| `msgpack.encode(value)` | `bytes` | Serialises any encodable value. Throws on unsupported types or int values outside int64 range. |
| `msgpack.decode(bytes)` | `any` | Parses bytes into a Geblang value. Throws on malformed input or trailing bytes. |
| `msgpack.tryDecode(bytes)` | `?any` | Like `decode` but returns `null` instead of throwing on malformed input. |
| `msgpack.validate(bytes)` | `bool` | True when `bytes` is exactly one well-formed MessagePack value. |

### Type mapping

| Geblang | MessagePack |
|---------|-------------|
| `null` | nil (0xc0) |
| `bool` | false / true (0xc2 / 0xc3) |
| `int` (in int64 range) | int family (positive / negative fixint, int 8/16/32/64) |
| `float` | float 64 (0xcb) |
| `decimal` | str (encoded as the canonical decimal string for lossless round-trip) |
| `string` | str family (fixstr, str 8/16/32) |
| `bytes` | bin family (bin 8/16/32) |
| `list<T>` | array family (fixarray, array 16/32) |
| `dict<K, V>` | map family (fixmap, map 16/32) |

`decimal` values round-trip as MessagePack strings because the
spec has no decimal type. Round-tripping `1.50` yields the string
`"1.5000000000"`; cast with `(value as string) as decimal` to
recover the typed Decimal.

### Limitations

- Ext types are not encoded or decoded. Decoding an ext-tagged
  value raises an error.
- The timestamp extension (ext type -1) is not recognised.
- `int` values whose magnitude exceeds 64 bits raise on encode.

## Streaming readers

`json.reader`, `yaml.reader`, and `xml.reader` return a token/event stream
for large or unbounded documents. Each call to `next()` returns an event dict
`{"type": string, "value": any}` or `null` at EOF. The reader also exposes
`hasNext()` and `close()`.

Event types for JSON: `startObject`, `endObject`, `startArray`, `endArray`,
`key`, `value`, `error`. Structure events (`startObject` etc.) have `null`
as their value; `key` carries a string; `value` carries the parsed scalar.

```gb
import json;

let r = json.reader("[1, 2, 3]");
let ev = r.next();
while (ev != null) {
    io.println(ev["type"] as string);
    ev = r.next();
}
r.close();
# startArray
# value
# value
# value
# endArray
```

Pull-style with `hasNext()`:

```gb
import json;

let r = json.reader('{"a": 1}');
while (r.hasNext()) {
    let ev = r.next();
    io.println(ev["type"] as string);
}
r.close();
```

The reader accepts a `string`, `bytes`, or an open `IOStream` (`io.open(...)`)
as its source.

## Serde - format-agnostic serialization

Import `serde` when the format is runtime-determined (for example, a tool
that accepts a `--format json|yaml|toml` flag):

```gb
import serde;

let value = serde.parse("json", '{"ok":true}');
let text  = serde.stringify("yaml", value);
```

Supported format strings: `"json"`, `"yaml"`, `"toml"`.

## Schema validation

Import `schema` for JSON Schema-style validation of dicts and lists. This is
useful for validating API request bodies, configuration files, and parsed
data before using it.

```gb
import schema;

let userSchema = {
    "type": "object",
    "required": ["name", "email"],
    "properties": {
        "name":  {"type": "string"},
        "email": {"type": "string"},
        "age":   {"type": "number", "minimum": 0, "maximum": 150},
        "roles": {
            "type": "array",
            "items": {"type": "string"}
        }
    }
};

let result = schema.validate({"name": "Ada", "email": "ada@example.com"}, userSchema);
if (result["valid"] as bool) {
    io.println("valid");
} else {
    for (err in result["errors"]) {
        io.println(err);
    }
}
```

`schema.validate(value, schema)` returns:
- `{"valid": true, "errors": []}` on success (the `errors` list is always
  present and empty when valid)
- `{"valid": false, "errors": list<string>}` on failure, with one message per violation

### Common schema keywords

| Keyword         | Applies to      | Description                                         |
|-----------------|-----------------|-----------------------------------------------------|
| `type`          | any             | `"string"`, `"number"`, `"bool"`, `"object"`, `"array"`, `"null"` |
| `required`      | object          | list of required property names                     |
| `properties`    | object          | map of property name to sub-schema                   |
| `items`         | array           | sub-schema applied to each element                  |
| `minimum`       | number          | inclusive lower bound                               |
| `maximum`       | number          | inclusive upper bound                               |
| `minLength`     | string          | minimum character count                             |
| `maxLength`     | string          | maximum character count                             |
| `minItems`      | array           | minimum element count                               |
| `maxItems`      | array           | maximum element count                               |
| `enum`          | any             | value must be one of the listed values              |
| `nullable`      | any             | allow `null` in addition to the declared type       |

Pair `schema.validate` with `json.parse` or `yaml.parse` to validate
untrusted external input:

```gb
let body = json.parse(io.stdinReadAll());
let check = schema.validate(body, requestSchema);
if (!(check["valid"] as bool)) {
    throw errors.new("ValidationError", "invalid request body");
}
```
