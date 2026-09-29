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

package main

import (
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestBindEnvVars_BindsEverySurfacingKey is the SECOND HALF of the two-loader
// gate.
//
// This daemon is loaded by two viper instances — pkg/config's Loader and this
// package's own global — and a knob bound on one of them is a setting the
// operator writes, reads back from the file, and never gets from the
// environment. pkg/config's drift gate proves the key list matches the struct
// and that BindSDLCSurfacingEnv reaches every key; nothing there can see
// whether THIS loader calls it. That is what this covers.
//
// The failing shape it is able to return: delete the
// config.BindSDLCSurfacingEnv call from bindEnvVars and every key below reports
// its environment variable not arriving.
func TestBindEnvVars_BindsEverySurfacingKey(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	// initConfig's own env wiring, so a key resolves here exactly as it does
	// in production.
	viper.SetEnvPrefix("QUICBAAS")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	bindEnvVars()

	if len(config.SDLCSurfacingKeys) == 0 {
		t.Fatal("no surfacing keys to check; this test would pass vacuously")
	}
	for _, key := range config.SDLCSurfacingKeys {
		envName := "QUICBAAS_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		t.Setenv(envName, "13")
		if !viper.IsSet(key) {
			t.Errorf("%s does not reach %q on the daemon's own viper; the knob is bound on one loader only", envName, key)
			continue
		}
		if got := viper.GetString(key); got != "13" {
			t.Errorf("%s reached %q as %q, want the value set", envName, key, got)
		}
	}
}

// TestBindEnvVars_BindsEveryLabelReconcileKey is the same two-loader gate for
// the repair sweep's cadence. pkg/config proves the key list matches the struct
// and that BindSDLCLabelReconcileEnv reaches every key; only this can see
// whether THIS loader calls it.
//
// The failing shape it is able to return: delete the
// config.BindSDLCLabelReconcileEnv call from bindEnvVars and every key below
// reports its environment variable not arriving.
func TestBindEnvVars_BindsEveryLabelReconcileKey(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	viper.SetEnvPrefix("QUICBAAS")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	bindEnvVars()

	if len(config.SDLCLabelReconcileKeys) == 0 {
		t.Fatal("no label-reconcile keys to check; this test would pass vacuously")
	}
	for _, key := range config.SDLCLabelReconcileKeys {
		envName := "QUICBAAS_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		t.Setenv(envName, "13s")
		if !viper.IsSet(key) {
			t.Errorf("%s does not reach %q on the daemon's own viper; the knob is bound on one loader only", envName, key)
			continue
		}
		if got := viper.GetString(key); got != "13s" {
			t.Errorf("%s reached %q as %q, want the value set", envName, key, got)
		}
	}
}

// TestInitConfig_RefusesARemovedSDLCKeyOnTheDaemonsOwnViper covers the OTHER
// half of the same two-loader divergence.
//
// pkg/config's Loader refuses a removed sdlc: key and the documentation says a
// configuration carrying one fails to load -- but this daemon unmarshals the
// global viper directly, so the promise was kept by the library path and broken
// by the production one. An operator who renamed nothing would boot a node that
// ignored the setting while believing it was enforced.
//
// The refusal itself is pkg/config's and is tested there; what this pins is
// that the daemon's own viper is the one being asked.
func TestInitConfig_RefusesARemovedSDLCKeyOnTheDaemonsOwnViper(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	viper.SetEnvPrefix("QUICBAAS")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	bindEnvVars()

	if err := config.RefuseRemovedKeys(viper.GetViper()); err != nil {
		t.Fatalf("a daemon viper setting no removed key was refused: %v", err)
	}

	// One removed key, set the way an operator's stale configuration sets it.
	var removed string
	for key := range config.RemovedKeys {
		if removed == "" || key < removed {
			removed = key
		}
	}
	if removed == "" {
		t.Fatal("no removed sdlc keys to check; this test would pass vacuously")
	}
	viper.Set(removed, "5m")

	err := config.RefuseRemovedKeys(viper.GetViper())
	if err == nil {
		t.Fatalf("the daemon's viper accepted the removed key %q", removed)
	}
	if !strings.Contains(err.Error(), removed) {
		t.Errorf("the refusal does not name %q: %v", removed, err)
	}
}

// TestBindEnvVars_BindsEverySDLCKey is the two-loader gate for the module's own
// keys, `sdlc.enabled` above all: the daemon is the loader that runs in
// production, and a module that cannot be switched on from the environment makes
// every other SDLC variable a half-truth in an env-only deployment.
//
// The failing shape it is able to return: delete the config.BindSDLCEnv call from
// bindEnvVars and every key below reports its environment variable not arriving.
func TestBindEnvVars_BindsEverySDLCKey(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	viper.SetEnvPrefix("QUICBAAS")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	bindEnvVars()

	if len(config.SDLCKeys) == 0 {
		t.Fatal("no SDLC keys to check; this test would pass vacuously")
	}
	for _, key := range config.SDLCKeys {
		envName := "QUICBAAS_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		t.Setenv(envName, "7")
		if !viper.IsSet(key) {
			t.Errorf("%s does not reach %q on the daemon's own viper; the knob is bound on one loader only", envName, key)
		}
	}
}
