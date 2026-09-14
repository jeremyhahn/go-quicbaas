// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// TestSDLCLabelReconcile_UnsetSectionTakesTheDefaults is the positive half:
// zero is how an operator says "take the module's cadence", and the validator
// must not read it as "never sweep".
func TestSDLCLabelReconcile_UnsetSectionTakesTheDefaults(t *testing.T) {
	cfg := Default()
	cfg.SDLC.Enabled = true
	// Enabling the module now requires the node to say where it lives;
	// onboarding provisions forge webhooks pointing at it.
	cfg.SDLC.Webhook.PublicURL = "https://factory.test"
	cfg.SDLC.LabelReconcile = SDLCLabelReconcile{}

	v := NewValidator()
	v.validateSDLC(&cfg.SDLC)
	if err := v.Errors(); err != nil {
		t.Fatalf("an unset label_reconcile section was refused: %v", err)
	}
}

// TestSDLCLabelReconcile_NegativeIsRefusedByValidation is the negative half: a
// negative interval builds a ticker the runtime panics on, and a negative
// spacing paces nothing.
func TestSDLCLabelReconcile_NegativeIsRefusedByValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		section SDLCLabelReconcile
		key     string
	}{
		"negative interval":       {SDLCLabelReconcile{Interval: -time.Second}, "sdlc.label_reconcile.interval"},
		"negative forge interval": {SDLCLabelReconcile{ForgeInterval: -time.Second}, "sdlc.label_reconcile.forge_interval"},
	} {
		cfg := Default()
		cfg.SDLC.Enabled = true
		// Enabling the module now requires the node to say where it lives;
		// onboarding provisions forge webhooks pointing at it.
		cfg.SDLC.Webhook.PublicURL = "https://factory.test"
		cfg.SDLC.LabelReconcile = tc.section

		v := NewValidator()
		v.validateSDLC(&cfg.SDLC)
		err := v.Errors()
		if err == nil {
			t.Fatalf("%s: %+v validated", name, tc.section)
		}
		if !strings.Contains(err.Error(), tc.key) {
			t.Errorf("%s: error = %v, want the offending key named", name, err)
		}
	}
}

// =============================================================================
// The environment must reach the knob
// =============================================================================

// TestSDLCLabelReconcile_EveryKeyIsBoundOnAFreshViper: the daemon and the
// library Loader are two viper instances, and the daemon is the one that runs
// in production. Binding from one exported list is what keeps a key bound in
// the Loader from being unbound in cmd/quicbaasd — the shape of a variable an
// operator sets and never gets.
func TestSDLCLabelReconcile_EveryKeyIsBoundOnAFreshViper(t *testing.T) {
	v := viper.New()
	v.SetEnvPrefix("QUICBAAS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	BindSDLCLabelReconcileEnv(v)

	t.Setenv("QUICBAAS_SDLC_LABEL_RECONCILE_INTERVAL", "17m")
	if got := v.GetDuration("sdlc.label_reconcile.interval"); got != 17*time.Minute {
		t.Errorf("QUICBAAS_SDLC_LABEL_RECONCILE_INTERVAL did not reach the key (got %s)", got)
	}
	t.Setenv("QUICBAAS_SDLC_LABEL_RECONCILE_FORGE_INTERVAL", "750ms")
	if got := v.GetDuration("sdlc.label_reconcile.forge_interval"); got != 750*time.Millisecond {
		t.Errorf("QUICBAAS_SDLC_LABEL_RECONCILE_FORGE_INTERVAL did not reach the key (got %s)", got)
	}

	// That the LIST matches the struct field for field is checked by
	// TestSDLCLabelReconcile_EveryFieldHasABoundKey, which walks the type with
	// reflection. A length compared against a literal here would pass for the
	// case that matters — a field added with no key leaves the length alone.
}

// TestSDLCLabelReconcile_TheEnvironmentBeatsTheFile walks the real Loader,
// which is the path the daemon takes.
//
// THIS IS THE SETTING AN OPERATOR REACHES FOR UNDER LOAD. The forge_interval is
// what a node turns up when a forge starts refusing, and it would have been
// readable from a file and never from the environment — so the one deployment
// shape that cannot edit a file in a hurry was stuck on the shipped 200ms.
func TestSDLCLabelReconcile_TheEnvironmentBeatsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quicbaas.yaml")
	body := "sdlc:\n  enabled: true\n  label_reconcile:\n    interval: 9m\n    forge_interval: 300ms\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := NewLoader().Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SDLC.LabelReconcile.Interval != 9*time.Minute ||
		cfg.SDLC.LabelReconcile.ForgeInterval != 300*time.Millisecond {
		t.Fatalf("from file: %+v, want 9m/300ms", cfg.SDLC.LabelReconcile)
	}

	t.Setenv("QUICBAAS_SDLC_LABEL_RECONCILE_FORGE_INTERVAL", "2s")
	cfg, err = NewLoader().Load(path)
	if err != nil {
		t.Fatalf("Load with env: %v", err)
	}
	if cfg.SDLC.LabelReconcile.ForgeInterval != 2*time.Second {
		t.Errorf("forge_interval = %s, want the environment's 2s to beat the file's 300ms",
			cfg.SDLC.LabelReconcile.ForgeInterval)
	}
	if cfg.SDLC.LabelReconcile.Interval != 9*time.Minute {
		t.Errorf("interval = %s; one overridden field must not disturb another",
			cfg.SDLC.LabelReconcile.Interval)
	}
}

// TestSDLCLabelReconcile_TheEnvironmentReachesAnAbsentFile is the deployment
// this binding is really for: a container with no configuration file at all,
// where the environment is the ONLY layer. Without an explicit bind, Unmarshal
// never learns the key exists and the value is dropped in silence.
func TestSDLCLabelReconcile_TheEnvironmentReachesAnAbsentFile(t *testing.T) {
	t.Setenv("QUICBAAS_SDLC_LABEL_RECONCILE_INTERVAL", "31m")

	cfg, err := NewLoader().Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SDLC.LabelReconcile.Interval != 31*time.Minute {
		t.Errorf("interval = %s, want the environment's 31m with no file to read",
			cfg.SDLC.LabelReconcile.Interval)
	}
}

// TestSDLCLabelReconcile_AnUnparseableEnvironmentValueRefusesTheBoot is the
// silent-drop failure this section exists to prevent: a bound the operator set,
// the daemon ignored, and nothing anywhere reported.
func TestSDLCLabelReconcile_AnUnparseableEnvironmentValueRefusesTheBoot(t *testing.T) {
	t.Setenv("QUICBAAS_SDLC_LABEL_RECONCILE_INTERVAL", "five minutes")

	cfg, err := NewLoader().Load("")
	if err == nil {
		t.Fatalf("Load() = nil error with an unparseable interval; interval = %s",
			cfg.SDLC.LabelReconcile.Interval)
	}
	if !strings.Contains(err.Error(), "sdlc.label_reconcile.interval") {
		t.Errorf("the error does not name the key that could not be parsed: %v", err)
	}
}
