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

package config_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// An EXTERNAL test package, for the reason the stage-timeout drift test is one:
// pkg/sdlc reaches pkg/config through pkg/interfaces and an in-package test
// importing it would be a cycle.

// TestSDLCSurfacing_ShippedDefaultsMatchTheEngine pins the two defaults layers
// together.
//
// The shipped configuration states the retry so an operator reading it can see
// how hard a refusing forge is pressed, and the engine states it so an embedder
// that configures nothing still retries a bounded number of times. Two
// statements of one value drift; this is what stops them.
func TestSDLCSurfacing_ShippedDefaultsMatchTheEngine(t *testing.T) {
	shipped := config.Default().SDLC.Surfacing
	engine := sdlc.DefaultSurfacePolicy

	if shipped.RetryAttempts != engine.RetryAttempts {
		t.Errorf("retry_attempts: shipped default %d, engine default %d",
			shipped.RetryAttempts, engine.RetryAttempts)
	}
	if shipped.RetryInterval != engine.RetryInterval {
		t.Errorf("retry_interval: shipped default %s, engine default %s",
			shipped.RetryInterval, engine.RetryInterval)
	}
	if shipped.PostTimeout != engine.PostTimeout {
		t.Errorf("post_timeout: shipped default %s, engine default %s",
			shipped.PostTimeout, engine.PostTimeout)
	}

	// The shipped values must also be ones the engine accepts: a configuration
	// that fails the engine's own validation would refuse the boot it was
	// shipped to make work.
	policy := sdlc.SurfacePolicy{
		RetryAttempts: shipped.RetryAttempts,
		RetryInterval: shipped.RetryInterval,
		PostTimeout:   shipped.PostTimeout,
	}
	if err := policy.Validate(); err != nil {
		t.Errorf("the shipped surfacing policy does not validate: %v", err)
	}

	// AND THE ACCEPTED RANGE MATCHES THE ENGINE'S OWN. pkg/config spells the
	// bounds as literals because it cannot import pkg/sdlc; this is what stops
	// the two from separating, which would let the daemon accept a value its
	// own engine refuses at construction.
	if config.SDLCSurfacingMinPostTimeout != sdlc.MinPostTimeout {
		t.Errorf("config floor %s, engine floor %s", config.SDLCSurfacingMinPostTimeout, sdlc.MinPostTimeout)
	}
	if config.SDLCSurfacingMaxPostTimeout != sdlc.MaxPostTimeout {
		t.Errorf("config ceiling %s, engine ceiling %s", config.SDLCSurfacingMaxPostTimeout, sdlc.MaxPostTimeout)
	}
}

// TestSDLCSurfacing_EveryFieldHasABoundKey is the DRIFT GATE for this section.
//
// A knob is only a knob if the operator can reach it. This walks the struct
// with reflection rather than a hand-kept count, so a field added to
// SDLCSurfacing without a matching entry in SDLCSurfacingKeys — or with an
// entry BindSDLCSurfacingEnv does not actually bind — fails the suite instead
// of shipping as a setting that is read from the file and never from the
// environment. A hand-kept length check cannot catch that: adding a field and
// no key leaves the length unchanged.
//
// The failing shape this is able to return: delete "sdlc.surfacing.post_timeout"
// from SDLCSurfacingKeys and this reports the field with no key; make
// BindSDLCSurfacingEnv skip a key and it reports the environment not reaching
// it.
func TestSDLCSurfacing_EveryFieldHasABoundKey(t *testing.T) {
	const prefix = "sdlc.surfacing."

	keys := map[string]bool{}
	for _, key := range config.SDLCSurfacingKeys {
		if !strings.HasPrefix(key, prefix) {
			t.Errorf("key %q is not under %q", key, prefix)
		}
		keys[key] = true
	}

	structType := reflect.TypeOf(config.SDLCSurfacing{})
	durationType := reflect.TypeOf(time.Duration(0))
	named := map[string]bool{}

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		tag := field.Tag.Get("mapstructure")
		if tag == "" || tag == "-" {
			t.Errorf("field %s carries no mapstructure tag, so no configuration key names it", field.Name)
			continue
		}
		key := prefix + tag
		named[key] = true
		if !keys[key] {
			t.Errorf("field %s maps to %q, which SDLCSurfacingKeys does not name; the environment cannot reach it",
				field.Name, key)
			continue
		}

		// And the binding actually carries a value to that key on a viper
		// instance built the way both loaders build theirs.
		v := viper.New()
		v.SetEnvPrefix("QUICBAAS")
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		config.BindSDLCSurfacingEnv(v)

		envName := "QUICBAAS_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		switch {
		case field.Type == durationType:
			t.Setenv(envName, "47s")
			if got := v.GetDuration(key); got != 47*time.Second {
				t.Errorf("%s did not reach %s (got %s)", envName, key, got)
			}
		case field.Type.Kind() == reflect.Int:
			t.Setenv(envName, "7")
			if got := v.GetInt(key); got != 7 {
				t.Errorf("%s did not reach %s (got %d)", envName, key, got)
			}
		default:
			t.Errorf("field %s has type %s, which this drift gate cannot exercise; extend it",
				field.Name, field.Type)
		}
	}

	// The other direction: a key nothing on the struct answers to is a key an
	// operator sets and no field ever reads.
	for key := range keys {
		if !named[key] {
			t.Errorf("SDLCSurfacingKeys names %q, which no field of SDLCSurfacing maps to", key)
		}
	}
}
