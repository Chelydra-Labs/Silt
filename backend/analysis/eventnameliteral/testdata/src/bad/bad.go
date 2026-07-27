// Package bad is a test fixture for the eventnameliteral analyzer.
// Each emit/emitOrQueue call uses a bare string or EventName("…") — diagnostics expected.
package bad

type EventName string

const EventFoo EventName = "foo"

type App struct{}

func (a *App) emit(name EventName, data ...any)             {}
func (a *App) emitOrQueue(name EventName, data ...any)      {}
func aiStreamEventName(base EventName, id string) EventName { return base }

func (a *App) badBareEmit() {
	a.emit("vault:changed", nil) // want `emit/emitOrQueue: use an EventName const from events.go, not bare string literal "vault:changed"`
}

func (a *App) badBareEmitOrQueue() {
	a.emitOrQueue("ai:chunk", nil) // want `emit/emitOrQueue: use an EventName const from events.go, not bare string literal "ai:chunk"`
}

func (a *App) badConversion() {
	a.emit(EventName("nope"), nil) // want `emit/emitOrQueue: use an EventName const from events.go, not EventName\("nope"\) conversion`
}

func (a *App) badParenLit() {
	a.emit(("tpyo:event"), nil) // want `emit/emitOrQueue: use an EventName const from events.go, not bare string literal "tpyo:event"`
}

func (a *App) badStreamHelperLiteral() {
	a.emit(aiStreamEventName(EventName("stream-lit"), "id"), nil) // want `emit/emitOrQueue: use an EventName const from events.go, not EventName\("stream-lit"\) conversion`
}

func (a *App) badLocalFromConversion() {
	n := EventName("indirect-typo")
	a.emit(n, nil) // want `emit/emitOrQueue: use an EventName const from events.go, not an EventName\("indirect-typo"\) value carried through local 'n'`
}

func (a *App) badLocalOneArg() {
	n := EventName("one-arg-typo")
	a.emit(n) // want `emit/emitOrQueue: use an EventName const from events.go, not an EventName\("one-arg-typo"\) value carried through local 'n'`
}

func (a *App) badLocalReassignedTwice() {
	n := EventFoo
	n = EventName("now-typo")
	a.emitOrQueue(n, nil) // want `emit/emitOrQueue: use an EventName const from events.go, not an EventName\("now-typo"\) value carried through local 'n'`
}

func eventFor(i int) EventName { return EventName("helper-lit") }

func (a *App) badHelperReturn() {
	a.emit(eventFor(1), nil) // want `emit/emitOrQueue: use an EventName const from events.go, not an EventName\("helper-lit"\) value carried through helper 'eventFor'`
}

func (a *App) badIndirectStreamViaLocal() {
	n := aiStreamEventName(EventName("indirect-stream-lit"), "id")
	a.emit(n, nil) // want `emit/emitOrQueue: use an EventName const from events.go, not an EventName\("indirect-stream-lit"\) value carried through local 'n'`
}

func (a *App) badHelperReturnViaLocal() {
	n := eventFor(1)
	a.emit(n, nil) // want `emit/emitOrQueue: use an EventName const from events.go, not an EventName\("helper-lit"\) value carried through local 'n'`
}
