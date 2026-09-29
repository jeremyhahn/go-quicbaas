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

package workerimage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

const (
	testSecretTenant = "automatethethings"
	testSecretRef    = "sdlc/registry/worker-image/token"
)

// fakeSecrets is a tenant secrets store in memory.
type fakeSecrets struct {
	values map[string]*orchestration.AccountSecretValue
	err    error
	reads  []string
}

func (s *fakeSecrets) ResolveAccountSecret(_ context.Context, tenantID, ref string) (*orchestration.AccountSecretValue, error) {
	s.reads = append(s.reads, tenantID+":"+ref)
	if s.err != nil {
		return nil, s.err
	}
	return s.values[tenantID+":"+ref], nil
}

func secretsHolding(data string) *fakeSecrets {
	return &fakeSecrets{values: map[string]*orchestration.AccountSecretValue{
		testSecretTenant + ":" + testSecretRef: {Data: []byte(data), Generation: 3},
	}}
}

func TestSecretCredential_ReadsTheTokenFromTheTenantSecret(t *testing.T) {
	secrets := secretsHolding("s3cret\n")
	credential, err := NewSecretCredential(secrets, testSecretTenant, testSecretRef, "agent-company")
	if err != nil {
		t.Fatalf("NewSecretCredential: %v", err)
	}
	if len(secrets.reads) != 0 {
		t.Fatal("the secret must not be read until a push needs it")
	}

	auth, err := credential.Credential(t.Context())
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if auth.Username != "agent-company" || string(auth.Secret) != "s3cret" {
		t.Fatalf("auth = %q/%q, want the account and the trimmed token", auth.Username, auth.Secret)
	}
	if len(secrets.reads) != 1 || secrets.reads[0] != testSecretTenant+":"+testSecretRef {
		t.Fatalf("reads = %v, want one read in the configured tenant", secrets.reads)
	}
}

func TestSecretCredential_ARotatedSecretIsReadAtTheNextPush(t *testing.T) {
	secrets := secretsHolding("first")
	credential, err := NewSecretCredential(secrets, testSecretTenant, testSecretRef, "agent-company")
	if err != nil {
		t.Fatalf("NewSecretCredential: %v", err)
	}
	if _, err := credential.Credential(t.Context()); err != nil {
		t.Fatal(err)
	}
	secrets.values[testSecretTenant+":"+testSecretRef] = &orchestration.AccountSecretValue{Data: []byte("second")}

	auth, err := credential.Credential(t.Context())
	if err != nil || string(auth.Secret) != "second" {
		t.Fatalf("auth = %v, %v, want the rotated token", auth, err)
	}
}

func TestSecretCredential_Refuses(t *testing.T) {
	cases := map[string]*fakeSecrets{
		"unreadable": {err: errFake},
		"missing":    {values: map[string]*orchestration.AccountSecretValue{}},
		"empty":      secretsHolding(" \n"),
	}
	for name, secrets := range cases {
		t.Run(name, func(t *testing.T) {
			credential, err := NewSecretCredential(secrets, testSecretTenant, testSecretRef, "agent-company")
			if err != nil {
				t.Fatalf("NewSecretCredential: %v", err)
			}
			_, err = credential.Credential(t.Context())
			var credErr *CredentialError
			if !errors.Is(err, ErrCredential) || !errors.As(err, &credErr) {
				t.Fatalf("err = %v, want a CredentialError", err)
			}
			if !strings.Contains(err.Error(), testSecretTenant+":"+testSecretRef) {
				t.Fatalf("err = %v, want it to name the secret", err)
			}
		})
	}
	t.Run("unreadable keeps the cause", func(t *testing.T) {
		credential, err := NewSecretCredential(&fakeSecrets{err: errFake}, testSecretTenant, testSecretRef, "u")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := credential.Credential(t.Context()); !errors.Is(err, errFake) {
			t.Fatalf("err = %v, want the store's error", err)
		}
	})
}

func TestNewSecretCredential_RefusesAnIncompleteReference(t *testing.T) {
	secrets := secretsHolding("t")
	cases := map[string]struct {
		resolver orchestration.AccountSecretResolver
		tenant   string
		ref      string
		username string
		field    string
	}{
		"no secrets service":  {nil, testSecretTenant, testSecretRef, "u", "secrets"},
		"no tenant":           {secrets, " ", testSecretRef, "u", "registry_token_tenant"},
		"no reference":        {secrets, testSecretTenant, "", "u", "registry_token_secret"},
		"storage path":        {secrets, testSecretTenant, "tenant/automatethethings/sdlc/x", "u", "registry_token_secret"},
		"dot-dot segment":     {secrets, testSecretTenant, "sdlc/../x", "u", "registry_token_secret"},
		"whitespace in a ref": {secrets, testSecretTenant, "sdlc/x y", "u", "registry_token_secret"},
		"no username":         {secrets, testSecretTenant, testSecretRef, "", "registry_username"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewSecretCredential(tc.resolver, tc.tenant, tc.ref, tc.username)
			var invalid *InvalidConfigError
			if !errors.As(err, &invalid) || invalid.Field != tc.field {
				t.Fatalf("err = %v, want an InvalidConfigError on %s", err, tc.field)
			}
		})
	}
}
