package fleet

import (
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// nopVolumeProvider satisfies VolumeProvider for wiring tests; no method is
// called.
type nopVolumeProvider struct{ orchestration.VolumeProvider }

// nopRuntime satisfies ContainerRuntime for wiring tests.
type nopRuntime struct{ orchestration.ContainerRuntime }

func stubVolumeProvider(t *testing.T, gotName *string) {
	t.Helper()
	old := newVolumeProvider
	newVolumeProvider = func(name string, _ map[string]string) (orchestration.VolumeProvider, error) {
		*gotName = name
		return nopVolumeProvider{}, nil
	}
	t.Cleanup(func() { newVolumeProvider = old })
}

func TestWrapVolumeRuntimeDisabledPassesThrough(t *testing.T) {
	inner := &nopRuntime{}
	got, err := WrapVolumeRuntime(VolumeConfig{}, inner)
	if err != nil || got != inner {
		t.Fatalf("WrapVolumeRuntime(disabled) = %v, %v; want inner, nil", got, err)
	}
}

func TestWrapVolumeRuntimeWrapsWithDefaults(t *testing.T) {
	var name string
	stubVolumeProvider(t, &name)
	old := hostname
	hostname = func() (string, error) { return "node-7", nil }
	t.Cleanup(func() { hostname = old })

	got, err := WrapVolumeRuntime(VolumeConfig{Provider: "local"}, &nopRuntime{})
	if err != nil {
		t.Fatalf("WrapVolumeRuntime: %v", err)
	}
	vr, ok := got.(*orchestration.VolumeRuntime)
	if !ok {
		t.Fatalf("runtime = %T, want *orchestration.VolumeRuntime", got)
	}
	if name != "local" {
		t.Errorf("provider = %q, want local", name)
	}
	if n := vr.Attacher().Node(); n != "node-7" {
		t.Errorf("node = %q, want the host name", n)
	}
}

func TestWrapVolumeRuntimeRejectsBadConfig(t *testing.T) {
	var name string
	stubVolumeProvider(t, &name)
	_, err := WrapVolumeRuntime(VolumeConfig{Provider: "local", NodeID: "n", StagingDir: "relative/dir"}, &nopRuntime{})
	if !errors.Is(err, orchestration.ErrVolumeInvalidArgument) {
		t.Errorf("relative staging dir: err = %v", err)
	}

	newVolumeProvider = orchestration.NewVolumeProvider
	if _, err := WrapVolumeRuntime(VolumeConfig{Provider: "no-such-provider", NodeID: "n"}, &nopRuntime{}); err == nil {
		t.Error("unknown provider: err = nil")
	}
}

func TestWrapVolumeRuntimeNoHostName(t *testing.T) {
	errBoom := errors.New("boom")

	t.Run("lookup error", func(t *testing.T) {
		var name string
		stubVolumeProvider(t, &name)
		old := hostname
		hostname = func() (string, error) { return "", errBoom }
		t.Cleanup(func() { hostname = old })

		_, err := WrapVolumeRuntime(VolumeConfig{Provider: "local"}, &nopRuntime{})
		if err == nil {
			t.Fatal("WrapVolumeRuntime: want an error, got nil")
		}
		if !errors.Is(err, errBoom) {
			t.Errorf("err = %v, want it to wrap %v", err, errBoom)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		var name string
		stubVolumeProvider(t, &name)
		old := hostname
		hostname = func() (string, error) { return "", nil }
		t.Cleanup(func() { hostname = old })

		_, err := WrapVolumeRuntime(VolumeConfig{Provider: "local"}, &nopRuntime{})
		if err == nil {
			t.Fatal("WrapVolumeRuntime: want an error, got nil")
		}
		if !strings.Contains(err.Error(), "host name is empty") {
			t.Errorf("err = %v, want it to say the host name is empty", err)
		}
	})
}
