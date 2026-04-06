default:
    @just --list

build:
    go build -o sideshow ./cmd/sideshow

run *args:
    go run ./cmd/sideshow {{args}}

clean:
    rm -f sideshow

fmt:
    gofumpt -w .

lint:
    golangci-lint run ./...

vet:
    go vet ./...

test:
    go test ./... -v

test-race:
    go test ./... -v -race

test-cover:
    go test ./... -v -coverprofile=coverage.out -covermode=atomic
    go tool cover -func=coverage.out

ci: fmt lint vet test
