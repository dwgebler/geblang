# SQL and broker worker integration suite

Run `make test-jobs-backends` from the repository root with Docker Compose and
local Docker access. It builds the CLI, starts PostgreSQL 16, MySQL 8, RabbitMQ
(with STOMP) and Redpanda (Kafka protocol), then runs SQL and managed-consumer
contracts under the VM and evaluator. Test images are pinned in compose.yaml.

Each run owns an isolated project and removes its containers and volumes on
exit. PostgreSQL, MySQL and RabbitMQ use ephemeral loopback ports. Kafka must
advertise its external address, so its default host port is 19092. Override
GEBLANG_JOBS_KAFKA_PORT for concurrent runs or when that port is occupied.
Credentials are disposable local test values and ports bind to loopback only.

The runner exports GEBLANG_JOBS_PG_DSN, GEBLANG_JOBS_MYSQL_DSN,
GEBLANG_JOBS_AMQP_URL, GEBLANG_JOBS_STOMP_PORT and GEBLANG_JOBS_KAFKA for the tests.
Their absence skips live tests in ordinary language runs. The Go parity suite
also exercises SQS managed receipt/delete behavior against a local HTTP server;
no AWS account or credential is needed. This does not certify live AWS behavior.
