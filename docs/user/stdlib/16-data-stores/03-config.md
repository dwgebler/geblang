# Config

The `config` source-stdlib module provides helpers for nested configuration
dictionaries. It is useful for merging defaults, environment-specific override
layers, parsed config files, and application settings passed into framework
components.

```gb
import config;

let cfg = config.layer([
    {"debug": false, "db": {"host": "localhost", "port": 5432}},
    {"debug": true, "db": {"database": "app"}}
]);

io.println(config.require(cfg, "db.host"));
io.println(config.getOr(cfg, "cache.ttl", 300));
```

## Functions

| Function | Returns | Description |
|----------|---------|-------------|
| `clone(data)` | `dict<string, any>` | Deep-copy a configuration dictionary and its nested lists |
| `merge(base, overrides)` | `dict<string, any>` | Recursively merge overrides into a copy of base |
| `defaults(values, defaultValues)` | `dict<string, any>` | Apply defaults first, then explicit values |
| `layer(layers)` | `dict<string, any>` | Merge a list of dictionaries from first to last |
| `has(data, path)` | `bool` | Test whether a dotted path exists |
| `get(data, path)` | `any` | Return a required dotted-path value or throw `ValueError` |
| `getOr(data, path, fallback)` | `any` | Return a dotted-path value or fallback |
| `require(data, path)` | `any` | Alias for `get` when the call site wants explicit required semantics |
| `parse(format, text)` | `Config` | Parse a serde-supported document into a `Config` object |
| `load(options)` | `Config` | Load layered files, explicit environment bindings, and overrides |
| `validate(options)` | `dict<string, any>` | Load and report `{valid, errors, config}` without throwing for schema errors |

`merge` is recursive only when both sides at a key are dictionaries. Otherwise
the override value replaces the base value.

```gb
let base = {
    "app": {"name": "Geb", "debug": false},
    "cache": {"ttl": 60}
};

let local = {
    "app": {"debug": true},
    "cache": {"driver": "redis"}
};

let merged = config.merge(base, local);
io.println(merged["app"]["name"]);
io.println(merged["app"]["debug"]);
io.println(merged["cache"]["driver"]);
```

## Dotted Paths

Dotted paths walk nested dictionaries. They do not parse list indexes.

```gb
let settings = {
    "db": {
        "primary": {
            "host": "localhost",
            "port": 5432
        }
    }
};

io.println(config.has(settings, "db.primary.host"));
io.println(config.get(settings, "db.primary.port"));
io.println(config.getOr(settings, "db.replica.host", "none"));
```

`get` and `require` throw `ValueError` when any path segment is missing.
`getOr` returns the fallback if a segment is missing or a non-dictionary value is
encountered before the end of the path.

## `Config`

`Config` is an immutable-style wrapper around a cloned dictionary.

| Method | Returns | Description |
|--------|---------|-------------|
| `has(path)` | `bool` | Test whether a dotted path exists |
| `get(path)` | `any` | Return a required dotted-path value |
| `require(path)` | `any` | Alias for `get` |
| `getOr(path, fallback)` | `any` | Return a value or fallback |
| `toDict()` | `dict<string, any>` | Return a deep copy of the stored config |

```gb
let cfg = config.Config({
    "mail": {"from": "noreply@example.com"}
});

io.println(cfg.require("mail.from"));
io.println(cfg.getOr("mail.transport", "smtp"));
```

## Parsing

`config.parse(format, text)` delegates to `serde.parse`, so it supports the
formats available through the serialization modules, such as `json`, `yaml`,
`toml`, and `xml` where the parsed top-level value is an object.

```gb
let cfg = config.parse("json", '{"server":{"port":8080}}');
io.println(cfg.require("server.port"));
```

Use `Config.toDict()` when an API expects a plain dictionary:

```gb
let options = cfg.toDict();
```

## Layered loading

`config.load(options)` accepts the following keys. Unknown keys are errors.

| Key | Value |
| --- | --- |
| `baseDir` | Base directory for relative paths; defaults to the current working directory |
| `defaults` | Base dictionary; defaults to `{}` |
| `files` | Ordered list of `{path, format?}` entries; `format` is `json`, `yaml`, or `toml` and defaults to the file extension |
| `dotenvFiles` | Ordered list of paths parsed as environment data |
| `env` | Mapping from config dotted paths to `{name, type?}` environment bindings |
| `overrides` | Final dictionary applied after every other source |
| `schema` | Optional schema passed to `schema.validate` |

Sources are merged in this order: defaults, config files, dotenv files through
the named bindings, process environment through the same bindings, and final
overrides. Later values take precedence. Nested dictionaries merge recursively;
lists and scalar values replace earlier values. Inputs and returned dictionaries
are deep-copied, so modifying them does not change a loaded `Config`.

Bindings default to `string`. Their `type` can be `int`, `float`, `bool`, or
`json`. Boolean values accept `true` and `false` without regard to case;
numeric values must parse completely. An absent variable leaves the existing
value untouched. An empty variable is present and may fail a typed conversion.
Only named variables are read. Dotenv files do not change the process
environment. A dotted binding can create missing nested dictionaries; it fails
if an intermediate value is a scalar.

```gb
import config;

let settings = config.load({
    "defaults": {"server": {"host": "127.0.0.1", "port": 8080}},
    "files": [{"path": "settings.json"}, {"path": "local.toml"},
              {"path": "site.yaml"}],
    "dotenvFiles": [".env"],
    "env": {"server.port": {"name": "APP_PORT", "type": "int"}},
    "overrides": {"server": {"host": "0.0.0.0"}}
});
```

Files must exist and have a dictionary at the root. `load` throws a catchable
`ValueError` for bad options, files, conversion values, or schema failures.
`validate(options)` returns a dictionary with `valid`, `errors`, and `config`;
schema errors appear in `errors` while `config` retains the loaded values.
