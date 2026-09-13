#!/usr/bin/env bash
set -euo pipefail

source_directory="${1:-}"
destination_directory="${2:-}"
if [[ ! -d "$source_directory" || -z "$destination_directory" ]]; then
  echo "Kullanım: $0 <kaynak-dizin> <hedef-dizin>" >&2
  exit 2
fi

mkdir -p "$destination_directory"
artifact_count=0
while IFS= read -r -d '' artifact; do
  artifact_name="$(basename "$artifact")"
  destination="$destination_directory/$artifact_name"
  if [[ -e "$destination" ]]; then
    echo "Aynı adlı release artifact birden fazla kez bulundu: $artifact_name" >&2
    exit 1
  fi
  cp "$artifact" "$destination"
  artifact_count=$((artifact_count + 1))
done < <(find "$source_directory" -type f ! -name 'checksums.txt' ! -name '*.sigstore.json' -print0)

if [[ $artifact_count -eq 0 ]]; then
  echo "Yayımlanacak release artifact bulunamadı." >&2
  exit 1
fi
