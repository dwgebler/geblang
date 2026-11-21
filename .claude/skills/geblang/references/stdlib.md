# Geblang standard library map

A map of what each module is for. It is NOT a signature reference - module
surfaces change between versions. For exact signatures use the installed
toolchain:

- `geblang doc <module-or-file>` prints signatures.
- In the REPL or a script, `dir(moduleName)` lists members and `typeof(x)` /
  `dump(x)` inspect values.

Import a module before using it (`import io;`); using a module name as a
selector base without importing it is a semantic error.

## Core runtime and OS

| Module | Purpose |
|---|---|
| `io` | stdin/stdout, files (`open`, `readText`, `writeText`, `readBytes`), `exists`, `tempDir`, random access (`seek`, `tell`, `truncate`, `atEnd`), `copy`/`copyTree`/`move`/`scanDir`/`touch`/`writeTextAtomic`, `stat`/`lstat`. |
| `file` | `File` object (`file.open(path, mode)`) wrapping a handle with method-style read/write/seek, `with`-block auto-close, line iteration. |
| `sys` | env vars, `args`, `cwd`, `exit`, `platform`/`arch`/`osVersion`, `onSignal`/`clearSignal`/`raise`, `goroutineId`. |
| `path` / `pathlib` | `join`, `dir`, `base`, `ext`, `abs`; object-style paths. |
| `process` | own identity (`pid`, `ppid`, `uid`, `gid`, `euid`, `egid`, `groups`); inspect others (`list`, `info`, `exists`); gated control (`setuid`, `setgid`, `kill`, `signal`) behind `--allow-process-control`. |
| `watch` | filesystem change notifications. |
| `time` | `now`, `sleep`, `monotonicNs`. |
| `datetime` | `parse`, `format`, `Instant`, zones, arithmetic. |

## Data formats and encoding

| Module | Purpose |
|---|---|
| `json`, `yaml`, `toml`, `xml`, `csv`, `msgpack` | parse / stringify; insertion-ordered. `parseAs` reconstructs typed (and nested-class) values. |
| `encoding` | base64, hex, URL encoding. |
| `bytes` | hex/base64, `concat`, `slice`. |
| `strings` | `compare`, `equalsFold`, `fromCodePoint`, splitting helpers. |
| `unicode` | code-point / category helpers. |
| `re`, `pcre` | regex (Go engine / PCRE-style). `re.compile(p)` / `pcre.compile(p, flags)` return a reusable `Pattern`. |

## Security

| Module | Purpose |
|---|---|
| `crypt` | AES, HMAC, SHA, bcrypt password hashing, JWT (`jwtSign(p, k, {alg})`, `jwtVerify`), JWK/JWKS (`jwk`, `jwks`), X.509 chain support (`verifyCertChain`, `parseCert`, `asn1Decode`). |
| `secrets` | constant-time compare, random tokens, `requireEnv(name)` (throws when unset) / `getEnv`. |
| `dotenv` | `.env` files: `load`, `parse`, `apply`, `loadAndApply(path)`; applying overwrites variables already set. |

## Networking and data stores

| Module | Purpose |
|---|---|
| `http` | client (`get`, `post`, `request(url)` fluent builder, `fetchAll`, `requestStream`/`fetchStream` for streaming) and server (`serve`, `listen`, `wait`, `shutdown`, TLS, autocert, `maxBodyBytes`). |
| `sockets`, `net` | TCP/UDP, low-level networking. |
| `ssh` | SSH client. |
| `db` | SQL connect/query/exec; sqlite, postgres, mysql; transactions, prepared statements, streaming `Rows` cursor (`for (row in rows)`), pool options. `conn.queryOne(sql, ...)` returns the first row or `null` and releases the connection; `conn.migrate([{"id": ..., "sql": ...}])` applies each migration once in one transaction. Close a cursor you stop reading early. |
| `config` | layered configuration from files and the environment with typed binding. |
| `jobs` | durable SQL-backed job queue: retries, leases, scheduling, dead letters, worker processes. |
| `redis` | client incl. pub/sub. |
| `messaging` | RabbitMQ / Kafka / SQS / STOMP producers and consumers. |
| `store` | synchronised cross-request / cross-goroutine state. |

## Concurrency and collections

| Module | Purpose |
|---|---|
| `async` | tasks, channels (`async.channel`), `select`, worker pools; `async.tasks` adds `map`/`forEach`/`retry`/`settle`/`any`/`parallel`. |
| `collections` | `maxBy`, `groupBy`, `chunk`, `sortBy`, lazy `lazyMap`/`lazyFilter`, `range`. |
| `streams` (seq) | lazy fluent collection pipelines (`Stream`). |
| `strbuilder` | O(n) string accumulation. |
| `money` | immutable currency amounts (`Money("12.34", "GBP")`), explicit rounding, total-preserving `split`/`allocate`. |

## Numerics and data science

| Module | Purpose |
|---|---|
| `math` | trig, log, floor/ceil/round, prime tests, and special functions (gamma family, error functions, Bessel). |
| `ndarray` | N-dimensional numeric arrays: constructors, broadcasting arithmetic, views, reductions, linear algebra (`matmul`, `solve`, `inv`, `det`), seeded random. |
| `dataframe` | columnar frames: typed columns + null masks; expression `filter`/`withColumn`, `filterFn(row -> bool)`, `groupBy().agg()`, joins, `pivot`, CSV/JSON/SQL IO. |
| `vecmath` | vector ops: `normalize`, `topK`, `semanticSearch`. |

## AI / RAG (source-backed)

| Module | Purpose |
|---|---|
| `llm` | provider-neutral chat/embed client (OpenAI, Anthropic, Bedrock): `chat`, `chatStream`, tool calling, `embed`/`embedBatch`, `models`. |
| `vectorstore` | vector index/retrieve (incl. pgvector). |
| `rag` | retrieval-augmented pipelines; `LocalEmbedder` for on-device embeddings. |
| `transformers`, `onnx` | local model inference (WordPiece tokenize, ONNX sessions), gated behind `--allow-onnx`. |

## Observability, CLI, media, FFI

| Module | Purpose |
|---|---|
| `profiler` | `timer()`, `profile()` context managers; CPU/memory/wall. |
| `image` | decode/encode PNG/JPEG/GIF/WebP, resize, crop, rotate. |
| `cli`, `cli.widgets` | terminal prompts (`choose`, `multiChoose`), `io.withStdin` for testable prompts, TUI widgets. |
| `browser` | headless Chrome/Chromium automation over DevTools (experimental), gated behind `--allow-browser`. |
| `ffi`, `clib.*` | call C libraries (zstd, magic, ncurses, systemd); gated behind `--allow-ffi`. |
| `errors` | `wrap`, `is`, `stackTrace`, `frames`. |
| `reflect` | class / function / decorator metadata. |
| `test` | the test framework (see `testing.md` / the toolchain reference). |

## Gating note

Privileged native operations are default-deny and require an opt-in launch flag
(or a `geblang.yaml` `permissions:` entry for built binaries): `--allow-ffi`,
`--allow-process-control`, `--allow-onnx`, `--allow-browser`. A gated call
without permission throws `PermissionError`.
