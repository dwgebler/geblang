# Redis

The `redis` source-stdlib module provides a minimal RESP client over the native
`net` module. It is intended for application cache/session infrastructure,
queues backed by Redis lists, simple counters, and framework integrations.

```gb
import redis;

let client = redis.connect("127.0.0.1:6379");
defer client.close();

client.set("name", "Ada");
io.println(client.get("name"));
```

## Connecting

`redis.connect(address)` returns a `redis.Client`. The address is a TCP
`host:port` string. The constructor sets a default five-second network deadline
on the underlying socket.

```gb
let client = redis.connect("127.0.0.1:6379");
defer client.close();

if (client.auth("secret")) {
    client.select(1);
}
```

## Client API

| Method | Returns | Description |
|--------|---------|-------------|
| `command(parts)` | `any` | Send a raw Redis command represented as a list of strings |
| `ping()` | `bool` | Send `PING` and return whether Redis replied `PONG` |
| `auth(password)` | `bool` | Authenticate with `AUTH` |
| `select(database)` | `bool` | Select a numeric Redis database |
| `get(key)` | `any` | Return a string value or `null` |
| `getBytes(key)` | `?bytes` | Return a value as raw `bytes`, or `null` |
| `set(key, value)` | `bool` | Set a string value |
| `del(key)` | `int` | Delete one key |
| `exists(key)` | `bool` | Return whether a key exists |
| `expire(key, seconds)` | `bool` | Set a TTL |
| `ttl(key)` | `int` | Return TTL seconds, or Redis negative sentinel values |
| `incr(key)` | `int` | Increment an integer counter |
| `lpush(key, value)` / `rpush(key, value)` | `int` | Push to a list |
| `lpop(key)` / `rpop(key)` | `any` | Pop from a list |
| `lrange(key, start, stop)` | `list<any>` | Return a list range |
| `sadd(key, member)` / `srem(key, member)` | `int` | Add/remove set members |
| `sismember(key, member)` | `bool` | Test set membership |
| `smembers(key)` | `list<any>` | Return all set members |
| `hset(key, field, value)` | `int` | Set a hash field |
| `hget(key, field)` | `any` | Get a hash field |
| `hdel(key, field)` | `int` | Delete a hash field |
| `hgetAll(key)` | `dict<string, any>` | Return all hash fields as a dictionary |
| `close()` | `void` | Close the TCP connection |

`ttl(key)` returns the remaining time-to-live in seconds, or one of Redis's
negative sentinels: `-2` if the key does not exist, and `-1` if the key exists
but has no expiry set.

## Binary values

String accessors such as `get` decode values as UTF-8. Values that are valid
UTF-8 come back as strings. If a stored value is not valid UTF-8, `get` raises a
clear error naming `getBytes`; read such a value with `getBytes(key)` instead,
which returns the raw `bytes` (or `null` when the key is missing).

```gb
client.set("name", "Ada");
io.println(client.get("name"));       # "Ada"

let raw = client.getBytes("thumbnail");
io.println(bytes.toHex(raw));
```

## Examples

Strings, counters, and expiry:

```gb
client.set("session:123", "Ada");
client.expire("session:123", 3600);

io.println(client.get("session:123"));
io.println(client.ttl("session:123"));
io.println(client.incr("metric:login"));
```

Lists:

```gb
client.rpush("jobs", "send-email");
client.rpush("jobs", "sync-report");

io.println(client.lpop("jobs"));
io.println(client.lrange("jobs", 0, -1));
```

Sets:

```gb
client.sadd("roles:ada", "admin");
client.sadd("roles:ada", "editor");

io.println(client.sismember("roles:ada", "admin"));
io.println(client.smembers("roles:ada"));
```

Hashes:

```gb
client.hset("user:1", "name", "Ada");
client.hset("user:1", "email", "ada@example.com");

let user = client.hgetAll("user:1");
io.println(user["email"]);
```

Raw commands are available when the typed wrapper does not yet expose a Redis
command:

```gb
let info = client.command(["INFO", "server"]);
io.println(info);
```

The current client is intentionally small. It does not implement connection
pooling, pub/sub helpers, cluster routing, streams, or automatic reconnection.
Use `command` for unsupported single-connection commands until higher-level
helpers are added.
