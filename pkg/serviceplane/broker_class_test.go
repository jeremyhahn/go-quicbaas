package serviceplane

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func launcherCalls(f *fixture) []string {
	f.launcher.mu.Lock()
	defer f.launcher.mu.Unlock()
	return slices.Clone(f.launcher.calls)
}

func TestPlane_RefusesBrokerClassUntilGranted(t *testing.T) {
	ctx := context.Background()

	t.Run("deploy", func(t *testing.T) {
		f := newFixture(t)
		m := validManifest()
		m.Class = ClassBroker
		_, err := f.plane.Deploy(ctx, "acme", f.sign(t, m))
		wantErr(t, err, ErrBrokerClass)
		if got := f.ledger.String(); got != "" {
			t.Fatalf("ledger = %q, want nothing granted", got)
		}
		if calls := launcherCalls(f); len(calls) != 0 {
			t.Fatalf("launcher calls = %v, want none", calls)
		}
		_, err = f.plane.Get(ctx, "acme", "web")
		wantErr(t, err, ErrNotFound)
	})

	t.Run("redeploy", func(t *testing.T) {
		f := newFixture(t)
		before := f.deploy(t, "web")
		ledger := f.ledger.String()
		calls := launcherCalls(f)

		m := validManifest()
		m.Class = ClassBroker
		_, err := f.plane.Replace(ctx, "acme", f.sign(t, m))
		wantErr(t, err, ErrBrokerClass)

		svc, err := f.plane.Get(ctx, "acme", "web")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if svc.Revision != before.Revision || svc.Manifest.Class != before.Manifest.Class {
			t.Fatalf("stored revision %d class %q, want %d %q",
				svc.Revision, svc.Manifest.Class, before.Revision, before.Manifest.Class)
		}
		if got := f.ledger.String(); got != ledger {
			t.Fatalf("ledger = %q, want %q", got, ledger)
		}
		if got := launcherCalls(f); !slices.Equal(got, calls) {
			t.Fatalf("launcher calls = %v, want %v", got, calls)
		}
	})
}

func TestManifest_ClassDefaultsToStandard(t *testing.T) {
	for _, c := range []Class{"", ClassStandard, ClassBroker} {
		m := validManifest()
		m.Class = c
		if err := m.Validate(); err != nil {
			t.Fatalf("class %q: %v", c, err)
		}
	}
	b, err := json.Marshal(validManifest())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"class"`) {
		t.Fatalf("an unset class is marshalled: %s", b)
	}
}
