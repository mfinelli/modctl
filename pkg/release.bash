#!/usr/bin/env bash

set -e

if [[ $# -ne 0 ]]; then
  echo >&2 "usage: $(basename "$0")"
  exit 1
fi

sqlc generate
go mod vendor

go-licenses save ./... --ignore github.com/mfinelli/modctl --save_path \
    licenses
find licenses -type f -exec chmod 0644 {} \;

mkdir "modctl_${GITHUB_REF_NAME}"
git archive HEAD | tar -x -C "modctl_${GITHUB_REF_NAME}"
cd "modctl_${GITHUB_REF_NAME}"
sqlc generate
cd ..
cp -r vendor "modctl_${GITHUB_REF_NAME}"
tar --owner=0 --group=0 -cavf "modctl_${GITHUB_REF_NAME}.tar.zst" "modctl_${GITHUB_REF_NAME}"
gpg -u pkg@modctl.org -ba "modctl_${GITHUB_REF_NAME}.tar.zst"
rm -rf "modctl_${GITHUB_REF_NAME}"

make
./modctl completion bash > modctl.bash
./modctl completion fish > modctl.fish
./modctl completion zsh > modctl.zsh

mkdir "modctl_${GITHUB_REF_NAME}_amd64"
mkdir "modctl_${GITHUB_REF_NAME}_arm64"

mv modctl "modctl_${GITHUB_REF_NAME}_amd64"
export CC=aarch64-linux-gnu-gcc
export GOARCH=arm64
make modctl
mv modctl "modctl_${GITHUB_REF_NAME}_arm64"

for arch in amd64 arm64; do
  cp LICENSE "modctl_${GITHUB_REF_NAME}_${arch}"
  cp -r licenses "modctl_${GITHUB_REF_NAME}_${arch}"
  cp modctl.bash "modctl_${GITHUB_REF_NAME}_${arch}"
  cp modctl.fish "modctl_${GITHUB_REF_NAME}_${arch}"
  cp modctl.zsh "modctl_${GITHUB_REF_NAME}_${arch}"
  tar --owner=0 --group=0 -cavf "modctl_${GITHUB_REF_NAME}_${arch}.tar.zst" "modctl_${GITHUB_REF_NAME}_${arch}"
  gpg -u pkg@modctl.org -ba "modctl_${GITHUB_REF_NAME}_${arch}.tar.zst"
done

sha256sum -b ./*.tar.zst > "modctl_${GITHUB_REF_NAME}.sha256"

exit 0
