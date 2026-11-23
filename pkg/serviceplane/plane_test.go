package serviceplane

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

var ctx = context.Background()

func TestDeploy_GrantsAllFiveInOrderAndLaunches(t *testing.T) {
	f := newFixture(t)
	svc := f.deploy(t, "web")
	if svc.State != StateRunning || svc.Revision != 1 || svc.Verb != "" {
		t.Fatalf("deployed service = %+v", svc)
	}
	want := "grant identity prior=,grant certificate prior=,grant name prior=,grant rbac prior=,grant secrets prior="
	if got := f.ledger.String(); got != want {
		t.Fatalf("grants = %s, want %s", got, want)
	}
	if svc.Grants[GrantRBAC] != "rbac:web:operator" || len(svc.Grants) != len(GrantKinds) {
		t.Fatalf("grant refs = %v", svc.Grants)
	}
	if got := strings.Join(f.launcher.calls, ","); got != "launch web" {
		t.Fatalf("launcher calls = %s", got)
	}
	stored, err := f.plane.Get(ctx, "acme", "web")
	if err != nil || stored.State != StateRunning || stored.Manifest.Image.Digest != svc.Manifest.Image.Digest {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}

func TestDeploy_RefusesAnUnverifiedManifest(t *testing.T) {
	f := newFixture(t)
	sm := f.sign(t, validManifest())
	sm.Payload = []byte(strings.Replace(string(sm.Payload), `"operator"`, `"admin"`, 1))
	_, err := f.plane.Deploy(ctx, "acme", sm)
	wantErr(t, err, ErrSignature)
	if _, err := f.plane.Get(ctx, "acme", "web"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unverified manifest was stored: %v", err)
	}
	if f.ledger.String() != "" || len(f.launcher.calls) != 0 {
		t.Fatal("an unverified manifest was granted or launched")
	}
}

func TestDeploy_RefusesAManifestForAnotherTenant(t *testing.T) {
	f := newFixture(t)
	sm := f.sign(t, validManifest())
	for _, tenant := range []string{"", "acme2"} {
		_, err := f.plane.Deploy(ctx, tenant, sm)
		wantErr(t, err, ErrTenant)
		_, err = f.plane.Redeploy(ctx, tenant, sm)
		wantErr(t, err, ErrTenant)
	}
	if _, err := f.plane.Get(ctx, "acme", "web"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a manifest deployed outside its tenant was stored: %v", err)
	}
	if f.ledger.String() != "" || len(f.launcher.calls) != 0 {
		t.Fatal("a manifest for another tenant was granted or launched")
	}
}

func TestDeploy_TheNameIsTakenOnce(t *testing.T) {
	f := newFixture(t)
	sm := f.sign(t, validManifest())
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.plane.Deploy(ctx, "acme", sm)
		}()
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		if err == nil {
			won++
		} else {
			wantErr(t, err, ErrExists)
		}
	}
	if won != 1 {
		t.Fatalf("%d deploys of one name won, want 1", won)
	}
}

func TestDeploy_AFailedGrantRevokesTheMadeOnesAndReleasesTheName(t *testing.T) {
	f := newFixture(t)
	f.grants[GrantRBAC].fail = errors.New("role store down")
	_, err := f.plane.Deploy(ctx, "acme", f.sign(t, validManifest()))
	wantErr(t, err, ErrGrant)
	want := "grant identity prior=,grant certificate prior=,grant name prior=," +
		"revoke name:web:operator,revoke certificate:web:operator,revoke identity:web:operator"
	if got := f.ledger.String(); got != want {
		t.Fatalf("ledger = %s, want %s", got, want)
	}
	if len(f.launcher.calls) != 0 {
		t.Fatal("launched without its grants")
	}
	f.grants[GrantRBAC].fail = nil
	if _, err := f.plane.Deploy(ctx, "acme", f.sign(t, validManifest())); err != nil {
		t.Fatalf("the name was not released: %v", err)
	}
}

func TestDeploy_AFailedRollbackKeepsTheRecordForUndeploy(t *testing.T) {
	f := newFixture(t)
	f.grants[GrantRBAC].fail = errors.New("role store down")
	f.grants[GrantIdentity].revoked = errors.New("CA unreachable")
	_, err := f.plane.Deploy(ctx, "acme", f.sign(t, validManifest()))
	wantErr(t, err, ErrGrant)
	svc, err := f.plane.Get(ctx, "acme", "web")
	if err != nil {
		t.Fatalf("the record holding an outstanding grant was removed: %v", err)
	}
	if svc.State != StateHalted || len(svc.Grants) != 1 || svc.Grants[GrantIdentity] != "identity:web:operator" || svc.Reason == "" {
		t.Fatalf("after a failed rollback = %+v", svc)
	}
	f.grants[GrantIdentity].revoked = nil
	if err := f.plane.Undeploy(ctx, "acme", "web"); err != nil {
		t.Fatalf("undeploy: %v", err)
	}
	if got := f.ledger.String(); !strings.HasSuffix(got, ",revoke identity:web:operator,revoke identity:web:operator") {
		t.Fatalf("ledger = %s, want the undeploy to revoke the identity again", got)
	}
	if _, err := f.plane.Get(ctx, "acme", "web"); err == nil {
		t.Fatal("undeploy left the record")
	}
}

func TestDeploy_AFailedLaunchHaltsAndResumeRecovers(t *testing.T) {
	f := newFixture(t)
	f.launcher.launchErr = errors.New("image pull failed")
	_, err := f.plane.Deploy(ctx, "acme", f.sign(t, validManifest()))
	wantErr(t, err, ErrLaunch)
	svc, _ := f.plane.Get(ctx, "acme", "web")
	if svc.State != StateHalted || svc.Reason != "image pull failed" || len(svc.Grants) != len(GrantKinds) {
		t.Fatalf("after a failed launch = %+v", svc)
	}
	f.launcher.launchErr = nil
	svc, err = f.plane.Resume(ctx, "acme", "web")
	if err != nil || svc.State != StateRunning || svc.Reason != "" {
		t.Fatalf("resume = %+v, %v", svc, err)
	}
}

// TestLaunch_ACancelledRequestStillSettles checks a request cancelled
// during its launch still leaves the service halted, not transitioning.
func TestLaunch_ACancelledRequestStillSettles(t *testing.T) {
	f := newFixture(t)
	cancelling := func(cancel context.CancelFunc) func(context.Context) error {
		return func(c context.Context) error {
			cancel()
			return c.Err()
		}
	}

	dctx, cancel := context.WithCancel(ctx)
	f.launcher.onLaunch = cancelling(cancel)
	if _, err := f.plane.Deploy(dctx, "acme", f.sign(t, validManifest())); err == nil {
		t.Fatal("deploy: want the cancellation")
	}
	svc, err := f.plane.Get(ctx, "acme", "web")
	if err != nil || svc.State != StateHalted || len(svc.Grants) != len(GrantKinds) {
		t.Fatalf("after a cancelled deploy = %+v, %v", svc, err)
	}

	f.launcher.onLaunch = nil
	if _, err := f.plane.Resume(ctx, "acme", "web"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	rctx, cancel := context.WithCancel(ctx)
	f.launcher.onLaunch = cancelling(cancel)
	if _, err := f.plane.Restart(rctx, "acme", "web"); err == nil {
		t.Fatal("restart: want the cancellation")
	}
	svc, err = f.plane.Get(ctx, "acme", "web")
	if err != nil || svc.State != StateHalted {
		t.Fatalf("after a cancelled restart = %+v, %v", svc, err)
	}
}

// TestMarkHalted_RecoversAStaleClaim checks a claim whose settlement write
// failed is recovered once it is older than the stale bound, and not before.
func TestMarkHalted_RecoversAStaleClaim(t *testing.T) {
	f := newFixture(t)
	f.deploy(t, "web")
	f.launcher.onLaunch = func(context.Context) error {
		f.guard.setLoseSwaps(true) // the final write is lost
		return nil
	}
	if _, err := f.plane.Restart(ctx, "acme", "web"); err == nil {
		t.Fatal("restart: want the lost settlement write")
	}
	f.guard.setLoseSwaps(false)
	f.launcher.onLaunch = nil
	svc, err := f.plane.Get(ctx, "acme", "web")
	if err != nil || svc.State != StateTransitioning {
		t.Fatalf("after a lost settlement = %+v, %v", svc, err)
	}

	_, err = f.plane.MarkHalted(ctx, "acme", "web", "lost node")
	wantErr(t, err, ErrTransition)
	f.advance(DefaultStaleClaim + time.Second)
	svc, err = f.plane.MarkHalted(ctx, "acme", "web", "claim abandoned")
	if err != nil || svc.State != StateHalted || svc.Verb != "" || svc.Reason != "claim abandoned" || len(svc.Grants) != len(GrantKinds) {
		t.Fatalf("mark halted a stale claim = %+v, %v", svc, err)
	}
	svc, err = f.plane.Resume(ctx, "acme", "web")
	if err != nil || svc.State != StateRunning {
		t.Fatalf("resume = %+v, %v", svc, err)
	}
}

func TestDeploy_ALostSettlementKeepsEveryGrantForUndeploy(t *testing.T) {
	f := newFixture(t)
	f.launcher.onLaunch = func(context.Context) error {
		f.guard.setLoseSwaps(true) // the final write is lost
		return nil
	}
	m := validManifest()
	if _, err := f.plane.Deploy(ctx, "acme", f.sign(t, m)); err == nil {
		t.Fatal("deploy: want the lost settlement write")
	}
	f.guard.setLoseSwaps(false)
	f.launcher.onLaunch = nil
	svc, err := f.plane.Get(ctx, "acme", "web")
	if err != nil || svc.State != StateTransitioning || len(svc.Grants) != len(GrantKinds) {
		t.Fatalf("after a lost settlement = %+v, %v", svc, err)
	}

	f.advance(DefaultStaleClaim + time.Second)
	svc, err = f.plane.MarkHalted(ctx, "acme", "web", "claim abandoned")
	if err != nil || svc.State != StateHalted || len(svc.Grants) != len(GrantKinds) {
		t.Fatalf("mark halted = %+v, %v", svc, err)
	}
	if err := f.plane.Undeploy(ctx, "acme", "web"); err != nil {
		t.Fatal(err)
	}
	want := "revoke secrets:web:operator,revoke rbac:web:operator,revoke name:web:operator," +
		"revoke certificate:web:operator,revoke identity:web:operator"
	if got := f.ledger.String(); !strings.HasSuffix(got, want) {
		t.Fatalf("ledger = %s, want it to end with %s", got, want)
	}
}

func TestDeploy_AFailedCheckpointRollsBackTheGrant(t *testing.T) {
	f := newFixture(t)
	// The identity checkpoint is written; the certificate's is lost.
	f.guard.setLoseAfter(1)
	_, err := f.plane.Deploy(ctx, "acme", f.sign(t, validManifest()))
	wantErr(t, err, ErrGrant)
	want := "grant identity prior=,grant certificate prior=," +
		"revoke certificate:web:operator,revoke identity:web:operator"
	if got := f.ledger.String(); got != want {
		t.Fatalf("ledger = %s, want %s", got, want)
	}
	f.guard.setLoseAfter(0)
	if _, err := f.plane.Get(ctx, "acme", "web"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the name is released: get = %v", err)
	}
	f.deploy(t, "web")
}

// TestRedeploy_AFailedCheckpointRestoresAReplacedGrant checks a redeploy
// that fails after replacing held grants (operator to admin) gives the prior
// manifest's grants back, so the record holds what is in force.
func TestRedeploy_AFailedCheckpointRestoresAReplacedGrant(t *testing.T) {
	f := newFixture(t)
	f.deploy(t, "web")
	f.ledger.reset()
	m := validManifest()
	m.Role = "admin"
	// The claim and the identity, certificate and name checkpoints are
	// written; the RBAC one is lost.
	f.guard.setLoseOnce(4)
	_, err := f.plane.Redeploy(ctx, "acme", f.sign(t, m))
	wantErr(t, err, ErrGrant)
	want := "grant identity prior=identity:web:operator,grant certificate prior=certificate:web:operator," +
		"grant name prior=name:web:operator,grant rbac prior=rbac:web:operator," +
		"grant rbac prior=rbac:web:admin,grant name prior=name:web:admin," +
		"grant certificate prior=certificate:web:admin,grant identity prior=identity:web:admin"
	if got := f.ledger.String(); got != want {
		t.Fatalf("ledger = %s, want %s", got, want)
	}
	svc, err := f.plane.Get(ctx, "acme", "web")
	if err != nil || svc.State != StateHalted || svc.Cleanup || svc.Grants[GrantRBAC] != "rbac:web:operator" ||
		svc.Grants[GrantName] != "name:web:operator" || len(svc.Grants) != len(GrantKinds) {
		t.Fatalf("after a failed redeploy = %+v, %v", svc, err)
	}
	if _, err := f.plane.Resume(ctx, "acme", "web"); err != nil {
		t.Fatalf("resume the restored service: %v", err)
	}
}

// TestRedeploy_AFailedRestoreKeepsTheReplacementForUndeploy checks a
// replacement grant whose restore fails stays recorded, so undeploy revokes
// it, and the record is not launched with it.
func TestRedeploy_AFailedRestoreKeepsTheReplacementForUndeploy(t *testing.T) {
	f := newFixture(t)
	f.deploy(t, "web")
	m := validManifest()
	m.Role = "admin"
	// The name checkpoint is lost, and giving the operator name back fails.
	f.guard.setLoseOnce(3)
	f.grants[GrantName].failRole = "operator"
	_, err := f.plane.Redeploy(ctx, "acme", f.sign(t, m))
	wantErr(t, err, ErrGrant)
	svc, err := f.plane.Get(ctx, "acme", "web")
	if err != nil || svc.State != StateHalted || !svc.Cleanup || svc.Grants[GrantName] != "name:web:admin" ||
		svc.Grants[GrantRBAC] != "rbac:web:operator" {
		t.Fatalf("after a failed restore = %+v, %v", svc, err)
	}
	f.launcher.calls = nil
	for name, verb := range map[string]func(context.Context, string, string) (*Service, error){
		"resume": f.plane.Resume, "restart": f.plane.Restart,
	} {
		_, err := verb(ctx, "acme", "web")
		wantErr(t, err, ErrGrant)
		if got, _ := f.plane.Get(ctx, "acme", "web"); got.State != StateHalted {
			t.Fatalf("a refused %s left the record %s", name, got.State)
		}
	}
	if len(f.launcher.calls) != 0 {
		t.Fatalf("launched a record whose grants are part way: %v", f.launcher.calls)
	}
	f.ledger.reset()
	if err := f.plane.Undeploy(ctx, "acme", "web"); err != nil {
		t.Fatal(err)
	}
	if got := f.ledger.String(); !strings.Contains(got, "revoke name:web:admin") {
		t.Fatalf("undeploy ledger = %s, want the replacement name revoked", got)
	}
}

// TestRelaunch_RefusesARecordWithGrantsMissingOrPartlyRevoked checks start,
// resume and restart refuse a record left by a rollback or an undeploy that
// failed part way, and that undeploy still finishes it.
func TestRelaunch_RefusesARecordWithGrantsMissingOrPartlyRevoked(t *testing.T) {
	t.Run("deploy rollback", func(t *testing.T) {
		f := newFixture(t)
		f.grants[GrantRBAC].fail = errors.New("role store down")
		f.grants[GrantName].revoked = errors.New("registry down")
		_, err := f.plane.Deploy(ctx, "acme", f.sign(t, validManifest()))
		wantErr(t, err, ErrGrant)
		f.grants[GrantRBAC].fail = nil
		_, err = f.plane.Resume(ctx, "acme", "web")
		wantErr(t, err, ErrGrant)
		if len(f.launcher.calls) != 0 {
			t.Fatalf("launched without its grants: %v", f.launcher.calls)
		}
		f.grants[GrantName].revoked = nil
		if err := f.plane.Undeploy(ctx, "acme", "web"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("partial undeploy", func(t *testing.T) {
		f := newFixture(t)
		f.deploy(t, "web")
		f.grants[GrantIdentity].revoked = errors.New("CA unreachable")
		wantErr(t, f.plane.Undeploy(ctx, "acme", "web"), ErrGrant)
		f.launcher.calls = nil
		_, err := f.plane.Resume(ctx, "acme", "web")
		wantErr(t, err, ErrGrant)
		_, err = f.plane.Restart(ctx, "acme", "web")
		wantErr(t, err, ErrGrant)
		if len(f.launcher.calls) != 0 {
			t.Fatalf("launched with revoked grants: %v", f.launcher.calls)
		}
		f.grants[GrantIdentity].revoked = nil
		if err := f.plane.Undeploy(ctx, "acme", "web"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("redeploy grants anew", func(t *testing.T) {
		f := newFixture(t)
		f.deploy(t, "web")
		f.grants[GrantIdentity].revoked = errors.New("CA unreachable")
		wantErr(t, f.plane.Undeploy(ctx, "acme", "web"), ErrGrant)
		svc, err := f.plane.Redeploy(ctx, "acme", f.sign(t, validManifest()))
		if err != nil || svc.State != StateRunning || svc.Cleanup {
			t.Fatalf("redeploy = %+v, %v", svc, err)
		}
	})
}

// TestStop_ACancelledRequestStillSettlesStopped checks a request cancelled
// after the containers halted still leaves the service stopped.
func TestStop_ACancelledRequestStillSettlesStopped(t *testing.T) {
	f := newFixture(t)
	f.deploy(t, "web")
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	f.launcher.onHalt = cancel
	if _, err := f.plane.Stop(sctx, "acme", "web"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	svc, err := f.plane.Get(ctx, "acme", "web")
	if err != nil || svc.State != StateStopped {
		t.Fatalf("after a cancelled stop = %+v, %v", svc, err)
	}
}

func TestStopStartRestart(t *testing.T) {
	f := newFixture(t)
	f.deploy(t, "web")
	steps := []struct {
		verb  func(context.Context, string, string) (*Service, error)
		state State
	}{
		{f.plane.Stop, StateStopped},
		{f.plane.Start, StateRunning},
		{f.plane.Restart, StateRunning},
	}
	for i, s := range steps {
		svc, err := s.verb(ctx, "acme", "web")
		if err != nil || svc.State != s.state {
			t.Fatalf("step %d = %+v, %v", i, svc, err)
		}
	}
	svc, _ := f.plane.Get(ctx, "acme", "web")
	if svc.Restarts != 1 || svc.Revision != 1 {
		t.Fatalf("restarts %d revision %d", svc.Restarts, svc.Revision)
	}
	want := "launch web,halt web,launch web,halt web,launch web"
	if got := strings.Join(f.launcher.calls, ","); got != want {
		t.Fatalf("launcher = %s, want %s", got, want)
	}
}

func TestRedeploy_ReplacesTheManifestAndReappliesGrants(t *testing.T) {
	f := newFixture(t)
	f.deploy(t, "web")
	m := validManifest()
	m.Role = "user"
	m.Image.Digest = "sha256:" + strings.Repeat("cd", 32)
	svc, err := f.plane.Redeploy(ctx, "acme", f.sign(t, m))
	if err != nil {
		t.Fatal(err)
	}
	if svc.Revision != 2 || svc.Manifest.Image.Digest != m.Image.Digest || svc.Grants[GrantRBAC] != "rbac:web:user" {
		t.Fatalf("redeployed = %+v", svc)
	}
	if !strings.Contains(f.ledger.String(), "grant rbac prior=rbac:web:operator") {
		t.Fatalf("redeploy did not pass the prior grant: %s", f.ledger.String())
	}
	other := validManifest()
	other.Name = "api"
	_, err = f.plane.Redeploy(ctx, "acme", f.sign(t, other))
	wantErr(t, err, ErrNotFound)
}

func TestUndeploy_HaltsRevokesInReverseAndRemoves(t *testing.T) {
	f := newFixture(t)
	f.deploy(t, "web")
	if err := f.plane.Undeploy(ctx, "acme", "web"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(f.ledger.String(), "revoke secrets:web:operator,revoke rbac:web:operator,"+
		"revoke name:web:operator,revoke certificate:web:operator,revoke identity:web:operator") {
		t.Fatalf("ledger = %s", f.ledger.String())
	}
	if _, err := f.plane.Get(ctx, "acme", "web"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("undeployed service still stored: %v", err)
	}
	wantErr(t, f.plane.Undeploy(ctx, "acme", "web"), ErrNotFound)
}

func TestUndeploy_AFailedRevokeKeepsTheServiceHalted(t *testing.T) {
	f := newFixture(t)
	f.deploy(t, "web")
	f.grants[GrantSecrets].revoked = errors.New("vault sealed")
	wantErr(t, f.plane.Undeploy(ctx, "acme", "web"), ErrGrant)
	svc, err := f.plane.Get(ctx, "acme", "web")
	if err != nil || svc.State != StateHalted {
		t.Fatalf("after a failed revoke = %+v, %v", svc, err)
	}
}

func TestVerbs_TheStateMachine(t *testing.T) {
	verbs := map[Verb]func(f *fixture) error{
		VerbUndeploy: func(f *fixture) error { return f.plane.Undeploy(ctx, "acme", "web") },
		VerbRedeploy: func(f *fixture) error {
			_, err := f.plane.Redeploy(ctx, "acme", f.sign(t, validManifest()))
			return err
		},
		VerbStart:   func(f *fixture) error { _, err := f.plane.Start(ctx, "acme", "web"); return err },
		VerbStop:    func(f *fixture) error { _, err := f.plane.Stop(ctx, "acme", "web"); return err },
		VerbResume:  func(f *fixture) error { _, err := f.plane.Resume(ctx, "acme", "web"); return err },
		VerbRestart: func(f *fixture) error { _, err := f.plane.Restart(ctx, "acme", "web"); return err },
	}
	into := map[State]func(f *fixture){
		StateRunning: func(*fixture) {},
		StateStopped: func(f *fixture) { _, _ = f.plane.Stop(ctx, "acme", "web") },
		StateHalted:  func(f *fixture) { _, _ = f.plane.MarkHalted(ctx, "acme", "web", "node lost") },
		StateTransitioning: func(f *fixture) {
			if _, err := f.plane.claim(ctx, VerbRestart, "acme", "web"); err != nil {
				t.Fatal(err)
			}
		},
	}
	for state, enter := range into {
		for verb, run := range verbs {
			f := newFixture(t)
			f.deploy(t, "web")
			enter(f)
			if svc, _ := f.plane.Get(ctx, "acme", "web"); svc.State != state {
				t.Fatalf("setup: state %s, want %s", svc.State, state)
			}
			legal := false
			for _, s := range allowed[verb] {
				legal = legal || s == state
			}
			err := run(f)
			if legal && err != nil {
				t.Errorf("%s from %s: %v", verb, state, err)
			}
			if !legal {
				wantErr(t, err, ErrTransition)
			}
		}
	}
}

func TestVerbs_ALostClaimActsOnNothing(t *testing.T) {
	f := newFixture(t)
	f.deploy(t, "web")
	f.launcher.calls = nil
	f.guard.loseSwaps = true
	_, err := f.plane.Stop(ctx, "acme", "web")
	wantErr(t, err, ErrConflict)
	if len(f.launcher.calls) != 0 {
		t.Fatalf("a verb that lost its claim acted: %v", f.launcher.calls)
	}
}

func TestNewPlane_RequiresEveryGrantKindOnce(t *testing.T) {
	f := newFixture(t)
	base := Config{Store: f.plane.store, Verifier: Keyring{}, Launcher: f.launcher}
	var four, six []Grant
	for _, k := range GrantKinds {
		six = append(six, f.grants[k])
		if k != GrantSecrets {
			four = append(four, f.grants[k])
		}
	}
	six = append(six, f.grants[GrantName])
	for name, gs := range map[string][]Grant{"missing": four, "duplicate": six, "nil": {nil}} {
		cfg := base
		cfg.Grants = gs
		if _, err := NewPlane(cfg); !errors.Is(err, ErrConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := NewPlane(Config{}); !errors.Is(err, ErrConfig) {
		t.Errorf("empty config: %v", err)
	}
	if _, err := NewStore(nil, nil); !errors.Is(err, ErrConfig) {
		t.Errorf("empty store: %v", err)
	}
}

func TestList_IsScopedToTheTenant(t *testing.T) {
	f := newFixture(t)
	f.deploy(t, "web")
	f.deploy(t, "api")
	other := validManifest()
	other.Tenant = "acme2"
	if _, err := f.plane.Deploy(ctx, "acme2", f.sign(t, other)); err != nil {
		t.Fatal(err)
	}
	got, err := f.plane.List(ctx, "acme")
	if err != nil || len(got) != 2 || got[0].Name != "api" || got[1].Name != "web" {
		t.Fatalf("list = %v, %v", got, err)
	}
	_, err = f.plane.List(ctx, "")
	wantErr(t, err, ErrNotFound)
}
