// This directory holds the web console, not Go code.
//
// The file exists solely to make `frontend/` a module boundary, so that Go's
// package matching — `go build ./...`, `go vet ./...`, `go test ./...` — does
// not descend into `frontend/node_modules`. Some npm packages ship vendored Go
// sources (`flatted`, pulled in transitively by eslint, contains a Go
// implementation), and without this marker those would be matched as packages
// of the API's module once anyone runs `npm install`.
//
// This does NOT protect gofmt, which walks the filesystem and ignores module
// boundaries entirely. The Makefile and CI feed gofmt the tracked Go files
// (`git ls-files '*.go'`) for that reason.
//
// Nothing here is built, imported, or published.
module github.com/nemes1s/interbellum/frontend

go 1.25
