VERSION ?= $(shell date -u +%Y.%m.%d)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test dist clean

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o machine-probe .

test:
	go vet ./...
	go test ./...

dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/machine-probe-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/machine-probe-linux-arm64 .

clean:
	rm -rf machine-probe dist
