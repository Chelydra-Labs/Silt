// Package bad is a test fixture for the eventnameliteral analyzer.
// Each emit/emitOrQueue call uses a bare string or EventName("…") — diagnostics expected.
package bad

type EventName string

type App struct{}

func (a *App) emit(name EventName, data any)                {}
func (a *App) emitOrQueue(name EventName, data any)         {}
func aiStreamEventName(base EventName, id string) EventName { return base }

func (a *App) badBareEmit() {
	a.emit("vault:changed", nil) // want `emit/emitOrQueue event name must be an EventName const, not a string literal`
}

func (a *App) badBareEmitOrQueue() {
	a.emitOrQueue("ai:chunk", nil) // want `emit/emitOrQueue event name must be an EventName const, not a string literal`
}

func (a *App) badConversion() {
	a.emit(EventName("nope"), nil) // want `emit/emitOrQueue event name must be an EventName const, not a string literal`
}

func (a *App) badStreamHelperLiteral() {
	a.emit(aiStreamEventName(EventName("stream-lit"), "id"), nil) // want `emit/emitOrQueue event name must be an EventName const, not a string literal`
}
