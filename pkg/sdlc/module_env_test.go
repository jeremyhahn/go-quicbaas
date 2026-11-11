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

package sdlc

import (
	"errors"
	"testing"
)

// TestModuleEnv_RewritesTheImportPrefixToTheForgeOverSSH: the prefix's
// modules are private to Go, and git reaches them at the forge beside the
// project — same scheme, user, host, port and owner, repository dropped.
func TestModuleEnv_RewritesTheImportPrefixToTheForgeOverSSH(t *testing.T) {
	env, err := ModuleEnv(ModulePolicy{RewritePrefix: "https://github.com/acme/"}, "ssh://git@forge.test:2022/acme/api.git")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"GOPRIVATE":          "github.com/acme/*",
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "url.ssh://git@forge.test:2022/acme/.insteadOf",
		"GIT_CONFIG_VALUE_0": "https://github.com/acme/",
	}
	if len(env) != len(want) {
		t.Fatalf("env = %v, want %v", env, want)
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
}

// TestModuleEnv_AnHTTPSRemoteRewritesToHTTPS: a project cloned over https has
// its siblings reached the same way; the derivation copies the scheme rather
// than assuming ssh.
func TestModuleEnv_AnHTTPSRemoteRewritesToHTTPS(t *testing.T) {
	env, err := ModuleEnv(ModulePolicy{RewritePrefix: "https://github.com/acme/"}, "https://forge.test/acme/api.git")
	if err != nil {
		t.Fatal(err)
	}
	if got := env["GIT_CONFIG_KEY_0"]; got != "url.https://forge.test/acme/.insteadOf" {
		t.Errorf("rewrite target = %q", got)
	}
}

// TestModuleEnv_NoPolicyInjectsNothing: a project with no private modules
// gets no GOPRIVATE and no rewrite, so its Go behaves exactly as the image
// ships it.
func TestModuleEnv_NoPolicyInjectsNothing(t *testing.T) {
	env, err := ModuleEnv(ModulePolicy{}, "ssh://git@forge.test:2022/acme/api.git")
	if err != nil {
		t.Fatal(err)
	}
	if env != nil {
		t.Fatalf("env = %v, want none", env)
	}
}

// TestModuleEnv_RefusesWhatItCannotDerive: an invalid prefix, and a repository
// URL with no owner segment, are misconfigurations reported before the spawn,
// not a worker charged for a build that could never resolve.
func TestModuleEnv_RefusesWhatItCannotDerive(t *testing.T) {
	cases := map[string]struct {
		policy  ModulePolicy
		repoURL string
		field   string
	}{
		"bad prefix":     {ModulePolicy{RewritePrefix: "github.com/acme/"}, "ssh://git@forge.test/acme/api.git", "modules.rewrite_prefix"},
		"no owner":       {ModulePolicy{RewritePrefix: "https://github.com/acme/"}, "ssh://git@forge.test/api.git", "project.git_repo_url"},
		"no scheme":      {ModulePolicy{RewritePrefix: "https://github.com/acme/"}, "forge.test/acme/api.git", "project.git_repo_url"},
		"empty remote":   {ModulePolicy{RewritePrefix: "https://github.com/acme/"}, "", "project.git_repo_url"},
		"prefix no host": {ModulePolicy{RewritePrefix: "https:///acme/"}, "ssh://git@forge.test/acme/api.git", "modules.rewrite_prefix"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env, err := ModuleEnv(tc.policy, tc.repoURL)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want invalid input", err)
			}
			var invalid *InvalidInputError
			if !errors.As(err, &invalid) || invalid.Field != tc.field {
				t.Errorf("err = %v, want field %s", err, tc.field)
			}
			if env != nil {
				t.Errorf("env = %v on error", env)
			}
		})
	}
}

// TestModulePolicy_GoPrivate: the pattern is the prefix's host and owner
// with a wildcard, and nothing for an empty policy.
func TestModulePolicy_GoPrivate(t *testing.T) {
	if got := (ModulePolicy{RewritePrefix: "https://github.com/acme/"}).GoPrivate(); got != "github.com/acme/*" {
		t.Errorf("GoPrivate = %q", got)
	}
	if got := (ModulePolicy{}).GoPrivate(); got != "" {
		t.Errorf("empty GoPrivate = %q", got)
	}
}
