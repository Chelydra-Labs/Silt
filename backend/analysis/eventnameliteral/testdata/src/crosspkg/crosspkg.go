// Package crosspkg is a multi-package fixture: emit uses an imported EventName const.
// No diagnostics expected once eventNameConstValues includes imported packages.
package crosspkg

import "evpkg"

type App struct{}

func (a *App) emit(name evpkg.EventName, data ...any)        {}
func (a *App) emitOrQueue(name evpkg.EventName, data ...any) {}

func (a *App) goodImportedConst() {
	a.emit(evpkg.EventImported, nil)
}

func (a *App) goodImportedConstOrQueue() {
	a.emitOrQueue(evpkg.EventImported, nil)
}
