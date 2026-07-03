BINARY := arrsenal

.PHONY: build test vet lint snapshot clean

build:
	go build -o $(BINARY) ./cmd/arrsenal

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run

# Full release dry-run: linux binaries + checksums under dist/, nothing published.
snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf dist $(BINARY)
