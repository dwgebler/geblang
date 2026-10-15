# Async I/O

The `async.io`, `async.http`, `async.net`, and `async.stream` source modules
provide task-returning wrappers around file, HTTP, socket, and parser work.
They establish the public API for async I/O while the lower-level event-loop
runtime is still roadmap work.

Example file reads:

```gb
import async.io as aio;
import io;

let configTask = aio.readText("config/app.json");
let templateTask = aio.readText("templates/page.html");

io.println("both reads started");

let config = await configTask;
let template = await templateTask;
```

`async.io` functions:

- `readText`, `writeText`, `appendText`
- `readBytes`, `writeBytes`, `appendBytes`
- handle operations: `read`, `readAll`, `write`, `writeln`, `flush`, `close`
- metadata: `stat`, `listDir`

`async.http` functions:

- `get`, `post`, `postJson`
- `request`, `requestWithOptions`
- `parseJson`

`async.net` functions:

- `lookupHost`
- TCP: `listenTcp`, `connectTcp`, `accept`, `read`, `write`, `close`
- UDP: `listenUdp`, `dialUdp`, `readFrom`, `writeTo`

`async.stream` functions:

- `jsonStream`, `jsonReader`
- `yamlStream`, `yamlReader`
- `xmlStream`, `xmlReader`
- `csvStream`, `csvReader`

Socket example:

```gb
import async;
import async.net as anet;
import bytes;
import net;
import io;

let listener = await anet.listenTcp("127.0.0.1:0");
let address = net.localAddr(listener);

let server = async.run(func(): string {
    let conn = await anet.accept(listener);
    let message = await anet.read(conn, 4);
    await anet.write(conn, "pong");
    await anet.close(conn);
    return bytes.toString(message);
});

let client = await anet.connectTcp(address);
await anet.write(client, "ping");
io.println(bytes.toString(await anet.read(client, 4)));
io.println(await server);
```
