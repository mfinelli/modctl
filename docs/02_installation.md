# Installation

modctl is available as a native package for all major Linux distributions,
as a precompiled binary, and as a Docker image. The recommended method is
to use your distribution's package manager; it handles updates and ensures
`bsdtar` is installed as a dependency automatically.

Packages are available for `x86_64`(`amd64`)  and `aarch64` (`arm64`).

## Debian / Ubuntu

**1. Import the signing key**
```bash
sudo curl -fsSo /usr/share/keyrings/modctl-archive-keyring.gpg \
  https://pkg.modctl.org/pubkey.gpg
```

**2. Add the repository**
```bash
sudo curl -fsSo /etc/apt/sources.list.d/modctl.sources \
  https://pkg.modctl.org/apt/modctl.sources
```

**3. Install**
```bash
sudo apt update && sudo apt install modctl
```

The package lists `libarchive-tools` as a dependency, which provides `bsdtar`.
It will be installed automatically alongside modctl.

## Fedora / RHEL / Rocky Linux / AlmaLinux

**1. Import the signing key**
```bash
sudo rpm --import https://pkg.modctl.org/pubkey.asc
```

**2. Add the repository**
```bash
sudo curl -fsSo /etc/yum.repos.d/modctl.repo \
  https://pkg.modctl.org/rpm/modctl.repo
```

**3. Install**
```bash
sudo dnf install modctl
```

The package lists `bsdtar` as a dependency. It will be installed automatically
alongside modctl.

## Arch Linux

modctl is available either from a custom package repository or from the AUR.

### Custom repository (recommended)

**1. Import the signing key**
```bash
curl -fsS https://pkg.modctl.org/pubkey.asc | sudo pacman-key -a -
```

**2. Locally sign the key**
```bash
sudo pacman-key --lsign-key 2AF87031171950F11C460B5AEF5F1F6026B2C9C5
```

The key must be locally signed because it is not part of the Arch Linux
keyring.

**3. Add the repository**

Append the following to `/etc/pacman.conf`, before the `[core]` section:
```ini
[modctl]
Server = https://pkg.modctl.org/arch/$arch
SigLevel = Required DatabaseRequired
```

**4. Install**
```bash
sudo pacman -Sy modctl
```

The package lists `libarchive` as a dependency, which provides `bsdtar`. It
will be installed automatically alongside modctl.

### AUR

If you prefer to install from the AUR, the package is available as `modctl`.
Use your preferred AUR helper:
```bash
yay -S modctl
# or
paru -S modctl
```

## Precompiled binary

Precompiled binaries for `x86_64` and `aarch64` are available on the
[GitHub releases page](https://github.com/mfinelli/modctl/releases). Download
the archive for your architecture, extract it, and place the `modctl` binary
somewhere on your `$PATH`, for example `/usr/local/bin`.

You will need to install `bsdtar` manually if it is not already present on
your system:

| Distribution        | Command                             |
|---------------------|-------------------------------------|
| Debian / Ubuntu     | `sudo apt install libarchive-tools` |
| Fedora / RHEL       | `sudo dnf install bsdtar`           |
| Arch Linux          | `sudo pacman -S libarchive`         |

### Verifying the download

Each release includes a SHA256 checksum file and a detached GPG signature.
The files follow this naming convention:

- `modctl_v$VERSION_$ARCH.tar.zst` - the binary archive
- `modctl_v$VERSION_$ARCH.tar.zst.asc` - GPG signature of the binary archive
- `modctl_v$VERSION.sha256` - SHA256 checksums
- `modctl_v$VERSION.sha256.asc` - GPG signature of the checksum file

To verify:
```bash
# Verify the checksum file signature
gpg --verify modctl_v$VERSION.sha256.asc modctl_v$VERSION.sha256

# Verify the binary archive
sha256sum --check --ignore-missing modctl_v$VERSION.sha256

# Verify the binary archive signature
gpg --verify modctl_v$VERSION_$ARCH.tar.zst.asc modctl_v$VERSION_$ARCH.tar.zst
```

Where `$VERSION` is the version that you downloaded and `$ARCH` is your
system architecture (`aarch64` or `x86_64`).

The signing key is available at
[pkg.modctl.org/pubkey.asc](https://pkg.modctl.org/pubkey.asc).

## Docker

A Docker image is available for environments where a native install is not
practical. However, modctl needs access to your Steam library, game
directories, and local data directories to function, which requires mounting
several host paths into the container. For most users a native install is
significantly more convenient.

If you still want to use the Docker image, it is available at
`ghcr.io/mfinelli/modctl`. See the
[GitHub releases page](https://github.com/mfinelli/modctl/releases) for
available tags.

## Building from source

For instructions on building modctl from source, see the
[GitHub repository](https://github.com/mfinelli/modctl).

---

Once modctl is installed, verify it is working:
```bash
modctl --version
```

Then move on to the [Quick Start](../quickstart) guide to set up your first
game and profile.
