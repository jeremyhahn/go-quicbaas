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

package service

import (
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// The service plane keeps a deployed service's credentials under the
// tenant's services/<name>/ (serviceplane/grants.SecretPrefix), and its
// secrets grant binds that prefix to the service's workload identity: the
// record at services/<name>/binding names the SPIFFE ID in its bound_to
// metadata. The secrets service enforces the binding on every verb, so the
// tenant's other principals, secrets:read or not, never reach a service's
// SVID or TLS key.
const (
	serviceSecretsPrefix = "services/"
	serviceBindingKey    = "binding"
	serviceBindingMeta   = "bound_to"
)

// servicePlaneKeys are the secrets the plane itself writes under a
// service's prefix; the bound workload reads them but never writes them.
var servicePlaneKeys = map[string]bool{serviceBindingKey: true, "svid": true, "tls": true}

// secretsWriteVerbs are the secrets verbs that change a secret.
var secretsWriteVerbs = map[string]bool{
	"StoreSecret": true, "EnsureSecret": true, "DeleteSecret": true, "RollbackSecret": true,
	"RotateSecret": true, "SetRotationPolicy": true, "UpdateSecretStatus": true,
}

// serviceSecretName splits a tenant-relative secret ID under services/ into
// the service name and the path below it.
func serviceSecretName(secretID string) (name, rel string, ok bool) {
	rest, under := strings.CutPrefix(strings.TrimLeft(secretID, "/"), serviceSecretsPrefix)
	if !under {
		return "", "", false
	}
	name, rel, _ = strings.Cut(rest, "/")
	return name, rel, name != ""
}

// enforceServiceBinding refuses a verb on a secret under services/<name>/
// unless the caller is the workload the service's binding names, or an
// in-process system call. A prefix with no binding is the plane's alone. A
// write of a key the plane manages (the binding, the SVID, the TLS
// certificate) is refused to the workload too. secretID is
// tenant-relative.
func (s *secretsService) enforceServiceBinding(ctx context.Context, tenantID, secretID, operation string, write bool) error {
	name, rel, ok := serviceSecretName(secretID)
	if !ok || auth.IsSystemContext(ctx) {
		return nil
	}
	op := "secrets." + operation
	bound, err := s.serviceBoundTo(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if bound == "" {
		return NewError(errors.ErrForbidden, op, "services/"+name+"/ is reserved for the service plane")
	}
	if auth.PrincipalFromContext(ctx) != bound {
		return NewError(errors.ErrForbidden, op, "services/"+name+"/ is bound to the service's own workload")
	}
	if write && servicePlaneKeys[strings.TrimSuffix(rel, "/")] {
		return NewError(errors.ErrForbidden, op, "services/"+name+"/"+rel+" is written by the service plane only")
	}
	return nil
}

// serviceBoundTo returns the workload the binding of the tenant's service
// name names, or "" when it has none. A read failure other than absence
// is returned, so the check fails closed.
func (s *secretsService) serviceBoundTo(ctx context.Context, tenantID, name string) (string, error) {
	path := secretsTenantScopedID(tenantID, serviceSecretsPrefix+name+"/"+serviceBindingKey)
	_, meta, err := s.secretsManager.Get(ctx, path)
	if err != nil {
		if secretLookupCode(err) == errors.ErrNotFound {
			return "", nil
		}
		return "", &errors.QBError{Code: secretLookupCode(err), Op: "secrets.binding", Err: err}
	}
	return meta[serviceBindingMeta], nil
}

// serviceBindingVisible reports whether a listing may show the caller the
// tenant-scoped path: one under services/<name>/ only to its bound workload.
func (s *secretsService) serviceBindingVisible(ctx context.Context, tenantID, scopedPath string) bool {
	return s.enforceServiceBinding(ctx, tenantID, secretsTenantStripID(tenantID, scopedPath), "list", false) == nil
}
