#!/usr/bin/env bash
set -euo pipefail

version="${1:-${VERSION:-}}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "Kullanım: $0 <semver>" >&2
  exit 2
fi

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
release_dir="$project_root/dist/bazusop-$version"
nfpm_command="${NFPM:-nfpm}"

if ! command -v "$nfpm_command" >/dev/null 2>&1; then
  echo "nFPM bulunamadı. NFPM değişkeniyle binary yolunu belirtin." >&2
  exit 1
fi

for architecture in amd64 arm64; do
  archive="$release_dir/bazusop-hub_${version}_linux_${architecture}.tar.gz"
  if [[ ! -f "$archive" ]]; then
    echo "Linux arşivi bulunamadı: $archive" >&2
    exit 1
  fi
  stage_dir="$(mktemp -d)"
  tar -C "$stage_dir" -xzf "$archive"
  binary="$stage_dir/bazusop-hub_${version}_linux_${architecture}/bazusop-hub"
  for packager in deb rpm; do
    VERSION="$version" ARCH="$architecture" BINARY="$binary" \
      "$nfpm_command" package \
        --config "$project_root/packaging/nfpm.yaml" \
        --packager "$packager" \
        --target "$release_dir/bazusop-hub_${version}_linux_${architecture}.${packager}"
  done
  rm -r "$stage_dir"
done

"$project_root/scripts/write-checksums.sh" "$release_dir"
