BINARY := ovh-dynamic-dns

.PHONY: build clean

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) .

clean:
	rm -f $(BINARY)
