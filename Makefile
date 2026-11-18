BIN := primedos
BOT := primedos-bot

build:
	go build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/primedos
	go build -trimpath -ldflags "-s -w" -o $(BOT) ./cmd/primedos-bot

run:
	go run ./cmd/primedos

fmt:
	gofmt -w ./cmd ./internal

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -f $(BIN) $(BOT)

.PHONY: build run fmt vet test clean
