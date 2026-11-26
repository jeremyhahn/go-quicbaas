package orchestration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	pkgorch "github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/orchestration"
)

// The SDK names the server's coordination types, so a coordinator built
// through sdk/go is the one the node agent's HA controller takes.
func TestVolumeCoordinatorCrossBoundary(t *testing.T) {
	var coord orchestration.VolumeCoordinator = orchestration.NewLocalVolumeCoordinator()
	var _ pkgorch.VolumeCoordinator = coord
	var _ *pkgorch.QRDBVolumeCoordinator = (*orchestration.QRDBVolumeCoordinator)(nil)

	ctx := context.Background()
	lease, err := coord.AcquireLease(ctx, "vol/root", time.Second)
	if err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	defer lease.Release(ctx)
	if _, err := coord.AcquireLease(ctx, "vol/root", time.Second); !errors.Is(err, orchestration.ErrVolumeLeaseHeld) {
		t.Errorf("contended AcquireLease = %v, want ErrVolumeLeaseHeld", err)
	}
	if _, err := orchestration.NewQRDBVolumeCoordinator(nil); err == nil {
		t.Error("NewQRDBVolumeCoordinator(nil) = nil error")
	}
	if _, err := orchestration.NewVolumeHAController(nil, coord, orchestration.VolumeHAConfig{NodeID: "n"}); err == nil {
		t.Error("NewVolumeHAController without a provider = nil error")
	}
}
