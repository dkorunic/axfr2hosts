- To see source files from a dependancy, or to answer questions about a dependancy, run `go mod download -json MODULE`
  and use the returned `Dir` path to read the files.

- Use `go doc foo.bar` or `go doc -all foo` to read documentation for packages, types, functions, etc.

- Use `go run .` or `go run ./cmd/foo` instead of `go build` to run programs, to avoid leaving behind build artifacts.
