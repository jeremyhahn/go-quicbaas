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

package quic

import (
	"encoding/json"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// backupServicePrefix is the service name prefix for Backup operations.
const backupServicePrefix = "quicbaas.Backup"

// isBackupService reports whether svc names the QuicBaaS Backup service.
func isBackupService(svc string) bool {
	return strings.HasPrefix(svc, backupServicePrefix) || svc == "Backup"
}

// backupResponseConverter decodes raw wire JSON into a typed SDK value.
type backupResponseConverter func(data []byte) (interface{}, error)

// backupResponseConverters maps Backup method names to their response
// converters for O(1) dispatch, mirroring the orchestrator converter table.
//
// The QUIC server marshals the canonical service.*BackupResponse structs with
// encoding/json. Those structs carry no JSON tags, so the wire field names are
// the Go field names (PascalCase). Unmarshalling the same bytes back into the
// canonical type round-trips exactly, which is what every other transport
// (embedded/gRPC/REST) returns to the caller.
var backupResponseConverters = map[string]backupResponseConverter{
	"Create":  convertCreateBackupResponse,
	"List":    convertListBackupsResponse,
	"Restore": convertRestoreBackupResponse,
	"Delete":  convertDeleteBackupResponse,
	"Verify":  convertVerifyBackupResponse,
}

// decodeBackupResponse decodes raw wire bytes into the canonical SDK backup
// type for the given method. Unknown methods fall back to a generic map decode.
func decodeBackupResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}

	converter, ok := backupResponseConverters[method]
	if !ok {
		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal,
				"quic.decodeBackupResponse", "failed to decode response")
		}
		return decoded, nil
	}

	return converter(data)
}

// convertCreateBackupResponse decodes JSON into a *service.CreateBackupResponse.
func convertCreateBackupResponse(data []byte) (interface{}, error) {
	var resp service.CreateBackupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertCreateBackupResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertListBackupsResponse decodes JSON into a *service.ListBackupsResponse.
func convertListBackupsResponse(data []byte) (interface{}, error) {
	var resp service.ListBackupsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertListBackupsResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertRestoreBackupResponse decodes JSON into a *service.RestoreBackupResponse.
func convertRestoreBackupResponse(data []byte) (interface{}, error) {
	var resp service.RestoreBackupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertRestoreBackupResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertDeleteBackupResponse decodes JSON into a *service.DeleteBackupResponse.
func convertDeleteBackupResponse(data []byte) (interface{}, error) {
	var resp service.DeleteBackupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertDeleteBackupResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertVerifyBackupResponse decodes JSON into a *service.VerifyBackupResponse.
func convertVerifyBackupResponse(data []byte) (interface{}, error) {
	var resp service.VerifyBackupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertVerifyBackupResponse", "failed to unmarshal response")
	}
	return &resp, nil
}
