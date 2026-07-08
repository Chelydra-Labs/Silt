//go:build tools

// Command withaipreflightdefer is a vettool binary that runs the
// withAIPreflightdefer analyzer. Built with -tags tools and invoked as
// `go vet -vettool=./withaipreflightdefer ./...`. The build tag keeps this
// binary (and its golang.org/x/tools dependency) out of the production build.
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"silt/backend/analysis/withaipreflightdefer"
)

func main() { singlechecker.Main(withaipreflightdefer.Analyzer) }
