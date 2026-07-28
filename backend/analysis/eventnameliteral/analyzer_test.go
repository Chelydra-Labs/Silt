package eventnameliteral_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"silt/backend/analysis/eventnameliteral"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), eventnameliteral.Analyzer, "bad", "good", "crosspkg")
}
