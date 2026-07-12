package spellcheck

import "sync"

// Per-pack mutexes so concurrent Ensure calls for the same id serialize
// (double-click / parallel UI + editor load).
var ensureLocks sync.Map // string -> *sync.Mutex

func withEnsureLock(key string, fn func() error) error {
	v, _ := ensureLocks.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	return fn()
}
