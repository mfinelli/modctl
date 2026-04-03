GO := go
SQLC := sqlc
GREP := grep

ifeq ($(shell uname), Darwin)
	GREP := ggrep
endif

SOURCES := $(wildcard *.go cmd/*.go internal/*.go \
	   internal/archivescanner/*.go internal/argresolver/*.go \
	   internal/blobstore/*.go internal/completion/*.go \
	   internal/exporter/*.go internal/extractor/*.go \
	   internal/importer/*.go internal/lock/*.go internal/nexus/*.go \
	   internal/nexusclient/*.go internal/patchapply/*.go \
	   internal/planner/*.go internal/remap/*.go internal/restore/*.go \
	   internal/state/*.go migrations/*.sql)

VERSION ?= $(shell $(GREP) -P "^\tVersion:" cmd/root.go | awk -F\" '{print $$2}')
TODAY ?= $(shell date +%Y-%m-%d)

# Detect target OS (respect GOOS if set, fall back to host)
TARGET_OS ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
ifdef GOOS
	TARGET_OS = $(GOOS)
endif

# Detect target architecture (respect GOARCH if set, fall back to host arch)
TARGET_ARCH ?= $(shell uname -m)
ifdef GOARCH
	ifeq ($(GOARCH),arm64)
		TARGET_ARCH = aarch64
	endif
	ifeq ($(GOARCH),amd64)
		TARGET_ARCH = x86_64
	endif
endif

# hardening flags adapted from archlinux makepkg.conf (GNU ld only)
LDFLAGS_linux ?= -Wl,-O1 -Wl,--sort-common -Wl,--as-needed -Wl,-z,relro \
		 -Wl,-z,now -Wl,-z,pack-relative-relocs

# macOS linker (ld64/lld) doesn't support GNU ld flags;
# PIE and ASLR are enforced by the OS; dead_strip ~= --as-needed
LDFLAGS_darwin ?= -Wl,-dead_strip

LDFLAGS ?= $(LDFLAGS_$(TARGET_OS))

# base flags for all architectures (linux only)
CGO_CFLAGS_BASE_linux ?= -O2 -fno-plt -fexceptions \
			 -Wp,-U_FORTIFY_SOURCE,-D_FORTIFY_SOURCE=3 \
			 -Wformat -Werror=format-security \
			 -fstack-clash-protection \
			 -fno-omit-frame-pointer \
			 -mno-omit-leaf-frame-pointer

# macOS: conservative base flags, let the SDK handle hardening
CGO_CFLAGS_BASE_darwin ?= -O2 -fexceptions \
			   -Wformat -Werror=format-security \
			   -fno-omit-frame-pointer

CGO_CFLAGS_BASE ?= $(CGO_CFLAGS_BASE_$(TARGET_OS))

# x86_64 only flags (linux only)
CGO_CFLAGS_x86_64_linux ?= -fcf-protection

# final flags to actually use
CGO_CFLAGS ?= $(CGO_CFLAGS_BASE) $(CGO_CFLAGS_$(TARGET_ARCH)_$(TARGET_OS))

all: modctl

clean:
	rm -rf modctl dbq sample.tar.gz

modctl: export CGO_ENABLED = 1
modctl: export CGO_CFLAGS := $(CGO_CFLAGS)
modctl: export CGO_LDFLAGS := $(LDFLAGS)
modctl: $(SOURCES) go.mod go.sum dbq/db.go internal/nexusclient/dbc/db.go \
	sample.tar.gz
	$(GO) build -o $@ \
		-buildmode=pie \
		-trimpath \
		-mod=readonly \
		-ldflags "-s -w -linkmode=external -extldflags '$(LDFLAGS)'" \
		-tags='no_clickhouse no_libsql no_mssql no_mysql no_postgres \
			no_vertica no_ydb' \
		main.go

sample.tar.gz:
	echo hello > hello.txt
	bsdtar \
		--format=ustar \
		--uid=0 \
		--gid=0 \
		--uname=root \
		--gname=root \
		-czf $@ \
		hello.txt
	rm hello.txt

dbq/db.go: sqlc.yaml queries.sql $(wildcard migrations/*.sql)
	$(SQLC) generate

internal/nexusclient/dbc/db.go: sqlc.yaml internal/nexusclient/queries.sql \
	internal/nexusclient/schema.sql
	$(SQLC) generate

modctl.1: modctl.1.scd
	sed -e "s/__VERSION__/$(VERSION)/" -e "s/__DATE__/$(TODAY)/" \
		$< | scdoc > $@

.PHONY: all clean
