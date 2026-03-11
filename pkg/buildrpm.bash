#!/usr/bin/env bash

set -e

if [[ $# -ne 0 ]]; then
  echo >&2 "usage: $(basename "$0")"
  exit 1
fi

mkdir -p pkg/rpmbuild/{SPECS,SOURCES,BUILD,RPMS,SRPMS}
pkgver="$(grep -P "^\tVersion:" cmd/root.go | awk -F\" '{print $2}')"

rm modctl
cp LICENSE pkg/rpmbuild/SOURCES
cp CHANGELOG.md pkg/rpmbuild/SOURCES
go-licenses save ./... --ignore github.com/mfinelli/modctl --save_path \
  pkg/rpmbuild/SOURCES/licenses
find pkg/rpmbuild/SOURCES/licenses -type f -exec chmod 0644 {} \;

cp modctl.bash modctl.fish modctl.zsh pkg/rpmbuild/SOURCES

make
cp modctl pkg/rpmbuild/SOURCES

sed -e "s/PKGVER/$pkgver/" pkg/modctl.spec > \
  "pkg/rpmbuild/SPECS/modctl.spec"

rpmbuild --define "_topdir $(pwd)/pkg/rpmbuild" \
  --define "_build_arch x86_64" \
  --target x86_64 \
  -bb pkg/rpmbuild/SPECS/modctl.spec

rpm --addsign \
  --define "_gpg_name pkg@modctl.org" \
  --define "__gpg /usr/bin/gpg" \
  "pkg/rpmbuild/RPMS/x86_64/modctl-${pkgver}-1.x86_64.rpm"

rm pkg/rpmbuild/SOURCES/modctl
rm modctl
export CC=aarch64-linux-gnu-gcc
export GOARCH=arm64
make modctl

cp modctl pkg/rpmbuild/SOURCES

sed -e "s/PKGVER/$pkgver/" pkg/modctl.spec > \
  "pkg/rpmbuild/SPECS/modctl.spec"

rpmbuild --define "_topdir $(pwd)/pkg/rpmbuild" \
  --define "_build_arch aarch64" \
  --target aarch64 \
  -bb pkg/rpmbuild/SPECS/modctl.spec

rpm --addsign \
  --define "_gpg_name pkg@modctl.org" \
  --define "__gpg /usr/bin/gpg" \
  "pkg/rpmbuild/RPMS/aarch64/modctl-${pkgver}-1.aarch64.rpm"

exit 0
