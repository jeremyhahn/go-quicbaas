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

package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Reference checks owned by the service layer
// =============================================================================
//
// The checks for objects the service layer holds — secrets and KMS-held SSH
// keys — live here, once, so every transport's save path inherits them. The
// orchestration-owned references (workflows, fleet nodes, tenants) are
// checked by orchestration.Require*. Every check distinguishes an absent
// object (*quicbaaserrors.ReferenceNotFoundError) from a store that could not
// answer (*quicbaaserrors.ReferenceUnavailableError).

// defaultSSHKeyScope is the key-store scope an SSH key reference with no
// scope resolves in — the same default the signer resolver applies at use.
const defaultSSHKeyScope = "user"

// SSHKeyGetter loads one SSH key's metadata; SSHService satisfies it.
type SSHKeyGetter interface {
	GetKey(ctx context.Context, req xkmsssh.GetKeyRequest) (*xkmsssh.Key, error)
}

// requireSecretRef resolves the secret a reference field names in tenantID
// and returns its value. An empty value is returned as-is: whether an empty
// secret is usable is the caller's rule, not a question of existence.
func requireSecretRef(ctx context.Context, secrets SecretsService, tenantID, field, ref string) ([]byte, error) {
	if secrets == nil {
		return nil, &quicbaaserrors.ReferenceUnavailableError{Field: field, Kind: quicbaaserrors.ReferenceKindSecret, Ref: ref}
	}
	resp, err := secrets.GetSecret(tenantctx.WithTenant(ctx, tenantID), &GetSecretRequest{SecretID: ref})
	if err != nil {
		if quicbaaserrors.IsNotFound(err) {
			return nil, &quicbaaserrors.ReferenceNotFoundError{Field: field, Kind: quicbaaserrors.ReferenceKindSecret, Ref: ref}
		}
		return nil, &quicbaaserrors.ReferenceUnavailableError{Field: field, Kind: quicbaaserrors.ReferenceKindSecret, Ref: ref, Err: err}
	}
	if resp == nil {
		return nil, nil
	}
	return resp.Data, nil
}

// requireSSHKeyRef refuses a KMS key reference that names no SSH key. The
// scope and owner default exactly as they do when the key is used: the scope
// to defaultSSHKeyScope and the owner to the project's tenant.
func requireSSHKeyRef(ctx context.Context, keys SSHKeyGetter, tenantID, field string, ref orchestration.KMSKeyRef) error {
	req := resolveSSHKeyRequest(tenantID, ref)
	display := req.Scope + "/" + req.Owner + "/" + req.Name
	if keys == nil {
		return &quicbaaserrors.ReferenceUnavailableError{Field: field, Kind: quicbaaserrors.ReferenceKindSSHKey, Ref: display}
	}
	_, err := keys.GetKey(ctx, req)
	if err == nil {
		return nil
	}
	if isSSHKeyAbsent(err) {
		return &quicbaaserrors.ReferenceNotFoundError{Field: field, Kind: quicbaaserrors.ReferenceKindSSHKey, Ref: display}
	}
	return &quicbaaserrors.ReferenceUnavailableError{Field: field, Kind: quicbaaserrors.ReferenceKindSSHKey, Ref: display, Err: err}
}

// resolveSSHKeyRequest applies the scope and owner defaults to a reference.
func resolveSSHKeyRequest(tenantID string, ref orchestration.KMSKeyRef) xkmsssh.GetKeyRequest {
	scope := strings.TrimSpace(ref.Scope)
	if scope == "" {
		scope = defaultSSHKeyScope
	}
	owner := strings.TrimSpace(ref.Owner)
	if owner == "" {
		owner = tenantID
	}
	return xkmsssh.GetKeyRequest{Scope: scope, Owner: owner, Name: strings.TrimSpace(ref.Name)}
}

// isSSHKeyAbsent reports whether the key store answered that no such key can
// be found: a missing key, or an address (scope) no key can live at. The
// remote clients report a missing key as *xkmsssh.NotFoundError; the embedded
// store's own types are classified by the SDK's status table.
func isSSHKeyAbsent(err error) bool {
	var notFound *xkmsssh.NotFoundError
	if errors.As(err, &notFound) {
		return true
	}
	switch xkmsssh.HTTPStatusForError(err) {
	case http.StatusNotFound, http.StatusBadRequest:
		return true
	default:
		return false
	}
}
