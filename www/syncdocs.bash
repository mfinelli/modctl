#!/usr/bin/env bash

set -e

if [[ $# -gt 1 ]]; then
  echo >&2 "usage: $(basename "$0") [LANG_CODE]"
  exit 1
fi

if [[ ! -d www ]] || [[ ! -d docs ]]; then
  echo >&2 "error: run this from project root"
  exit 1
fi

# Optional language argument, e.g. "it", "fr"
# No argument = English (default language, no suffix)
LANG_CODE="${1:-}"

DST=www/content/docs

if [[ -n "$LANG_CODE" ]]; then
    SRC="docs-i18n/$LANG_CODE"
    SUFFIX=".$LANG_CODE"
else
    SRC="docs"
    SUFFIX=""
fi

# Wipe stale content but preserve _index.md
find "$DST" -mindepth 1 -not -name '_index.md' -delete
# Remove empty directories left behind (but not DST itself)
find "$DST" -mindepth 1 -type d -empty -delete

process_file() {
    local src_file="$1"
    local rel_dir="$2"   # relative subdir path, empty string for root
    local filename="$(basename "$src_file")"

    # Extract leading number prefix for weight (e.g. "01" from "01_overview.md")
    local weight=""
    local clean_name="$filename"
    if [[ "$filename" =~ ^([0-9]+)_(.+)$ ]]; then
        weight="${BASH_REMATCH[1]}"
        clean_name="${BASH_REMATCH[2]}"
        # Strip leading zeros for the weight value (e.g. "01" -> "1")
        weight=$((10#$weight))
    fi

    # Extract first h1 as title
    local title
    title=$(grep -m1 '^# ' "$src_file" | sed 's/^# //')

    # Build destination path
    local dst_dir="$DST"
    if [[ -n "$rel_dir" ]]; then
        dst_dir="$DST/$rel_dir"
        mkdir -p "$dst_dir"

        # Create _index.md for the section if it doesn't exist
        local section_index="$dst_dir/_index.md"
        if [[ ! -f "$section_index" ]]; then
            # Derive a section title from the directory name
            # e.g. "02_configuration" -> "Configuration"
            local dir_basename="$(basename "$rel_dir")"
            local section_weight=""
            local section_title="$dir_basename"
            if [[ "$dir_basename" =~ ^([0-9]+)_(.+)$ ]]; then
                section_weight=$((10#${BASH_REMATCH[1]}))
                section_title="${BASH_REMATCH[2]}"
            fi
            # Capitalise first letter
            section_title="$(tr '[:lower:]' '[:upper:]' <<< "${section_title:0:1}")${section_title:1}"
            # Replace underscores/hyphens with spaces
            section_title="${section_title//_/ }"
            section_title="${section_title//-/ }"

            printf '+++\ntitle = "%s"\nsort_by = "weight"\ntemplate = "docs.html"\n' \
                "$section_title" > "$section_index"
            if [[ -n "$section_weight" ]]; then
                printf 'weight = %d\n' "$section_weight" >> "$section_index"
            fi
            printf '+++\n' >> "$section_index"
        fi
    fi

    local dst_file="$dst_dir/${clean_name%.md}${SUFFIX}.md"

    # Prepend front matter
    {
        printf '+++\n'
        if [[ -n "$title" ]]; then
            printf 'title = "%s"\n' "$title"
        fi
        if [[ -n "$weight" ]]; then
            printf 'weight = %d\n' "$weight"
        fi
        printf 'template = "docs.html"\n'
        printf '+++\n\n'
        cat "$src_file"
    } > "$dst_file"
}

# Process all markdown files, preserving subdirectory structure
while IFS= read -r -d '' src_file; do
    # Get path relative to SRC
    rel_path="${src_file#"$SRC"/}"
    rel_dir="$(dirname "$rel_path")"

    # dirname returns "." for files in the root
    if [[ "$rel_dir" == "." ]]; then
        rel_dir=""
    fi

    process_file "$src_file" "$rel_dir"
done < <(find "$SRC" -name '*.md' -not -name '_index.md' -print0 | sort -z)

exit 0
