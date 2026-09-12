#!/usr/bin/env bash
set -euo pipefail

artifact_directory="${1:-}"
if [[ ! -d "$artifact_directory" ]]; then
  echo "Kullanım: $0 <artifact-dizini>" >&2
  exit 2
fi

(
  cd "$artifact_directory"
  artifacts=()
  while IFS= read -r artifact; do
    artifacts+=("$artifact")
  done < <(find . -maxdepth 1 -type f ! -name 'checksums.txt' ! -name '*.sigstore.json' -print | sed 's|^./||' | sort)
  if [[ ${#artifacts[@]} -eq 0 ]]; then
    echo "Checksum üretilecek artifact bulunamadı." >&2
    exit 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${artifacts[@]}" > checksums.txt
  else
    shasum -a 256 "${artifacts[@]}" > checksums.txt
  fi
)
