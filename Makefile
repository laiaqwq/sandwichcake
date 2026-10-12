PREFIX ?= /usr/local
MODULE := github.com/baba/sandwichcake
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X $(MODULE)/pkg/version.Version=$(VERSION)

.PHONY: build test install vet

build:
	go build -ldflags "$(LDFLAGS)" -o bin/sandwichcake ./cmd/sandwichcake

test:
	go test ./...

vet:
	go vet ./...

install: build
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 755 bin/sandwichcake $(DESTDIR)$(PREFIX)/bin/sandwichcake
	ln -sf sandwichcake $(DESTDIR)$(PREFIX)/bin/swcake
