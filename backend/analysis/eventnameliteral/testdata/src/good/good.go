// Package good is a test fixture for the eventnameliteral analyzer.
// Every emit/emitOrQueue call uses an EventName const (or helper of one) — no diagnostics.
package good

type EventName string

const EventFoo EventName = "foo"
const EventBar EventName = "bar"

type App struct{}

func (a *App) emit(name EventName, data ...any)             {}
func (a *App) emitOrQueue(name EventName, data ...any)      {}
func aiStreamEventName(base EventName, id string) EventName { return base }

func (a *App) goodConstEmit() {
	a.emit(EventFoo, nil)
}

func (a *App) goodConstEmitOrQueue() {
	a.emitOrQueue(EventBar, nil)
}

func (a *App) goodStreamHelper() {
	a.emit(aiStreamEventName(EventFoo, "id"), nil)
}

func (a *App) goodParam(name EventName) {
	a.emit(name, nil)
}

func (a *App) goodLocal() {
	n := EventFoo
	a.emitOrQueue(n, nil)
}

// A helper composing the name dynamically must stay allowed.
func dynHelper(base EventName, id string) EventName {
	return EventName(string(base) + ":" + id)
}

func (a *App) goodDynamicHelper() {
	a.emit(dynHelper(EventFoo, "id"), nil)
}

func (a *App) goodConditionalLocalAssign(cond bool) {
	var n EventName
	if cond {
		n = EventName("branch-typo")
	}
	a.emit(n, nil) // phi merge → can't prove → allowed (no diagnostic)
}

// A recursive EventName-returning helper must not crash the analyzer — the
// cycle guard in helperReturnsLiteral short-circuits to a conservative allow.
// (analyzed only; never executed, so the self-call is not a runtime loop.)
func recEvent() EventName { return recEvent() }

func (a *App) goodRecursiveHelper() {
	a.emit(recEvent(), nil) // cycle → allowed, no crash
}

// A helper returning two different literals must not flag — conservative: can't
// prove which branch wins, so the whole helper is allowed.
func twoLiterals(i int) EventName {
	if i > 0 {
		return EventName("lit-a")
	}
	return EventName("lit-b")
}

func (a *App) goodMultiLiteralHelper() {
	a.emit(twoLiterals(1), nil) // two literals → allowed (no diagnostic)
}

// Store after emit must not poison the earlier use (dominance-aware locals).
func (a *App) goodEmitThenLaterStore() {
	n := EventFoo
	a.emit(n, nil) // const at emit — allowed
	n = EventName("later-store-typo")
	_ = n
}
