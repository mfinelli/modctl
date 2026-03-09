#!/usr/bin/env bash

AMD64_DEB=$(find pkg -name "*_amd64.deb")
ARM64_DEB=$(find pkg -name "*_arm64.deb")
AMD64_RPM=$(find pkg -name "*.x86_64.rpm")
ARM64_RPM=$(find pkg -name "*.aarch64.rpm")

echo "AMD: ${AMD64_DEB}"
echo "ARM: ${ARM64_DEB}"
echo "AMD: ${AMD64_RPM}"
echo "ARM: ${ARM64_RPM}"
