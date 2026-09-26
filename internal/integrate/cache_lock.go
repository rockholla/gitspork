package integrate

import (
	"fmt"

	"github.com/gofrs/flock"
)

// Cache entries are guarded by flock(2) on a per-URL lock file. Every
// acquisition opens its own *flock.Flock (its own file descriptor): flock(2)
// locks belong to the open file description, so two descriptors conflict
// even within one process, and goroutines are excluded from each other the
// same way separate processes are. Sharing one *flock.Flock between callers
// would not work — its Lock returns immediately when that instance is already
// locked, and the first Unlock drops the lock for everyone.
//
// Writers (populate, refresh, wipe-and-retry) hold the exclusive lock;
// readers (working clones from the mirror) hold the shared lock, so a clone
// never reads a mirror that another call is writing to or deleting.

// lockCacheEntry blocks until it holds the exclusive lock on lockFile and
// returns the function that releases it.
func lockCacheEntry(lockFile string) (unlock func(), err error) {
	fl := flock.New(lockFile)
	if err := fl.Lock(); err != nil {
		return nil, fmt.Errorf("acquiring upstream cache lock at %s: %w", lockFile, err)
	}
	return func() { _ = fl.Unlock() }, nil
}

// rLockCacheEntry blocks until it holds a shared lock on lockFile and returns
// the function that releases it.
func rLockCacheEntry(lockFile string) (unlock func(), err error) {
	fl := flock.New(lockFile)
	if err := fl.RLock(); err != nil {
		return nil, fmt.Errorf("acquiring shared upstream cache lock at %s: %w", lockFile, err)
	}
	return func() { _ = fl.Unlock() }, nil
}
