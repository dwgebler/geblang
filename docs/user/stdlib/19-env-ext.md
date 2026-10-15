# Environment and Extensions

## Dotenv

Import `dotenv` for `.env`-style environment files.

```gb
import dotenv;
import sys;
import io;

# Load a .env file and apply it to the process environment
dotenv.loadAndApply(".env");
io.println(sys.getenv("APP_ENV"));
```

### Functions

`dotenv.parse(text)` parses a `.env`-format string and returns a
`dict<string, string>`. Does not modify the process environment.

```gb
let vars = dotenv.parse("APP_ENV=production\nDEBUG=false\n");
io.println(vars["APP_ENV"]);   # production
```

`dotenv.load(path)` reads the file at `path` and parses it. Returns the same
dict as `parse`. Does not modify the process environment.

```gb
let vars = dotenv.load(".env.local");
```

`dotenv.apply(vars)` sets each key-value pair in a dict as a process
environment variable using `sys.setenv`. Does not read any files.

```gb
dotenv.apply({"PORT": "3000", "LOG_LEVEL": "debug"});
```

`dotenv.loadAndApply(path)` is shorthand for `dotenv.apply(dotenv.load(path))`.
This is the most common usage.

### `.env` file format

```
# Comments start with #
APP_ENV=production
DATABASE_URL=postgres://localhost/myapp
DEBUG=false
SECRET_KEY=abc123

# Values with spaces need quotes
WELCOME_MESSAGE="Hello, world!"
```

### Layered environments

```gb
# Apply defaults first, then override with local settings
dotenv.loadAndApply(".env");
if (io.exists(".env.local")) {
    dotenv.loadAndApply(".env.local");
}
```

### Requiring variables

Use `secrets.requireEnv` when a variable must exist:

```gb
import secrets;

let db = secrets.requireEnv("DATABASE_URL");   # throws if missing
```

---

## Secrets

Import `secrets` for safely accessing required environment variables and secret
files at startup.

```gb
import secrets;

let token  = secrets.requireEnv("API_TOKEN");     # throws if not set
let key    = secrets.getEnv("OPTIONAL_KEY");      # returns null if not set
let cert   = secrets.readFile("/run/secrets/tls.crt");   # read from filesystem
```

### Random values

```gb
let bytes  = secrets.randomBytes(32);         # returns bytes value
let n      = secrets.randomInt(1, 100);       # inclusive range int
let hex    = secrets.randomHex(16);           # 32-char hex string
let b64    = secrets.randomBase64(32);        # base64-encoded string
```

### Constant-time comparison

```gb
let equal = secrets.constantTimeEqual(submitted, stored);
```

Use `constantTimeEqual` when comparing authentication tokens or HMAC values to
prevent timing attacks.

---

## Extensions

Import `ext` to call managed or pre-started external processes over Geblang's
extension protocol. This is how you integrate Python libraries, Go binaries,
Node helpers, or any other runtime without building them into the VM.

For in-process calls into C-ABI shared libraries (libtorch, libsqlite,
libcurl, ...), see [Foreign Function Interface](ffi.html). FFI has lower
per-call latency than `ext` because it skips the IPC round-trip, but it
also runs the library in the script's process, so trust requirements are
higher.

### What the extension protocol is

An extension is a subprocess that communicates with the Geblang host over a
framed JSON request/response protocol on stdin/stdout. The extension protocol
is language-agnostic - any process that implements the wire format can act as
an extension.

See `docs/extension-protocol.md` for the wire format and `examples/extensions/`
for sample extension servers in Python and Go.

### Calling a pre-started extension

```gb
import ext;
import io;

let handle = ext.connect("/tmp/my-extension.sock");
# or, if the extension listens on a port:
# let handle = ext.connect("localhost:9100");

let result = ext.call(handle, "process_image", {
    "path":   "/tmp/photo.jpg",
    "width":  800,
    "height": 600
});

io.println(result["output_path"]);
ext.close(handle);
```

### Loading and managing an extension

`ext.load(config)` starts the extension process and returns a handle:

```gb
let handle = ext.load({
    "command": "python3",
    "args":    ["-m", "myextension"],
    "cwd":     "/opt/extensions"
});

let fns = ext.functions(handle);   # list of function names the extension exposes
io.println(fns);

let result = ext.call(handle, "summarize", {"text": longText});
ext.close(handle);
```

### Timeout and frame size

Use `ext.callWithOptions` when you need a per-call timeout or frame size limit.
Options are passed as a dict before the extension arguments:

```gb
let result = ext.callWithOptions(handle, "slow_op", {"input": data}, {
    "timeoutMs": 5000
});
```

`maxResponseBytes` limits how large any single response frame may be:

```gb
let result = ext.callWithOptions(handle, "generate", {"prompt": prompt}, {
    "timeoutMs":        10000,
    "maxResponseBytes": 4194304   # 4 MiB per frame
});
```

The runtime enforces a hard cap of 64 MiB per frame regardless of the per-call
setting. Responses that exceed the limit are surfaced as a runtime error.

---

### Binary data

Extensions support native binary frames so you can pass and receive `bytes`
values without base64 encoding. This is how image processing, audio, file
content, model embeddings, cryptographic material, and other binary payloads
travel efficiently between Geblang and an extension.

#### How the slot system works

The extension protocol carries binary data in *binary frames* that travel
alongside the JSON request and response. Each binary value gets a numbered
*slot*. Inside the JSON, binary values appear as a marker object:

```json
{"$type": "bytes", "slot": 0}
```

The actual bytes arrive in a separate binary frame immediately following the
JSON frame. If a call has three binary arguments they occupy slots 0, 1, and 2
and three binary frames follow the JSON request in slot order.

Geblang handles this automatically. On the Geblang side you just pass and
receive `bytes` values. The slot marshalling is invisible.

#### Passing binary to an extension

```gb
import ext;
import bytes;
import io;

let conn = ext.connect("127.0.0.1:9103");

# bytes values are transparently sent as binary frames
let raw = bytes.fromHex("0102ff");
let result = ext.call(conn, "process", raw);

# The returned bytes value comes back through a binary frame too
io.println(result.toHex());   # 0102ff (or whatever the extension returned)

ext.close(conn);
```

Multiple binary arguments each get their own slot:

```gb
let image = io.readBytes("/tmp/photo.jpg");
let mask  = io.readBytes("/tmp/mask.png");

let output = ext.call(conn, "composite", image, mask) as bytes;
io.writeBytes("/tmp/result.jpg", output);
```

Named-argument binary works exactly the same way:

```gb
let result = ext.call(conn, "resize",
    data:   io.readBytes("/tmp/photo.jpg"),
    width:  800,
    height: 600
);
```

#### Implementing binary in a Python extension

The `gebext.py` helper handles slot assembly and disassembly. Receive binary
arguments as `bytes` objects; return `bytes` to send a binary response:

```python
import gebext

def handle_call(fn, args, kwargs):
    if fn == "echo":
        # args[0] is already a bytes object - gebext decoded the slot marker
        data = args[0]
        assert isinstance(data, bytes)
        # Returning bytes causes gebext to send a binary frame response
        return data

    if fn == "process":
        image_bytes = args[0]           # bytes
        width       = kwargs["width"]   # int
        result = do_resize(image_bytes, width)
        return result   # bytes to binary frame

    raise ValueError(f"unknown function: {fn}")
```

The helper's `decode_value` converts `{"$type": "bytes", "slot": N}` markers
to `bytes` before calling your handler, and `encode_value` converts any `bytes`
in the return value back to a slot marker with the corresponding binary frame.

Multiple binary arguments arrive as separate `bytes` in `args`:

```python
if fn == "composite":
    image = args[0]   # bytes
    mask  = args[1]   # bytes
    return composite(image, mask)
```

#### Implementing binary in a Go extension

The `gebext.go` helper passes binary slots as `[][]byte`. Use `bytesArg` to
extract a slot from an argument:

```go
func handleCall(fn string, args []any, kwargs map[string]any, slots [][]byte) (any, [][]byte, error) {
    switch fn {
    case "echo":
        // args[0] is a {"$type":"bytes","slot":0} marker; bytesArg resolves it
        data, err := bytesArg(args[0], slots)
        if err != nil {
            return nil, nil, err
        }
        // Return the bytes in an output slot and a slot marker in the JSON
        outSlot := 0
        return map[string]any{"$type": "bytes", "slot": outSlot}, [][]byte{data}, nil

    case "composite":
        image, _ := bytesArg(args[0], slots)
        mask,  _ := bytesArg(args[1], slots)
        result := composite(image, mask)
        return map[string]any{"$type": "bytes", "slot": 0}, [][]byte{result}, nil
    }
    return nil, nil, fmt.Errorf("unknown function: %s", fn)
}
```

For multiple binary return values, add more entries to the output slice and
increment the slot index in each marker:

```go
case "split":
    data, _ := bytesArg(args[0], slots)
    left, right := splitHalf(data)
    return map[string]any{
        "left":  map[string]any{"$type": "bytes", "slot": 0},
        "right": map[string]any{"$type": "bytes", "slot": 1},
    }, [][]byte{left, right}, nil
```

#### Implementing binary in a Node extension

The `gebext.js` helper maps `Buffer` values to slot markers automatically.
Your handler receives binary arguments as `Buffer` objects and returns `Buffer`
to send binary responses:

```js
const gebext = require("./gebext");

async function handler(fn, args, kwargs) {
    if (fn === "echo") {
        // args[0] is a Buffer - gebext decoded the slot marker
        return args[0];                // Buffer to binary frame
    }
    if (fn === "composite") {
        const image = args[0];         // Buffer
        const mask  = args[1];         // Buffer
        return composite(image, mask); // Buffer to binary frame
    }
    throw new Error(`unknown function: ${fn}`);
}
```

#### Implementing binary in a PHP extension

The `gebext.php` helper uses a `"bytes:"` prefix convention to signal binary
return values. Binary arguments arrive as PHP strings containing raw bytes:

```php
function handle_call(string $fn, array $args, array $kwargs): mixed {
    if ($fn === 'echo') {
        // $args[0] is a raw PHP binary string (decoded from the binary frame)
        // Return with the "bytes:" prefix so gebext sends a binary frame
        return 'bytes:' . $args[0];
    }
    if ($fn === 'process') {
        $image = $args[0];   // raw binary string
        $result = do_process($image);
        return 'bytes:' . $result;
    }
    throw new RuntimeException("unknown function: $fn");
}
```

---

### Opaque extension handles

Extensions can return opaque handles - stateful resource references that live
inside the extension process and are only meaningful to it. A handle is a
`{"$type": "handle", "id": N}` marker in the JSON response:

```json
{"id": 1, "ok": true, "value": {"$type": "handle", "id": 42}}
```

On the Geblang side, an opaque handle is an ordinary value you can store and
pass back to the extension in later calls:

```gb
import ext;

let conn = ext.connect("127.0.0.1:9200");

# open() returns an opaque handle representing a resource inside the extension
let session = ext.call(conn, "open", "/data/large-model.bin");

# Pass the handle back to identify the resource in subsequent calls
let result1 = ext.call(conn, "infer", session, "What is 2+2?");
let result2 = ext.call(conn, "infer", session, "Summarize this document.");

# Release the resource when done
ext.call(conn, "close", session);
ext.close(conn);
```

The extension receives a handle argument as `{"$type": "handle", "id": N}` in
its args. Use the `id` field to look up the corresponding server-side resource
in a table keyed by handle id.

In Python:

```python
sessions = {}
next_id = 0

def handle_call(fn, args, kwargs):
    global next_id
    if fn == "open":
        path = args[0]
        sess = load_model(path)
        handle_id = next_id
        next_id += 1
        sessions[handle_id] = sess
        return {"$type": "handle", "id": handle_id}

    if fn == "infer":
        handle_id = int(args[0]["id"])   # args[0] is the raw marker dict
        sess = sessions[handle_id]
        return sess.run(args[1])

    if fn == "close":
        handle_id = int(args[0]["id"])
        sessions.pop(handle_id, None)
        return None
```

Handles and binary slots can be combined in the same call. A call that takes
an opaque session handle and a binary image payload:

```gb
let output = ext.call(conn, "apply_model", session, io.readBytes("/tmp/photo.jpg"));
```

---

### Use cases

- Calling a Python ML model from Geblang without rewriting it
- Passing raw image, audio, or file bytes to an extension without base64 overhead
- Keeping stateful extension resources (models, connections, caches) open across
  multiple calls using opaque handles
- Using a Go or Rust library that is not available as a Geblang native module
- Bridging to legacy code without refactoring it
