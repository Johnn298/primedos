BIN := primedos

build:
	go build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/primedos

run:
	go run ./cmd/primedos

fmt:
	gofmt -w ./cmd ./internal

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -f $(BIN)

.PHONY: build run fmt vet test clean
