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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// sdlcWorkerImageConfig is a valid, enabled module with the refresh on.
func sdlcWorkerImageConfig() *Config {
	cfg := Default()
	cfg.SDLC.Enabled = true
	cfg.SDLC.Webhook.PublicURL = "https://factory.example.com"
	return cfg
}

func TestValidateSDLCWorkerImage_TheShippedDefaultsAreValid(t *testing.T) {
	cfg := sdlcWorkerImageConfig()
	v := NewValidator()
	v.validateSDLC(&cfg.SDLC)
	if err := v.Errors(); err != nil {
		t.Fatalf("the shipped worker image defaults were refused: %v", err)
	}
	if !cfg.SDLC.WorkerImage.Enabled || cfg.SDLC.WorkerImage.Registry != "" {
		t.Fatalf("defaults = %+v, want enabled in local mode", cfg.SDLC.WorkerImage)
	}
}

func TestValidateSDLCWorkerImage_Refuses(t *testing.T) {
	cases := map[string]func(*SDLCWorkerImage){
		"tagged repository":     func(w *SDLCWorkerImage) { w.Repository = "worker@sha256:1" },
		"empty repository":      func(w *SDLCWorkerImage) { w.Repository = " " },
		"registry without path": func(w *SDLCWorkerImage) { w.Registry = "198.51.100.50:3001" },
		"username only":         func(w *SDLCWorkerImage) { w.Registry = "r.x/o/w"; w.RegistryUsername = "agent" },
		"secret without tenant": func(w *SDLCWorkerImage) {
			withSecretCredential(w)
			w.RegistryTokenTenant = " "
		},
		"tenant without secret": func(w *SDLCWorkerImage) {
			withSecretCredential(w)
			w.RegistryTokenSecret = ""
		},
		"credential, no registry": func(w *SDLCWorkerImage) {
			withSecretCredential(w)
			w.Registry = ""
		},
		"bad npm url":      func(w *SDLCWorkerImage) { w.NPMRegistryURL = "registry.npmjs.org" },
		"no smoke model":   func(w *SDLCWorkerImage) { w.SmokeModel = "" },
		"negative refresh": func(w *SDLCWorkerImage) { w.RefreshInterval = -time.Second },
		"negative timeout": func(w *SDLCWorkerImage) { w.PassTimeout = -time.Second },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := sdlcWorkerImageConfig()
			mutate(&cfg.SDLC.WorkerImage)
			v := NewValidator()
			v.validateSDLC(&cfg.SDLC)
			if err := v.Errors(); err == nil || !strings.Contains(err.Error(), "sdlc.worker_image.") {
				t.Fatalf("err = %v, want a sdlc.worker_image refusal", err)
			}
		})
	}
}

// withSecretCredential names a registry and the full secret-backed credential.
func withSecretCredential(w *SDLCWorkerImage) {
	w.Registry = "r.x/o/w"
	w.RegistryUsername = "company-agent"
	w.RegistryTokenSecret = "sdlc/registry/worker-image/token"
	w.RegistryTokenTenant = "automatethethings"
}

func TestValidateSDLCWorkerImage_AcceptsASecretBackedCredential(t *testing.T) {
	cfg := sdlcWorkerImageConfig()
	withSecretCredential(&cfg.SDLC.WorkerImage)
	v := NewValidator()
	v.validateSDLCWorkerImage(&cfg.SDLC.WorkerImage)
	if err := v.Errors(); err != nil {
		t.Fatalf("a registry with its account, secret and tenant was refused: %v", err)
	}
}

func TestSDLCWorkerImage_TheTokenFileIsARemovedKey(t *testing.T) {
	where, ok := RemovedKeys["sdlc.worker_image.registry_token_file"]
	if !ok || !strings.Contains(where, "registry_token_secret") {
		t.Fatalf("RemovedKeys[registry_token_file] = %q, %v; want it to point at the secret", where, ok)
	}
}

func TestValidateSDLCWorkerImage_ADisabledRefreshIsNotValidated(t *testing.T) {
	cfg := sdlcWorkerImageConfig()
	cfg.SDLC.WorkerImage = SDLCWorkerImage{Enabled: false, Repository: "@@"}
	v := NewValidator()
	v.validateSDLC(&cfg.SDLC)
	if err := v.Errors(); err != nil {
		t.Fatalf("a disabled refresh was validated: %v", err)
	}
}

// TestSDLCWorkerImage_EveryFieldHasABoundKey is the drift gate between the
// struct and SDLCWorkerImageKeys, in both directions, and proves each binding
// carries an environment value to its key.
func TestSDLCWorkerImage_EveryFieldHasABoundKey(t *testing.T) {
	const prefix = "sdlc.worker_image."
	keys := map[string]bool{}
	for _, key := range SDLCWorkerImageKeys {
		if !strings.HasPrefix(key, prefix) {
			t.Errorf("key %q is not under %q", key, prefix)
		}
		keys[key] = true
	}
	structType := reflect.TypeOf(SDLCWorkerImage{})
	named := map[string]bool{}
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		key := prefix + field.Tag.Get("mapstructure")
		named[key] = true
		if !keys[key] {
			t.Errorf("field %s maps to %q, which SDLCWorkerImageKeys does not name", field.Name, key)
			continue
		}
		v := viper.New()
		v.SetEnvPrefix("QUICBAAS")
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		BindSDLCWorkerImageEnv(v)
		envName := "QUICBAAS_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		switch field.Type.Kind() {
		case reflect.Bool:
			t.Setenv(envName, "false")
			if v.GetBool(key) || !v.IsSet(key) {
				t.Errorf("%s did not reach %s", envName, key)
			}
		case reflect.Int64:
			t.Setenv(envName, "47s")
			if v.GetDuration(key) != 47*time.Second {
				t.Errorf("%s did not reach %s", envName, key)
			}
		case reflect.String:
			t.Setenv(envName, "x")
			if v.GetString(key) != "x" {
				t.Errorf("%s did not reach %s", envName, key)
			}
		default:
			t.Errorf("field %s has kind %s, which this gate cannot exercise; extend it", field.Name, field.Type.Kind())
		}
	}
	for key := range keys {
		if !named[key] {
			t.Errorf("SDLCWorkerImageKeys names %q, which no field maps to", key)
		}
	}
}
