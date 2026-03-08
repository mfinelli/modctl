#!/usr/bin/env bash

set -e

if [[ $# -ne 0 ]]; then
  echo >&2 "usage: $(basename "$0")"
  exit 1
fi

pkgver="$(grep -P "^\tVersion:" cmd/root.go | awk -F\" '{print $2}')"

for arch in amd64 arm64; do
  dir="pkg/modctl_${pkgver}_${arch}"
  mkdir -p "${dir}/DEBIAN"

  # mkdir -p "pkg/modctl_${pkgver}_${arch}/usr/share/man/man1"
  # mkdir -p "pkg/modctl_${pkgver}_${arch}/usr/share/doc/modctl"

  install -vDm0644 LICENSE "${dir}/usr/share/doc/modctl/copyright"
  go-licenses save ./... --ignore github.com/mfinelli/modctl --save_path \
    "${dir}/usr/share/doc/modctl/licenses"
done


make
./modctl completion bash > modctl.bash
./modctl completion fish > modctl.fish
./modctl completion zsh > modctl.zsh

minlibc="$(objdump -p ./modctl | grep GLIBC | grep -oP 'GLIBC_\K[\d.]+' |
  sort -V | tail -1)"

for arch in amd64 arm64; do
  dir="pkg/modctl_${pkgver}_${arch}"

  install -vDm0644 modctl.bash \
    "${dir}/usr/share/bash-completions/completions/modctl"
  install -vDm0644 modctl.fish \
    "${dir}/usr/share/fish/vendor_completions.d/modctl.fish"
  install -vDm0644 modctl.zsh \
    "${dir}/usr/share/zsh/vendor-completions/_modctl"
done

sed -e "s/MINGLIBC/$minlibc/" -e "s/PKGARCH/amd64/" -e "s/PKGVER/$pkgver/" \
  pkg/control > "pkg/modctl_${pkgver}_amd64/DEBIAN/control"
install -vDm0755 modctl "pkg/modctl_${pkgver}_amd64/usr/bin/modctl"

rm modctl
export CC=aarch64-linux-gnu-gcc
export GOARCH=arm64
make modctl

minlibc="$(aarch64-linux-gnu-objdump -p ./modctl | grep GLIBC |
  grep -oP 'GLIBC_\K[\d.]+' | sort -V | tail -1)"

sed -e "s/MINGLIBC/$minlibc/" -e "s/PKGARCH/arm64/" -e "s/PKGVER/$pkgver/" \
  pkg/control > "pkg/modctl_${pkgver}_arm64/DEBIAN/control"
install -vDm0755 modctl "pkg/modctl_${pkgver}_arm64/usr/bin/modctl"

for arch in amd64 arm64; do
  dpkg-deb --build --root-owner-group "pkg/modctl_${pkgver}_${arch}"
done

exit 0
