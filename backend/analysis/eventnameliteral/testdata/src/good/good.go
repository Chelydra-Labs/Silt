// Package good is a test fixture for the eventnameliteral analyzer.
// Every emit/emitOrQueue call uses an EventName const (or helper of one) — no diagnostics.
package good

type EventName string

const EventFoo EventName = "foo"
const EventBar EventName = "bar"

type App struct{}

func (a *App) emit(name EventName, data any)                {}
func (a *App) emitOrQueue(name EventName, data any)         {}
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

// Intentional scope limit: the analyzer does not track EventName("…") through
// locals (no SSA). Construction-site typos remain a human/review concern.
func (a *App) goodLocalFromConversion() {
	n := EventName("indirect-typo")
	a.emit(n, nil)
}
