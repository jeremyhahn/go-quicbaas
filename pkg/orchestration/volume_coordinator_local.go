package orchestration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	bscommon "github.com/jeremyhahn/go-blockstore/pkg/common"
)

// errVolumeLeaseLost is returned by Renew on a lease that has expired or been
// released.
var errVolumeLeaseLost = errors.New("orchestration: volume lease lost")

// LocalVolumeCoordinator is the single-node VolumeCoordinator: a mutex-guarded
// map with TTL leases on timers and watch fan-out, serving the whole contract
// without raft. Its arbitration stops at the process boundary, so it is safe
// only where one process manages the volume; a replicated pair managed from
// both of its nodes needs NewQRDBVolumeCoordinator over a raft-mode go-qrdb.
type LocalVolumeCoordinator struct {
	mu       sync.Mutex
	kv       map[string][]byte
	leases   map[string]*localVolumeLease
	watchers map[*localVolumeWatcher]struct{}

	// afterFunc schedules a lease's expiry; tests replace it to fire expiry
	// without waiting.
	afterFunc func(time.Duration, func()) interface{ Stop() bool }
}

var _ VolumeCoordinator = (*LocalVolumeCoordinator)(nil)

// NewLocalVolumeCoordinator returns an empty in-process coordinator.
func NewLocalVolumeCoordinator() *LocalVolumeCoordinator {
	return &LocalVolumeCoordinator{
		kv:       map[string][]byte{},
		leases:   map[string]*localVolumeLease{},
		watchers: map[*localVolumeWatcher]struct{}{},
		afterFunc: func(d time.Duration, f func()) interface{ Stop() bool } {
			return time.AfterFunc(d, f)
		},
	}
}

// leaseTTL rounds ttl up to whole seconds with a floor of one, as the
// contract specifies.
func leaseTTL(ttl time.Duration) time.Duration {
	if ttl <= time.Second {
		return time.Second
	}
	return (ttl + time.Second - 1).Truncate(time.Second)
}

// AcquireLease binds key to a new lease when the key is absent, and refuses
// with ErrVolumeLeaseHeld when it exists.
func (c *LocalVolumeCoordinator) AcquireLease(_ context.Context, key string, ttl time.Duration) (VolumeLease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, held := c.leases[key]; held {
		return nil, fmt.Errorf("%w: %q", ErrVolumeLeaseHeld, key)
	}
	if _, exists := c.kv[key]; exists {
		return nil, fmt.Errorf("%w: %q", ErrVolumeLeaseHeld, key)
	}
	l := &localVolumeLease{c: c, key: key, ttl: leaseTTL(ttl), done: make(chan struct{})}
	c.leases[key] = l
	c.setLocked(key, []byte{})
	l.scheduleLocked()
	return l, nil
}

// CompareAndSwap sets key to set when its value is expect (nil: absent).
func (c *LocalVolumeCoordinator) CompareAndSwap(_ context.Context, key string, expect, set []byte) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, exists := c.kv[key]
	if expect == nil && exists || expect != nil && (!exists || !bytes.Equal(cur, expect)) {
		return false, nil
	}
	c.setLocked(key, set)
	return true, nil
}

// Delete removes key; an absent key is not an error.
func (c *LocalVolumeCoordinator) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteLocked(key)
	return nil
}

// Get returns a copy of key's value, or (nil, nil) when it is absent.
func (c *LocalVolumeCoordinator) Get(_ context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.kv[key]
	if !ok {
		return nil, nil
	}
	return append([]byte{}, v...), nil
}

// Put stores val at key.
func (c *LocalVolumeCoordinator) Put(_ context.Context, key string, val []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setLocked(key, val)
	return nil
}

// Watch delivers every change under prefix, in order, until ctx is done.
func (c *LocalVolumeCoordinator) Watch(ctx context.Context, prefix string) (<-chan VolumeCoordinatorEvent, error) {
	w := &localVolumeWatcher{prefix: prefix, notify: make(chan struct{}, 1)}
	c.mu.Lock()
	c.watchers[w] = struct{}{}
	c.mu.Unlock()

	out := make(chan VolumeCoordinatorEvent)
	go func() {
		defer close(out)
		defer func() {
			c.mu.Lock()
			delete(c.watchers, w)
			c.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.notify:
			}
			for _, ev := range w.drain() {
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (c *LocalVolumeCoordinator) setLocked(key string, val []byte) {
	v := append([]byte{}, val...)
	c.kv[key] = v
	c.emitLocked(VolumeCoordinatorEvent{Type: bscommon.EventTypePut, Key: key, Value: append([]byte{}, v...)})
}

func (c *LocalVolumeCoordinator) deleteLocked(key string) {
	if _, ok := c.kv[key]; !ok {
		return
	}
	delete(c.kv, key)
	c.emitLocked(VolumeCoordinatorEvent{Type: bscommon.EventTypeDelete, Key: key})
}

func (c *LocalVolumeCoordinator) emitLocked(ev VolumeCoordinatorEvent) {
	for w := range c.watchers {
		if strings.HasPrefix(ev.Key, w.prefix) {
			w.push(ev)
		}
	}
}

// endLocked ends l if it still holds its key: the key is deleted and Done
// closes.
func (c *LocalVolumeCoordinator) endLocked(l *localVolumeLease) bool {
	if c.leases[l.key] != l {
		return false
	}
	delete(c.leases, l.key)
	if l.timer != nil {
		l.timer.Stop()
	}
	c.deleteLocked(l.key)
	close(l.done)
	return true
}

// localVolumeWatcher queues events for one Watch without blocking the writer.
type localVolumeWatcher struct {
	prefix string
	notify chan struct{}
	mu     sync.Mutex
	queue  []VolumeCoordinatorEvent
}

func (w *localVolumeWatcher) push(ev VolumeCoordinatorEvent) {
	w.mu.Lock()
	w.queue = append(w.queue, ev)
	w.mu.Unlock()
	select {
	case w.notify <- struct{}{}:
	default:
	}
}

func (w *localVolumeWatcher) drain() []VolumeCoordinatorEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	q := w.queue
	w.queue = nil
	return q
}

// localVolumeLease is a lease held on a LocalVolumeCoordinator.
type localVolumeLease struct {
	c     *LocalVolumeCoordinator
	key   string
	ttl   time.Duration
	done  chan struct{}
	gen   uint64
	timer interface{ Stop() bool }
}

// scheduleLocked (re)arms the expiry; an expiry armed before a renewal finds
// a newer generation and does nothing.
func (l *localVolumeLease) scheduleLocked() {
	if l.timer != nil {
		l.timer.Stop()
	}
	l.gen++
	gen := l.gen
	l.timer = l.c.afterFunc(l.ttl, func() {
		l.c.mu.Lock()
		defer l.c.mu.Unlock()
		if l.gen == gen {
			l.c.endLocked(l)
		}
	})
}

func (l *localVolumeLease) Renew(context.Context) error {
	l.c.mu.Lock()
	defer l.c.mu.Unlock()
	if l.c.leases[l.key] != l {
		return fmt.Errorf("%w: %q", errVolumeLeaseLost, l.key)
	}
	l.scheduleLocked()
	return nil
}

// Release ends the lease and deletes its key. Releasing a lease already lost
// is a no-op: the key is no longer this lease's to delete.
func (l *localVolumeLease) Release(context.Context) error {
	l.c.mu.Lock()
	defer l.c.mu.Unlock()
	l.c.endLocked(l)
	return nil
}

func (l *localVolumeLease) Done() <-chan struct{} { return l.done }

func (l *localVolumeLease) Key() string { return l.key }
