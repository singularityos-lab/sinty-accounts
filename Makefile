BIN := sinty-accountsd
PREFIX ?= /usr

.PHONY: all build static test vet install clean

all: build

build:
	go build -o $(BIN) ./cmd/sinty-accountsd

static:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/sinty-accountsd

test:
	go test ./...

vet:
	go vet ./...

install: static
	install -Dm755 $(BIN) $(DESTDIR)$(PREFIX)/libexec/$(BIN)
	install -Dm644 packaging/org.freedesktop.Accounts.conf $(DESTDIR)$(PREFIX)/share/dbus-1/system.d/org.freedesktop.Accounts.conf
	install -Dm644 packaging/org.freedesktop.Accounts.service $(DESTDIR)$(PREFIX)/share/dbus-1/system-services/org.freedesktop.Accounts.service
	install -Dm644 packaging/sinty-accounts.service $(DESTDIR)$(PREFIX)/lib/systemd/system/sinty-accounts.service

clean:
	rm -f $(BIN)
