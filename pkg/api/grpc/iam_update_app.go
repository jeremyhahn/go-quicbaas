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

package grpc

import (
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// updateAppOptionsFromProto maps every field of the request to the IAM update
// options. Proto3 scalars and lists without presence count as set when
// non-empty; optional fields and messages count as set when present. The app
// manager applies or refuses each field, so none is dropped here.
func updateAppOptionsFromProto(req *iampb.UpdateAppRequest) iam.UpdateAppOptions {
	return iam.UpdateAppOptions{
		Name:                    nonEmptyString(req.Name),
		RedirectURIs:            nonEmptyStrings(req.RedirectUris),
		ResourcePrefix:          nonEmptyString(req.ResourcePrefix),
		GrantTypes:              nonEmptyStrings(req.GrantTypes),
		ResponseTypes:           nonEmptyStrings(req.ResponseTypes),
		Scopes:                  nonEmptyStrings(req.Scopes),
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
		MTLSCertThumbprint:      req.MtlsCertThumbprint,
		Description:             req.Description,
		PostLogoutRedirectURIs:  nonEmptyStrings(req.PostLogoutRedirectUris),
		Contacts:                nonEmptyStrings(req.Contacts),
		LogoURI:                 req.LogoUri,
		PolicyURI:               req.PolicyUri,
		TOSURI:                  req.TosUri,
		JWKSURI:                 req.JwksUri,
		JWKS:                    req.Jwks,
		AccessTokenTTL:          durationOrNil(req.AccessTokenTtl),
		RefreshTokenTTL:         durationOrNil(req.RefreshTokenTtl),
		IDTokenTTL:              durationOrNil(req.IdTokenTtl),
		Metadata:                nonEmptyStringMap(req.Metadata),
		Enabled:                 req.Enabled,
	}
}

// nonEmptyString returns a pointer to s, or nil when s is empty.
func nonEmptyString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nonEmptyStrings returns a pointer to list, or nil when list is empty.
func nonEmptyStrings(list []string) *[]string {
	if len(list) == 0 {
		return nil
	}
	return &list
}

// nonEmptyStringMap returns a pointer to m, or nil when m is empty.
func nonEmptyStringMap(m map[string]string) *map[string]string {
	if len(m) == 0 {
		return nil
	}
	return &m
}

// durationOrNil converts a present proto duration, or returns nil.
func durationOrNil(d *durationpb.Duration) *time.Duration {
	if d == nil {
		return nil
	}
	v := d.AsDuration()
	return &v
}
