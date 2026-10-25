// This empty module only exists so that `go build ./...` / `go vet ./...`
// in the repository root skip web/ (and its node_modules, where some npm
// packages ship stray .go files). Nothing here is compiled.
module bailing/web

go 1.23
