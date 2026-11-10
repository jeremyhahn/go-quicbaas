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
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// =============================================================================
// The module's own keys must be reachable from the environment
// =============================================================================

// TestSDLC_EnvironmentOnlyDeploymentCanEnableTheModule is the test whose absence
// made every other SDLC environment variable a half-truth.
//
// The module is OPT-IN. With `sdlc.enabled` unbound, a deployment whose only
// configuration layer is the environment could set the cadence of the label
// reconciliation sweep, have it decoded, and still boot with SDLC off — so the
// sweep never starts and every variable that DID arrive looks like it worked.
// This proves the on-switch arrives through the same door as the knobs.
func TestSDLC_EnvironmentOnlyDeploymentCanEnableTheModule(t *testing.T) {
	t.Setenv("QUICBAAS_SDLC_ENABLED", "true")
	t.Setenv("QUICBAAS_SDLC_LABEL_RECONCILE_INTERVAL", "31m")

	// No configuration file at all: the environment is the only layer.
	cfg, err := NewLoader().Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.SDLC.Enabled {
		t.Fatal("QUICBAAS_SDLC_ENABLED=true did not enable the module, so the sweep it paces never starts")
	}
	if cfg.SDLC.LabelReconcile.Interval != 31*time.Minute {
		t.Errorf("interval = %s, want the environment's 31m", cfg.SDLC.LabelReconcile.Interval)
	}
}

// TestSDLC_EveryKeyIsBoundOnAFreshViper is the two-loader gate for the module's
// own keys: bound from one exported list so a key reachable through the library
// Loader cannot be unreachable in cmd/quicbaasd.
func TestSDLC_EveryKeyIsBoundOnAFreshViper(t *testing.T) {
	if len(SDLCKeys) == 0 {
		t.Fatal("no SDLC keys to check; this test would pass vacuously")
	}
	for _, key := range SDLCKeys {
		v := viper.New()
		v.SetEnvPrefix("QUICBAAS")
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		BindSDLCEnv(v)

		envName := "QUICBAAS_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		t.Setenv(envName, "true")
		if !v.IsSet(key) {
			t.Errorf("%s does not reach %q", envName, key)
		}
	}
}

// TestSDLC_EveryScalarFieldHasABoundKey is the DRIFT GATE over the module's own
// section, and it is the gate whose absence let `enabled` ship unbound while the
// sweep's cadence was bound beside it.
//
// It walks the struct rather than counting, so a scalar added to SDLCConfig
// without an entry in SDLCKeys fails the suite. Nested structs are skipped
// because they carry their own lists and their own gates; slices are skipped
// because they are documented file-only on SDLCKeys — and the skip is by TYPE, so
// neither exemption can quietly swallow a scalar.
func TestSDLC_EveryScalarFieldHasABoundKey(t *testing.T) {
	const prefix = "sdlc."

	keys := map[string]bool{}
	for _, key := range SDLCKeys {
		keys[key] = true
	}

	structType := reflect.TypeOf(SDLCConfig{})
	checked := 0
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		tag := field.Tag.Get("mapstructure")
		if tag == "" || tag == "-" {
			t.Errorf("field %s carries no mapstructure tag, so no configuration key names it", field.Name)
			continue
		}
		switch field.Type.Kind() {
		case reflect.Struct:
			continue // its own key list and its own gate
		case reflect.Slice:
			continue // documented file-only on SDLCKeys
		}
		checked++
		if !keys[prefix+tag] {
			t.Errorf("field %s maps to %q, which SDLCKeys does not name; the environment cannot reach it",
				field.Name, prefix+tag)
		}
	}
	if checked == 0 {
		t.Fatal("no scalar fields were checked; the gate would pass vacuously")
	}

	// The other direction: a key no field answers to is one an operator sets
	// and nothing ever reads.
	//
	// The nested webhook scalars are DERIVED rather than listed, because a
	// hand-written allowlist is the thing this gate exists to replace — one
	// added to SDLCKeys and forgotten here would have to be added twice, and
	// the second edit is the one that gets missed.
	named := map[string]bool{}
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		tag := field.Tag.Get("mapstructure")
		if tag == "" || tag == "-" {
			continue
		}
		named[prefix+tag] = true
		if field.Type.Kind() != reflect.Struct {
			continue
		}
		for j := 0; j < field.Type.NumField(); j++ {
			if nested := field.Type.Field(j).Tag.Get("mapstructure"); nested != "" && nested != "-" {
				named[prefix+tag+"."+nested] = true
			}
		}
	}
	for key := range keys {
		if !named[key] {
			t.Errorf("SDLCKeys names %q, which no field of SDLCConfig maps to", key)
		}
	}
}

// TestValidateSDLC_RefusesANodeThatCannotSayWhereItLives.
//
// Onboarding provisions a forge webhook pointing at this node's public
// origin. An absent or malformed one is refused AT LOAD rather than at the
// first onboarding, because the alternative is twenty-eight repositories
// configured to deliver somewhere that does not answer — each one reported
// provisioned, and each failure silent on both sides.
func TestValidateSDLC_RefusesANodeThatCannotSayWhereItLives(t *testing.T) {
	cases := map[string]struct {
		public  string
		wantErr bool
	}{
		"absent":         {"", true},
		"blank":          {"   ", true},
		"relative":       {"/api/v1", true},
		"no host":        {"https://", true},
		"wrong scheme":   {"ftp://factory.example.com", true},
		"bare hostname":  {"factory.example.com", true},
		"https":          {"https://factory.example.com", false},
		"http with port": {"http://192.0.2.10:8443", false},

		// AN ORIGIN, NOT MERELY A URL. The webhook route is appended to this
		// value textually, so each of these parses, carries a scheme and a
		// host — and yields a delivery URL that does not address the webhook
		// endpoint. None of them fails at delivery time in a way anybody
		// sees: the hook is provisioned, read back, and reported conformant.
		"query":           {"https://factory.example.com?x=1", true},
		"bare query":      {"https://factory.example.com?", true},
		"fragment":        {"https://factory.example.com#frag", true},
		"userinfo":        {"https://user:pass@factory.example.com", true},
		"path is allowed": {"https://factory.example.com/quicbaas", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			cfg.SDLC.Enabled = true
			cfg.SDLC.Webhook.PublicURL = tc.public

			v := NewValidator()
			v.validateSDLC(&cfg.SDLC)
			err := v.Errors()
			if tc.wantErr && err == nil {
				t.Fatalf("public_url %q was accepted; a provisioned delivery would have nowhere to go", tc.public)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("public_url %q was refused: %v", tc.public, err)
			}
		})
	}
}

// TestValidateSDLC_PublicURLIsOnlyRequiredWhenTheModuleRuns: a node with the
// module off has no webhooks to provision, and demanding an origin from it
// would make an unrelated deployment fail to load.
func TestValidateSDLC_PublicURLIsOnlyRequiredWhenTheModuleRuns(t *testing.T) {
	cfg := Default()
	cfg.SDLC.Enabled = false
	cfg.SDLC.Webhook.PublicURL = ""

	v := NewValidator()
	v.validateSDLC(&cfg.SDLC)
	if err := v.Errors(); err != nil {
		t.Fatalf("a node with the module off was refused: %v", err)
	}
}
