Name: modctl
Version: PKGVER
Release: 1
Requires: bsdtar
Summary: command line mod manager
License: GPL-3.0-or-later

# Skip the build system entirely
%global debug_package %{nil}
# don't include build ID symlinks
%global _build_id_links none

%description
modctl installs mods by extracting archives directly into game directories
while tracking every installed file. It supports per-profile priority
ordering, conflict detection, and safe rollback via automatic backups.

%install
install -vDm0755 %{_sourcedir}/modctl %{buildroot}/usr/bin/modctl
install -vDm0644 %{_sourcedir}/LICENSE %{buildroot}/usr/share/licenses/%{name}/LICENSE
install -vDm0644 %{_sourcedir}/README.md %{buildroot}/usr/share/doc/%{name}/README.md
install -vDm0644 %{_sourcedir}/CHANGELOG.md %{buildroot}/usr/share/doc/%{name}/CHANGELOG.md
cp -r %{_sourcedir}/docs/. %{buildroot}/usr/share/doc/%{name}/
install -vdm0755 %{buildroot}/usr/share/licenses/%{name}/vendor
cp -r %{_sourcedir}/licenses/. %{buildroot}/usr/share/licenses/%{name}/vendor/
install -vDm0644 %{_sourcedir}/modctl.bash %{buildroot}/usr/share/bash-completion/completions/modctl
install -vDm0644 %{_sourcedir}/modctl.fish %{buildroot}/usr/share/fish/vendor_completions.d/modctl.fish
install -vDm0644 %{_sourcedir}/modctl.zsh %{buildroot}/usr/share/zsh/site-functions/_modctl

%files
/usr/bin/modctl
/usr/share/bash-completion/completions/modctl
/usr/share/fish/vendor_completions.d/modctl.fish
/usr/share/zsh/site-functions/_modctl
/usr/share/doc/%{name}/
%license /usr/share/licenses/%{name}/LICENSE
%license /usr/share/licenses/%{name}/vendor/
