GO := go

SOURCES := $(wildcard *.go cmd/*.go)

all: modctl

modctl: $(SOURCES) go.mod go.sum
	$(GO) build -o $@ \
		-buildmode=pie \
		-trimpath \
		-mod=readonly \
		-ldflags "-linkmode=external" \
		main.go

.PHONY: all
