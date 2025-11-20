package state

import (
	"sync"
)

var (
	busyMu sync.Mutex
	// Used as an indicator when content is being downloaded by any arbitrary worker
	busy bool
)

func setBusy(v bool) {
	busyMu.Lock()
	busy = v
	busyMu.Unlock()
}

func isBusy() bool {
	busyMu.Lock()
	defer busyMu.Unlock()
	return busy
}
