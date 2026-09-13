#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
project_name="${BAZUSOP_SMOKE_PROJECT:-bazusop-smoke-$$}"
database_password="release-smoke-password"

export BAZUSOP_PORT="${BAZUSOP_SMOKE_PORT:-0}"
export POSTGRES_PASSWORD="$database_password"
export BAZUSOP_ENROLLMENT_TOKEN="release-smoke-enrollment"
export BAZUSOP_OPERATOR_TOKEN="release-smoke-operator"
export BAZUSOP_ADMIN_TOKEN="release-smoke-admin"

cleanup() {
  docker compose --project-directory "$project_root" --project-name "$project_name" down --volumes >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker compose --project-directory "$project_root" --project-name "$project_name" config --quiet
docker compose --project-directory "$project_root" --project-name "$project_name" up --detach --build --wait

health="$(docker compose --project-directory "$project_root" --project-name "$project_name" exec -T hub \
  wget -q -O - http://127.0.0.1:8080/api/v1/health)"
if [[ "$health" != *'"status":"ok"'* ]]; then
  echo "Hub sağlık kontrolü başarısız: $health" >&2
  exit 1
fi

configuration="$(docker compose --project-directory "$project_root" --project-name "$project_name" exec -T hub \
  wget -q -O - http://127.0.0.1:8080/api/v1/system/configuration)"
for expected in '"storage":"postgresql"' '"timescale_enabled":true' '"telemetry_retention_days":30' '"log_retention_days":14'; do
  if [[ "$configuration" != *"$expected"* ]]; then
    echo "Hub yapılandırması beklenen değeri içermiyor: $expected" >&2
    echo "$configuration" >&2
    exit 1
  fi
done

docker run --rm \
  --network "${project_name}_default" \
  --volume "$project_root:/work:ro" \
  --workdir /work \
  --env "BAZUSOP_TEST_DATABASE_URL=postgres://bazusop:${database_password}@postgres:5432/bazusop?sslmode=disable" \
  golang:1.26-alpine \
  sh -c 'GOCACHE=/tmp/bazusop-go-cache go test -count=1 ./internal/storage/postgres -run TestPostgres'

printf 'Compose smoke testi başarılı: %s\n' "$configuration"
