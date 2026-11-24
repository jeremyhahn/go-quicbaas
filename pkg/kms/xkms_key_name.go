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

package kms

import (
	"strings"

	"github.com/jeremyhahn/go-xkms/pkg/validation"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// tenantKeyRefPrefix starts the service's reference to a tenant's key,
// "tenant/<tenantID>/<name>".
const tenantKeyRefPrefix = "tenant/"

// xkmsKeyRef maps a service key reference to go-xkms's own form of the key.
// A tenant's key "tenant/<T>/<K>" is go-xkms CN=K with TenantID=T, so go-xkms
// keeps tenants apart itself (KeyAttributes.ID() prefixes "<T>:"). Any other
// reference is a platform key: its CN is the reference and its TenantID is
// empty. Nothing is encoded, so a platform name is never read as a tenant key.
func xkmsKeyRef(keyID string) (tenantID, cn string, err error) {
	rest, isTenant := strings.CutPrefix(keyID, tenantKeyRefPrefix)
	if !isTenant {
		if err := validation.ValidateKeyID(keyID); err != nil {
			return "", "", api.NewInvalidInputCauseError("invalid key name "+keyID, err)
		}
		return "", keyID, nil
	}
	tenantID, cn, ok := strings.Cut(rest, "/")
	if !ok || tenantID == "" {
		return "", "", api.NewInvalidInputError("malformed tenant key reference " + keyID)
	}
	if err := validation.ValidateKeyID(cn); err != nil {
		return "", "", api.NewInvalidInputCauseError("invalid key name in "+keyID, err)
	}
	return tenantID, cn, nil
}

// keyIDFromXKMS is the inverse of xkmsKeyRef: the service reference of the
// go-xkms key CN under tenantID.
func keyIDFromXKMS(tenantID, cn string) string {
	if tenantID == "" {
		return cn
	}
	return tenantKeyRefPrefix + tenantID + "/" + cn
}
