// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

// TestSDLCSurfacing_EveryKeyIsBoundOnAFreshViper: the daemon and the library
// Loader are two viper instances, and the daemon is the one that runs in
// production. Binding from one exported list is what keeps a key bound in the
// Loader from being unbound in cmd/quicbaasd — the shape of a variable an
// operator sets and never gets.
func TestSDLCSurfacing_EveryKeyIsBoundOnAFreshViper(t *testing.T) {
	v := viper.New()
	v.SetEnvPrefix("QUICBAAS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	BindSDLCSurfacingEnv(v)

	t.Setenv("QUICBAAS_SDLC_SURFACING_RETRY_ATTEMPTS", "9")
	if got := v.GetInt("sdlc.surfacing.retry_attempts"); got != 9 {
		t.Errorf("QUICBAAS_SDLC_SURFACING_RETRY_ATTEMPTS did not reach the key (got %d)", got)
	}
	t.Setenv("QUICBAAS_SDLC_SURFACING_RETRY_INTERVAL", "45s")
	if got := v.GetDuration("sdlc.surfacing.retry_interval"); got != 45*time.Second {
		t.Errorf("QUICBAAS_SDLC_SURFACING_RETRY_INTERVAL did not reach the key (got %s)", got)
	}

	t.Setenv("QUICBAAS_SDLC_SURFACING_POST_TIMEOUT", "12s")
	if got := v.GetDuration("sdlc.surfacing.post_timeout"); got != 12*time.Second {
		t.Errorf("QUICBAAS_SDLC_SURFACING_POST_TIMEOUT did not reach the key (got %s)", got)
	}

	// That the LIST matches the struct field for field is checked by
	// TestSDLCSurfacing_EveryFieldHasABoundKey, which walks the type with
	// reflection. A length compared against a literal here would pass for the
	// case that matters — a field added with no key leaves the length alone.
}

// TestSDLCSurfacing_TheEnvironmentBeatsTheFile walks the real Loader, which is
// the path the daemon takes.
func TestSDLCSurfacing_TheEnvironmentBeatsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quicbaas.yaml")
	body := "sdlc:\n  enabled: true\n  surfacing:\n    retry_attempts: 4\n    retry_interval: 3m\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := NewLoader().Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SDLC.Surfacing.RetryAttempts != 4 || cfg.SDLC.Surfacing.RetryInterval != 3*time.Minute {
		t.Fatalf("from file: %+v, want 4 attempts at 3m", cfg.SDLC.Surfacing)
	}

	t.Setenv("QUICBAAS_SDLC_SURFACING_RETRY_INTERVAL", "11s")
	cfg, err = NewLoader().Load(path)
	if err != nil {
		t.Fatalf("Load with env: %v", err)
	}
	if cfg.SDLC.Surfacing.RetryInterval != 11*time.Second {
		t.Errorf("retry_interval = %s, want the environment's 11s to beat the file's 3m",
			cfg.SDLC.Surfacing.RetryInterval)
	}
	if cfg.SDLC.Surfacing.RetryAttempts != 4 {
		t.Errorf("retry_attempts = %d; one overridden field must not disturb another",
			cfg.SDLC.Surfacing.RetryAttempts)
	}
}

// TestSDLCSurfacing_AnUnparseableEnvironmentValueRefusesTheBoot is the
// silent-drop failure this section exists to prevent: a bound the operator set,
// the daemon ignored, and nothing anywhere reported.
func TestSDLCSurfacing_AnUnparseableEnvironmentValueRefusesTheBoot(t *testing.T) {
	t.Setenv("QUICBAAS_SDLC_SURFACING_RETRY_INTERVAL", "two minutes")

	cfg, err := NewLoader().Load("")
	if err == nil {
		t.Fatalf("Load() = nil error with an unparseable interval; retry_interval = %s",
			cfg.SDLC.Surfacing.RetryInterval)
	}
	if !strings.Contains(err.Error(), "sdlc.surfacing.retry_interval") {
		t.Errorf("error = %v, want the offending key named", err)
	}
}

// TestSDLCSurfacing_NegativeIsRefusedByValidation: zero takes the defaults
// layer, and a negative value read literally abandons every park before its
// first post.
func TestSDLCSurfacing_NegativeIsRefusedByValidation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		surfacing SDLCSurfacing
		key       string
	}{
		{"negative attempts", SDLCSurfacing{RetryAttempts: -1}, "sdlc.surfacing.retry_attempts"},
		{"negative interval", SDLCSurfacing{RetryInterval: -time.Second}, "sdlc.surfacing.retry_interval"},
		{"negative post timeout", SDLCSurfacing{PostTimeout: -time.Second}, "sdlc.surfacing.post_timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.SDLC.Enabled = true
			// Enabling the module now requires the node to say where it lives;
			// onboarding provisions forge webhooks pointing at it.
			cfg.SDLC.Webhook.PublicURL = "https://factory.test"
			cfg.SDLC.Surfacing = tc.surfacing

			v := NewValidator()
			v.validateSDLC(&cfg.SDLC)
			err := v.Errors()
			if err == nil {
				t.Fatalf("%+v validated", tc.surfacing)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Errorf("error = %v, want the offending key named", err)
			}
		})
	}

	// And an unset section is NOT refused: zero is how an operator says "take
	// the default".
	cfg := Default()
	cfg.SDLC.Enabled = true
	// Enabling the module now requires the node to say where it lives;
	// onboarding provisions forge webhooks pointing at it.
	cfg.SDLC.Webhook.PublicURL = "https://factory.test"
	cfg.SDLC.Surfacing = SDLCSurfacing{}
	v := NewValidator()
	v.validateSDLC(&cfg.SDLC)
	if err := v.Errors(); err != nil {
		t.Errorf("an unset surfacing section was refused: %v", err)
	}
}
