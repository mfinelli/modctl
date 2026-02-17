GO := go

SOURCES := $(wildcard *.go cmd/*.go internal/*.go migrations/*.sql)

all: modctl

clean:
	rm -rf modctl

modctl: export CGO_ENABLED = 1
modctl: $(SOURCES) go.mod go.sum
	$(GO) build -o $@ \
		-buildmode=pie \
		-trimpath \
		-mod=readonly \
		-ldflags "-linkmode=external" \
		-tags='no_clickhouse no_libsql no_mssql no_mysql no_postgres \
			no_vertica no_ydb' \
		main.go

.PHONY: all clean
