BINARY  := sqldumper
VERSION := v1.3.0

PREFIX      ?= /usr/local
BINDIR      := $(PREFIX)/bin
MANDIR      := $(PREFIX)/share/man/man1
MANPAGE     := docs/sqldumper.1

.PHONY: all build install uninstall install-man clean

all: build

## Build the binary for the current platform
build:
	go build -ldflags="-s -w" -o $(BINARY) .

## Build for Linux and macOS (arm64 + x64)
build-all:
	GOOS=linux  GOARCH=amd64 go build -ldflags="-s -w" -o bin/$(BINARY)-linux-x64 .
	GOOS=linux  GOARCH=arm64 go build -ldflags="-s -w" -o bin/$(BINARY)-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o bin/$(BINARY)-darwin-x64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o bin/$(BINARY)-darwin-arm64 .

## Install binary + man page to $(PREFIX) (default: /usr/local)
install: build install-man
	install -d $(BINDIR)
	install -m 0755 $(BINARY) $(BINDIR)/$(BINARY)
	@echo "✅ Installed $(BINDIR)/$(BINARY)"

## Install only the man page
install-man:
	install -d $(MANDIR)
	install -m 0644 $(MANPAGE) $(MANDIR)/$(BINARY).1
	@which gzip >/dev/null 2>&1 && gzip -f $(MANDIR)/$(BINARY).1 || true
	@echo "✅ Man page installed — run: man $(BINARY)"

## Remove binary and man page
uninstall:
	rm -f $(BINDIR)/$(BINARY)
	rm -f $(MANDIR)/$(BINARY).1 $(MANDIR)/$(BINARY).1.gz
	@echo "✅ Uninstalled $(BINARY)"

## Remove local build artefacts
clean:
	rm -f $(BINARY)

