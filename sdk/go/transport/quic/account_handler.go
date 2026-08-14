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

package quic

import (
	"encoding/json"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// accountServicePrefix is the service name prefix for pooled provider account
// operations.
const accountServicePrefix = "quicbaas.Account"

// isAccountService reports whether svc names the Account service.
func isAccountService(svc string) bool {
	return strings.HasPrefix(svc, accountServicePrefix) || svc == "Account"
}

// The server writes the pkg/service response envelopes verbatim (JSON), so the
// wire shapes below mirror service.*Response exactly. The nested account and
// usage objects reuse the SDK types because orchestration.Account and
// orchestration.UsageReport carry the same snake_case tags.
//
// SECURITY INVARIANT: none of these envelopes has a credential field, because
// no service response type has one.

// accountWireEnvelope is the single-account response envelope.
type accountWireEnvelope struct {
	Account *account.AccountInfo `json:"account"`
}

// accountWireList is the list response envelope.
type accountWireList struct {
	Accounts []*account.AccountInfo `json:"accounts"`
	Total    int                    `json:"total"`
}

// accountWireAck is the acknowledgement envelope for rotate / set-default.
type accountWireAck struct {
	AccountID string `json:"account_id"`
}

// accountWireDelete is the delete response envelope.
type accountWireDelete struct {
	AccountID string `json:"account_id"`
	Deleted   bool   `json:"deleted"`
}

// accountWireUsage is the usage response envelope.
type accountWireUsage struct {
	Usage *account.UsageReport `json:"usage"`
}

// accountResponseConverter turns raw wire bytes into the typed SDK value.
type accountResponseConverter func(data []byte) (interface{}, error)

// accountResponseConverters maps method names to their response converters
// for O(1) dispatch.
var accountResponseConverters = map[string]accountResponseConverter{
	"Create":           convertAccountEnvelope,
	"Get":              convertAccountEnvelope,
	"Update":           convertAccountEnvelope,
	"Revoke":           convertAccountEnvelope,
	"List":             convertAccountList,
	"Delete":           convertAccountDelete,
	"RotateCredential": convertAccountAck,
	"SetDefault":       convertAccountAck,
	"Usage":            convertAccountUsage,
}

// decodeAccountResponse decodes raw wire bytes into the SDK account type for
// method. An unknown method is a programming error on the SDK side: the
// account surface is a closed set, so it fails loudly rather than degrading to
// an untyped map the client would then reject as "invalid response type".
func decodeAccountResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	converter, ok := accountResponseConverters[method]
	if !ok {
		return nil, errors.NewInvalidInput("quic.decodeAccountResponse",
			"unknown account method: "+method)
	}
	return converter(data)
}

func convertAccountEnvelope(data []byte) (interface{}, error) {
	var env accountWireEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertAccountEnvelope", "failed to unmarshal response")
	}
	if env.Account == nil {
		return nil, errors.NewInternal("quic.convertAccountEnvelope", "server returned no account")
	}
	return env.Account, nil
}

func convertAccountList(data []byte) (interface{}, error) {
	var env accountWireList
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertAccountList", "failed to unmarshal response")
	}
	if env.Accounts == nil {
		env.Accounts = []*account.AccountInfo{}
	}
	return &account.ListAccountsResponse{Accounts: env.Accounts, Total: env.Total}, nil
}

func convertAccountDelete(data []byte) (interface{}, error) {
	var env accountWireDelete
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertAccountDelete", "failed to unmarshal response")
	}
	return &account.DeleteAccountResponse{AccountID: env.AccountID, Deleted: env.Deleted}, nil
}

func convertAccountAck(data []byte) (interface{}, error) {
	var env accountWireAck
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertAccountAck", "failed to unmarshal response")
	}
	return &account.AccountAckResponse{AccountID: env.AccountID}, nil
}

func convertAccountUsage(data []byte) (interface{}, error) {
	var env accountWireUsage
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertAccountUsage", "failed to unmarshal response")
	}
	if env.Usage == nil {
		return nil, errors.NewInternal("quic.convertAccountUsage", "server returned no usage report")
	}
	return env.Usage, nil
}
