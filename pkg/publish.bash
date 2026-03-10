#!/usr/bin/env bash

set -e

AMD64_DEB=$(find pkg -name "*_amd64.deb" | head -1)
ARM64_DEB=$(find pkg -name "*_arm64.deb" | head -1)
AMD64_RPM=$(find pkg -name "*.x86_64.rpm" | head -1)
ARM64_RPM=$(find pkg -name "*.aarch64.rpm" | head -1)

echo "Found packages:"
echo "  deb amd64:    $AMD64_DEB"
echo "  deb arm64:    $ARM64_DEB"
echo "  rpm x86_64:   $AMD64_RPM"
echo "  rpm aarch64:  $ARM64_RPM"

for f in "$AMD64_DEB" "$ARM64_DEB" "$AMD64_RPM" "$ARM64_RPM"; do
    if [[ -z "$f" ]]; then
        echo "error: Could not find all required package files" >&2
        exit 1
    fi
done

echo "$RCLONE_CONFIG" > pkg/rclone.conf

WORKDIR="$(mktemp -d)"
mkdir -p "$WORKDIR/repo"

if [[ "${GITHUB_REF}" == refs/tags/v* ]]; then
  echo "Pulling current repo state from R2..."
  rclone sync --config pkg/rclone.conf r2:modctl-pkgs "$WORKDIR/repo"
fi

echo "Exporting public key..."
gpg --armor --export pkg@modctl.org > "$WORKDIR/repo/pubkey.asc"
gpg --export pkg@modctl.org > "$WORKDIR/repo/pubkey.gpg"

echo "Building APT repository..."
APT_ROOT="$WORKDIR/repo/apt"
APT_POOL="$APT_ROOT/pool/main"
APT_DISTS="$APT_ROOT/dists/stable/main"
mkdir -p "$APT_POOL"
mkdir -p "$APT_DISTS/binary-amd64"
mkdir -p "$APT_DISTS/binary-arm64"

# Remove old debs, copy new ones
rm -f "$APT_POOL"/*.deb
cp "$AMD64_DEB" "$APT_POOL/"
cp "$ARM64_DEB" "$APT_POOL/"

# Generate Packages files filtered by architecture
echo "Generating APT Packages files..."
apt-ftparchive packages --arch amd64 "$APT_POOL" \
    > "$APT_DISTS/binary-amd64/Packages"
gzip -k -f "$APT_DISTS/binary-amd64/Packages"

apt-ftparchive packages --arch arm64 "$APT_POOL" \
    > "$APT_DISTS/binary-arm64/Packages"
gzip -k -f "$APT_DISTS/binary-arm64/Packages"

# Generate Release file
echo "Generating APT Release file..."
apt-ftparchive \
    -c "pkg/apt-ftparchive.conf" \
    release "$APT_ROOT/dists/stable" \
    > "$APT_ROOT/dists/stable/Release"

# Sign Release file
echo "Signing APT Release file..."
gpg --detach-sign \
    -u pkg@modctl.org \
    -o "$APT_ROOT/dists/stable/Release.gpg" \
    "$APT_ROOT/dists/stable/Release"

gpg --clearsign \
    -u pkg@modctl.org \
    -o "$APT_ROOT/dists/stable/InRelease" \
    "$APT_ROOT/dists/stable/Release"

echo "Building YUM repository..."

RPM_ROOT="$WORKDIR/repo/rpm"
mkdir -p "$RPM_ROOT/x86_64"
mkdir -p "$RPM_ROOT/aarch64"
cp pkg/modctl.repo "$RPM_ROOT/"

# Remove old rpms, copy new ones
rm -f "$RPM_ROOT/x86_64"/*.rpm
rm -f "$RPM_ROOT/aarch64"/*.rpm
cp "$AMD64_RPM" "$RPM_ROOT/x86_64/"
cp "$ARM64_RPM" "$RPM_ROOT/aarch64/"

# Generate/update repo metadata
echo "Generating YUM repo metadata..."
createrepo_c --update "$RPM_ROOT/x86_64/"
createrepo_c --update "$RPM_ROOT/aarch64/"

# Sign repomd.xml for each arch
echo "Signing YUM repomd.xml..."
for arch in x86_64 aarch64; do
    gpg --detach-sign --armor \
        -u pkg@modctl.org \
        -o "$RPM_ROOT/$arch/repodata/repomd.xml.asc" \
        "$RPM_ROOT/$arch/repodata/repomd.xml"
done

tree "$WORKDIR"

if [[ "${GITHUB_REF}" == refs/tags/v* ]]; then
  echo "Pushing updated repo to R2..."
  rclone sync --config pkg/rclone.conf "$WORKDIR/repo" r2:modctl-pkgs
fi

exit 0
