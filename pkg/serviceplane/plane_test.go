package serviceplane

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
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
