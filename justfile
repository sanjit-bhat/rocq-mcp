ci: test fmt vet fix tidy

test:
    go test ./...

fmt:
    test -z "$(gofmt -l -s .)"

vet:
    go vet ./...

fix:
    go fix -diff ./...

tidy:
    go mod tidy && git diff --exit-code go.mod go.sum
