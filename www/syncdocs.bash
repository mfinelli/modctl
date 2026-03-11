#!/usr/bin/env bash

set -e

if [[ $# -ne 0 ]]; then
  echo >&2 "usage: $(basename "$0")"
  exit 1
fi

if [[ ! -d www ]] || [[ ! -d docs ]]; then
  echo >&2 "error: run this from project root"
  exit 1
fi

DST=www/content/docs

# Wipe stale content but preserve _index.md
find "$DST" -mindepth 1 -not -name '_index.md' -delete
# Remove empty directories left behind (but not DST itself)
find "$DST" -mindepth 1 -type d -empty -delete

for file in docs/*.md; do
  filename="$(basename "$file")"
  dst_file="$DST/$filename"

  # Extract first h1
  title="$(grep -m1 '^# ' "$file" | sed 's/^# //')"

  if [[ -n $title ]]; then
    # Prepend front matter, then the original content
    printf '+++\ntitle = "%s"\n+++\n\n' "$title" | cat - "$file" > "$dst_file"
  else
    cp "$file" "$dst_file"
  fi
done

exit 0
