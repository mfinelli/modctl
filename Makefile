GO := go

SOURCES := $(wildcard *.go cmd/*.go internal/*.go migrations/*.sql)

all: modctl

clean:
	rm -rf modctl sample.tar.gz

modctl: export CGO_ENABLED = 1
modctl: $(SOURCES) go.mod go.sum sample.tar.gz
	$(GO) build -o $@ \
		-buildmode=pie \
		-trimpath \
		-mod=readonly \
		-ldflags "-linkmode=external" \
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

.PHONY: all clean
