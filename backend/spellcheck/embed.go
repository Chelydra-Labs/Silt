package spellcheck

import _ "embed"

//go:embed data/software-terms.txt
var embeddedSoftwareTerms string

func init() {
	BundledSoftwareTerms = embeddedSoftwareTerms
}
