install:
    go install

test:
    go test -v ./...

format:
    gofmt -w -s .
    git diff --exit-code

vet:
    go vet ./...

fix:
    go fix ./...
    git diff --exit-code

lint:
    staticcheck ./...

lint-all: format vet fix lint
