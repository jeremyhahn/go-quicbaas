package orchestration

import (
	"context"
	"errors"
	"fmt"
	"time"

	bscommon "github.com/jeremyhahn/go-blockstore/pkg/common"
	bsha "github.com/jeremyhahn/go-blockstore/pkg/ha"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// Coordination surface of go-blockstore, aliased so the orchestration layer
// and sdk/go name one set of types.
type (
	// VolumeCoordinator is go-blockstore's common.Coordinator: the lease, KV
	// and watch primitive its HA controller elects a replicated volume's
	// primary with.
	VolumeCoordinator = bscommon.Coordinator
	// VolumeLease is a held lease from a VolumeCoordinator.
	VolumeLease = bscommon.Lease
	// VolumeCoordinatorEvent is one change delivered by VolumeCoordinator.Watch.
	VolumeCoordinatorEvent = bscommon.Event
	// VolumeHAConfig configures the HA controller.
	VolumeHAConfig = bsha.Config
	// VolumeHAController elects the primary of each replicated volume.
	VolumeHAController = bsha.Controller
)

// ErrVolumeLeaseHeld is returned by AcquireLease when another holder owns the
// key: the expected outcome of losing an election.
var ErrVolumeLeaseHeld = bscommon.ErrLeaseHeld

// NewVolumeHAController returns go-blockstore's HA controller for provider,
// electing through coord. coord must lease: NewQRDBVolumeCoordinator over a
// raft-mode go-qrdb when the volume is managed from more than one node,
// NewLocalVolumeCoordinator on a genuinely single-node deployment.
func NewVolumeHAController(provider ReplicatedVolumeProvider, coord VolumeCoordinator, cfg VolumeHAConfig) (*VolumeHAController, error) {
	return bsha.New(provider, coord, cfg)
}

// qrdbCoordination is the part of go-qrdb's *sdk.Coordinator the adapter uses.
type qrdbCoordination interface {
	AcquireLease(ctx context.Context, key string, ttl time.Duration) (qrdbsdk.Lease, error)
	CompareAndSwap(ctx context.Context, key string, expect, set []byte) (bool, error)
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, val []byte) error
	Watch(ctx context.Context, prefix string) (qrdbsdk.WatchStream, error)
}

// qrdbDeleter is the delete verb of go-qrdb's transport.KVClient, which the
// sdk Coordinator does not carry.
type qrdbDeleter interface {
	Delete(ctx context.Context, key string, opts ...transport.WriteOption) error
}

// QRDBVolumeCoordinator is a VolumeCoordinator over go-qrdb. In raft mode its
// leases are cross-node ownership arbitrated by a quorum, which is what a
// replicated pair managed from both of its nodes needs. A standalone go-qrdb
// keeps no leases and refuses AcquireLease and Watch; use
// NewLocalVolumeCoordinator there instead.
type QRDBVolumeCoordinator struct {
	coord qrdbCoordination
	kv    qrdbDeleter
}

var _ VolumeCoordinator = (*QRDBVolumeCoordinator)(nil)

// NewQRDBVolumeCoordinator adapts a connected go-qrdb client.
func NewQRDBVolumeCoordinator(client *qrdbsdk.Client) (*QRDBVolumeCoordinator, error) {
	if client == nil {
		return nil, errors.New("orchestration: volume coordinator needs a go-qrdb client")
	}
	coord, err := client.Coordination()
	if err != nil {
		return nil, fmt.Errorf("orchestration: go-qrdb coordination: %w", err)
	}
	return newQRDBVolumeCoordinator(coord, client.KV()), nil
}

func newQRDBVolumeCoordinator(coord qrdbCoordination, kv qrdbDeleter) *QRDBVolumeCoordinator {
	return &QRDBVolumeCoordinator{coord: coord, kv: kv}
}

// AcquireLease takes an exclusive TTL lease on key; a held key wraps both
// ErrVolumeLeaseHeld and go-qrdb's *sdk.LeaseHeldError.
func (c *QRDBVolumeCoordinator) AcquireLease(ctx context.Context, key string, ttl time.Duration) (VolumeLease, error) {
	lease, err := c.coord.AcquireLease(ctx, key, ttl)
	if err != nil {
		var held *qrdbsdk.LeaseHeldError
		if errors.As(err, &held) {
			return nil, fmt.Errorf("%w: %w", ErrVolumeLeaseHeld, err)
		}
		return nil, err
	}
	return lease, nil
}

// CompareAndSwap sets key to set when its value is expect (nil: absent).
func (c *QRDBVolumeCoordinator) CompareAndSwap(ctx context.Context, key string, expect, set []byte) (bool, error) {
	return c.coord.CompareAndSwap(ctx, key, expect, set)
}

// Delete removes key; an absent key is not an error.
func (c *QRDBVolumeCoordinator) Delete(ctx context.Context, key string) error {
	err := c.kv.Delete(ctx, key)
	var missing *transport.KeyNotFoundError
	if errors.As(err, &missing) {
		return nil
	}
	return err
}

// Get returns key's value, or (nil, nil) when it is absent.
func (c *QRDBVolumeCoordinator) Get(ctx context.Context, key string) ([]byte, error) {
	return c.coord.Get(ctx, key)
}

// Put stores val at key.
func (c *QRDBVolumeCoordinator) Put(ctx context.Context, key string, val []byte) error {
	return c.coord.Put(ctx, key, val)
}

// Watch delivers every change under prefix until ctx is done or go-qrdb ends
// the watch; either way the channel closes.
func (c *QRDBVolumeCoordinator) Watch(ctx context.Context, prefix string) (<-chan VolumeCoordinatorEvent, error) {
	stream, err := c.coord.Watch(ctx, prefix)
	if err != nil {
		return nil, err
	}
	out := make(chan VolumeCoordinatorEvent)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-stream.Events():
				if !ok {
					return
				}
				e := VolumeCoordinatorEvent{Type: bscommon.EventTypePut, Key: ev.Key, Value: ev.Value}
				if ev.Type == transport.EventDelete {
					e.Type, e.Value = bscommon.EventTypeDelete, nil
				}
				select {
				case out <- e:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}
