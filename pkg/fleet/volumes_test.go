package fleet

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
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
	got, err := WrapVolumeRuntime(VolumeConfig{}, t.TempDir(), inner)
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

	got, err := WrapVolumeRuntime(VolumeConfig{Provider: "local"}, t.TempDir(), &nopRuntime{})
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

	got, err := WrapVolumeRuntime(VolumeConfig{Provider: "drbd"}, t.TempDir(), &nopRuntime{})
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

// TestWrapVolumeRuntimeScopesStagingToWorkDirAndProvider: two daemons with
// their own work dirs never share a staging dir (and its attachment state),
// and the provider name is appended to an explicit staging_dir too.
func TestWrapVolumeRuntimeScopesStagingToWorkDirAndProvider(t *testing.T) {
	var name string
	stubVolumeProvider(t, &name)
	cfg := VolumeConfig{Provider: "local", NodeID: "n"}
	staging := func(c VolumeConfig, workDir string) string {
		t.Helper()
		got, err := WrapVolumeRuntime(c, workDir, &nopRuntime{})
		if err != nil {
			t.Fatalf("WrapVolumeRuntime: %v", err)
		}
		vr := got.(*orchestration.VolumeRuntime)
		t.Cleanup(func() { _ = vr.Close() })
		return vr.Attacher().StagingDir()
	}
	wa, wb := t.TempDir(), t.TempDir()
	a, b := staging(cfg, wa), staging(cfg, wb)
	if a == b {
		t.Errorf("both daemons stage under %q", a)
	}
	if want := filepath.Join(wa, "volumes", "local"); a != want {
		t.Errorf("staging = %q, want %q", a, want)
	}
	if want := filepath.Join(wb, "volumes", "local"); b != want {
		t.Errorf("staging = %q, want %q", b, want)
	}
	explicit := t.TempDir()
	c := cfg
	c.StagingDir = explicit
	if got, want := staging(c, t.TempDir()), filepath.Join(explicit, "local"); got != want {
		t.Errorf("explicit staging = %q, want %q", got, want)
	}
}

// stubSharedVolumeProvider makes every built provider the one backing store.
func stubSharedVolumeProvider(t *testing.T) *detachCountingProvider {
	t.Helper()
	prov := &detachCountingProvider{}
	old := newVolumeProvider
	newVolumeProvider = func(string, map[string]string) (orchestration.VolumeProvider, error) { return prov, nil }
	t.Cleanup(func() { newVolumeProvider = old })
	return prov
}

// TestWrapVolumeRuntimeLocksAVolumeAcrossWorkDirs: two daemons with their own
// work dirs and node IDs over the same backing storage never own one volume
// at once.
func TestWrapVolumeRuntimeLocksAVolumeAcrossWorkDirs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the volume lock is a Linux flock")
	}
	stubSharedVolumeProvider(t)
	locks := t.TempDir()
	wrap := func(node, workDir string) orchestration.ContainerRuntime {
		t.Helper()
		inner := &capableContainerRuntime{}
		inner.available = true
		got, err := WrapVolumeRuntime(VolumeConfig{
			Provider: "local", NodeID: node, LockDir: locks,
			Settings: map[string]string{"volume_group": "vg0"},
		}, workDir, inner)
		if err != nil {
			t.Fatalf("WrapVolumeRuntime: %v", err)
		}
		t.Cleanup(func() { _ = got.(*orchestration.VolumeRuntime).Close() })
		return got
	}
	wa, wb := t.TempDir(), t.TempDir()
	a, b := wrap("n1", wa), wrap("n2", wb)
	ctx := context.Background()
	if _, err := a.Run(ctx, ownedVolumeSvcSpec(wa)); err != nil {
		t.Fatalf("a.Run: %v", err)
	}
	if _, err := b.Run(ctx, ownedVolumeSvcSpec(wb)); !errors.Is(err, orchestration.ErrVolumeInUse) {
		t.Fatalf("b.Run of a volume a holds = %v, want ErrVolumeInUse", err)
	}
	if volumeNamespace(map[string]string{"volume_group": "vg0"}) == volumeNamespace(map[string]string{"volume_group": "vg1"}) {
		t.Error("vg0 and vg1 share a lock namespace")
	}
	if volumeNamespace(map[string]string{"volume_group": "vg0"}) != volumeNamespace(map[string]string{"volume_group": "vg0", "node_id": "n1"}) {
		t.Error("node_id changes the lock namespace")
	}
}

// ownedVolumeSvcSpec is volumeSvcSpec labelled with the owner workDir.
func ownedVolumeSvcSpec(workDir string) *orchestration.ContainerSpec {
	spec := volumeSvcSpec()
	spec.Labels = map[string]string{orchestration.ContainerOwnerLabel: workDir}
	return spec
}

// TestWrapVolumeRuntimeEnforcesTheOwnerLabel: every volume container carries
// the daemon's owner label, so the startup sweep removes it before the
// recovered volumes are released.
func TestWrapVolumeRuntimeEnforcesTheOwnerLabel(t *testing.T) {
	stubSharedVolumeProvider(t)
	inner := &capableContainerRuntime{}
	inner.available = true
	w := t.TempDir()
	got, err := WrapVolumeRuntime(VolumeConfig{Provider: "local", NodeID: "n", LockDir: t.TempDir()}, w, inner)
	if err != nil {
		t.Fatalf("WrapVolumeRuntime: %v", err)
	}
	t.Cleanup(func() { _ = got.(*orchestration.VolumeRuntime).Close() })
	ctx := context.Background()
	if _, err := got.Run(ctx, volumeSvcSpec()); !errors.Is(err, orchestration.ErrVolumeOwnerLabel) {
		t.Fatalf("Run without the owner label = %v, want ErrVolumeOwnerLabel", err)
	}
	if _, err := got.Run(ctx, ownedVolumeSvcSpec(w)); err != nil {
		t.Fatalf("Run with the owner label: %v", err)
	}
}

func TestWrapVolumeRuntimeRejectsARelativeLockDir(t *testing.T) {
	var name string
	stubVolumeProvider(t, &name)
	_, err := WrapVolumeRuntime(VolumeConfig{Provider: "local", NodeID: "n", LockDir: "relative/locks"}, t.TempDir(), &nopRuntime{})
	if !errors.Is(err, orchestration.ErrVolumeInvalidArgument) {
		t.Fatalf("relative lock dir: err = %v, want ErrVolumeInvalidArgument", err)
	}
}

// TestWrapVolumeRuntimeNeedsAWorkDirOrStagingDir: with neither there is no
// daemon-scoped place for the attachment state.
func TestWrapVolumeRuntimeNeedsAWorkDirOrStagingDir(t *testing.T) {
	var name string
	stubVolumeProvider(t, &name)
	_, err := WrapVolumeRuntime(VolumeConfig{Provider: "local", NodeID: "n"}, "", &nopRuntime{})
	if !errors.Is(err, orchestration.ErrVolumeInvalidArgument) {
		t.Fatalf("WrapVolumeRuntime(no dirs) = %v, want ErrVolumeInvalidArgument", err)
	}
}

func TestWrapVolumeRuntimeRejectsBadConfig(t *testing.T) {
	var name string
	stubVolumeProvider(t, &name)
	_, err := WrapVolumeRuntime(VolumeConfig{Provider: "local", NodeID: "n", StagingDir: "relative/dir"}, t.TempDir(), &nopRuntime{})
	if !errors.Is(err, orchestration.ErrVolumeInvalidArgument) {
		t.Errorf("relative staging dir: err = %v", err)
	}

	newVolumeProvider = orchestration.NewVolumeProvider
	if _, err := WrapVolumeRuntime(VolumeConfig{Provider: "no-such-provider", NodeID: "n"}, t.TempDir(), &nopRuntime{}); err == nil {
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

		_, err := WrapVolumeRuntime(VolumeConfig{Provider: "local"}, t.TempDir(), &nopRuntime{})
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

		_, err := WrapVolumeRuntime(VolumeConfig{Provider: "local"}, t.TempDir(), &nopRuntime{})
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
	got, err := WrapVolumeRuntime(VolumeConfig{Provider: "local", NodeID: "fleet-a", Settings: cfgSettings}, t.TempDir(), &nopRuntime{})
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
	_, err := WrapVolumeRuntime(VolumeConfig{Provider: "local", NodeID: "a", Settings: map[string]string{"node_id": "b"}}, t.TempDir(), &nopRuntime{})
	if !errors.Is(err, orchestration.ErrVolumeInvalidArgument) {
		t.Fatalf("err = %v, want ErrVolumeInvalidArgument", err)
	}
}

func TestWrapVolumeRuntimeTakesNodeIDFromSettings(t *testing.T) {
	var settings map[string]string
	stubVolumeProviderSettings(t, &settings)
	stubHostname(t, "host-b")
	got, err := WrapVolumeRuntime(VolumeConfig{Provider: "local", Settings: map[string]string{"node_id": "b"}}, t.TempDir(), &nopRuntime{})
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
