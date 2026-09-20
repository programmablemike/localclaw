#!/bin/sh
# The one check entry point. Run as: flox activate -- ./scripts/check.sh
# CI and AGENTS.md call exactly this. Every step prints its name first so a
# failure is easy to place.
set -eu
cd "$(dirname "$0")/.."

echo "==> gofmt"
unformatted=$(find . -name '*.go' -not -path './vendor/*' -not -path './.flox/*' -print0 | xargs -0 gofmt -l)
if [ -n "$unformatted" ]; then
  echo "$unformatted"
  echo "gofmt: the files above need formatting (run: gofmt -w <file>)" >&2
  exit 1
fi

echo "==> go vet"
go vet ./...

echo "==> staticcheck"
staticcheck ./...

echo "==> govulncheck"
govulncheck ./...

echo "==> go test -race"
go test -race ./...

echo "==> go mod tidy -diff"
go mod tidy -diff

echo "==> vendor drift"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
go mod vendor -o "$tmp/vendor"
if [ -d vendor ] || [ -d "$tmp/vendor" ]; then
  diff -r vendor "$tmp/vendor"
fi

echo "==> ok"
