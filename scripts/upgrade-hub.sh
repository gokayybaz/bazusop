#!/usr/bin/env bash
set -euo pipefail

candidate=""
target="/usr/bin/bazusop-hub"
expected_version=""
expected_sha256=""
health_url="http://127.0.0.1:8080/api/v1/health"
service="bazusop-hub"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --candidate) candidate="${2:-}"; shift 2 ;;
    --target) target="${2:-}"; shift 2 ;;
    --version) expected_version="${2:-}"; shift 2 ;;
    --sha256) expected_sha256="${2:-}"; shift 2 ;;
    --health-url) health_url="${2:-}"; shift 2 ;;
    --service) service="${2:-}"; shift 2 ;;
    *) echo "Bilinmeyen argüman: $1" >&2; exit 2 ;;
  esac
done

if [[ ! -f "$candidate" || ! -f "$target" || -z "$expected_version" || ! "$expected_sha256" =~ ^[0-9a-fA-F]{64}$ ]]; then
  echo "Kullanım: $0 --candidate <dosya> --target <dosya> --version <semver> --sha256 <özet> [--health-url <url>] [--service <ad>]" >&2
  exit 2
fi
if [[ ! "$expected_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "Beklenen sürüm geçerli SemVer değil: $expected_version" >&2
  exit 2
fi

calculate_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

actual_sha256="$(calculate_sha256 "$candidate")"
actual_sha256="$(printf '%s' "$actual_sha256" | tr '[:upper:]' '[:lower:]')"
expected_sha256="$(printf '%s' "$expected_sha256" | tr '[:upper:]' '[:lower:]')"
if [[ "$actual_sha256" != "$expected_sha256" ]]; then
  echo "Aday binary SHA-256 doğrulaması başarısız." >&2
  exit 1
fi

candidate_identity="$("$candidate" --version)"
if [[ "$candidate_identity" != "bazUSOP hub $expected_version ("* ]]; then
  echo "Aday binary beklenen $expected_version sürümünü taşımıyor: $candidate_identity" >&2
  exit 1
fi

lock_directory="${target}.upgrade.lock"
if ! mkdir "$lock_directory" 2>/dev/null; then
  echo "Başka bir yükseltme devam ediyor: $lock_directory" >&2
  exit 1
fi
trap 'rmdir "$lock_directory" 2>/dev/null || true' EXIT

next="${target}.next"
previous="${target}.previous"
install -m 0755 "$candidate" "$next"
install -m 0755 "$target" "${previous}.next"
mv "${previous}.next" "$previous"
mv "$next" "$target"

attempts="${BAZUSOP_UPGRADE_ATTEMPTS:-15}"
delay="${BAZUSOP_UPGRADE_DELAY:-2}"
healthy=false
if systemctl restart "$service"; then
  for ((attempt = 1; attempt <= attempts; attempt++)); do
    if curl --fail --silent --show-error "$health_url" >/dev/null; then
      healthy=true
      break
    fi
    sleep "$delay"
  done
fi

if [[ "$healthy" != true ]]; then
  echo "Yeni sürüm sağlıklı başlamadı; önceki binary geri yükleniyor." >&2
  install -m 0755 "$previous" "${target}.rollback"
  mv "${target}.rollback" "$target"
  systemctl restart "$service" || true
  exit 1
fi

printf 'Yükseltme tamamlandı: %s\n' "$candidate_identity"
