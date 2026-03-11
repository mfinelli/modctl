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

bname="modctl_${GITHUB_REF_NAME//\//-}"

mkdir "${bname}"
git archive HEAD | tar -x -C "${bname}"
cd "${bname}"
sqlc generate
cd ..
cp -r vendor "${bname}"
tar --owner=0 --group=0 --sort=name -cavf "${bname}.tar.zst" "${bname}"
gpg -u pkg@modctl.org -ba "${bname}.tar.zst"

make
./modctl completion bash > modctl.bash
./modctl completion fish > modctl.fish
./modctl completion zsh > modctl.zsh

mkdir "${bname}_amd64"
mkdir "${bname}_arm64"

mv modctl "${bname}_amd64"
export CC=aarch64-linux-gnu-gcc
export GOARCH=arm64
make modctl
mv modctl "${bname}_arm64"

for arch in amd64 arm64; do
  cp LICENSE "${bname}_${arch}"
  cp -r licenses "${bname}_${arch}"
  cp modctl.bash "${bname}_${arch}"
  cp modctl.fish "${bname}_${arch}"
  cp modctl.zsh "${bname}_${arch}"
  tar --owner=0 --group=0 --sort=name -cavf "${bname}_${arch}.tar.zst" \
    "${bname}_${arch}"
  gpg -u pkg@modctl.org -ba "${bname}_${arch}.tar.zst"
done

sha256sum -b ./*.tar.zst > "${bname}.sha256"

exit 0
