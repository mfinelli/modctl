#!/usr/bin/env bash

set -e

if [[ $# -ne 0 ]]; then
  echo >&2 "usage: $(basename "$0")"
  exit 1
fi

pkgver="$(grep -P "^\tVersion:" cmd/root.go | awk -F\" '{print $2}')"

mkdir pkg/arch
sed -e "s/PKGVER/$pkgver/" pkg/PKGBUILD > pkg/arch/PKGBUILD
go-licenses save ./... --ignore github.com/mfinelli/modctl --save_path \
  pkg/licenses
find pkg/licenses -type f -exec chmod 0644 {} \;

(
  cd pkg || exit 1
  tar cvf arch/licenses.tar licenses
)

make
./modctl completion bash > pkg/arch/modctl.bash
./modctl completion fish > pkg/arch/modctl.fish
./modctl completion zsh > pkg/arch/modctl.zsh
cp LICENSE pkg/arch
mv modctl pkg/arch

(
  set -e
  chown -R builder:builder pkg/arch
  cd pkg/arch
  su builder -c "PKGEXT='.pkg.tar.zst' makepkg --nodeps"
  find . -name '*-x86_64.pkg.tar.zst' -exec \
    gpg --detach-sign \
      -u pkg@modctl.org \
      {} \;
)

export CC=aarch64-linux-gnu-gcc
export GOARCH=arm64
make modctl
mv modctl pkg/arch

(
  set -e
  cd pkg/arch
  su builder -c "CARCH=aarch64 PKGEXT='.pkg.tar.zst' makepkg --nodeps"
  find . -name '*-aarch64.pkg.tar.zst' -exec \
    gpg --detach-sign \
      -u pkg@modctl.org \
      {} \;
)

exit 0
