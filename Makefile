BINARY := ovh-dynamic-dns
PREFIX := /usr/local
BINDIR := $(PREFIX)/bin
CONFDIR := /etc/ovh-dynamic-dns
SYSTEMDDIR := /etc/systemd/system

.PHONY: build clean install uninstall

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) .

clean:
	rm -f $(BINARY)

install: build
	install -m 0755 $(BINARY) $(BINDIR)/$(BINARY)
	install -d $(CONFDIR)
	test -f $(CONFDIR)/config.json || install -m 0600 config.json.example $(CONFDIR)/config.json
	install -m 0644 $(BINARY).service $(SYSTEMDDIR)/$(BINARY).service
	install -m 0644 $(BINARY).timer $(SYSTEMDDIR)/$(BINARY).timer

uninstall:
	rm -f $(BINDIR)/$(BINARY)
	rm -f $(SYSTEMDDIR)/$(BINARY).service
	rm -f $(SYSTEMDDIR)/$(BINARY).timer
