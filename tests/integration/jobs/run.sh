#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/../../.." && pwd)
compose_file="$repo_root/tests/integration/jobs/compose.yaml"
project="geblang-jobs-$$"
cleanup() { docker compose -f "$compose_file" -p "$project" down --volumes --remove-orphans; }
trap cleanup EXIT
if ! docker compose -f "$compose_file" -p "$project" up -d --wait --wait-timeout 180; then
    docker compose -f "$compose_file" -p "$project" logs --tail 100
    exit 1
fi
pg_address=$(docker compose -f "$compose_file" -p "$project" port postgres 5432)
mysql_address=$(docker compose -f "$compose_file" -p "$project" port mysql 3306)
amqp_address=$(docker compose -f "$compose_file" -p "$project" port rabbitmq 5672)
stomp_address=$(docker compose -f "$compose_file" -p "$project" port rabbitmq 61613)
export GEBLANG_JOBS_PG_DSN="postgres://geblang:queue_test@$pg_address/jobs?sslmode=disable"
export GEBLANG_JOBS_MYSQL_DSN="geblang:queue_test@tcp($mysql_address)/jobs?parseTime=true"
export GEBLANG_JOBS_AMQP_URL="amqp://geblang:queue_test@$amqp_address/"
export GEBLANG_JOBS_STOMP_PORT="${stomp_address##*:}"
export GEBLANG_JOBS_KAFKA="127.0.0.1:${GEBLANG_JOBS_KAFKA_PORT:-19092}"
cd "$repo_root"
./geblang test tests/stdlib/jobs_backends_live_test.gb
./geblang test --runtime=evaluator tests/stdlib/jobs_backends_live_test.gb
./geblang test tests/stdlib/messaging_managed_live_test.gb
./geblang test --runtime=evaluator tests/stdlib/messaging_managed_live_test.gb
