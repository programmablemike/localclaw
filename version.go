// Package localclaw is the module root. It exists to embed the VERSION file
// so every build, including `go run` and the Nix build, reports the same
// version without linker flags.
package localclaw

import _ "embed"

// Version is the contents of the VERSION file at the repository root. It may
// end in a newline; callers trim it.
//
//go:embed VERSION
var Version string
