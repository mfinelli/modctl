#!/usr/bin/env bash

set -e

# normalizes docs file names like in www/syncdocs but without any zola
# front-matter suitable for being included in final packages
# usage: ./copydocs.bash ./path/to/pkg

if [[ $# -ne 1 ]]; then
  echo >&2 "usage: $(basename "$0") destination"
  exit 1
fi

SRC=docs
DST="$1"

mkdir -p "$DST"

process_file() {
    local src_file="$1"
    local rel_dir="$2"
    local filename
    filename="$(basename "$src_file")"

    # Strip numeric prefix from filename (e.g. "01_overview.md" -> "overview.md")
    local clean_name="$filename"
    if [[ "$filename" =~ ^[0-9]+_(.+)$ ]]; then
        clean_name="${BASH_REMATCH[1]}"
    fi

    local dst_dir="$DST"
    if [[ -n "$rel_dir" ]]; then
        # Strip numeric prefix from each path segment
        local clean_rel_dir=""
        while IFS= read -r segment; do
            local clean_segment="$segment"
            if [[ "$segment" =~ ^[0-9]+_(.+)$ ]]; then
                clean_segment="${BASH_REMATCH[1]}"
            fi
            clean_rel_dir="${clean_rel_dir:+$clean_rel_dir/}$clean_segment"
        done < <(echo "$rel_dir" | tr '/' '\n')
        dst_dir="$DST/$clean_rel_dir"
    fi

    mkdir -p "$dst_dir"
    cp "$src_file" "$dst_dir/$clean_name"
}

# Process all markdown files preserving subdirectory structure
while IFS= read -r -d '' src_file; do
    rel_path="${src_file#"$SRC"/}"
    rel_dir="$(dirname "$rel_path")"

    if [[ "$rel_dir" == "." ]]; then
        rel_dir=""
    fi

    process_file "$src_file" "$rel_dir"
done < <(find "$SRC" -name '*.md' -print0 | sort -z)

exit 0
