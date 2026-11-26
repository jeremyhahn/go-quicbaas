package orchestration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	bscommon "github.com/jeremyhahn/go-blockstore/pkg/common"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// manualTimers captures each scheduled expiry so a test fires it on demand.
type manualTimers struct {
	mu    sync.Mutex
	fires []func()
	ttls  []time.Duration
}

type stopTimer struct{}

func (stopTimer) Stop() bool { return true }

func (m *manualTimers) after(d time.Duration, f func()) interface{ Stop() bool } {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fires = append(m.fires, f)
	m.ttls = append(m.ttls, d)
	return stopTimer{}
}

func (m *manualTimers) fire(i int) { m.fires[i]() }

func localCoordinator() (*LocalVolumeCoordinator, *manualTimers) {
	c := NewLocalVolumeCoordinator()
	timers := &manualTimers{}
	c.afterFunc = timers.after
	return c, timers
}

func TestLocalVolumeCoordinatorLeaseIsExclusive(t *testing.T) {
	ctx := context.Background()
	c, timers := localCoordinator()
	l, err := c.AcquireLease(ctx, "vol/a", 1500*time.Millisecond)
	if err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	if timers.ttls[0] != 2*time.Second {
		t.Errorf("ttl = %v, want rounded up to 2s", timers.ttls[0])
	}
	if v, _ := c.Get(ctx, "vol/a"); v == nil {
		t.Error("lease key absent while held")
	}
	if _, err := c.AcquireLease(ctx, "vol/a", time.Second); !errors.Is(err, ErrVolumeLeaseHeld) {
		t.Errorf("second AcquireLease = %v, want ErrVolumeLeaseHeld", err)
	}
	if err := l.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
	select {
	case <-l.Done():
	default:
		t.Error("Done open after Release")
	}
	if err := l.Release(ctx); err != nil {
		t.Errorf("second Release = %v, want nil", err)
	}
	if v, _ := c.Get(ctx, "vol/a"); v != nil {
		t.Error("lease key survives Release")
	}
	if _, err := c.AcquireLease(ctx, "vol/a", time.Second); err != nil {
		t.Errorf("AcquireLease after Release = %v", err)
	}
}

// An expiry armed before a renewal is stale; the renewed one ends the lease,
// after which Renew fails and the key is free.
func TestLocalVolumeCoordinatorLeaseExpiry(t *testing.T) {
	ctx := context.Background()
	c, timers := localCoordinator()
	l, err := c.AcquireLease(ctx, "vol/a", time.Second)
	if err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	if err := l.Renew(ctx); err != nil {
		t.Fatalf("Renew: %v", err)
	}
	timers.fire(0)
	if err := l.Renew(ctx); err != nil {
		t.Fatalf("stale expiry ended the lease: Renew = %v", err)
	}
	timers.fire(2)
	select {
	case <-l.Done():
	default:
		t.Fatal("Done open after expiry")
	}
	if err := l.Renew(ctx); !errors.Is(err, errVolumeLeaseLost) {
		t.Errorf("Renew after expiry = %v", err)
	}
	if _, err := c.AcquireLease(ctx, "vol/a", time.Second); err != nil {
		t.Errorf("AcquireLease after expiry = %v", err)
	}
	// The expired lease's Release must not free its successor's key.
	_ = l.Release(ctx)
	if v, _ := c.Get(ctx, "vol/a"); v == nil {
		t.Error("stale Release deleted the successor's key")
	}
}

func TestLocalVolumeCoordinatorKV(t *testing.T) {
	ctx := context.Background()
	c := NewLocalVolumeCoordinator()
	if v, err := c.Get(ctx, "k"); v != nil || err != nil {
		t.Errorf("Get absent = %v, %v", v, err)
	}
	if ok, _ := c.CompareAndSwap(ctx, "k", []byte{}, []byte("x")); ok {
		t.Error("non-nil expect matched an absent key")
	}
	if ok, _ := c.CompareAndSwap(ctx, "k", nil, []byte{}); !ok {
		t.Error("create-only CAS failed on an absent key")
	}
	if v, _ := c.Get(ctx, "k"); v == nil || len(v) != 0 {
		t.Errorf("Get empty = %v, want non-nil empty", v)
	}
	if ok, _ := c.CompareAndSwap(ctx, "k", nil, []byte("x")); ok {
		t.Error("create-only CAS overwrote a present key")
	}
	if ok, _ := c.CompareAndSwap(ctx, "k", []byte{}, []byte("x")); !ok {
		t.Error("CAS on a matching value failed")
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Errorf("Delete = %v", err)
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Errorf("Delete absent = %v", err)
	}
}

func TestLocalVolumeCoordinatorWatch(t *testing.T) {
	c := NewLocalVolumeCoordinator()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := c.Watch(ctx, "vol/")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	bg := context.Background()
	_ = c.Put(bg, "other", []byte("no"))
	_ = c.Put(bg, "vol/a", []byte("1"))
	_ = c.Delete(bg, "vol/a")
	want := []VolumeCoordinatorEvent{
		{Type: bscommon.EventTypePut, Key: "vol/a", Value: []byte("1")},
		{Type: bscommon.EventTypeDelete, Key: "vol/a"},
	}
	for i, w := range want {
		select {
		case ev := <-ch:
			if ev.Type != w.Type || ev.Key != w.Key || string(ev.Value) != string(w.Value) {
				t.Errorf("event %d = %+v, want %+v", i, ev, w)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("event %d not delivered", i)
		}
	}
	cancel()
	for range ch {
	}
}

// fakeQRDB is go-qrdb's coordinator and KV delete, scripted.
type fakeQRDB struct {
	acquireErr error
	deleteErr  error
	events     chan qrdbsdk.Event
}

func (f *fakeQRDB) AcquireLease(context.Context, string, time.Duration) (qrdbsdk.Lease, error) {
	return nil, f.acquireErr
}
func (f *fakeQRDB) CompareAndSwap(context.Context, string, []byte, []byte) (bool, error) {
	return true, nil
}
func (f *fakeQRDB) Get(context.Context, string) ([]byte, error) { return nil, nil }
func (f *fakeQRDB) Put(context.Context, string, []byte) error   { return nil }
func (f *fakeQRDB) Events() <-chan qrdbsdk.Event                { return f.events }
func (f *fakeQRDB) Err() error                                  { return nil }
func (f *fakeQRDB) Delete(context.Context, string, ...transport.WriteOption) error {
	return f.deleteErr
}
func (f *fakeQRDB) Watch(context.Context, string) (qrdbsdk.WatchStream, error) { return f, nil }

func TestQRDBVolumeCoordinatorMapsLeaseHeld(t *testing.T) {
	f := &fakeQRDB{acquireErr: &qrdbsdk.LeaseHeldError{Key: "vol/a"}}
	c := newQRDBVolumeCoordinator(f, f)
	_, err := c.AcquireLease(context.Background(), "vol/a", time.Second)
	var held *qrdbsdk.LeaseHeldError
	if !errors.Is(err, ErrVolumeLeaseHeld) || !errors.As(err, &held) || held.Key != "vol/a" {
		t.Errorf("AcquireLease = %v, want ErrVolumeLeaseHeld wrapping *LeaseHeldError", err)
	}
	f.acquireErr = errors.New("outage")
	if _, err := c.AcquireLease(context.Background(), "vol/a", time.Second); err == nil || errors.Is(err, ErrVolumeLeaseHeld) {
		t.Errorf("outage AcquireLease = %v, want a plain error", err)
	}
}

func TestQRDBVolumeCoordinatorDeleteAbsent(t *testing.T) {
	f := &fakeQRDB{deleteErr: &transport.KeyNotFoundError{Key: "k"}}
	c := newQRDBVolumeCoordinator(f, f)
	if err := c.Delete(context.Background(), "k"); err != nil {
		t.Errorf("Delete absent = %v, want nil", err)
	}
	f.deleteErr = errors.New("outage")
	if err := c.Delete(context.Background(), "k"); err == nil {
		t.Error("Delete outage = nil")
	}
}

func TestQRDBVolumeCoordinatorWatch(t *testing.T) {
	f := &fakeQRDB{events: make(chan qrdbsdk.Event, 2)}
	f.events <- qrdbsdk.Event{Type: transport.EventPut, Key: "vol/a", Value: []byte("1")}
	f.events <- qrdbsdk.Event{Type: transport.EventDelete, Key: "vol/a", Value: []byte("stale")}
	close(f.events)
	ch, err := newQRDBVolumeCoordinator(f, f).Watch(context.Background(), "vol/")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	var got []VolumeCoordinatorEvent
	for ev := range ch {
		got = append(got, ev)
	}
	if len(got) != 2 || got[0].Type != bscommon.EventTypePut || string(got[0].Value) != "1" ||
		got[1].Type != bscommon.EventTypeDelete || got[1].Value != nil {
		t.Errorf("events = %+v", got)
	}
}

func TestNewQRDBVolumeCoordinatorNeedsClient(t *testing.T) {
	if _, err := NewQRDBVolumeCoordinator(nil); err == nil {
		t.Error("NewQRDBVolumeCoordinator(nil) = nil error")
	}
}

// promoteSignal is a replicated provider whose Promote is safe to call from
// the HA controller's goroutines and reports on a channel.
type promoteSignal struct {
	fakeReplicatedProvider
	promoted chan string
}

func (p promoteSignal) Promote(_ context.Context, id string) error {
	select {
	case p.promoted <- id:
	default:
	}
	return nil
}

func (p promoteSignal) Demote(context.Context, string) error { return nil }

func (p promoteSignal) ReplicationState(context.Context, string) (bscommon.ReplState, error) {
	return bscommon.ReplState{Conn: bscommon.ReplStateUpToDate, Role: bscommon.ReplRoleSecondary}, nil
}

// The HA controller elects through the single-node coordinator: it takes the
// volume's lease and promotes the local copy.
func TestVolumeHAControllerPromotesOverLocalCoordinator(t *testing.T) {
	p := promoteSignal{fakeReplicatedProvider{newFakeVolumeProvider()}, make(chan string, 1)}
	coord := NewLocalVolumeCoordinator()
	ctrl, err := NewVolumeHAController(p, coord, VolumeHAConfig{NodeID: "node-1"})
	if err != nil {
		t.Fatalf("NewVolumeHAController: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- ctrl.Manage(ctx, "data") }()
	defer func() {
		cancel()
		if err := <-stopped; err != nil {
			t.Errorf("Manage: %v", err)
		}
	}()
	select {
	case id := <-p.promoted:
		if id != "data" {
			t.Errorf("promoted %q, want data", id)
		}
	case <-time.After(10 * time.Second):
		st, _ := ctrl.Status("data")
		t.Fatalf("the controller never promoted over the local coordinator: %+v", st)
	}
	if _, err := coord.AcquireLease(context.Background(), ctrl.Config().KeyPrefix+"data", time.Second); !errors.Is(err, ErrVolumeLeaseHeld) {
		t.Errorf("volume lease not held by the controller: %v", err)
	}
}
