# Development

Keep the public import path in `pgislet.go`. Place implementation in the relevant `internal` package, and separate responsibilities into focused files. Do not expose database connections, credentials, or implementation hooks just to accommodate tests.

Place unit tests next to their implementation with a matching `_test.go` filename. Place PostgreSQL-dependent tests in `tests/integration`, grouped by behavior. The integration suite owns its Docker database and privileged test fixtures.

Use blank lines between logical groups and around loops and control-flow blocks. Keep assignments and their immediate error checks together. Avoid compressed one-line functions, and run gofmt. Do not add source-code comments, following the project specification.

```go
func Check(sql, schema string, local map[string]bool) error {
    copyLocal := map[string]bool{}

    for k, v := range local {
        copyLocal[k] = v
    }

    c := checker{schema: schema, local: copyLocal}
    return c.parse(sql, true)
}
```

Run unit tests without Docker:

```sh
go test . ./internal/... ./examples/...
```

Run integration tests with Docker:

```sh
go test ./tests/integration
```

Run all checks with coverage across package boundaries:

```sh
go test -race -coverpkg=./... -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go vet ./...
```
