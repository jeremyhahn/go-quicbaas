package fleet

import (
	"errors"
	"reflect"
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

// TestWrapVolumeRuntimeRefusesDRBD: the node agent does not promote or demote
// drbd volumes on launch, so it refuses the provider before building one.
func TestWrapVolumeRuntimeRefusesDRBD(t *testing.T) {
	var name string
	stubVolumeProvider(t, &name)
	old := hostname
	hostname = func() (string, error) { return "node-7", nil }
	t.Cleanup(func() { hostname = old })

	got, err := WrapVolumeRuntime(VolumeConfig{Provider: "drbd"}, &nopRuntime{})
	if !errors.Is(err, orchestration.ErrVolumeUnsupported) {
		t.Fatalf("WrapVolumeRuntime(drbd) = %v, %v; want ErrVolumeUnsupported", got, err)
	}
	if got != nil {
		t.Errorf("runtime = %T, want none", got)
	}
	if name != "" {
		t.Errorf("provider %q was built, want none", name)
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

// stubVolumeProviderSettings records the settings the provider is built with.
func stubVolumeProviderSettings(t *testing.T, got *map[string]string) {
	t.Helper()
	old := newVolumeProvider
	newVolumeProvider = func(_ string, settings map[string]string) (orchestration.VolumeProvider, error) {
		*got = settings
		return nopVolumeProvider{}, nil
	}
	t.Cleanup(func() { newVolumeProvider = old })
}

func stubHostname(t *testing.T, name string) {
	t.Helper()
	old := hostname
	hostname = func() (string, error) { return name, nil }
	t.Cleanup(func() { hostname = old })
}

// The configured node identity reaches the local provider, which otherwise
// takes the host name and refuses Attach for the attacher's node.
func TestWrapVolumeRuntimeNodeIDReachesLocalProvider(t *testing.T) {
	var settings map[string]string
	stubVolumeProviderSettings(t, &settings)
	stubHostname(t, "host-b")
	cfgSettings := map[string]string{"x": "1"}
	got, err := WrapVolumeRuntime(VolumeConfig{Provider: "local", NodeID: "fleet-a", Settings: cfgSettings}, &nopRuntime{})
	if err != nil {
		t.Fatalf("WrapVolumeRuntime: %v", err)
	}
	if want := map[string]string{"x": "1", "node_id": "fleet-a"}; !reflect.DeepEqual(settings, want) {
		t.Errorf("provider settings = %v, want %v", settings, want)
	}
	if n := got.(*orchestration.VolumeRuntime).Attacher().Node(); n != "fleet-a" {
		t.Errorf("attacher node = %q, want fleet-a", n)
	}
	if len(cfgSettings) != 1 {
		t.Errorf("the caller's settings were modified: %v", cfgSettings)
	}
}

func TestWrapVolumeRuntimeRejectsNodeIDMismatch(t *testing.T) {
	var settings map[string]string
	stubVolumeProviderSettings(t, &settings)
	_, err := WrapVolumeRuntime(VolumeConfig{Provider: "local", NodeID: "a", Settings: map[string]string{"node_id": "b"}}, &nopRuntime{})
	if !errors.Is(err, orchestration.ErrVolumeInvalidArgument) {
		t.Fatalf("err = %v, want ErrVolumeInvalidArgument", err)
	}
}

func TestWrapVolumeRuntimeTakesNodeIDFromSettings(t *testing.T) {
	var settings map[string]string
	stubVolumeProviderSettings(t, &settings)
	stubHostname(t, "host-b")
	got, err := WrapVolumeRuntime(VolumeConfig{Provider: "local", Settings: map[string]string{"node_id": "b"}}, &nopRuntime{})
	if err != nil {
		t.Fatalf("WrapVolumeRuntime: %v", err)
	}
	if n := got.(*orchestration.VolumeRuntime).Attacher().Node(); n != "b" {
		t.Errorf("attacher node = %q, want b", n)
	}
	if settings["node_id"] != "b" {
		t.Errorf("provider node_id = %q, want b", settings["node_id"])
	}
}
