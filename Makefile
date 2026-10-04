APP     := permwatch
GO_DIRS := cmd internal

.PHONY: build test test-integration lint check run docker clean

build: ## build bin/permwatch
	CGO_ENABLED=0 go build -trimpath -o bin/$(APP) ./cmd/$(APP)

test: ## unit tests with the race detector
	go test -race ./...

test-integration: ## tests that read the live KleverChain testnet API (needs network)
	go test -race -tags integration ./...

lint: ## go vet (with and without build tags) and gofmt
	go vet ./...
	go vet -tags integration ./...
	@out="$$(gofmt -l $(GO_DIRS))"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

check: lint test ## must pass before any change is done

run: build ## watch the accounts configured in .env
	set -a && . ./.env && set +a && ./bin/$(APP) watch

docker: ## build the container image
	docker build -f deploy/Dockerfile -t $(APP):dev .

clean:
	rm -rf bin
