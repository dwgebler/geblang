# Messaging

The `messaging` module is a backend-agnostic queue + topic surface.
Application code uses one API across brokers; the chosen backend
handles the underlying protocol (AWS SQS, RabbitMQ AMQP, ActiveMQ
STOMP, Kafka).

## Connecting

`messaging.connect(opts)` returns a queue handle. The `driver` key
selects the backend; remaining keys are validated by that backend.

```gb
import messaging;

let q = messaging.connect({
    "driver":    "sqs",
    "region":    "us-east-1",
    "queueUrl":  "https://sqs.us-east-1.amazonaws.com/123456789012/orders",
    "accessKey": env.get("AWS_ACCESS_KEY_ID"),
    "secretKey": env.get("AWS_SECRET_ACCESS_KEY")
});
```

## Publishing

`publish(payload)` accepts a string, bytes, or any value that JSON
can serialise. The text-based brokers (SQS, SNS, STOMP) send a `bytes`
payload as text when it is valid UTF-8 and raise a clear error on binary
bytes, so encode those first (for example as base64). The binary-capable
brokers (Kafka, RabbitMQ) send raw bytes as-is. Other non-string payloads
are JSON-encoded before being sent so the consumer can `json.parse` them
back.

```gb
q.publish("ping");
q.publish({"orderId": 42, "amount": 100});
```

## Receiving

`receive(timeoutMs)` blocks up to the timeout. Returns the message
dict or `null` when the window expires with no message. Each dict
carries:

| Key | Type | Notes |
|-----|------|-------|
| `body` | string | Raw payload. JSON dicts come back as encoded JSON; parse client-side. |
| `id` | string | Broker-side identifier (informational; not needed for ack). |
| `handle` | string | The receipt handle - pass to `ack()` to delete. |

```gb
let msg = q.receive(20000);   /* up to 20s long-poll */
if (msg != null) {
    io.println(msg["body"]);
    q.ack(msg["handle"]);
}
```

`ack()` accepts either the handle string or the whole message dict.

## Consuming continuously

`consume(handler)` blocks in a receive loop, dispatching every
delivered message to the supplied callable and acknowledging on
clean return:

```gb
q.consume(func(any msg): void {
    let order = json.parse(msg["body"] as string);
    process(order);
});
```

The callable runs synchronously inside the consume loop; spawn a
task with `async.run` if you need parallel processing.

## Topics (pub/sub)

A queue delivers each message to exactly one consumer. A topic
broadcasts each published message to every active subscriber. The
two surfaces share the same option keys; the difference is how
many subscribers receive a given message.

`messaging.topic(opts)` returns a topic handle:

```gb
let topic = messaging.topic({
    "driver": "rabbitmq",
    "url":    "amqp://localhost/",
    "topic":  "events.user.signup"
});

topic.publish({"userId": 42});

topic.subscribe(func(any msg): void {
    io.println("received: " + (msg["body"] as string));
});
```

Backends differ in how they implement fan-out:

| Driver | Fan-out mechanism |
|--------|-------------------|
| `rabbitmq` | Fanout exchange named after `topic`. Each `subscribe()` declares a server-named, exclusive, auto-delete queue bound to the exchange. |
| `stomp` / `activemq` | `/topic/<name>` destination namespace; the broker fans out at the destination. Pass `destination` like a queue would; a bare name is auto-prefixed with `/topic/`. |
| `kafka` | Single topic; each `subscribe()` opens its own consumer group so every subscriber sees every record. |
| `sns` | AWS SNS Publish for the producer side; subscribers pair an SQS queue with the topic (subscription set up out of band) and poll it. |
| `sqs` | Not supported. SQS itself does not fan out; AWS SNS is the proper pub/sub primitive and can target SQS subscriptions. Calling `messaging.topic({"driver":"sqs",...})` throws with this guidance. |

`subscribe(handler)` blocks for the lifetime of the topic handle.
Spawn it on its own task if you want to keep publishing
concurrently:

```gb
import async;

async.run(func(): void {
    topic.subscribe(func(any msg): void { handleEvent(msg); });
});

topic.publish({"event": "tick"});
```

`close()` releases broker-side resources (channel + connection for
RabbitMQ, socket for STOMP, writer for Kafka).

## RabbitMQ specifics

The RabbitMQ backend speaks AMQP 0.9.1 over TCP. Connect with an
`amqp://` URL; the backend opens one connection and one channel per
queue handle and declares the queue durably on construction.

```gb
let q = messaging.connect({
    "driver":     "rabbitmq",
    "url":        "amqp://guest:guest@localhost:5672/",
    "queue":      "orders",
    "exchange":   "",           # default: direct-to-queue
    "routingKey": "orders",     # default: queue name
    "durable":    true          # default
});
```

Required options:

| Key | Notes |
|-----|-------|
| `driver` | `"rabbitmq"` |
| `url` | Standard AMQP URL (user / password / host / port / vhost). |
| `queue` | Queue name. Declared on connect with the `durable` flag. |

Optional:

| Key | Default | Notes |
|-----|---------|-------|
| `exchange` | `""` | Empty string = default exchange (direct-to-queue routing). |
| `routingKey` | queue name | Routing key for publishes. |
| `durable` | `true` | Queue survives broker restart. |

`receive(timeoutMs)` polls with AMQP `basic.get`. The poll cadence
inside the timeout window is 50 ms, so `timeoutMs=200` makes up to
four broker round-trips before returning `null`. Use a smaller value
for a tight latency budget; use the long-polling `receive(20000)`
pattern from SQS instead if you want broker-side waiting.

The lower-level `amqp` module (`amqp.dial`, `amqp.channel`,
`amqp.declareQueue`, `amqp.publish`, `amqp.get`, `amqp.ack`,
`amqp.close`) is available for cases the `MessageQueue` facade
doesn't cover (custom exchange topologies, QoS prefetch, multiple
consumers per channel).

## STOMP specifics (ActiveMQ + RabbitMQ-via-plugin)

STOMP 1.2 is a small text-based protocol supported natively by
ActiveMQ, ActiveMQ Artemis, and several other brokers, and
available on RabbitMQ via the `rabbitmq_stomp` plugin. The driver
name `"stomp"` and the alias `"activemq"` both select this backend.

```gb
let q = messaging.connect({
    "driver":      "stomp",
    "host":        "broker.internal",
    "port":        61613,
    "destination": "/queue/orders",
    "login":       env.get("STOMP_USER"),
    "passcode":    env.get("STOMP_PASS")
});
```

Required options:

| Key | Notes |
|-----|-------|
| `driver` | `"stomp"` or `"activemq"` |
| `host` | Broker host. |
| `port` | Broker port (typically 61613 for plain, 61614 for TLS). |
| `destination` | Queue or topic name. ActiveMQ uses `/queue/foo` / `/topic/foo`; RabbitMQ STOMP uses `/queue/foo` / `/exchange/foo`. |

Optional:

| Key | Default | Notes |
|-----|---------|-------|
| `login` | `""` | Broker username. |
| `passcode` | `""` | Broker password. |
| `virtualHost` | `"/"` | STOMP `host` header. |
| `ackMode` | `"client-individual"` | STOMP subscription ack mode. |

The driver opens one TCP connection, sends `CONNECT`, expects
`CONNECTED`, then sends one `SUBSCRIBE` against the destination.
Subsequent `receive()` calls block reading the next `MESSAGE` frame
on that subscription.

`publish()` uses the standard STOMP `SEND` frame with explicit
`content-length` for binary-safe payloads. `ack()` sends an `ACK`
frame referencing the subscription's ack id (preferred over
`message-id` per STOMP 1.2). `close()` sends `DISCONNECT` and closes
the socket.

## Kafka specifics

The Kafka backend speaks the native Kafka protocol via the
underlying `segmentio/kafka-go` client. The `MessageQueue` facade
maps onto a single topic with one consumer-group member per
handle: `publish()` produces records, `receive()` fetches the
next record assigned to this group, and `ack()` commits the
offset for the last fetched record.

```gb
let q = messaging.connect({
    "driver":  "kafka",
    "brokers": ["kafka-0:9092", "kafka-1:9092"],
    "topic":   "orders",
    "groupId": "order-processor"
});
```

Required options:

| Key | Notes |
|-----|-------|
| `driver` | `"kafka"` |
| `brokers` | list of `host:port` bootstrap brokers. |
| `topic` | Topic name. |
| `groupId` | Consumer-group id used by `receive` / `ack`. |

Optional:

| Key | Default | Notes |
|-----|---------|-------|
| `autoCreateTopic` | `false` | When true, the writer asks the broker to create the topic on first publish. |

`receive(timeoutMs)` calls `FetchMessage` with that deadline. Unlike
the queue brokers, Kafka delivers records at the consumer's pace -
fetching does not remove anything from the log. `ack()` commits the
offset of the last fetched record to the group's committed-offset
store; restarting the consumer with the same `groupId` resumes
after the last committed offset.

For lower-level access (manual partition assignment, custom
balancers, headers), the `kafka` native module is exposed:
`kafka.writer`, `kafka.write`, `kafka.reader`, `kafka.read`,
`kafka.commit`, `kafka.close`.

## SQS specifics

The SQS backend speaks the REST API (query-string form) signed with
AWS Signature V4. No long-lived connection: each call is an
independent HTTPS request. `receive(timeoutMs)` maps to the SQS
`WaitTimeSeconds` parameter rounded up to whole seconds, capped at
the SQS-side maximum of 20.

Required options:

| Key | Notes |
|-----|-------|
| `driver` | `"sqs"` |
| `region` | AWS region of the queue (used in the credential scope). |
| `queueUrl` | Full URL returned by SQS at queue creation. |
| `accessKey` | IAM access key ID. Read from env in production. |
| `secretKey` | IAM secret access key. Read from env in production. |

The implementation is in `stdlib/messaging/sqs.gb` and can be
inspected as a reference for adding new backends.

## SNS specifics

The SNS backend handles pub/sub on AWS. `publish()` signs each
request with sigv4 and POSTs to the regional SNS endpoint
(`https://sns.<region>.amazonaws.com/`). `subscribe()` polls a
paired SQS queue: the SNS->SQS subscription is set up out of band
(`aws sns subscribe ... --protocol sqs --endpoint <queue-arn>`),
then `subscribe(handler)` drives the SQS consume loop and
forwards each delivered notification to the handler.

```gb
let topic = messaging.topic({
    "driver":    "sns",
    "region":    "us-east-1",
    "topicArn":  "arn:aws:sns:us-east-1:123:orders",
    "queueUrl":  "https://sqs.us-east-1.amazonaws.com/123/orders-sub",
    "accessKey": env.get("AWS_ACCESS_KEY_ID"),
    "secretKey": env.get("AWS_SECRET_ACCESS_KEY")
});
topic.publish({"orderId": 42});
topic.subscribe(func(any msg): void {
    let payload = msg["body"] as string;
    /* payload is the raw SNS notification JSON; if you want just
     * the original Message field, parse and extract:
     * json.parse(payload)["Message"]. */
});
```

Required options:

| Key | Notes |
|-----|-------|
| `driver` | `"sns"` |
| `region` | AWS region of the topic. |
| `topicArn` | Full ARN of the SNS topic. |
| `accessKey` | IAM access key ID with `sns:Publish` (and `sqs:ReceiveMessage` on the subscription queue if subscribing). |
| `secretKey` | IAM secret access key. |
| `queueUrl` | Optional. SQS queue subscribed to the topic. Required to call `subscribe()`; omit if you only publish. |

Publish-only setups can skip `queueUrl`; calling `subscribe()`
without one throws with a clear error. The implementation is in
`stdlib/messaging/sns.gb`.

## Adding a backend

Implement a class with the same method set as the SQS backend (the
`messaging.MessageQueue` interface), register it under a new driver
name in `messaging.gb`, and write tests under
`tests/stdlib/messaging_*_test.gb`. The interface is small by
design - one method per logical operation, no broker-specific
features leak into the facade.
