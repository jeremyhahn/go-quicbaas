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

	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isSSHService reports whether svc names the SSH service.
func isSSHService(svc string) bool {
	return strings.HasPrefix(svc, sshclient.ServiceName) || svc == "SSH"
}

// The server writes the sdk/go/client/ssh wire envelopes verbatim (JSON), so
// each converter below simply unmarshals into the matching SDK type. No
// per-transport shape translation is needed — that is the point of having one
// envelope definition shared by every transport.

// sshResponseConverter turns raw wire bytes into the typed SDK value.
type sshResponseConverter func(data []byte) (interface{}, error)

// sshResponseConverters maps method names to their response converters for
// O(1) dispatch.
var sshResponseConverters = map[string]sshResponseConverter{
	sshclient.MethodGenerateKey:         convertSSHKey,
	sshclient.MethodImportKey:           convertSSHKey,
	sshclient.MethodGetKey:              convertSSHKey,
	sshclient.MethodListKeys:            convertSSHKeyList,
	sshclient.MethodDeleteKey:           convertSSHAck,
	sshclient.MethodExportPublicKey:     convertSSHPublicKey,
	sshclient.MethodSignDigest:          convertSSHSignature,
	sshclient.MethodAddKnownHost:        convertSSHAck,
	sshclient.MethodListKnownHosts:      convertSSHKnownHostList,
	sshclient.MethodRemoveKnownHost:     convertSSHAck,
	sshclient.MethodAddAuthorizedKey:    convertSSHAck,
	sshclient.MethodListAuthorizedKeys:  convertSSHAuthorizedList,
	sshclient.MethodRemoveAuthorizedKey: convertSSHAck,
	sshclient.MethodEmitAuthorizedKey:   convertSSHEmit,
}

// decodeSSHResponse decodes raw wire bytes into the SDK type for method. An
// unknown method is a programming error on the SDK side: the SSH surface is a
// closed set, so it fails loudly rather than degrading to an untyped map the
// client would reject as "invalid response type".
func decodeSSHResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	converter, ok := sshResponseConverters[method]
	if !ok {
		return nil, errors.NewInvalidInput("quic.decodeSSHResponse",
			"unknown ssh method: "+method)
	}
	return converter(data)
}

func convertSSHKey(data []byte) (interface{}, error) {
	var out sshclient.Key
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertSSHKey", "failed to unmarshal response")
	}
	return &out, nil
}

func convertSSHKeyList(data []byte) (interface{}, error) {
	var out sshclient.ListKeysResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertSSHKeyList", "failed to unmarshal response")
	}
	if out.Items == nil {
		out.Items = []*sshclient.Key{}
	}
	return &out, nil
}

func convertSSHKnownHostList(data []byte) (interface{}, error) {
	var out sshclient.ListKnownHostsResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertSSHKnownHostList", "failed to unmarshal response")
	}
	if out.Items == nil {
		out.Items = []*sshclient.Pin{}
	}
	return &out, nil
}

func convertSSHAuthorizedList(data []byte) (interface{}, error) {
	var out sshclient.ListAuthorizedKeysResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertSSHAuthorizedList", "failed to unmarshal response")
	}
	if out.Items == nil {
		out.Items = []*sshclient.AuthorizedEntry{}
	}
	return &out, nil
}

func convertSSHPublicKey(data []byte) (interface{}, error) {
	var out sshclient.PublicKey
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertSSHPublicKey", "failed to unmarshal response")
	}
	return &out, nil
}

func convertSSHSignature(data []byte) (interface{}, error) {
	var out sshclient.SignResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertSSHSignature", "failed to unmarshal response")
	}
	return &out, nil
}

func convertSSHEmit(data []byte) (interface{}, error) {
	var out sshclient.EmitResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertSSHEmit", "failed to unmarshal response")
	}
	return &out, nil
}

func convertSSHAck(data []byte) (interface{}, error) {
	var out sshclient.AckResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertSSHAck", "failed to unmarshal response")
	}
	return &out, nil
}
