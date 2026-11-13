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

package config_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// An EXTERNAL test package for the reason the surfacing drift test is one:
// pkg/orchestration/sdlc reaches pkg/config through pkg/interfaces, so an in-package test
// importing it would be a cycle.

// TestSDLCLabelReconcile_ShippedDefaultsMatchTheEngine pins the two defaults
// layers together.
//
// The shipped configuration states the cadence so an operator reading it can
// see how often the forge is re-read, and the module states it so an embedder
// that configures nothing still repairs stale labels. Two statements of one
// value drift; this is what stops them.
func TestSDLCLabelReconcile_ShippedDefaultsMatchTheEngine(t *testing.T) {
	shipped := config.Default().SDLC.LabelReconcile

	if shipped.Interval != sdlc.DefaultLabelReconcileInterval {
		t.Errorf("interval: shipped default %s, module default %s",
			shipped.Interval, sdlc.DefaultLabelReconcileInterval)
	}
	if shipped.ForgeInterval != sdlc.DefaultForgeCallInterval {
		t.Errorf("forge_interval: shipped default %s, module default %s",
			shipped.ForgeInterval, sdlc.DefaultForgeCallInterval)
	}
}

// TestSDLCLabelReconcile_EveryFieldHasABoundKey is the DRIFT GATE for this
// section, and it is the gate that was missing when the section shipped: both
// knobs were readable from a file and neither from the environment.
//
// A knob is only a knob if the operator can reach it. This walks the struct
// with reflection rather than a hand-kept count, so a field added to
// SDLCLabelReconcile without a matching entry in SDLCLabelReconcileKeys — or
// with an entry BindSDLCLabelReconcileEnv does not actually bind — fails the
// suite instead of shipping as a setting read from the file and never from the
// environment. A hand-kept length check cannot catch that: adding a field and
// no key leaves the length unchanged.
func TestSDLCLabelReconcile_EveryFieldHasABoundKey(t *testing.T) {
	const prefix = "sdlc.label_reconcile."

	keys := map[string]bool{}
	for _, key := range config.SDLCLabelReconcileKeys {
		if !strings.HasPrefix(key, prefix) {
			t.Errorf("key %q is not under %q", key, prefix)
		}
		keys[key] = true
	}

	structType := reflect.TypeOf(config.SDLCLabelReconcile{})
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
			t.Errorf("field %s maps to %q, which SDLCLabelReconcileKeys does not name; the environment cannot reach it",
				field.Name, key)
			continue
		}

		// And the binding actually carries a value to that key on a viper
		// instance built the way both loaders build theirs.
		v := viper.New()
		v.SetEnvPrefix("QUICBAAS")
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		config.BindSDLCLabelReconcileEnv(v)

		envName := "QUICBAAS_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		switch field.Type {
		case durationType:
			t.Setenv(envName, "47s")
			if got := v.GetDuration(key); got != 47*time.Second {
				t.Errorf("%s did not reach %s (got %s)", envName, key, got)
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
			t.Errorf("SDLCLabelReconcileKeys names %q, which no field of SDLCLabelReconcile maps to", key)
		}
	}
}
