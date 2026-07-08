// Package deferred is a test fixture for the withaipreflightdefer analyzer.
// Every call to withAIPreflight correctly defers the done func — no diagnostics.
package deferred

type provider struct{}
type app struct {
	wg int
}

func (a *app) withAIPreflight(id, token, which string) (provider, string, func(), error) {
	a.wg++
	return provider{}, "model", func() { a.wg-- }, nil
}

// Correct: done captured and deferred directly.
func (a *app) goodDirect(id, token string) error {
	_, _, done, err := a.withAIPreflight(id, token, "chat")
	if err != nil {
		return err
	}
	defer done()
	return nil
}

// Correct: done deferred inside a closure wrapper.
func (a *app) goodClosure(id, token string) error {
	_, _, done, err := a.withAIPreflight(id, token, "chat")
	if err != nil {
		return err
	}
	defer func() { done() }()
	return nil
}
