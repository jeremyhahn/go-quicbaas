package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/rollout"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
)

// rolloutGuard is the controller's guard over an in-memory store: Create
// writes only an absent key, Swap compares and swaps.
type rolloutGuard struct {
	mu sync.Mutex
	kv *kvstore.Store
}

func (g *rolloutGuard) Create(ctx context.Context, key string, value []byte) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ok, err := g.kv.Exists(ctx, key); err != nil || ok {
		return false, err
	}
	return true, g.kv.Put(ctx, key, value)
}

func (g *rolloutGuard) Swap(ctx context.Context, key string, held, next []byte) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.kv.CompareAndSwap(ctx, key, held, next)
}

// passAppraisal passes every slot.
type passAppraisal struct{}

func (passAppraisal) Appraise(context.Context, string, string, rollout.Slot, *serviceplane.Release) error {
	return nil
}

// notKeyring verifies manifests but is not a serviceplane.Keyring.
type notKeyring struct{ serviceplane.Keyring }

// rolloutLeader answers a fixed leadership.
type rolloutLeader bool

func (l rolloutLeader) IsLeader() (bool, error) { return bool(l), nil }

type rolloutRig struct {
	srv   *Server
	rt    *serviceplanetest.Runtime
	plane *serviceplane.Plane
	guard *rolloutGuard
	kv    *kvstore.Store
}

func newRolloutRig(t *testing.T) *rolloutRig {
	t.Helper()
	kv := kvstore.NewMemory()
	rt := serviceplanetest.NewRuntime(t)
	return &rolloutRig{srv: grantsTestServer(t), rt: rt, plane: rt.Plane(t), guard: &rolloutGuard{kv: kv}, kv: kv}
}

// deploy deploys acme/web and installs the node's roller over the plane.
func (r *rolloutRig) deploy(t *testing.T, leader scheduler.LeaderChecker) {
	t.Helper()
	if _, err := r.plane.Deploy(context.Background(), "acme", r.rt.Sign(t, "acme", "web")); err != nil {
		t.Fatal(err)
	}
	rr := r.srv.servicePlaneRoller(service.ServicePlaneRuntime{Verifier: r.rt.Verifier, Appraiser: passAppraisal{}},
		r.plane, r.guard, r.kv, leader)
	if rr == nil {
		t.Fatal("servicePlaneRoller = nil, want a roller")
	}
	r.plane.SetRoller(rr)
}

func (r *rolloutRig) release(t *testing.T, version string) *serviceplane.SignedRelease {
	t.Helper()
	sr, err := serviceplane.SignRelease(&serviceplane.Release{
		Kind:         serviceplane.ReleaseService,
		Version:      version,
		Manifest:     *serviceplanetest.Manifest("acme", "web"),
		PolicyDigest: "sha256:" + strings.Repeat("cd", 32),
		Provenance:   "slsa://registry/web/" + version,
	}, serviceplanetest.KeyID, r.rt.Key)
	if err != nil {
		t.Fatal(err)
	}
	return sr
}

func TestServicePlaneRoller_NoAppraiserIsNil(t *testing.T) {
	r := newRolloutRig(t)
	if got := r.srv.servicePlaneRoller(service.ServicePlaneRuntime{Verifier: r.rt.Verifier}, r.plane, r.guard, r.kv, nil); got != nil {
		t.Fatalf("servicePlaneRoller = %#v, want nil with no appraiser", got)
	}
}

func TestServicePlaneRoller_NonKeyringVerifierIsNil(t *testing.T) {
	r := newRolloutRig(t)
	rt := service.ServicePlaneRuntime{Verifier: notKeyring{r.rt.Verifier}, Appraiser: passAppraisal{}}
	if got := r.srv.servicePlaneRoller(rt, r.plane, r.guard, r.kv, nil); got != nil {
		t.Fatalf("servicePlaneRoller = %#v, want nil with a verifier that is not a keyring", got)
	}
}

func TestServicePlaneRoller_EmptyKeyringIsNil(t *testing.T) {
	r := newRolloutRig(t)
	rt := service.ServicePlaneRuntime{Verifier: serviceplane.Keyring{}, Appraiser: passAppraisal{}}
	if got := r.srv.servicePlaneRoller(rt, r.plane, r.guard, r.kv, nil); got != nil {
		t.Fatalf("servicePlaneRoller = %#v, want nil with an empty keyring", got)
	}
}

func TestServicePlaneRoller_NoRollerRefusesARelease(t *testing.T) {
	r := newRolloutRig(t)
	if _, err := r.plane.Deploy(context.Background(), "acme", r.rt.Sign(t, "acme", "web")); err != nil {
		t.Fatal(err)
	}
	if rr := r.srv.servicePlaneRoller(service.ServicePlaneRuntime{Verifier: r.rt.Verifier}, r.plane, r.guard, r.kv, nil); rr != nil {
		r.plane.SetRoller(rr)
	}
	_, err := r.plane.Redeploy(context.Background(), "acme", r.release(t, "2.0.0"))
	if !errors.Is(err, serviceplane.ErrConfig) {
		t.Fatalf("Redeploy err = %v, want ErrConfig with no appraiser", err)
	}
}

func TestServicePlaneRoller_RollsThroughThePlane(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	r.deploy(t, nil)
	svc, err := r.plane.Redeploy(ctx, "acme", r.release(t, "2.0.0"))
	if err != nil {
		t.Fatalf("Redeploy: %v", err)
	}
	if svc.State != serviceplane.StateRunning {
		t.Fatalf("state = %s, want running", svc.State)
	}
	if svc.Slot != string(rollout.SlotA) {
		t.Fatalf("slot = %q, want %q", svc.Slot, rollout.SlotA)
	}
	if svc.Release == nil || svc.Release.Version != "2.0.0" {
		t.Fatalf("release = %+v, want 2.0.0", svc.Release)
	}
	if len(svc.Grants) != len(serviceplane.GrantKinds) {
		t.Fatalf("grants = %v, want all %d", svc.Grants, len(serviceplane.GrantKinds))
	}
	// Deploy launched the first instance; the fill launched the slot's.
	if got := r.rt.Launcher.Launched(); len(got) != 2 || got[1] != "acme/web" {
		t.Fatalf("launched = %v, want the deployed and the slot instance", got)
	}
}

func TestServicePlaneRoller_NotLeaderRefuses(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	r.deploy(t, rolloutLeader(false))
	_, err := r.plane.Redeploy(ctx, "acme", r.release(t, "2.0.0"))
	if !errors.Is(err, rollout.ErrNotLeader) || !errors.Is(err, serviceplane.ErrRollout) {
		t.Fatalf("Redeploy err = %v, want ErrRollout wrapping ErrNotLeader", err)
	}
	svc, err := r.plane.Get(ctx, "acme", "web")
	if err != nil {
		t.Fatal(err)
	}
	if svc.State != serviceplane.StateRunning || svc.Release != nil || svc.Slot != "" {
		t.Fatalf("service = %s slot %q release %+v, want the deployed manifest running", svc.State, svc.Slot, svc.Release)
	}
	if got := r.rt.Launcher.Launched(); len(got) != 1 {
		t.Fatalf("launched = %v, want only the deployed instance", got)
	}
}

// rolloutStepDown leads for its first check only.
type rolloutStepDown struct {
	mu     sync.Mutex
	checks int
}

func (l *rolloutStepDown) IsLeader() (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.checks++
	return l.checks == 1, nil
}

// strand deploys acme/web under a leader that steps down after its first
// check, so the release redeploy of 2.0.0 stops with its rollout in flight.
func (r *rolloutRig) strand(t *testing.T) {
	t.Helper()
	r.deploy(t, &rolloutStepDown{})
	if _, err := r.plane.Redeploy(context.Background(), "acme", r.release(t, "2.0.0")); !errors.Is(err, rollout.ErrNotLeader) {
		t.Fatalf("stranded Redeploy err = %v, want ErrNotLeader", err)
	}
}

// slots reads acme/web's slot record.
func (r *rolloutRig) slots(t *testing.T) *rollout.Slots {
	t.Helper()
	raw, err := r.kv.Get(context.Background(), "serviceplane/rollouts/acme/web")
	if err != nil {
		t.Fatal(err)
	}
	var slots rollout.Slots
	if err := json.Unmarshal(raw, &slots); err != nil {
		t.Fatal(err)
	}
	if slots.Rollout == nil {
		t.Fatal("the slot record holds no rollout")
	}
	return &slots
}

// takeOver installs a second roller over the rig's plane, as the node that
// leads next builds it.
func (r *rolloutRig) takeOver(t *testing.T, leader scheduler.LeaderChecker) {
	t.Helper()
	rr := r.srv.servicePlaneRoller(service.ServicePlaneRuntime{Verifier: r.rt.Verifier, Appraiser: passAppraisal{}},
		r.plane, r.guard, r.kv, leader)
	if rr == nil {
		t.Fatal("servicePlaneRoller = nil, want a roller")
	}
	r.plane.SetRoller(rr)
}

func (r *rolloutRig) resolve(context.Context) (*serviceplane.Plane, error) { return r.plane, nil }

func TestServicePlaneRecovery_ResumesARolloutInFlight(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	r.strand(t)
	if phase := r.slots(t).Rollout.Phase; phase.Terminal() {
		t.Fatalf("stranded phase = %s, want one in flight", phase)
	}

	r.takeOver(t, rolloutLeader(true))
	r.srv.recoverServicePlane(ctx, r.resolve)

	if slots := r.slots(t); slots.Rollout.Phase != rollout.PhaseDone || slots.Active != rollout.SlotA {
		t.Fatalf("slots = %+v, want the rollout done in slot a", slots)
	}
	svc, err := r.plane.Get(ctx, "acme", "web")
	if err != nil {
		t.Fatal(err)
	}
	if svc.State != serviceplane.StateRunning || svc.Slot != string(rollout.SlotA) {
		t.Fatalf("service = %s slot %q, want running in slot a", svc.State, svc.Slot)
	}
	if svc.Release == nil || svc.Release.Version != "2.0.0" {
		t.Fatalf("release = %+v, want 2.0.0", svc.Release)
	}
}

func TestServicePlaneRecovery_FollowerResumesNothing(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	r.strand(t)
	before := r.slots(t).Rollout

	r.takeOver(t, rolloutLeader(false))
	r.srv.recoverServicePlane(ctx, r.resolve)

	after := r.slots(t).Rollout
	if after.Phase != before.Phase || after.Epoch != before.Epoch {
		t.Fatalf("rollout = %s epoch %d, want %s epoch %d untouched", after.Phase, after.Epoch, before.Phase, before.Epoch)
	}
	if got := r.rt.Launcher.Launched(); len(got) != 1 {
		t.Fatalf("launched = %v, want only the deployed instance", got)
	}
}

func TestServicePlaneRecovery_NoRollerResumesNothing(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	if _, err := r.plane.Deploy(ctx, "acme", r.rt.Sign(t, "acme", "web")); err != nil {
		t.Fatal(err)
	}
	if r.plane.Roller() != nil {
		t.Fatal("the plane has a roller, want none")
	}
	r.srv.recoverServicePlane(ctx, r.resolve)

	svc, err := r.plane.Get(ctx, "acme", "web")
	if err != nil {
		t.Fatal(err)
	}
	if svc.State != serviceplane.StateRunning {
		t.Fatalf("state = %s, want running", svc.State)
	}
}
