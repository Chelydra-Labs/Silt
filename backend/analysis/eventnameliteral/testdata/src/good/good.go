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
