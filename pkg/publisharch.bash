#!/usr/bin/env bash

ARM64_PKG=$(find pkg/arch -name "*-aarch64.pkg.tar.zst" | head -1)
ARM64_SIG=$(find pkg/arch -name "*-aarch64.pkg.tar.zst.sig" | head -1)
AMD64_PKG=$(find pkg/arch -name "*-x86_64.pkg.tar.zst" | head -1)
AMD64_SIG=$(find pkg/arch -name "*-x86_64.pkg.tar.zst.sig" | head -1)

echo "Found packages:"
echo "  arch amd64:   $AMD64_PKG"
echo "  arch arm64:   $ARM64_PKG"
echo "  sig x86_64:   $AMD64_SIG"
echo "  sig aarch64:  $ARM64_SIG"

for f in "$AMD64_PKG" "$ARM64_PKG" "$AMD64_SIG" "$ARM64_SIG"; do
    if [[ -z "$f" ]]; then
        echo "error: Could not find all required package files" >&2
        exit 1
    fi
done

echo "$RCLONE_CONFIG" > pkg/rclone.conf

WORKDIR="$(mktemp -d)"
mkdir -p "$WORKDIR/repo/arch"

# we download the repo even though we could just bootstrap the repo files
# everytime because we will eventually unify the publish scripts (when we
# up the ubuntu minimum to noble) and so we need to make sure that we can
# handle it already today

# if [[ "${GITHUB_REF}" == refs/tags/v* ]]; then
  echo "Pulling current repo state from R2..."
  rclone sync --config pkg/rclone.conf r2:modctl-pkgs/arch "$WORKDIR/repo/arch"
# fi

mkdir -p "$WORKDIR/repo/arch/aarch64"
mkdir -p "$WORKDIR/repo/arch/x86_64"
repo-add --key pkg@modctl.org --sign "$WORKDIR/repo/arch/x86_64/modctl.db.tar.gz" "$AMD64_PKG"
repo-add --key pkg@modctl.org --sign "$WORKDIR/repo/arch/aarch64/modctl.db.tar.gz" "$ARM64_PKG"

cp "$AMD64_PKG" "$WORKDIR/repo/arch/x86_64/"
cp "$AMD64_SIG" "$WORKDIR/repo/arch/x86_64/"
cp "$ARM64_PKG" "$WORKDIR/repo/arch/aarch64/"
cp "$ARM64_SIG" "$WORKDIR/repo/arch/aarch64/"

find "$WORKDIR/repo/arch" -name '*.old' -exec rm {} \;
find "$WORKDIR/repo/arch" -name '*.old.sig' -exec rm {} \;

# Dereference all symlinks so rclone can push regular files
find "$WORKDIR/repo/arch" -type l | while read -r link; do
    cp --remove-destination "$(readlink -f "$link")" "$link"
done

tree "$WORKDIR"

# if [[ "${GITHUB_REF}" == refs/tags/v* ]]; then
  echo "Pushing updated repo to R2..."
  rclone sync --config pkg/rclone.conf "$WORKDIR/repo/arch" r2:modctl-pkgs/arch
# fi
