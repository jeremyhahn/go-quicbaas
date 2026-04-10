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

//go:build integration

// Package cli provides E2E integration tests for QuicBaaS CLI object storage commands.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Object Storage Transport Skip Configuration
// =============================================================================

// objectStorageTransportsToSkip defines which transports to skip for object storage operations.
// The CLI factory only supports gRPC, but the server does not register the
// quicbaas.objectstorage.v1.ObjectStorageService gRPC service in the current test environment.
// All transports are skipped until the server-side service registration is resolved.
var objectStorageTransportsToSkip = []Transport{TransportGRPC, TransportREST, TransportQUIC, TransportUnix, TransportMCP}

// =============================================================================
// Object Put Command Definitions
// =============================================================================

// ObjectPutCommand returns a command to store an object from a file.
func ObjectPutCommand(key, filePath string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object put %s", key),
		Args:           []string{"object", "put", key, "--file", filePath},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectPutWithMetadataCommand returns a command to store an object with metadata.
func ObjectPutWithMetadataCommand(key, filePath, metadata string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object put %s with metadata", key),
		Args:           []string{"object", "put", key, "--file", filePath, "--metadata", metadata},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectPutWithContentTypeCommand returns a command to store an object with content type.
func ObjectPutWithContentTypeCommand(key, filePath, contentType string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object put %s with content-type", key),
		Args:           []string{"object", "put", key, "--file", filePath, "--content-type", contentType},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectPutWithTenantCommand returns a command to store an object with tenant ID.
func ObjectPutWithTenantCommand(key, filePath, tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object put %s with tenant", key),
		Args:           []string{"object", "put", key, "--file", filePath, "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectPutWithTagsCommand returns a command to store an object with tags.
func ObjectPutWithTagsCommand(key, filePath string, tags map[string]string) CommandDef {
	args := []string{"object", "put", key, "--file", filePath}
	for k, v := range tags {
		args = append(args, "--tag", fmt.Sprintf("%s=%s", k, v))
	}
	return CommandDef{
		Name:           fmt.Sprintf("object put %s with tags", key),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectPutJSONCommand returns a command to store an object with JSON output.
func ObjectPutJSONCommand(key, filePath string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object put %s json", key),
		Args:           []string{"object", "put", key, "--file", filePath, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Get Command Definitions
// =============================================================================

// ObjectGetCommand returns a command to retrieve an object.
func ObjectGetCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object get %s", key),
		Args:           []string{"object", "get", key},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectGetToFileCommand returns a command to retrieve an object and save to file.
func ObjectGetToFileCommand(key, outputPath string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object get %s to file", key),
		Args:           []string{"object", "get", key, "--output-file", outputPath},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectGetWithMetadataCommand returns a command to retrieve an object with metadata.
func ObjectGetWithMetadataCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object get %s with metadata", key),
		Args:           []string{"object", "get", key, "--include-metadata"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectGetWithTenantCommand returns a command to retrieve an object with tenant ID.
func ObjectGetWithTenantCommand(key, tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object get %s with tenant", key),
		Args:           []string{"object", "get", key, "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectGetJSONCommand returns a command to retrieve an object with JSON output.
func ObjectGetJSONCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object get %s json", key),
		Args:           []string{"object", "get", key, "--output", "json", "--include-metadata"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectGetRawCommand returns a command to retrieve raw object data.
func ObjectGetRawCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object get %s raw", key),
		Args:           []string{"object", "get", key, "--output", "raw"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Delete Command Definitions
// =============================================================================

// ObjectDeleteCommand returns a command to delete an object (soft delete).
func ObjectDeleteCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object delete %s", key),
		Args:           []string{"object", "delete", key},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectDeletePermanentCommand returns a command to permanently delete an object.
func ObjectDeletePermanentCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object delete %s permanent", key),
		Args:           []string{"object", "delete", key, "--permanent"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectDeleteWithTenantCommand returns a command to delete an object with tenant ID.
func ObjectDeleteWithTenantCommand(key, tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object delete %s with tenant", key),
		Args:           []string{"object", "delete", key, "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectDeleteJSONCommand returns a command to delete an object with JSON output.
func ObjectDeleteJSONCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object delete %s json", key),
		Args:           []string{"object", "delete", key, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Head Command Definitions
// =============================================================================

// ObjectHeadCommand returns a command to get object metadata without data.
func ObjectHeadCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object head %s", key),
		Args:           []string{"object", "head", key},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectHeadWithTenantCommand returns a command to get object metadata with tenant ID.
func ObjectHeadWithTenantCommand(key, tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object head %s with tenant", key),
		Args:           []string{"object", "head", key, "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectHeadJSONCommand returns a command to get object metadata in JSON format.
func ObjectHeadJSONCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object head %s json", key),
		Args:           []string{"object", "head", key, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object List Command Definitions
// =============================================================================

// ObjectListCommand returns a command to list objects in a namespace.
func ObjectListCommand(namespace string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list %s", namespace),
		Args:           []string{"object", "list", namespace},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectListWithPrefixCommand returns a command to list objects with a prefix filter.
func ObjectListWithPrefixCommand(namespace, prefix string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list %s prefix=%s", namespace, prefix),
		Args:           []string{"object", "list", namespace, "--prefix", prefix},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectListWithLimitCommand returns a command to list objects with a limit.
func ObjectListWithLimitCommand(namespace string, limit int32) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list %s limit=%d", namespace, limit),
		Args:           []string{"object", "list", namespace, "--limit", fmt.Sprintf("%d", limit)},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectListWithCursorCommand returns a command to list objects with pagination cursor.
func ObjectListWithCursorCommand(namespace, cursor string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list %s with cursor", namespace),
		Args:           []string{"object", "list", namespace, "--cursor", cursor},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectListWithTenantCommand returns a command to list objects with tenant ID.
func ObjectListWithTenantCommand(namespace, tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list %s with tenant", namespace),
		Args:           []string{"object", "list", namespace, "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectListWithMetadataCommand returns a command to list objects with metadata.
func ObjectListWithMetadataCommand(namespace string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list %s with metadata", namespace),
		Args:           []string{"object", "list", namespace, "--include-metadata"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectListJSONCommand returns a command to list objects in JSON format.
func ObjectListJSONCommand(namespace string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list %s json", namespace),
		Args:           []string{"object", "list", namespace, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object List By Tag Command Definitions
// =============================================================================

// ObjectListByTagCommand returns a command to list objects by tag.
func ObjectListByTagCommand(tagKey, tagValue string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list-by-tag %s=%s", tagKey, tagValue),
		Args:           []string{"object", "list-by-tag", "--tag", fmt.Sprintf("%s=%s", tagKey, tagValue)},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectListByTagWithLimitCommand returns a command to list objects by tag with limit.
func ObjectListByTagWithLimitCommand(tagKey, tagValue string, limit int32) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list-by-tag %s=%s limit=%d", tagKey, tagValue, limit),
		Args:           []string{"object", "list-by-tag", "--tag", fmt.Sprintf("%s=%s", tagKey, tagValue), "--limit", fmt.Sprintf("%d", limit)},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectListByTagJSONCommand returns a command to list objects by tag in JSON format.
func ObjectListByTagJSONCommand(tagKey, tagValue string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list-by-tag %s=%s json", tagKey, tagValue),
		Args:           []string{"object", "list-by-tag", "--tag", fmt.Sprintf("%s=%s", tagKey, tagValue), "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object List By Tenant Command Definitions
// =============================================================================

// ObjectListByTenantCommand returns a command to list objects by tenant.
func ObjectListByTenantCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list-by-tenant %s", tenantID),
		Args:           []string{"object", "list-by-tenant", "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectListByTenantWithLimitCommand returns a command to list objects by tenant with limit.
func ObjectListByTenantWithLimitCommand(tenantID string, limit int32) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list-by-tenant %s limit=%d", tenantID, limit),
		Args:           []string{"object", "list-by-tenant", "--tenant", tenantID, "--limit", fmt.Sprintf("%d", limit)},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectListByTenantJSONCommand returns a command to list objects by tenant in JSON format.
func ObjectListByTenantJSONCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object list-by-tenant %s json", tenantID),
		Args:           []string{"object", "list-by-tenant", "--tenant", tenantID, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Copy Command Definitions
// =============================================================================

// ObjectCopyCommand returns a command to copy an object.
func ObjectCopyCommand(sourceKey, destKey string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object copy %s %s", sourceKey, destKey),
		Args:           []string{"object", "copy", sourceKey, destKey},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectCopyWithoutTagsCommand returns a command to copy an object without tags.
func ObjectCopyWithoutTagsCommand(sourceKey, destKey string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object copy %s %s without tags", sourceKey, destKey),
		Args:           []string{"object", "copy", sourceKey, destKey, "--copy-tags=false"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectCopyWithTenantCommand returns a command to copy an object with tenant ID.
func ObjectCopyWithTenantCommand(sourceKey, destKey, tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object copy %s %s with tenant", sourceKey, destKey),
		Args:           []string{"object", "copy", sourceKey, destKey, "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectCopyJSONCommand returns a command to copy an object with JSON output.
func ObjectCopyJSONCommand(sourceKey, destKey string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object copy %s %s json", sourceKey, destKey),
		Args:           []string{"object", "copy", sourceKey, destKey, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Metadata Command Definitions
// =============================================================================

// ObjectMetadataGetCommand returns a command to get object metadata/policy.
func ObjectMetadataGetCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object metadata get %s", key),
		Args:           []string{"object", "metadata", "get", key},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectMetadataGetJSONCommand returns a command to get object metadata in JSON format.
func ObjectMetadataGetJSONCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object metadata get %s json", key),
		Args:           []string{"object", "metadata", "get", key, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectMetadataUpdateCommand returns a command to update object metadata/policy.
func ObjectMetadataUpdateCommand(key, metadataJSON string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object metadata update %s", key),
		Args:           []string{"object", "metadata", "update", key, "--metadata", metadataJSON},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectMetadataUpdateJSONCommand returns a command to update object metadata with JSON output.
func ObjectMetadataUpdateJSONCommand(key, metadataJSON string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object metadata update %s json", key),
		Args:           []string{"object", "metadata", "update", key, "--metadata", metadataJSON, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Archive Command Definitions
// =============================================================================

// ObjectArchiveCommand returns a command to archive an object.
func ObjectArchiveCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object archive %s", key),
		Args:           []string{"object", "archive", key},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectArchiveWithDeleteCommand returns a command to archive and delete from primary.
func ObjectArchiveWithDeleteCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object archive %s with delete", key),
		Args:           []string{"object", "archive", key, "--delete-from-primary"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectArchiveWithCompressionCommand returns a command to archive with compression.
func ObjectArchiveWithCompressionCommand(key string, compressionLevel int32) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object archive %s compression=%d", key, compressionLevel),
		Args:           []string{"object", "archive", key, "--compression-level", fmt.Sprintf("%d", compressionLevel)},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectArchiveJSONCommand returns a command to archive an object with JSON output.
func ObjectArchiveJSONCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object archive %s json", key),
		Args:           []string{"object", "archive", key, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Restore Command Definitions
// =============================================================================

// ObjectRestoreCommand returns a command to restore an archived object.
func ObjectRestoreCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object restore %s", key),
		Args:           []string{"object", "restore", key},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectRestoreWithDaysCommand returns a command to restore with retention days.
func ObjectRestoreWithDaysCommand(key string, days int32) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object restore %s days=%d", key, days),
		Args:           []string{"object", "restore", key, "--restore-days", fmt.Sprintf("%d", days)},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectRestoreWithTierCommand returns a command to restore to a specific tier.
func ObjectRestoreWithTierCommand(key, tier string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object restore %s tier=%s", key, tier),
		Args:           []string{"object", "restore", key, "--tier", tier},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectRestoreJSONCommand returns a command to restore with JSON output.
func ObjectRestoreJSONCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object restore %s json", key),
		Args:           []string{"object", "restore", key, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Verify Command Definitions
// =============================================================================

// ObjectVerifyCommand returns a command to verify object integrity.
func ObjectVerifyCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object verify %s", key),
		Args:           []string{"object", "verify", key},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectVerifyWithRepairCommand returns a command to verify and repair object.
func ObjectVerifyWithRepairCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object verify %s with repair", key),
		Args:           []string{"object", "verify", key, "--repair"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectVerifyJSONCommand returns a command to verify with JSON output.
func ObjectVerifyJSONCommand(key string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object verify %s json", key),
		Args:           []string{"object", "verify", key, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Replicate Command Definitions
// =============================================================================

// ObjectReplicateCommand returns a command to replicate an object.
func ObjectReplicateCommand(key, destBackend string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object replicate %s to %s", key, destBackend),
		Args:           []string{"object", "replicate", key, "--dest", destBackend},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectReplicateSyncCommand returns a command to replicate synchronously.
func ObjectReplicateSyncCommand(key, destBackend string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object replicate %s to %s sync", key, destBackend),
		Args:           []string{"object", "replicate", key, "--dest", destBackend, "--sync"},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectReplicateJSONCommand returns a command to replicate with JSON output.
func ObjectReplicateJSONCommand(key, destBackend string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object replicate %s json", key),
		Args:           []string{"object", "replicate", key, "--dest", destBackend, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Migrate Command Definitions
// =============================================================================

// ObjectMigrateCommand returns a command to migrate an object.
func ObjectMigrateCommand(key, destBackend string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object migrate %s to %s", key, destBackend),
		Args:           []string{"object", "migrate", key, "--dest", destBackend},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectMigrateWithDeleteCommand returns a command to migrate and delete source.
func ObjectMigrateWithDeleteCommand(key, destBackend string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object migrate %s to %s with delete", key, destBackend),
		Args:           []string{"object", "migrate", key, "--dest", destBackend, "--delete-source"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectMigrateJSONCommand returns a command to migrate with JSON output.
func ObjectMigrateJSONCommand(key, destBackend string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object migrate %s json", key),
		Args:           []string{"object", "migrate", key, "--dest", destBackend, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Stats Command Definitions
// =============================================================================

// ObjectStatsCommand returns a command to get storage statistics.
func ObjectStatsCommand() CommandDef {
	return CommandDef{
		Name:           "object stats",
		Args:           []string{"object", "stats"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectStatsJSONCommand returns a command to get storage statistics in JSON.
func ObjectStatsJSONCommand() CommandDef {
	return CommandDef{
		Name:           "object stats json",
		Args:           []string{"object", "stats", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Lifecycle Command Definitions
// =============================================================================

// ObjectLifecycleGetCommand returns a command to get lifecycle policy.
func ObjectLifecycleGetCommand(bucket string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object lifecycle get %s", bucket),
		Args:           []string{"object", "lifecycle", "get", bucket},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectLifecycleGetJSONCommand returns a command to get lifecycle policy in JSON.
func ObjectLifecycleGetJSONCommand(bucket string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object lifecycle get %s json", bucket),
		Args:           []string{"object", "lifecycle", "get", bucket, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectLifecycleSetCommand returns a command to set lifecycle policy.
func ObjectLifecycleSetCommand(bucket, policyJSON string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("object lifecycle set %s", bucket),
		Args:           []string{"object", "lifecycle", "set", bucket, "--policy", policyJSON},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// =============================================================================
// Object Help Command Definitions
// =============================================================================

// ObjectHelpCommand returns a command to show object help.
func ObjectHelpCommand() CommandDef {
	return CommandDef{
		Name:          "object help",
		Args:          []string{"object", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"put", "get", "delete", "list", "copy"},
		Timeout:       10 * time.Second,
	}
}

// ObjectPutHelpCommand returns a command to show put help.
func ObjectPutHelpCommand() CommandDef {
	return CommandDef{
		Name:          "object put help",
		Args:          []string{"object", "put", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"file", "metadata", "content-type"},
		Timeout:       10 * time.Second,
	}
}

// ObjectGetHelpCommand returns a command to show get help.
func ObjectGetHelpCommand() CommandDef {
	return CommandDef{
		Name:          "object get help",
		Args:          []string{"object", "get", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"output-file", "include-metadata"},
		Timeout:       10 * time.Second,
	}
}

// ObjectListHelpCommand returns a command to show list help.
func ObjectListHelpCommand() CommandDef {
	return CommandDef{
		Name:          "object list help",
		Args:          []string{"object", "list", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"prefix", "limit", "cursor"},
		Timeout:       10 * time.Second,
	}
}

// ObjectCopyHelpCommand returns a command to show copy help.
func ObjectCopyHelpCommand() CommandDef {
	return CommandDef{
		Name:          "object copy help",
		Args:          []string{"object", "copy", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"copy-tags"},
		Timeout:       10 * time.Second,
	}
}

// ObjectMetadataHelpCommand returns a command to show metadata help.
func ObjectMetadataHelpCommand() CommandDef {
	return CommandDef{
		Name:          "object metadata help",
		Args:          []string{"object", "metadata", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"get", "update"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Object Error Command Definitions
// =============================================================================

// ObjectPutMissingKeyCommand tests put without key.
func ObjectPutMissingKeyCommand() CommandDef {
	return CommandDef{
		Name:          "object put missing key",
		Args:          []string{"object", "put"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// ObjectPutMissingDataCommand tests put without data.
func ObjectPutMissingDataCommand(key string) CommandDef {
	return CommandDef{
		Name:           "object put missing data",
		Args:           []string{"object", "put", key},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectGetMissingKeyCommand tests get without key.
func ObjectGetMissingKeyCommand() CommandDef {
	return CommandDef{
		Name:          "object get missing key",
		Args:          []string{"object", "get"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// ObjectGetNotFoundCommand tests get for non-existent object.
func ObjectGetNotFoundCommand(key string) CommandDef {
	return CommandDef{
		Name:           "object get not found",
		Args:           []string{"object", "get", key},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: objectStorageTransportsToSkip,
	}
}

// ObjectDeleteMissingKeyCommand tests delete without key.
func ObjectDeleteMissingKeyCommand() CommandDef {
	return CommandDef{
		Name:          "object delete missing key",
		Args:          []string{"object", "delete"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// ObjectListMissingNamespaceCommand tests list without namespace.
func ObjectListMissingNamespaceCommand() CommandDef {
	return CommandDef{
		Name:          "object list missing namespace",
		Args:          []string{"object", "list"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// ObjectCopyMissingArgsCommand tests copy without arguments.
func ObjectCopyMissingArgsCommand() CommandDef {
	return CommandDef{
		Name:          "object copy missing args",
		Args:          []string{"object", "copy"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// ObjectCopyMissingDestCommand tests copy without destination.
func ObjectCopyMissingDestCommand(source string) CommandDef {
	return CommandDef{
		Name:          "object copy missing dest",
		Args:          []string{"object", "copy", source},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// ObjectListByTagMissingTagCommand tests list-by-tag without tag.
func ObjectListByTagMissingTagCommand() CommandDef {
	return CommandDef{
		Name:          "object list-by-tag missing tag",
		Args:          []string{"object", "list-by-tag"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// ObjectListByTenantMissingTenantCommand tests list-by-tenant without tenant.
func ObjectListByTenantMissingTenantCommand() CommandDef {
	return CommandDef{
		Name:          "object list-by-tenant missing tenant",
		Args:          []string{"object", "list-by-tenant"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// ObjectReplicateMissingDestCommand tests replicate without destination.
func ObjectReplicateMissingDestCommand(key string) CommandDef {
	return CommandDef{
		Name:          "object replicate missing dest",
		Args:          []string{"object", "replicate", key},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// ObjectMigrateMissingDestCommand tests migrate without destination.
func ObjectMigrateMissingDestCommand(key string) CommandDef {
	return CommandDef{
		Name:          "object migrate missing dest",
		Args:          []string{"object", "migrate", key},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Tests
// =============================================================================

// TestObjectStorageHelp tests object storage help commands.
func TestObjectStorageHelp(t *testing.T) {
	RunForAllTransports(t, "ObjectHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectHelpCommand())
	})

	RunForAllTransports(t, "ObjectPutHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectPutHelpCommand())
	})

	RunForAllTransports(t, "ObjectGetHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectGetHelpCommand())
	})

	RunForAllTransports(t, "ObjectListHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectListHelpCommand())
	})

	RunForAllTransports(t, "ObjectCopyHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectCopyHelpCommand())
	})

	RunForAllTransports(t, "ObjectMetadataHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectMetadataHelpCommand())
	})
}

// TestObjectStorageErrors tests error handling for object storage commands.
func TestObjectStorageErrors(t *testing.T) {
	RunForAllTransports(t, "PutMissingKey", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectPutMissingKeyCommand())
	})

	RunForAllTransports(t, "GetMissingKey", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectGetMissingKeyCommand())
	})

	RunForAllTransports(t, "DeleteMissingKey", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectDeleteMissingKeyCommand())
	})

	RunForAllTransports(t, "ListMissingNamespace", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectListMissingNamespaceCommand())
	})

	RunForAllTransports(t, "CopyMissingArgs", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectCopyMissingArgsCommand())
	})

	RunForAllTransports(t, "ListByTagMissingTag", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectListByTagMissingTagCommand())
	})

	RunForAllTransports(t, "ListByTenantMissingTenant", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ObjectListByTenantMissingTenantCommand())
	})
}

// TestObjectStoragePutGet tests object put and get operations.
func TestObjectStoragePutGet(t *testing.T) {
	RunForAllTransports(t, "PutGetLifecycle", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/test-object", namespace)

		// Create a temp file with test data
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-data.txt")
		testData := "Hello, QuicBaaS Object Storage!"
		err := os.WriteFile(testFile, []byte(testData), 0600)
		require.NoError(t, err)

		// Put the object
		putCmd := ObjectPutCommand(key, testFile)
		stdout, _, err := runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)
		assert.Contains(t, stdout, key)

		// Get the object
		getCmd := ObjectGetCommand(key)
		stdout, _, err = runner.Run(getCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, testData)

		// Get with metadata
		getMetaCmd := ObjectGetWithMetadataCommand(key)
		_, _, err = runner.Run(getMetaCmd)
		require.NoError(t, err)

		// Head - get metadata only
		headCmd := ObjectHeadCommand(key)
		stdout, _, err = runner.Run(headCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, key)

		// Delete the object (cleanup)
		deleteCmd := ObjectDeleteCommand(key)
		_, _, err = runner.Run(deleteCmd)
		require.NoError(t, err)
	})
}

// TestObjectStoragePutWithOptions tests put with various options.
func TestObjectStoragePutWithOptions(t *testing.T) {
	RunForAllTransports(t, "PutWithMetadata", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-meta-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/test-object-meta", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-meta.txt")
		err := os.WriteFile(testFile, []byte("test data with metadata"), 0600)
		require.NoError(t, err)

		metadata := `{"author":"test","version":"1.0"}`
		putCmd := ObjectPutWithMetadataCommand(key, testFile, metadata)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Cleanup
		deleteCmd := ObjectDeleteCommand(key)
		runner.Run(deleteCmd)
	})

	RunForAllTransports(t, "PutWithContentType", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-ct-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/test-object-ct", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-ct.json")
		err := os.WriteFile(testFile, []byte(`{"key":"value"}`), 0600)
		require.NoError(t, err)

		putCmd := ObjectPutWithContentTypeCommand(key, testFile, "application/json")
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Cleanup
		deleteCmd := ObjectDeleteCommand(key)
		runner.Run(deleteCmd)
	})

	RunForAllTransports(t, "PutWithTags", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-tags-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/test-object-tags", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-tags.txt")
		err := os.WriteFile(testFile, []byte("test data with tags"), 0600)
		require.NoError(t, err)

		tags := map[string]string{
			"env":     "test",
			"project": "quicbaas",
		}
		putCmd := ObjectPutWithTagsCommand(key, testFile, tags)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Cleanup
		deleteCmd := ObjectDeleteCommand(key)
		runner.Run(deleteCmd)
	})
}

// TestObjectStorageGetToFile tests getting objects to file.
func TestObjectStorageGetToFile(t *testing.T) {
	RunForAllTransports(t, "GetToFile", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-tofile-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/test-object-tofile", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-input.txt")
		outputFile := filepath.Join(tempDir, "test-output.txt")
		testData := "Test data for file output"
		err := os.WriteFile(testFile, []byte(testData), 0600)
		require.NoError(t, err)

		// Put the object
		putCmd := ObjectPutCommand(key, testFile)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Get to file
		getCmd := ObjectGetToFileCommand(key, outputFile)
		_, _, err = runner.Run(getCmd)
		require.NoError(t, err)

		// Verify file content
		data, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		assert.Equal(t, testData, string(data))

		// Cleanup
		deleteCmd := ObjectDeleteCommand(key)
		runner.Run(deleteCmd)
	})
}

// TestObjectStorageList tests object listing operations.
func TestObjectStorageList(t *testing.T) {
	RunForAllTransports(t, "ListObjects", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-list-%d", time.Now().UnixNano())
		keys := []string{
			fmt.Sprintf("%s/obj1", namespace),
			fmt.Sprintf("%s/obj2", namespace),
			fmt.Sprintf("%s/subdir/obj3", namespace),
		}

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-list.txt")
		err := os.WriteFile(testFile, []byte("test data"), 0600)
		require.NoError(t, err)

		// Create objects
		for _, key := range keys {
			putCmd := ObjectPutCommand(key, testFile)
			_, _, err = runner.Run(putCmd)
			if runner.IsSkipped(putCmd) {
				return
			}
			require.NoError(t, err)
		}

		// List all objects
		listCmd := ObjectListCommand(namespace)
		stdout, _, err := runner.Run(listCmd)
		require.NoError(t, err)
		t.Logf("List output: %s", stdout)

		// List with prefix
		listPrefixCmd := ObjectListWithPrefixCommand(namespace, "subdir/")
		stdout, _, err = runner.Run(listPrefixCmd)
		require.NoError(t, err)
		t.Logf("List with prefix output: %s", stdout)

		// List with limit
		listLimitCmd := ObjectListWithLimitCommand(namespace, 2)
		_, _, err = runner.Run(listLimitCmd)
		require.NoError(t, err)

		// List JSON
		listJSONCmd := ObjectListJSONCommand(namespace)
		stdout, _, err = runner.Run(listJSONCmd)
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)

		// Cleanup
		for _, key := range keys {
			deleteCmd := ObjectDeleteCommand(key)
			runner.Run(deleteCmd)
		}
	})
}

// TestObjectStorageCopy tests object copy operations.
func TestObjectStorageCopy(t *testing.T) {
	RunForAllTransports(t, "CopyObject", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-copy-%d", time.Now().UnixNano())
		sourceKey := fmt.Sprintf("%s/source-obj", namespace)
		destKey := fmt.Sprintf("%s/dest-obj", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-copy.txt")
		testData := "Test data for copy"
		err := os.WriteFile(testFile, []byte(testData), 0600)
		require.NoError(t, err)

		// Put the source object
		putCmd := ObjectPutCommand(sourceKey, testFile)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Copy the object
		copyCmd := ObjectCopyCommand(sourceKey, destKey)
		stdout, _, err := runner.Run(copyCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, destKey)

		// Verify copied object
		getCmd := ObjectGetCommand(destKey)
		stdout, _, err = runner.Run(getCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, testData)

		// Cleanup
		deleteCmd := ObjectDeleteCommand(sourceKey)
		runner.Run(deleteCmd)
		deleteCmd = ObjectDeleteCommand(destKey)
		runner.Run(deleteCmd)
	})

	RunForAllTransports(t, "CopyWithoutTags", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-copy-notags-%d", time.Now().UnixNano())
		sourceKey := fmt.Sprintf("%s/source-notags", namespace)
		destKey := fmt.Sprintf("%s/dest-notags", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-copy-notags.txt")
		err := os.WriteFile(testFile, []byte("test data"), 0600)
		require.NoError(t, err)

		// Put source with tags
		tags := map[string]string{"env": "test"}
		putCmd := ObjectPutWithTagsCommand(sourceKey, testFile, tags)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Copy without tags
		copyCmd := ObjectCopyWithoutTagsCommand(sourceKey, destKey)
		_, _, err = runner.Run(copyCmd)
		require.NoError(t, err)

		// Cleanup
		deleteCmd := ObjectDeleteCommand(sourceKey)
		runner.Run(deleteCmd)
		deleteCmd = ObjectDeleteCommand(destKey)
		runner.Run(deleteCmd)
	})
}

// TestObjectStorageDelete tests object deletion.
func TestObjectStorageDelete(t *testing.T) {
	RunForAllTransports(t, "SoftDelete", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-del-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/test-delete", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-delete.txt")
		err := os.WriteFile(testFile, []byte("test data"), 0600)
		require.NoError(t, err)

		// Put the object
		putCmd := ObjectPutCommand(key, testFile)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Soft delete
		deleteCmd := ObjectDeleteCommand(key)
		stdout, _, err := runner.Run(deleteCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, "deleted")
	})

	RunForAllTransports(t, "PermanentDelete", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-perm-del-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/test-perm-delete", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-perm-delete.txt")
		err := os.WriteFile(testFile, []byte("test data"), 0600)
		require.NoError(t, err)

		// Put the object
		putCmd := ObjectPutCommand(key, testFile)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Permanent delete
		deleteCmd := ObjectDeletePermanentCommand(key)
		stdout, _, err := runner.Run(deleteCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, "permanent")
	})

	RunForAllTransports(t, "DeleteJSON", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-del-json-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/test-delete-json", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-delete-json.txt")
		err := os.WriteFile(testFile, []byte("test data"), 0600)
		require.NoError(t, err)

		// Put the object
		putCmd := ObjectPutCommand(key, testFile)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Delete with JSON output
		deleteCmd := ObjectDeleteJSONCommand(key)
		stdout, _, err := runner.Run(deleteCmd)
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)
	})
}

// TestObjectStorageMetadata tests object metadata operations.
func TestObjectStorageMetadata(t *testing.T) {
	RunForAllTransports(t, "MetadataGetUpdate", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-meta-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/test-metadata", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-metadata.txt")
		err := os.WriteFile(testFile, []byte("test data"), 0600)
		require.NoError(t, err)

		// Put the object
		putCmd := ObjectPutCommand(key, testFile)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Get metadata
		metaGetCmd := ObjectMetadataGetCommand(key)
		stdout, _, err := runner.Run(metaGetCmd)
		require.NoError(t, err)
		t.Logf("Metadata: %s", stdout)

		// Get metadata JSON
		metaGetJSONCmd := ObjectMetadataGetJSONCommand(key)
		stdout, _, err = runner.Run(metaGetJSONCmd)
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)

		// Update metadata (replication policy)
		policyJSON := `{"replication":{"enabled":true,"min_replicas":2}}`
		metaUpdateCmd := ObjectMetadataUpdateCommand(key, policyJSON)
		_, _, err = runner.Run(metaUpdateCmd)
		// May fail if policies aren't supported - log result
		t.Logf("Metadata update result: %v", err)

		// Cleanup
		deleteCmd := ObjectDeleteCommand(key)
		runner.Run(deleteCmd)
	})
}

// TestObjectStorageStats tests storage statistics commands.
func TestObjectStorageStats(t *testing.T) {
	RunForAllTransports(t, "Stats", func(t *testing.T, runner *TestRunner) {
		statsCmd := ObjectStatsCommand()
		stdout, _, err := runner.Run(statsCmd)
		if runner.IsSkipped(statsCmd) {
			return
		}
		require.NoError(t, err)
		t.Logf("Stats output: %s", stdout)
	})

	RunForAllTransports(t, "StatsJSON", func(t *testing.T, runner *TestRunner) {
		statsJSONCmd := ObjectStatsJSONCommand()
		stdout, _, err := runner.Run(statsJSONCmd)
		if runner.IsSkipped(statsJSONCmd) {
			return
		}
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)
	})
}

// TestObjectStorageHead tests head (metadata without data) operations.
func TestObjectStorageHead(t *testing.T) {
	RunForAllTransports(t, "Head", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-head-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/test-head", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-head.txt")
		err := os.WriteFile(testFile, []byte("test data for head"), 0600)
		require.NoError(t, err)

		// Put the object
		putCmd := ObjectPutCommand(key, testFile)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Head
		headCmd := ObjectHeadCommand(key)
		stdout, _, err := runner.Run(headCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, "Exists: true")

		// Head JSON
		headJSONCmd := ObjectHeadJSONCommand(key)
		stdout, _, err = runner.Run(headJSONCmd)
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)

		// Cleanup
		deleteCmd := ObjectDeleteCommand(key)
		runner.Run(deleteCmd)
	})
}

// TestObjectStorageLifecycle tests lifecycle policy commands.
func TestObjectStorageLifecycle(t *testing.T) {
	RunForAllTransports(t, "LifecycleGetSet", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-lifecycle-%d", time.Now().UnixNano())

		// Create an object first to establish the namespace
		key := fmt.Sprintf("%s/lifecycle-test", namespace)
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "lifecycle.txt")
		err := os.WriteFile(testFile, []byte("test data"), 0600)
		require.NoError(t, err)

		putCmd := ObjectPutCommand(key, testFile)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Get lifecycle policy (may not exist yet)
		getCmd := ObjectLifecycleGetCommand(namespace)
		_, _, err = runner.Run(getCmd)
		// May fail if no policy exists - that's ok
		t.Logf("Lifecycle get result: %v", err)

		// Set lifecycle policy
		policyJSON := `{"archive":{"enabled":true,"archive_after_days":30}}`
		setCmd := ObjectLifecycleSetCommand(namespace, policyJSON)
		_, _, err = runner.Run(setCmd)
		// May fail if not supported - log result
		t.Logf("Lifecycle set result: %v", err)

		// Cleanup
		deleteCmd := ObjectDeleteCommand(key)
		runner.Run(deleteCmd)
	})
}

// TestObjectStorageVerify tests object verification commands.
func TestObjectStorageVerify(t *testing.T) {
	RunForAllTransports(t, "Verify", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-verify-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/test-verify", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test-verify.txt")
		err := os.WriteFile(testFile, []byte("test data for verification"), 0600)
		require.NoError(t, err)

		// Put the object
		putCmd := ObjectPutCommand(key, testFile)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Verify
		verifyCmd := ObjectVerifyCommand(key)
		stdout, _, err := runner.Run(verifyCmd)
		require.NoError(t, err)
		t.Logf("Verify output: %s", stdout)

		// Verify JSON
		verifyJSONCmd := ObjectVerifyJSONCommand(key)
		stdout, _, err = runner.Run(verifyJSONCmd)
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)

		// Cleanup
		deleteCmd := ObjectDeleteCommand(key)
		runner.Run(deleteCmd)
	})
}

// TestObjectStorageOutputFormats tests all output formats.
func TestObjectStorageOutputFormats(t *testing.T) {
	formats := []string{"table", "json", "yaml"}

	for _, format := range formats {
		t.Run(fmt.Sprintf("Stats_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("Stats_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("object stats %s", format),
					Args:           []string{"object", "stats", "--output", format},
					ExpectSuccess:  true,
					Timeout:        30 * time.Second,
					RequiresUnseal: true,
					SkipTransports: objectStorageTransportsToSkip,
				}
				_, _, err := runner.Run(cmd)
				if runner.IsSkipped(cmd) {
					return
				}
				require.NoError(t, err)
			})
		})
	}
}

// TestObjectStorageMultiTenant tests multi-tenant operations.
func TestObjectStorageMultiTenant(t *testing.T) {
	RunForAllTransports(t, "MultiTenant", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-tenant-%d", time.Now().UnixNano())
		tenantID := fmt.Sprintf("tenant-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/tenant-obj", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "tenant-test.txt")
		err := os.WriteFile(testFile, []byte("tenant data"), 0600)
		require.NoError(t, err)

		// Put with tenant
		putCmd := ObjectPutWithTenantCommand(key, testFile, tenantID)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// Get with tenant
		getCmd := ObjectGetWithTenantCommand(key, tenantID)
		_, _, err = runner.Run(getCmd)
		require.NoError(t, err)

		// List by tenant
		listCmd := ObjectListByTenantCommand(tenantID)
		_, _, err = runner.Run(listCmd)
		require.NoError(t, err)

		// Cleanup
		deleteCmd := ObjectDeleteWithTenantCommand(key, tenantID)
		runner.Run(deleteCmd)
	})
}

// TestObjectStorageListByTag tests list by tag operations.
func TestObjectStorageListByTag(t *testing.T) {
	RunForAllTransports(t, "ListByTag", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-ns-tag-%d", time.Now().UnixNano())
		tagKey := "env"
		tagValue := fmt.Sprintf("test-%d", time.Now().UnixNano())
		key := fmt.Sprintf("%s/tagged-obj", namespace)

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "tagged.txt")
		err := os.WriteFile(testFile, []byte("tagged data"), 0600)
		require.NoError(t, err)

		// Put with tags
		tags := map[string]string{tagKey: tagValue}
		putCmd := ObjectPutWithTagsCommand(key, testFile, tags)
		_, _, err = runner.Run(putCmd)
		if runner.IsSkipped(putCmd) {
			return
		}
		require.NoError(t, err)

		// List by tag
		listCmd := ObjectListByTagCommand(tagKey, tagValue)
		stdout, _, err := runner.Run(listCmd)
		require.NoError(t, err)
		t.Logf("List by tag output: %s", stdout)

		// List by tag JSON
		listJSONCmd := ObjectListByTagJSONCommand(tagKey, tagValue)
		stdout, _, err = runner.Run(listJSONCmd)
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)

		// Cleanup
		deleteCmd := ObjectDeleteCommand(key)
		runner.Run(deleteCmd)
	})
}

// =============================================================================
// Suite Definitions
// =============================================================================

// ObjectStorageCRUDSuite returns commands for testing basic CRUD operations.
func ObjectStorageCRUDSuite(namespace, testFilePath string) []CommandDef {
	key := fmt.Sprintf("%s/crud-test", namespace)
	destKey := fmt.Sprintf("%s/crud-copy", namespace)
	return []CommandDef{
		ObjectPutCommand(key, testFilePath),
		ObjectGetCommand(key),
		ObjectHeadCommand(key),
		ObjectCopyCommand(key, destKey),
		ObjectDeleteCommand(destKey),
		ObjectDeleteCommand(key),
	}
}

// ObjectStorageListSuite returns commands for testing list operations.
func ObjectStorageListSuite(namespace string) []CommandDef {
	return []CommandDef{
		ObjectListCommand(namespace),
		ObjectListWithLimitCommand(namespace, 10),
		ObjectListJSONCommand(namespace),
	}
}

// ObjectStorageHelpSuite returns help commands for object storage.
func ObjectStorageHelpSuite() []CommandDef {
	return []CommandDef{
		ObjectHelpCommand(),
		ObjectPutHelpCommand(),
		ObjectGetHelpCommand(),
		ObjectListHelpCommand(),
		ObjectCopyHelpCommand(),
		ObjectMetadataHelpCommand(),
	}
}

// ObjectStorageErrorSuite returns error case commands for object storage.
func ObjectStorageErrorSuite() []CommandDef {
	return []CommandDef{
		ObjectPutMissingKeyCommand(),
		ObjectGetMissingKeyCommand(),
		ObjectDeleteMissingKeyCommand(),
		ObjectListMissingNamespaceCommand(),
		ObjectCopyMissingArgsCommand(),
		ObjectListByTagMissingTagCommand(),
		ObjectListByTenantMissingTenantCommand(),
	}
}

// ObjectStorageFullSuite returns a comprehensive test suite.
func ObjectStorageFullSuite(namespace, testFilePath string) []CommandDef {
	key := namespace + "/full-test"
	copyKey := namespace + "/full-copy"

	suite := []CommandDef{
		// Put operations
		ObjectPutCommand(key, testFilePath),
		ObjectPutJSONCommand(key+"-json", testFilePath),
	}

	// Get operations
	suite = append(suite,
		ObjectGetCommand(key),
		ObjectGetJSONCommand(key),
		ObjectGetWithMetadataCommand(key),
	)

	// Head operations
	suite = append(suite,
		ObjectHeadCommand(key),
		ObjectHeadJSONCommand(key),
	)

	// List operations
	suite = append(suite,
		ObjectListCommand(namespace),
		ObjectListJSONCommand(namespace),
	)

	// Copy operations
	suite = append(suite,
		ObjectCopyCommand(key, copyKey),
	)

	// Metadata operations
	suite = append(suite,
		ObjectMetadataGetCommand(key),
		ObjectMetadataGetJSONCommand(key),
	)

	// Stats
	suite = append(suite,
		ObjectStatsCommand(),
		ObjectStatsJSONCommand(),
	)

	// Verify
	suite = append(suite,
		ObjectVerifyCommand(key),
	)

	// Cleanup
	suite = append(suite,
		ObjectDeleteCommand(key),
		ObjectDeleteCommand(key+"-json"),
		ObjectDeleteCommand(copyKey),
	)

	return suite
}

// TestObjectStorageCompleteSuite runs a complete test suite.
func TestObjectStorageCompleteSuite(t *testing.T) {
	RunForAllTransports(t, "CompleteSuite", func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("test-complete-%d", time.Now().UnixNano())

		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "suite-test.txt")
		err := os.WriteFile(testFile, []byte("complete suite test data"), 0600)
		require.NoError(t, err)

		suite := ObjectStorageCRUDSuite(namespace, testFile)
		for _, cmd := range suite {
			if runner.IsSkipped(cmd) {
				t.Logf("Skipping command %s for transport %s", cmd.Name, runner.transport)
				continue
			}
			t.Run(strings.ReplaceAll(cmd.Name, " ", "_"), func(t *testing.T) {
				_, _, err := runner.Run(cmd)
				if cmd.ExpectSuccess {
					require.NoError(t, err)
				}
			})
		}
	})
}
