BINARY := smsmail
PKG := ./cmd/smsmail
STATICCHECK_VERSION := 2025.1.1
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build test vet fmt staticcheck check run install docker clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

# The same checks the CI workflow runs, in the same order.
check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed for:"; echo "$$unformatted"; exit 1; \
	fi
	go vet ./...
	go test -race ./...
	go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...
	@echo "all checks passed"

run:
	go run $(PKG)

install:
	go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

docker:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) \
		-t $(BINARY) .

clean:
	rm -f $(BINARY) coverage.out
