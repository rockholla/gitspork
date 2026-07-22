package integrate

import (
	"sync"

	"github.com/gofrs/flock"
)

// In-process singleton registry of *flock.Flock instances keyed by lock-file
// path. POSIX flock(2) is per-open-file-description (not per-process): two
// goroutines in the same process each calling flock.New(path).Lock() would
// obtain separate fds and could BOTH claim the lock simultaneously. Routing
// every in-process caller through the same *flock.Flock instance for a given
// path resolves that — flock's own mutex serialises the shared instance.
//
// Cross-process callers each construct their own map entry in their own
// address space; the OS flock coordinates them via the kernel.
var (
	flocksMu sync.Mutex
	flocks   = map[string]*flock.Flock{}
)

func getOrCreateFlock(path string) *flock.Flock {
	flocksMu.Lock()
	defer flocksMu.Unlock()
	if f, ok := flocks[path]; ok {
		return f
	}
	f := flock.New(path)
	flocks[path] = f
	return f
}
