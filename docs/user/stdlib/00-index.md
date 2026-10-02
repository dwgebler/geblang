# Standard Library Overview

Geblang's standard library is intentionally broad enough for everyday work, but
not designed as a monolithic application framework. It should give you the
building blocks for CLI tools, scripts, APIs, SSR web apps, background jobs,
data processing, integration code, and small framework layers without forcing a
single style of application structure.

The library has two implementation layers:

- Native modules are implemented in Go and are available in normal Geblang
  builds. These cover host integration, runtime-sensitive behavior, networking,
  parsing, database drivers, bytecode/VM parity surfaces, and value classes
  that need native state.
- Source modules are written in Geblang and distributed under `stdlib/`. These
  provide higher-level composition, convenience wrappers, framework-style web
  helpers, and APIs that can evolve in userland.

Both layers are imported the same way. Modules are not injected into the global
namespace, so programs should import the capabilities they use:

```gb
import io;
import json;
import async.io as aio;
import web.http as wh;
import web.router as router;
```

Most modules use typed values for stable concepts and plain dictionaries or
lists at dynamic boundaries such as parsed JSON, request metadata, validation
errors, and driver options. Application code can keep those structures dynamic
or convert them into typed classes when the shape matters.

## Module Families

Use the pages below as the user-facing reference. Each page explains the main
modules in that area, important value types, common calls, and executable-style
examples.

Core runtime and host integration:

- [I/O And Filesystem](io.html): standard streams, text and binary file helpers,
  file handles, directories, and filesystem operations.
- [System, Environment, And Processes](sys.html): environment variables,
  process metadata, permissions, temp files, subprocesses, and shell integration.
- [Subprocess Streaming](proc.html): `proc.spawn` with concurrent stdin /
  stdout / stderr streams, signals, and PTY support.
- [Paths](path.html): path manipulation and object-oriented path helpers
  (functional `path` plus the class-based `pathlib`).
- [Watch](watch.html): filesystem watching.

Concurrency and streaming:

- [Async](async.html): tasks, `await`, scheduling helpers, and async program
  structure.
- [Async I/O](async-io.html): async file, HTTP, stream, socket, and parser
  helpers for event-loop-style programs.

Data and transformation:

- [Data Formats](data-formats.html): JSON, YAML, TOML, XML, CSV, serde, and
  stream interfaces.
- [Collections](collections.html): list, dict, set, range, and higher-level
  collection algorithms.
- [Text, Regex, Markdown, And Templates](text.html): strings, regex, markdown
  rendering/parsing, and template helpers.
- [HTML](html.html): lenient HTML5 parsing and CSS-selector queries for
  scraping and data extraction.
- [Bytes, Encoding, And Compression](bytes.html): bytes, base encodings,
  checksums, hashes, and compression.
- [Math, Dates, And UUIDs](math-datetime.html): numeric helpers, time values,
  durations, zones, and UUID generation/parsing.
- [Localization And Translation](localization.html): explicit-locale number,
  currency, and date formatting, collation, and message catalogs.
- [Security](security.html): secrets, cryptographic helpers, passwords,
  certificates, keys, and CSRs.

Application building:

- [CLI](cli.html): command-line arguments, prompts, masked input, terminal
  output, progress UI, and command helpers.
- [HTTP, Networking, And WebSockets](http-net.html): HTTP client/server,
  sockets, networking helpers, WebSocket client/server support, and SSE
  primitives.
- [TCP/TLS Sockets](sockets.html): `sockets.dial` / `sockets.serve` for
  stream-protocol-shaped TCP and TLS clients and servers.
- [SSH Client](ssh.html): connect with password / key / agent auth, run
  commands, stream sessions, transfer files via SFTP, and forward ports.
- [Mailer And SMTP](mailer-smtp.html): mail messages, MIME parsing,
  attachments, and SMTP delivery.
- [Web Modules](web-router.html): request/response wrappers, routing,
  decorators, middleware, sessions, cache/auth/form helpers, SSE, and web
  testing support.

Infrastructure and data stores:

- [Data Stores](data-stores.html): overview for persistence, cache,
  configuration, and schema validation.
- [Database](data-stores/database.html): PDO-style database wrapper with SQLite,
  PostgreSQL, and MySQL connection examples.
- [Redis](data-stores/redis.html): Redis strings, hashes, lists, sets, expiry,
  counters, and connection handling.
- [Config](data-stores/config.html): layered configuration loading and dotted
  path access.
- [Schema](data-stores/schema.html): validation primitives and schema helpers.

Operations, introspection, and extension:

- [Logging](logging.html): structured logging to stdout, stderr, files,
  streams, syslog, or custom handlers.
- [Observability](observability.html): metrics, tracing, and profiling.
- [Reflection And Testing](reflect-test.html): runtime metadata, docblocks,
  decorators, function/class/module reflection, and class-based testing.
- [Environment and Extensions](env-ext.html): dotenv, external extension
  processes, extension protocol calls, and binary frame handling.
- [Foreign Function Interface](ffi.html): in-process calls into C-ABI shared
  libraries (libtorch, libsqlite, libcurl, ...) with capability gating.
- [LLM](llm.html): provider-agnostic client for chat completions, embeddings,
  image analysis, and image generation across OpenAI, Anthropic, and AWS
  Bedrock.
- [Vector Search and RAG](vectorstore-rag.html): in-memory and SQLite-backed
  vector stores with similarity search, plus retrieval-augmented-generation
  helpers (chunking, indexing, retrieval, prompt-context assembly).
- [Local Models](onnx-transformers.html): offline WordPiece tokenization, ONNX
  model inference, and on-device sentence-transformer embeddings (experimental;
  needs ONNX Runtime and the `--allow-onnx` flag).
- [Headless Browser](browser.html): drive a headless Chrome over the DevTools
  Protocol for functional testing and scripted control - navigate, interact,
  read, screenshot, cookies, request interception (experimental; needs Chrome
  and the `--allow-browser` flag).
- [Zstd Compression](clib-zstd.html): fast Zstandard compression and
  decompression via libzstd, with FFI capability gating.
- [systemd Integration](clib-systemd.html): sd_notify readiness protocol
  (READY, WATCHDOG, STATUS) and structured journald logging. Linux only.
- [File Type and MIME Detection](clib-magic.html): content-based file type
  and MIME detection via libmagic, accurate even when extensions are missing.
- [Terminal UI](clib-curses.html): full-screen terminal UI via libncurses -
  window init, cursor movement, keyboard input, and colour/attribute control.
- [Image](image.html): portable native raster-image module (PNG, JPEG, GIF,
  WebP decode; PNG/JPEG/GIF encode); load, blank, resize, crop, rotate, and
  encode with no system library required.
- [ndarray](ndarray.html): N-dimensional numeric arrays - elementwise
  arithmetic with broadcasting, zero-copy views, reductions, linear algebra,
  and seeded random generation.
- [dataframe](dataframe.html): columnar data frames - typed columns with null
  masks, expression filtering, grouping and joins, and CSV/JSON/SQL IO; numeric
  columns bridge to ndarray.
- [stats](stats.html): probability distributions as objects - pdf, cdf, ppf,
  moments, and seeded sampling for 12 distributions (normal, uniform,
  exponential, gamma, beta, chiSquared, studentT, f, lognormal, weibull,
  binomial, poisson) - plus hypothesis tests, confidence intervals, and
  linear regression.
- [physics](physics.html): physical constants as zero-argument functions (speed
  of light, gravitational constant, Planck, Boltzmann, Avogadro, and more) and
  `convert` for unit conversion across length, mass, time, and temperature.
- [complex](complex.html): complex number type with constructors (`of`,
  `fromPolar`), a full method set (`re`, `im`, `abs`, `arg`, `conj`, `neg`,
  `exp`, `sqrt`, arithmetic), and operator overloads (`+`, `-`, `*`, `/`, `**`,
  unary `-`, `==`).
- [geo](geo.html): geodetic calculations on the sphere - great-circle distance
  (Haversine), initial bearing, midpoint, and destination point. Coordinates in
  decimal degrees; distance unit is km by default.
- [Utility Modules](utilities.html): miscellaneous result, option, and helper
  modules that do not fit one larger category.

Generated source API pages are available under the API section of the site. They
are useful for quick exported-signature lookup, but the module reference pages
above are the primary documentation because they include behavior notes and
examples.

## Import And Resolution Notes

Native modules are always available in a normal Geblang binary. Source modules
resolve from the bundled stdlib, package roots, and `GEBLANG_STDLIB`.

Built-in module names (native and stdlib) are reserved: a program or package may
not declare a module with one of these names, and built-in names always resolve
to the built-in identically on both backends. Use `import geblang.X` to refer to
a built-in explicitly. See "Reserved built-in module names" in the Modules and
Packages chapter for details.

Prefer explicit aliases when a module name would otherwise be noisy or collide
with local names:

```gb
import web.http as wh;
import async.io as aio;
import testing.assertions as assert;
```

Top-level code in source modules runs when the module is imported. Stdlib
modules avoid side effects except where the module is explicitly about host
integration, such as environment or extension loading. Application modules
should follow the same rule: export functions/classes for reusable behavior and
keep executable startup code in scripts or command entry points.
