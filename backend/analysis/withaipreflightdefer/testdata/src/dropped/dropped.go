// Package dropped is a test fixture for the withaipreflightdefer analyzer.
// Each call to withAIPreflight FAILS to defer the done func — diagnostics expected.
package dropped

type provider struct{}
type app struct {
	wg int
}

func (a *app) withAIPreflight(id, token, which string) (provider, string, func(), error) {
	a.wg++
	return provider{}, "model", func() { a.wg-- }, nil
}

// Bad: done captured but never deferred.
func (a *app) badNotDeferred(id, token string) error {
	_, _, done, err := a.withAIPreflight(id, token, "chat") // want `withAIPreflight's done func "done" must be deferred`
	if err != nil {
		return err
	}
	_ = done
	return nil
}

// Bad: done assigned to blank identifier.
func (a *app) badBlank(id, token string) error {
	_, _, _, err := a.withAIPreflight(id, token, "chat") // want `withAIPreflight's done func must be deferred \(assigned to blank identifier\)`
	if err != nil {
		return err
	}
	return nil
}

// Bad: done called but not deferred (a plain call runs immediately, not on return).
func (a *app) badCalledNotDeferred(id, token string) error {
	_, _, done, err := a.withAIPreflight(id, token, "chat") // want `withAIPreflight's done func "done" must be deferred`
	if err != nil {
		return err
	}
	done()
	return nil
}
