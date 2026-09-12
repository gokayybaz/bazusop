#!/usr/bin/env bash
set -euo pipefail

version="${1:-${VERSION:-}}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "Kullanım: $0 <semver> (örnek: 0.3.0)" >&2
  exit 2
fi

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
commit="${COMMIT:-$(git -C "$project_root" rev-parse --short=12 HEAD 2>/dev/null || printf unknown)}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
release_dir="$project_root/dist/bazusop-$version"
version_package="github.com/gokayybaz/bazusop/internal/version"
targets=("linux amd64" "linux arm64" "windows amd64")

if [[ -e "$release_dir" ]]; then
  echo "Release dizini zaten var: $release_dir" >&2
  exit 1
fi
mkdir -p "$release_dir"

make -C "$project_root" build-web

for target in "${targets[@]}"; do
  read -r operating_system architecture <<<"$target"
  for component in hub agent; do
    archive_base="bazusop-${component}_${version}_${operating_system}_${architecture}"
    stage_dir="$release_dir/$archive_base"
    binary_name="bazusop-${component}"
    if [[ "$operating_system" == "windows" ]]; then
      binary_name="bazusop-${component}.exe"
    fi
    mkdir -p "$stage_dir"
    CGO_ENABLED=0 GOOS="$operating_system" GOARCH="$architecture" \
      go build -trimpath \
        -ldflags="-s -w -X $version_package.Version=$version -X $version_package.Commit=$commit -X $version_package.BuildDate=$build_date" \
        -o "$stage_dir/$binary_name" "$project_root/cmd/bazusop-${component}"

    if [[ "$operating_system" == "windows" ]]; then
      (cd "$release_dir" && zip -q -r "$archive_base.zip" "$archive_base")
    else
      COPYFILE_DISABLE=1 tar -C "$release_dir" -czf "$release_dir/$archive_base.tar.gz" "$archive_base"
    fi
    rm -r "$stage_dir"
  done
done

(
  cd "$release_dir"
  artifacts=(bazusop-hub_* bazusop-agent_*)
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${artifacts[@]}" > checksums.txt
  else
    shasum -a 256 "${artifacts[@]}" > checksums.txt
  fi
)

printf 'Release hazır: %s\n' "$release_dir"
