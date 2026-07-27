//go:build tools

// Command eventnameliteral is a vettool binary that runs the
// eventnameliteral analyzer. Built with -tags tools and invoked as
// `go vet -vettool=./eventnameliteral ./...`. The build tag keeps this
// binary (and its golang.org/x/tools dependency) out of the production build.
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"silt/backend/analysis/eventnameliteral"
)

func main() { singlechecker.Main(eventnameliteral.Analyzer) }
