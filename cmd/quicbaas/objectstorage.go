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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	stdlog "log"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Object storage command flags.
var (
	// Common flags.
	objectOutputFormat string
	objectTimeout      time.Duration

	// TLS flags.
	objectTLSEnabled bool
	objectTLSCert    string
	objectTLSKey     string
	objectTLSCA      string

	// Connection flags.
	objectMaxRetries   int
	objectRetryBackoff time.Duration

	// Put flags.
	objectFile        string
	objectMetadata    string
	objectContentType string
	objectTags        map[string]string

	// Get flags.
	objectOutputPath  string
	objectIncludeMeta bool

	// List flags.
	objectPrefix string
	objectLimit  int32
	objectCursor string

	// List by tag flags.
	objectTagFilter string

	// Copy flags.
	objectCopyTags bool

	// Migrate/Replicate flags.
	objectDestBackend  string
	objectDeleteSource bool
	objectSync         bool

	// Archive flags.
	objectTargetStore       string
	objectDeleteFromPrimary bool
	objectCompressionLevel  int32

	// Restore flags.
	objectRestoreDays int32
	objectRestoreTier string

	// Verify flags.
	objectCheckAll bool
	objectRepair   bool

	// Lifecycle policy flags.
	objectPolicyJSON string
)

// objectStorageClientFactory is a function type for creating object storage clients.
// This allows for easier testing by injecting mock clients.
var objectStorageClientFactory = createObjectStorageClient

// newObjectStorageCommands creates the object storage command group.
func newObjectStorageCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "object",
		Aliases: []string{"obj", "os"},
		Short:   "Object storage commands",
		Long: `Object storage commands for interacting with QuicBaaS distributed object storage.

These commands allow you to store, retrieve, and manage objects with support for
multi-tenancy, replication, archival, and policy-based lifecycle management.
All commands connect to a running QuicBaaS server via gRPC.`,
	}

	// Add persistent flags for all object commands.
	cmd.PersistentFlags().DurationVar(&objectTimeout, "timeout", 30*time.Second, "Request timeout")
	cmd.PersistentFlags().StringVar(&objectOutputFormat, "output", "table", "Output format (json, yaml, table, raw)")

	// TLS flags.
	cmd.PersistentFlags().BoolVar(&objectTLSEnabled, "tls", false, "Enable TLS")
	cmd.PersistentFlags().StringVar(&objectTLSCert, "tls-cert", "", "TLS client certificate file")
	cmd.PersistentFlags().StringVar(&objectTLSKey, "tls-key", "", "TLS client key file")
	cmd.PersistentFlags().StringVar(&objectTLSCA, "tls-ca", "", "TLS CA certificate file")

	// Connection flags.
	cmd.PersistentFlags().IntVar(&objectMaxRetries, "max-retries", 3, "Maximum number of retry attempts")
	cmd.PersistentFlags().DurationVar(&objectRetryBackoff, "retry-backoff", 100*time.Millisecond, "Initial retry backoff duration")

	// Add object operation commands.
	cmd.AddCommand(newObjectPutCommand())
	cmd.AddCommand(newObjectGetCommand())
	cmd.AddCommand(newObjectDeleteCommand())
	cmd.AddCommand(newObjectHeadCommand())
	cmd.AddCommand(newObjectListCommand())
	cmd.AddCommand(newObjectListByTagCommand())
	cmd.AddCommand(newObjectListByTenantCommand())
	cmd.AddCommand(newObjectCopyCommand())
	cmd.AddCommand(newObjectArchiveCommand())
	cmd.AddCommand(newObjectRestoreCommand())
	cmd.AddCommand(newObjectVerifyCommand())
	cmd.AddCommand(newObjectReplicateCommand())
	cmd.AddCommand(newObjectMigrateCommand())

	// Add metadata subcommands.
	metadataCmd := &cobra.Command{
		Use:   "metadata",
		Short: "Manage object metadata and policies",
	}
	metadataCmd.AddCommand(newObjectMetadataGetCommand())
	metadataCmd.AddCommand(newObjectMetadataUpdateCommand())
	cmd.AddCommand(metadataCmd)

	// Add stats command.
	cmd.AddCommand(newObjectStatsCommand())

	// Add lifecycle subcommands.
	lifecycleCmd := &cobra.Command{
		Use:   "lifecycle",
		Short: "Manage lifecycle policies",
	}
	lifecycleCmd.AddCommand(newObjectLifecycleSetCommand())
	lifecycleCmd.AddCommand(newObjectLifecycleGetCommand())
	cmd.AddCommand(lifecycleCmd)

	return cmd
}

// =============================================================================
// Object CRUD Commands
// =============================================================================

// newObjectPutCommand creates the put command.
func newObjectPutCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "put <bucket>/<key>",
		Short: "Store an object",
		Long: `Store an object in the distributed object storage system.

The object key should be in the format bucket/key. Data can be provided
via stdin, file, or command line.

Examples:
  # Store data from a file
  quicbaas object put mybucket/mykey --file /path/to/file.txt

  # Store with metadata
  quicbaas object put mybucket/mykey --file data.json --metadata '{"author":"john","version":"1.0"}'

  # Store with content type
  quicbaas object put mybucket/mykey --file image.png --content-type image/png

  # Store from stdin
  echo "hello world" | quicbaas object put mybucket/mykey`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectPutCommand,
	}

	cmd.Flags().StringVarP(&objectFile, "file", "f", "", "File path to read object data from")
	cmd.Flags().StringVar(&objectMetadata, "metadata", "", "Object metadata as JSON string")
	cmd.Flags().StringVar(&objectContentType, "content-type", "application/octet-stream", "Content type of the object")
	cmd.Flags().StringToStringVar(&objectTags, "tag", nil, "Object tags as key=value pairs (repeatable)")

	return cmd
}

// runObjectPutCommand executes the put command.
func runObjectPutCommand(cmd *cobra.Command, args []string) error {
	key := args[0]

	// Read object data from file or stdin.
	var data []byte
	var err error

	if objectFile != "" {
		data, err = os.ReadFile(objectFile)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "object put", "failed to read file: %s", objectFile)
		}
	} else {
		// Check if stdin has data.
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			data, err = io.ReadAll(os.Stdin)
			if err != nil {
				return errors.Wrapf(err, errors.ErrInvalidInput, "object put", "failed to read stdin")
			}
		} else {
			return errors.Newf(errors.ErrInvalidInput, "object put", "no data provided; use --file or pipe data to stdin")
		}
	}

	// Parse metadata if provided.
	tags := objectTags
	if objectMetadata != "" && tags == nil {
		tags = make(map[string]string)
		if err := json.Unmarshal([]byte(objectMetadata), &tags); err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "object put", "invalid metadata JSON")
		}
	}

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.PutObjectRequest{
		Key:         key,
		Value:       data,
		ContentType: objectContentType,
		TenantId:    globalTenantID,
		Tags:        tags,
	}

	resp, err := client.ObjectStoragePutObject(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectPutResponse(resp)
}

// newObjectGetCommand creates the get command.
func newObjectGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <bucket>/<key>",
		Short: "Retrieve an object",
		Long: `Retrieve an object from the distributed object storage system.

Examples:
  # Get object and print to stdout
  quicbaas object get mybucket/mykey

  # Save object to a file
  quicbaas object get mybucket/mykey --output /path/to/file.txt

  # Get as JSON with metadata
  quicbaas object get mybucket/mykey --output json --include-metadata`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectGetCommand,
	}

	cmd.Flags().StringVarP(&objectOutputPath, "output-file", "o", "", "Output file path (defaults to stdout)")
	cmd.Flags().BoolVar(&objectIncludeMeta, "include-metadata", false, "Include full metadata in response")

	return cmd
}

// runObjectGetCommand executes the get command.
func runObjectGetCommand(cmd *cobra.Command, args []string) error {
	key := args[0]

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.GetObjectRequest{
		Key:             key,
		TenantId:        globalTenantID,
		IncludeMetadata: objectIncludeMeta,
	}

	resp, err := client.ObjectStorageGetObject(ctx, req)
	if err != nil {
		return formatError(err)
	}

	if !resp.Found {
		return errors.Newf(errors.ErrNotFound, "object get", "object not found: %s", key)
	}

	// Write to file if specified, otherwise to stdout.
	if objectOutputPath != "" {
		if err := os.WriteFile(objectOutputPath, resp.Value, 0600); err != nil {
			return errors.Wrapf(err, errors.ErrInternal, "object get", "failed to write to file: %s", objectOutputPath)
		}
		fmt.Printf("Object saved to: %s (%d bytes)\n", objectOutputPath, len(resp.Value))
		return nil
	}

	return formatObjectGetResponse(resp)
}

// newObjectDeleteCommand creates the delete command.
func newObjectDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <bucket>/<key>",
		Short: "Delete an object",
		Long: `Delete an object from the distributed object storage system.

Examples:
  # Delete an object (soft delete by default)
  quicbaas object delete mybucket/mykey

  # Permanently delete an object
  quicbaas object delete mybucket/mykey --permanent`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectDeleteCommand,
	}

	cmd.Flags().BoolVar(&objectDeleteSource, "permanent", false, "Permanently delete (bypass soft delete)")

	return cmd
}

// runObjectDeleteCommand executes the delete command.
func runObjectDeleteCommand(cmd *cobra.Command, args []string) error {
	key := args[0]

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.DeleteObjectRequest{
		Key:       key,
		TenantId:  globalTenantID,
		Permanent: objectDeleteSource,
	}

	resp, err := client.ObjectStorageDeleteObject(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectDeleteResponse(resp)
}

// newObjectHeadCommand creates the head command.
func newObjectHeadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "head <bucket>/<key>",
		Short: "Get object metadata without data",
		Long: `Retrieve object metadata without downloading the object data.

This is efficient for checking if an object exists or querying its properties.

Examples:
  quicbaas object head mybucket/mykey
  quicbaas object head mybucket/mykey --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectHeadCommand,
	}

	return cmd
}

// runObjectHeadCommand executes the head command.
func runObjectHeadCommand(cmd *cobra.Command, args []string) error {
	key := args[0]

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.HeadObjectRequest{
		Key:      key,
		TenantId: globalTenantID,
	}

	resp, err := client.ObjectStorageHeadObject(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectHeadResponse(resp)
}

// =============================================================================
// Object Listing Commands
// =============================================================================

// newObjectListCommand creates the list command.
func newObjectListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <bucket>",
		Short: "List objects in a bucket",
		Long: `List objects in a bucket with optional prefix filtering and pagination.

Examples:
  # List all objects in a bucket
  quicbaas object list mybucket

  # List with prefix filter
  quicbaas object list mybucket --prefix images/

  # List with pagination
  quicbaas object list mybucket --limit 50`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectListCommand,
	}

	cmd.Flags().StringVar(&objectPrefix, "prefix", "", "Filter objects by key prefix")
	cmd.Flags().Int32VarP(&objectLimit, "limit", "n", 100, "Maximum number of objects to return")
	cmd.Flags().StringVar(&objectCursor, "cursor", "", "Pagination cursor from previous response")
	cmd.Flags().BoolVar(&objectIncludeMeta, "include-metadata", false, "Include full metadata for each object")

	return cmd
}

// runObjectListCommand executes the list command.
func runObjectListCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.ListObjectsRequest{
		Namespace:       namespace,
		Prefix:          objectPrefix,
		TenantId:        globalTenantID,
		Limit:           objectLimit,
		Cursor:          objectCursor,
		IncludeMetadata: objectIncludeMeta,
	}

	resp, err := client.ObjectStorageListObjects(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectListResponse(resp)
}

// newObjectListByTagCommand creates the list-by-tag command.
func newObjectListByTagCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-by-tag",
		Short: "List objects by tag",
		Long: `List objects that match a specific tag key-value pair.

Examples:
  quicbaas object list-by-tag --tag type=document
  quicbaas object list-by-tag --tag author=john --limit 50`,
		RunE: runObjectListByTagCommand,
	}

	cmd.Flags().StringVar(&objectTagFilter, "tag", "", "Tag filter as key=value (required)")
	cmd.Flags().Int32VarP(&objectLimit, "limit", "n", 100, "Maximum number of objects to return")
	cmd.Flags().StringVar(&objectCursor, "cursor", "", "Pagination cursor from previous response")

	if err := cmd.MarkFlagRequired("tag"); err != nil {
		stdlog.Fatalf("failed to mark 'tag' flag as required: %v", err)
	}

	return cmd
}

// runObjectListByTagCommand executes the list-by-tag command.
func runObjectListByTagCommand(cmd *cobra.Command, args []string) error {
	// Parse tag filter.
	parts := strings.SplitN(objectTagFilter, "=", 2)
	if len(parts) != 2 {
		return errors.Newf(errors.ErrInvalidInput, "list-by-tag", "invalid tag format; expected key=value")
	}
	tagKey := parts[0]
	tagValue := parts[1]

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.ListByTagRequest{
		Tag:      tagKey,
		Value:    tagValue,
		TenantId: globalTenantID,
		Limit:    objectLimit,
		Cursor:   objectCursor,
	}

	resp, err := client.ObjectStorageListByTag(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectListByTagResponse(resp)
}

// newObjectListByTenantCommand creates the list-by-tenant command.
func newObjectListByTenantCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-by-tenant",
		Short: "List objects by tenant",
		Long: `List all objects belonging to a specific tenant.

Examples:
  quicbaas object list-by-tenant --tenant tenant-123
  quicbaas object list-by-tenant --tenant tenant-123 --limit 100`,
		RunE: runObjectListByTenantCommand,
	}

	cmd.Flags().Int32VarP(&objectLimit, "limit", "n", 100, "Maximum number of objects to return")
	cmd.Flags().StringVar(&objectCursor, "cursor", "", "Pagination cursor from previous response")
	cmd.Flags().BoolVar(&objectIncludeMeta, "include-metadata", false, "Include full metadata for each object")

	return cmd
}

// runObjectListByTenantCommand executes the list-by-tenant command.
func runObjectListByTenantCommand(cmd *cobra.Command, args []string) error {
	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.ListByTenantRequest{
		TenantId:        globalTenantID,
		Limit:           objectLimit,
		Cursor:          objectCursor,
		IncludeMetadata: objectIncludeMeta,
	}

	resp, err := client.ObjectStorageListByTenant(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectListByTenantResponse(resp)
}

// =============================================================================
// Object Operations Commands
// =============================================================================

// newObjectCopyCommand creates the copy command.
func newObjectCopyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "copy <source> <dest>",
		Short: "Copy an object",
		Long: `Copy an object to a new location (server-side copy).

Examples:
  # Copy within same bucket
  quicbaas object copy mybucket/source mybucket/dest

  # Copy across buckets
  quicbaas object copy bucket1/key bucket2/key

  # Copy without tags
  quicbaas object copy mybucket/source mybucket/dest --copy-tags=false`,
		Args: cobra.ExactArgs(2),
		RunE: runObjectCopyCommand,
	}

	cmd.Flags().BoolVar(&objectCopyTags, "copy-tags", true, "Copy tags from source to destination")

	return cmd
}

// runObjectCopyCommand executes the copy command.
func runObjectCopyCommand(cmd *cobra.Command, args []string) error {
	sourceKey := args[0]
	destKey := args[1]

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.CopyObjectRequest{
		SourceKey: sourceKey,
		DestKey:   destKey,
		TenantId:  globalTenantID,
		CopyTags:  objectCopyTags,
	}

	resp, err := client.ObjectStorageCopyObject(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectCopyResponse(resp)
}

// newObjectArchiveCommand creates the archive command.
func newObjectArchiveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "archive <bucket>/<key>",
		Short: "Archive an object to cold storage",
		Long: `Move an object to archive storage (cold tier) for cost optimization.

Examples:
  quicbaas object archive mybucket/mykey
  quicbaas object archive mybucket/mykey --delete-from-primary
  quicbaas object archive mybucket/mykey --compression-level 6`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectArchiveCommand,
	}

	cmd.Flags().StringVar(&objectTargetStore, "target-store", "", "Target archive backend (uses policy default if not specified)")
	cmd.Flags().BoolVar(&objectDeleteFromPrimary, "delete-from-primary", false, "Delete from primary storage after archival")
	cmd.Flags().Int32Var(&objectCompressionLevel, "compression-level", 0, "Compression level (0-9, 0 for default)")

	return cmd
}

// runObjectArchiveCommand executes the archive command.
func runObjectArchiveCommand(cmd *cobra.Command, args []string) error {
	key := args[0]

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.ArchiveObjectRequest{
		Key:               key,
		TargetStore:       objectTargetStore,
		DeleteFromPrimary: objectDeleteFromPrimary,
		CompressionLevel:  objectCompressionLevel,
	}

	resp, err := client.ObjectStorageArchiveObject(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectArchiveResponse(resp)
}

// newObjectRestoreCommand creates the restore command.
func newObjectRestoreCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <bucket>/<key>",
		Short: "Restore an archived object",
		Long: `Restore an object from archive storage to active storage.

Examples:
  quicbaas object restore mybucket/mykey
  quicbaas object restore mybucket/mykey --restore-days 14
  quicbaas object restore mybucket/mykey --tier hot`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectRestoreCommand,
	}

	cmd.Flags().StringVar(&objectDestBackend, "target-backend", "", "Target backend to restore to")
	cmd.Flags().Int32Var(&objectRestoreDays, "restore-days", 7, "Number of days to keep restored copy accessible")
	cmd.Flags().StringVar(&objectRestoreTier, "tier", "", "Restore tier (hot, warm)")

	return cmd
}

// runObjectRestoreCommand executes the restore command.
func runObjectRestoreCommand(cmd *cobra.Command, args []string) error {
	key := args[0]

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	var tier pb.StorageTier
	switch strings.ToLower(objectRestoreTier) {
	case "hot":
		tier = pb.StorageTier_STORAGE_TIER_HOT
	case "warm":
		tier = pb.StorageTier_STORAGE_TIER_WARM
	default:
		tier = pb.StorageTier_STORAGE_TIER_UNSPECIFIED
	}

	req := &pb.RestoreObjectRequest{
		Key:           key,
		TargetBackend: objectDestBackend,
		RestoreDays:   objectRestoreDays,
		Tier:          tier,
	}

	resp, err := client.ObjectStorageRestoreObject(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectRestoreResponse(resp)
}

// newObjectVerifyCommand creates the verify command.
func newObjectVerifyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <bucket>/<key>",
		Short: "Verify object integrity",
		Long: `Verify object integrity across all storage locations.

Checks checksums and replication status to ensure data consistency.

Examples:
  quicbaas object verify mybucket/mykey
  quicbaas object verify mybucket/mykey --repair`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectVerifyCommand,
	}

	cmd.Flags().BoolVar(&objectCheckAll, "check-all", true, "Verify all replica locations")
	cmd.Flags().BoolVar(&objectRepair, "repair", false, "Attempt to repair inconsistent replicas")

	return cmd
}

// runObjectVerifyCommand executes the verify command.
func runObjectVerifyCommand(cmd *cobra.Command, args []string) error {
	key := args[0]

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.VerifyObjectRequest{
		Key:               key,
		CheckAllLocations: objectCheckAll,
		Repair:            objectRepair,
	}

	resp, err := client.ObjectStorageVerifyObject(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectVerifyResponse(resp)
}

// newObjectReplicateCommand creates the replicate command.
func newObjectReplicateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replicate <bucket>/<key>",
		Short: "Replicate an object to another backend",
		Long: `Replicate an object to specified storage backends.

Examples:
  quicbaas object replicate mybucket/mykey --dest s3-backup
  quicbaas object replicate mybucket/mykey --dest gcs-archive --sync`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectReplicateCommand,
	}

	cmd.Flags().StringVar(&objectDestBackend, "dest", "", "Destination backend(s) for replication (required)")
	cmd.Flags().BoolVar(&objectSync, "sync", false, "Wait for replication to complete")

	if err := cmd.MarkFlagRequired("dest"); err != nil {
		stdlog.Fatalf("failed to mark 'dest' flag as required: %v", err)
	}

	return cmd
}

// runObjectReplicateCommand executes the replicate command.
func runObjectReplicateCommand(cmd *cobra.Command, args []string) error {
	key := args[0]

	// Support multiple destinations separated by comma.
	targets := strings.Split(objectDestBackend, ",")
	for i := range targets {
		targets[i] = strings.TrimSpace(targets[i])
	}

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.ReplicateObjectRequest{
		Key:          key,
		TargetStores: targets,
		Sync:         objectSync,
	}

	resp, err := client.ObjectStorageReplicateObject(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectReplicateResponse(resp)
}

// newObjectMigrateCommand creates the migrate command.
func newObjectMigrateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate <bucket>/<key>",
		Short: "Migrate an object to another backend",
		Long: `Migrate an object from one backend to another.

Unlike replication, migration moves the object rather than copying it.

Examples:
  quicbaas object migrate mybucket/mykey --dest s3-archive
  quicbaas object migrate mybucket/mykey --dest gcs-cold --delete-source`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectMigrateCommand,
	}

	cmd.Flags().StringVar(&objectDestBackend, "dest", "", "Destination backend for migration (required)")
	cmd.Flags().BoolVar(&objectDeleteSource, "delete-source", false, "Delete from source after successful migration")

	if err := cmd.MarkFlagRequired("dest"); err != nil {
		stdlog.Fatalf("failed to mark 'dest' flag as required: %v", err)
	}

	return cmd
}

// runObjectMigrateCommand executes the migrate command.
func runObjectMigrateCommand(cmd *cobra.Command, args []string) error {
	key := args[0]

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.MigrateObjectRequest{
		Key:           key,
		TargetBackend: objectDestBackend,
		DeleteSource:  objectDeleteSource,
		Verify:        true,
	}

	resp, err := client.ObjectStorageMigrateObject(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectMigrateResponse(resp)
}

// =============================================================================
// Metadata Commands
// =============================================================================

// newObjectMetadataGetCommand creates the metadata get command.
func newObjectMetadataGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <bucket>/<key>",
		Short: "Get object policies and metadata",
		Long: `Retrieve the effective policies for an object including replication,
archive, and retention policies.

Examples:
  quicbaas object metadata get mybucket/mykey
  quicbaas object metadata get mybucket/mykey --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectMetadataGetCommand,
	}

	return cmd
}

// runObjectMetadataGetCommand executes the metadata get command.
func runObjectMetadataGetCommand(cmd *cobra.Command, args []string) error {
	key := args[0]

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.GetObjectPolicyRequest{
		Key:              key,
		IncludeInherited: true,
	}

	resp, err := client.ObjectStorageGetObjectPolicy(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectPolicyResponse(resp)
}

// newObjectMetadataUpdateCommand creates the metadata update command.
func newObjectMetadataUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <bucket>/<key>",
		Short: "Update object policies",
		Long: `Update the policies for an object.

Examples:
  quicbaas object metadata update mybucket/mykey --metadata '{"replication":{"enabled":true,"min_replicas":3}}'`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectMetadataUpdateCommand,
	}

	cmd.Flags().StringVar(&objectMetadata, "metadata", "", "Policy configuration as JSON string (required)")

	if err := cmd.MarkFlagRequired("metadata"); err != nil {
		stdlog.Fatalf("failed to mark 'metadata' flag as required: %v", err)
	}

	return cmd
}

// runObjectMetadataUpdateCommand executes the metadata update command.
func runObjectMetadataUpdateCommand(cmd *cobra.Command, args []string) error {
	key := args[0]

	// Parse policy from JSON.
	var policyUpdate struct {
		Replication *pb.ReplicationPolicy `json:"replication,omitempty"`
		Archive     *pb.ArchivePolicy     `json:"archive,omitempty"`
		Retention   *pb.RetentionPolicy   `json:"retention,omitempty"`
	}

	if err := json.Unmarshal([]byte(objectMetadata), &policyUpdate); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "metadata update", "invalid policy JSON")
	}

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.SetObjectPolicyRequest{
		Key:               key,
		ReplicationPolicy: policyUpdate.Replication,
		ArchivePolicy:     policyUpdate.Archive,
		RetentionPolicy:   policyUpdate.Retention,
	}

	resp, err := client.ObjectStorageSetObjectPolicy(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectSetPolicyResponse(resp)
}

// =============================================================================
// Stats Command
// =============================================================================

// newObjectStatsCommand creates the stats command.
func newObjectStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Get storage statistics",
		Long: `Retrieve cluster-wide storage statistics including total storage,
object counts, and backend health.

Examples:
  quicbaas object stats
  quicbaas object stats --output json`,
		RunE: runObjectStatsCommand,
	}

	return cmd
}

// runObjectStatsCommand executes the stats command.
func runObjectStatsCommand(cmd *cobra.Command, args []string) error {
	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.ListBackendsRequest{
		IncludeHealth: true,
	}

	resp, err := client.ObjectStorageListBackends(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectStatsResponse(resp)
}

// =============================================================================
// Lifecycle Commands
// =============================================================================

// newObjectLifecycleSetCommand creates the lifecycle set command.
func newObjectLifecycleSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <bucket>",
		Short: "Set lifecycle policy for a bucket",
		Long: `Set lifecycle policies for a bucket including archival and retention rules.

Examples:
  quicbaas object lifecycle set mybucket --policy '{"archive":{"enabled":true,"archive_after_days":30}}'`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectLifecycleSetCommand,
	}

	cmd.Flags().StringVar(&objectPolicyJSON, "policy", "", "Lifecycle policy as JSON string (required)")

	if err := cmd.MarkFlagRequired("policy"); err != nil {
		stdlog.Fatalf("failed to mark 'policy' flag as required: %v", err)
	}

	return cmd
}

// runObjectLifecycleSetCommand executes the lifecycle set command.
func runObjectLifecycleSetCommand(cmd *cobra.Command, args []string) error {
	bucket := args[0]

	// Parse policy from JSON.
	var policy struct {
		Archive   *pb.ArchivePolicy   `json:"archive,omitempty"`
		Retention *pb.RetentionPolicy `json:"retention,omitempty"`
	}

	if err := json.Unmarshal([]byte(objectPolicyJSON), &policy); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "lifecycle set", "invalid policy JSON")
	}

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	// Set policy on the bucket (namespace) level.
	req := &pb.SetObjectPolicyRequest{
		Key:             bucket,
		ArchivePolicy:   policy.Archive,
		RetentionPolicy: policy.Retention,
	}

	resp, err := client.ObjectStorageSetObjectPolicy(ctx, req)
	if err != nil {
		return formatError(err)
	}

	fmt.Printf("Lifecycle policy set for bucket: %s (version %d)\n", bucket, resp.Version)
	return nil
}

// newObjectLifecycleGetCommand creates the lifecycle get command.
func newObjectLifecycleGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <bucket>",
		Short: "Get lifecycle policy for a bucket",
		Long: `Retrieve lifecycle policies for a bucket.

Examples:
  quicbaas object lifecycle get mybucket
  quicbaas object lifecycle get mybucket --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runObjectLifecycleGetCommand,
	}

	return cmd
}

// runObjectLifecycleGetCommand executes the lifecycle get command.
func runObjectLifecycleGetCommand(cmd *cobra.Command, args []string) error {
	bucket := args[0]

	client, err := objectStorageClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), objectTimeout)
	defer cancel()

	req := &pb.GetObjectPolicyRequest{
		Key:              bucket,
		IncludeInherited: false,
	}

	resp, err := client.ObjectStorageGetObjectPolicy(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatObjectLifecycleResponse(bucket, resp)
}

// =============================================================================
// Helper Functions
// =============================================================================

// createObjectStorageClient creates an object storage client based on transport.
// Object storage does not currently support direct transport-based client creation.
// All transports return an UnsupportedTransportError. Use objectStorageClientFactory
// to inject a client for production or testing.
func createObjectStorageClient() (*grpc.Client, error) {
	return nil, &UnsupportedTransportError{Transport: transport}
}

// createObjectStorageGRPCClient creates a gRPC client for object storage operations.
func createObjectStorageGRPCClient() (*grpc.Client, error) {
	// Create logger for client.
	logConfig := logger.LogConfig{
		Level:  "warn",
		Format: "json",
		Output: "stderr",
	}

	log, err := logger.New(logConfig)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrConfiguration,
			"createObjectStorageGRPCClient",
			"failed to create logger",
		)
	}

	// Build client options.
	opts := []grpc.ClientOption{
		grpc.WithLogger(log),
		grpc.WithTimeout(objectTimeout),
		grpc.WithRetry(objectMaxRetries, objectRetryBackoff),
		grpc.WithMaxRetryBackoff(5 * time.Second),
	}

	// Add TLS configuration if enabled.
	switch {
	case objectTLSEnabled && objectTLSCert != "" && objectTLSKey != "":
		opts = append(opts, grpc.WithTLS(objectTLSCert, objectTLSKey, objectTLSCA))
	case objectTLSEnabled && objectTLSCA != "":
		opts = append(opts, grpc.WithTLS("", "", objectTLSCA))
	case tlsSkipVerify:
		opts = append(opts, grpc.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // G402: user explicitly requested --tls-skip-verify
			MinVersion:         tls.VersionTLS13,
		}))
	default:
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createObjectStorageGRPCClient",
			"TLS is required for gRPC connections (use --tls-cert, --tls-key, and --tls-ca, or --tls-skip-verify flags)",
		)
	}

	client, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrUnavailable,
			"createObjectStorageGRPCClient",
			"failed to connect to server at %s", address,
		)
	}

	return client, nil
}

// =============================================================================
// Output Formatting Functions
// =============================================================================

// formatObjectPutResponse formats the put response.
func formatObjectPutResponse(resp *pb.PutObjectResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case cli.OutputFormatYAML:
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Object stored successfully\n")
		fmt.Printf("Key: %s\n", resp.Key)
		fmt.Printf("Size: %d bytes\n", resp.Size)
		fmt.Printf("Version: %d\n", resp.Version)
		fmt.Printf("Checksum: %s\n", hex.EncodeToString(resp.Checksum))
		if resp.CreatedAt != nil {
			fmt.Printf("Created: %s\n", resp.CreatedAt.AsTime().Format(time.RFC3339))
		}
		if len(resp.ReplicatedTo) > 0 {
			fmt.Printf("Replicated to: %s\n", strings.Join(resp.ReplicatedTo, ", "))
		}
	}
	return nil
}

// formatObjectGetResponse formats the get response.
func formatObjectGetResponse(resp *pb.GetObjectResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		// For JSON output, include metadata but base64 encode the value.
		output := map[string]interface{}{
			"key":          resp.Key,
			"found":        resp.Found,
			"size":         resp.Size,
			"version":      resp.Version,
			"content_type": resp.ContentType,
			"checksum":     hex.EncodeToString(resp.Checksum),
			"tags":         resp.Tags,
		}
		if objectIncludeMeta && resp.Metadata != nil {
			output["metadata"] = resp.Metadata
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case cli.OutputFormatYAML:
		output := map[string]interface{}{
			"key":          resp.Key,
			"found":        resp.Found,
			"size":         resp.Size,
			"version":      resp.Version,
			"content_type": resp.ContentType,
			"tags":         resp.Tags,
		}
		data, err := yaml.Marshal(output)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case cli.OutputFormatRaw:
		// Output raw data to stdout.
		os.Stdout.Write(resp.Value)
	default:
		// Default to raw output for object data.
		os.Stdout.Write(resp.Value)
	}
	return nil
}

// formatObjectDeleteResponse formats the delete response.
func formatObjectDeleteResponse(resp *pb.DeleteObjectResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case cli.OutputFormatYAML:
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if resp.Deleted {
			fmt.Printf("Object deleted successfully: %s\n", resp.Key)
			if resp.Permanent {
				fmt.Println("Deletion type: permanent")
			} else {
				fmt.Println("Deletion type: soft delete (recoverable)")
			}
			if resp.DeletedAt != nil {
				fmt.Printf("Deleted at: %s\n", resp.DeletedAt.AsTime().Format(time.RFC3339))
			}
		} else {
			fmt.Printf("Object not found: %s\n", resp.Key)
		}
	}
	return nil
}

// formatObjectHeadResponse formats the head response.
func formatObjectHeadResponse(resp *pb.HeadObjectResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case cli.OutputFormatYAML:
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if !resp.Exists {
			fmt.Printf("Object not found: %s\n", resp.Key)
			return nil
		}
		fmt.Printf("Key: %s\n", resp.Key)
		fmt.Printf("Exists: %v\n", resp.Exists)
		fmt.Printf("Size: %d bytes\n", resp.Size)
		fmt.Printf("Content-Type: %s\n", resp.ContentType)
		fmt.Printf("Version: %d\n", resp.Version)
		fmt.Printf("Checksum: %s\n", hex.EncodeToString(resp.Checksum))
		fmt.Printf("Status: %s\n", resp.Status.String())
		fmt.Printf("Tier: %s\n", resp.Tier.String())
		if resp.ModifiedAt != nil {
			fmt.Printf("Modified: %s\n", resp.ModifiedAt.AsTime().Format(time.RFC3339))
		}
		if len(resp.Tags) > 0 {
			fmt.Println("\nTags:")
			for k, v := range resp.Tags {
				fmt.Printf("  %s: %s\n", k, v)
			}
		}
	}
	return nil
}

// formatObjectListResponse formats the list response.
func formatObjectListResponse(resp *pb.ListObjectsResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case cli.OutputFormatYAML:
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Objects (Total: %d):\n", resp.Total)
		fmt.Println("KEY                                      SIZE         VERSION    MODIFIED")
		fmt.Println("-------------------------------------------------------------------------")
		for _, obj := range resp.Objects {
			modified := ""
			if obj.ModifiedAt != nil {
				modified = obj.ModifiedAt.AsTime().Format("2006-01-02 15:04")
			}
			fmt.Printf("%-40s %-12d %-10d %s\n",
				objectTruncateString(obj.Key, 40),
				obj.Size,
				obj.Version,
				modified)
		}
		if resp.NextCursor != "" {
			fmt.Printf("\nNext cursor: %s\n", resp.NextCursor)
		}
		if resp.HasMore {
			fmt.Println("(more results available)")
		}
	}
	return nil
}

// formatObjectListByTagResponse formats the list-by-tag response.
func formatObjectListByTagResponse(resp *pb.ListByTagResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case cli.OutputFormatYAML:
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Objects matching tag (Total: %d):\n", resp.Total)
		for _, key := range resp.Keys {
			fmt.Println(key)
		}
		if resp.NextCursor != "" {
			fmt.Printf("\nNext cursor: %s\n", resp.NextCursor)
		}
		if resp.HasMore {
			fmt.Println("(more results available)")
		}
	}
	return nil
}

// formatObjectListByTenantResponse formats the list-by-tenant response.
func formatObjectListByTenantResponse(resp *pb.ListByTenantResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case cli.OutputFormatYAML:
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Objects for tenant (Total: %d):\n", resp.Total)
		if len(resp.Objects) > 0 {
			fmt.Println("KEY                                      SIZE         VERSION")
			fmt.Println("--------------------------------------------------------------")
			for _, obj := range resp.Objects {
				fmt.Printf("%-40s %-12d %d\n",
					objectTruncateString(obj.Key, 40),
					obj.Size,
					obj.Version)
			}
		} else {
			for _, key := range resp.Keys {
				fmt.Println(key)
			}
		}
		if resp.NextCursor != "" {
			fmt.Printf("\nNext cursor: %s\n", resp.NextCursor)
		}
		if resp.HasMore {
			fmt.Println("(more results available)")
		}
	}
	return nil
}

// formatObjectCopyResponse formats the copy response.
func formatObjectCopyResponse(resp *pb.CopyObjectResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Object copied successfully\n")
		fmt.Printf("Destination: %s\n", resp.Key)
		fmt.Printf("Version: %d\n", resp.Version)
		fmt.Printf("Size: %d bytes\n", resp.Size)
		if resp.CreatedAt != nil {
			fmt.Printf("Created: %s\n", resp.CreatedAt.AsTime().Format(time.RFC3339))
		}
	}
	return nil
}

// formatObjectArchiveResponse formats the archive response.
func formatObjectArchiveResponse(resp *pb.ArchiveObjectResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if resp.Success {
			fmt.Printf("Object archived successfully\n")
			fmt.Printf("Key: %s\n", resp.Key)
			fmt.Printf("Archive store: %s\n", resp.ArchiveStore)
			fmt.Printf("Original size: %d bytes\n", resp.OriginalSize)
			fmt.Printf("Archived size: %d bytes\n", resp.ArchivedSize)
			if resp.ArchivedAt != nil {
				fmt.Printf("Archived at: %s\n", resp.ArchivedAt.AsTime().Format(time.RFC3339))
			}
		} else {
			fmt.Printf("Failed to archive object: %s\n", resp.Key)
		}
	}
	return nil
}

// formatObjectRestoreResponse formats the restore response.
func formatObjectRestoreResponse(resp *pb.RestoreObjectResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if resp.Success {
			fmt.Printf("Object restore initiated\n")
			fmt.Printf("Key: %s\n", resp.Key)
			fmt.Printf("Restored to: %s\n", resp.RestoredTo)
			if resp.OperationId != "" {
				fmt.Printf("Operation ID: %s\n", resp.OperationId)
			}
			if resp.EstimatedCompletion != nil {
				fmt.Printf("Estimated completion: %s\n", resp.EstimatedCompletion.AsTime().Format(time.RFC3339))
			}
		} else {
			fmt.Printf("Failed to initiate restore: %s\n", resp.Key)
		}
	}
	return nil
}

// formatObjectVerifyResponse formats the verify response.
func formatObjectVerifyResponse(resp *pb.VerifyObjectResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Object verification: %s\n", resp.Key)
		if resp.Valid {
			fmt.Println("Status: VALID")
		} else {
			fmt.Println("Status: INVALID")
		}
		if resp.Repaired {
			fmt.Println("Repair: attempted")
		}
		if len(resp.Locations) > 0 {
			fmt.Println("\nLocation verification:")
			for _, loc := range resp.Locations {
				status := "OK"
				if !loc.Valid {
					status = fmt.Sprintf("ERROR: %s", loc.Error)
				}
				fmt.Printf("  %s: %s (size: %d)\n", loc.Store, status, loc.Size)
			}
		}
	}
	return nil
}

// formatObjectReplicateResponse formats the replicate response.
func formatObjectReplicateResponse(resp *pb.ReplicateObjectResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Replication status for: %s\n", resp.Key)
		if resp.Success {
			fmt.Println("Status: SUCCESS")
		} else {
			fmt.Println("Status: PARTIAL/FAILED")
		}
		if len(resp.ReplicatedTo) > 0 {
			fmt.Printf("Replicated to: %s\n", strings.Join(resp.ReplicatedTo, ", "))
		}
		if len(resp.FailedStores) > 0 {
			fmt.Printf("Failed stores: %s\n", strings.Join(resp.FailedStores, ", "))
		}
		if resp.OperationId != "" {
			fmt.Printf("Operation ID: %s\n", resp.OperationId)
		}
	}
	return nil
}

// formatObjectMigrateResponse formats the migrate response.
func formatObjectMigrateResponse(resp *pb.MigrateObjectResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Migration status for: %s\n", resp.Key)
		if resp.Success {
			fmt.Println("Status: SUCCESS")
		} else {
			fmt.Println("Status: FAILED")
		}
		fmt.Printf("From: %s\n", resp.OldBackend)
		fmt.Printf("To: %s\n", resp.NewBackend)
		if resp.SourceDeleted {
			fmt.Println("Source deleted: yes")
		}
		if resp.Verified {
			fmt.Println("Verified: yes")
		}
	}
	return nil
}

// formatObjectPolicyResponse formats the policy get response.
func formatObjectPolicyResponse(resp *pb.GetObjectPolicyResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case cli.OutputFormatYAML:
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Policies for: %s\n", resp.Key)
		if resp.Policy != nil {
			fmt.Println("\nEffective Policy:")
			printObjectPolicy(resp.Policy)
		}
		if resp.ExplicitPolicy != nil {
			fmt.Println("\nExplicit (Object-level) Policy:")
			printObjectPolicy(resp.ExplicitPolicy)
		}
		if resp.InheritedPolicy != nil {
			fmt.Println("\nInherited (Namespace-level) Policy:")
			printObjectPolicy(resp.InheritedPolicy)
		}
	}
	return nil
}

// formatObjectSetPolicyResponse formats the set policy response.
func formatObjectSetPolicyResponse(resp *pb.SetObjectPolicyResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Policy updated for: %s\n", resp.Key)
		fmt.Printf("Version: %d\n", resp.Version)
		if resp.UpdatedAt != nil {
			fmt.Printf("Updated at: %s\n", resp.UpdatedAt.AsTime().Format(time.RFC3339))
		}
	}
	return nil
}

// formatObjectStatsResponse formats the stats response.
func formatObjectStatsResponse(resp *pb.ListBackendsResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case cli.OutputFormatYAML:
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Storage Backends: %d\n", resp.Total)
		fmt.Println("NAME                 TYPE       TIER       ENABLED  HEALTHY  USED        CAPACITY")
		fmt.Println("---------------------------------------------------------------------------------")

		var totalUsed, totalCapacity int64
		for _, backend := range resp.Backends {
			var healthStr string
			if backend.Healthy {
				healthStr = "YES"
			} else {
				healthStr = "NO"
			}

			usedStr := objectFormatStorageSize(backend.UsedBytes)
			capStr := objectFormatStorageSize(backend.CapacityBytes)

			fmt.Printf("%-20s %-10s %-10s %-8v %-8s %-11s %s\n",
				objectTruncateString(backend.Name, 20),
				backend.Type.String(),
				backend.Tier.String(),
				backend.Enabled,
				healthStr,
				usedStr,
				capStr)

			totalUsed += backend.UsedBytes
			totalCapacity += backend.CapacityBytes
		}

		fmt.Println("---------------------------------------------------------------------------------")
		fmt.Printf("Total: %s / %s\n", objectFormatStorageSize(totalUsed), objectFormatStorageSize(totalCapacity))
	}
	return nil
}

// formatObjectLifecycleResponse formats the lifecycle get response.
func formatObjectLifecycleResponse(bucket string, resp *pb.GetObjectPolicyResponse) error {
	format, err := cli.ParseOutputFormat(objectOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case cli.OutputFormatYAML:
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Lifecycle policy for bucket: %s\n", bucket)
		if resp.Policy == nil {
			fmt.Println("No lifecycle policy configured")
			return nil
		}
		printObjectPolicy(resp.Policy)
	}
	return nil
}

// printObjectPolicy prints an object policy.
func printObjectPolicy(policy *pb.ObjectPolicy) {
	if policy.ReplicationPolicy != nil {
		rp := policy.ReplicationPolicy
		fmt.Println("  Replication:")
		fmt.Printf("    Enabled: %v\n", rp.Enabled)
		fmt.Printf("    Min replicas: %d\n", rp.MinReplicas)
		fmt.Printf("    Mode: %s\n", rp.Mode.String())
		if len(rp.TargetStores) > 0 {
			fmt.Printf("    Target stores: %s\n", strings.Join(rp.TargetStores, ", "))
		}
	}
	if policy.ArchivePolicy != nil {
		ap := policy.ArchivePolicy
		fmt.Println("  Archive:")
		fmt.Printf("    Enabled: %v\n", ap.Enabled)
		fmt.Printf("    Archive after days: %d\n", ap.ArchiveAfterDays)
		fmt.Printf("    Target store: %s\n", ap.TargetStore)
		fmt.Printf("    Delete from primary: %v\n", ap.DeleteFromPrimary)
	}
	if policy.RetentionPolicy != nil {
		ret := policy.RetentionPolicy
		fmt.Println("  Retention:")
		fmt.Printf("    Enabled: %v\n", ret.Enabled)
		fmt.Printf("    Retention days: %d\n", ret.RetentionDays)
		fmt.Printf("    Max versions: %d\n", ret.MaxVersions)
		fmt.Printf("    Legal hold: %v\n", ret.LegalHold)
	}
}

// objectTruncateString truncates a string to the specified length.
func objectTruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// objectFormatStorageSize formats bytes into human-readable storage size.
func objectFormatStorageSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
