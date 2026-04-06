# Go Quality Rules

## Idiomatic Go

1. Use `fmt.Fprintf` — never `WriteString` + `Sprintf`
2. Never nil-check before `len` — len handles nil slices/maps
3. Always check error returns
4. Wrap errors with context using `%w`
5. Accept interfaces, return concrete types
6. `context.Context` is always the first parameter
7. Prefer value receivers unless mutation is needed
8. No `init()` functions — pass dependencies explicitly
9. Timestamps always in UTC: `time.Now().UTC()`

## Error Handling

- Every exported function that can fail returns `error` as last value
- Use `errors.Is()` and `errors.As()` — never string matching
- Define sentinel errors as package-level `var`
- No panics in library code

## Testing

- Table-driven tests for >2 test cases
- Use stdlib `testing` — no testify
- Use `t.Helper()` in test helpers
- Use `t.Cleanup()` instead of `defer`
- Test files alongside source: `foo.go` -> `foo_test.go`
- Mark independent tests with `t.Parallel()`

## Formatting & Linting

- `gofumpt` for formatting (stricter than `gofmt`)
- `golangci-lint run ./...` must pass with zero warnings
- Never disable linter rules without a justifying comment
