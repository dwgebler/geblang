# Data stores

Geblang ships database, Redis, configuration, and schema-validation modules for
application storage and infrastructure code. These modules are documented on
separate pages because they each have different lifecycle and API conventions.

- [Database](data-stores/database.html): SQL connections, portable query
  binding, transactions, prepared statements, streaming rows, pool
  configuration, stats, and migrations.
- [Redis](data-stores/redis.html): RESP client for strings, counters, expiry,
  lists, sets, hashes, and raw Redis commands.
- [Config](data-stores/config.html): layered dictionaries, recursive merges,
  dotted-path access, parsing, and immutable-style `Config` objects.
- [Schema](data-stores/schema.html): lightweight value validation and the
  `schema.validator.Validator` wrapper.

Use the class APIs for application code when a module provides them. Lower-level
function APIs remain available for adapters, tests, and framework internals.
