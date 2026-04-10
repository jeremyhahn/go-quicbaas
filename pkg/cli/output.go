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

// Package cli provides output formatting utilities for the QuicBaaS CLI.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"
)

// OutputFormat defines the output format type.
type OutputFormat string

const (
	// OutputFormatJSON outputs data as JSON.
	OutputFormatJSON OutputFormat = "json"
	// OutputFormatYAML outputs data as YAML.
	OutputFormatYAML OutputFormat = "yaml"
	// OutputFormatTable outputs data as a formatted table.
	OutputFormatTable OutputFormat = "table"
	// OutputFormatRaw outputs raw data without formatting.
	OutputFormatRaw OutputFormat = "raw"
)

// outputFormatParseMap provides O(1) lookup for parsing output format strings.
var outputFormatParseMap = map[string]OutputFormat{
	"json":  OutputFormatJSON,
	"yaml":  OutputFormatYAML,
	"yml":   OutputFormatYAML,
	"table": OutputFormatTable,
	"raw":   OutputFormatRaw,
}

// Formatter provides methods for formatting output.
type Formatter interface {
	// FormatPutResponse formats a Put operation response
	FormatPutResponse(resp *PutResponse, format OutputFormat, w io.Writer) error

	// FormatGetResponse formats a Get operation response
	FormatGetResponse(resp *GetResponse, format OutputFormat, w io.Writer) error

	// FormatDeleteResponse formats a Delete operation response
	FormatDeleteResponse(resp *DeleteResponse, format OutputFormat, w io.Writer) error

	// FormatListResponse formats a List operation response
	FormatListResponse(resp *ListResponse, format OutputFormat, w io.Writer) error

	// FormatQueryResponse formats a Query operation response
	FormatQueryResponse(resp *QueryResponse, format OutputFormat, w io.Writer) error

	// FormatClusterStatus formats cluster status information
	FormatClusterStatus(resp *ClusterStatusResponse, format OutputFormat, w io.Writer) error

	// FormatBackupCreateResponse formats a backup create response
	FormatBackupCreateResponse(resp *BackupCreateResponse, format OutputFormat, w io.Writer) error

	// FormatBackupListResponse formats a backup list response
	FormatBackupListResponse(resp *BackupListResponse, format OutputFormat, w io.Writer) error

	// FormatBackupRestoreResponse formats a backup restore response
	FormatBackupRestoreResponse(resp *BackupRestoreResponse, format OutputFormat, w io.Writer) error

	// FormatBackupDeleteResponse formats a backup delete response
	FormatBackupDeleteResponse(resp *BackupDeleteResponse, format OutputFormat, w io.Writer) error

	// FormatBackupVerifyResponse formats a backup verify response
	FormatBackupVerifyResponse(resp *BackupVerifyResponse, format OutputFormat, w io.Writer) error

	// FormatBackupStatusResponse formats a backup status response
	FormatBackupStatusResponse(resp *BackupStatusResponse, format OutputFormat, w io.Writer) error

	// FormatCacheStatsResponse formats a cache stats response
	FormatCacheStatsResponse(resp *CacheStatsResponse, format OutputFormat, w io.Writer) error

	// FormatCacheAllStatsResponse formats all cache stats response
	FormatCacheAllStatsResponse(resp *CacheAllStatsResponse, format OutputFormat, w io.Writer) error

	// FormatCacheClearResponse formats a cache clear response
	FormatCacheClearResponse(resp *CacheClearResponse, format OutputFormat, w io.Writer) error

	// FormatCacheConfigResponse formats a cache config response
	FormatCacheConfigResponse(resp *CacheConfigResponse, format OutputFormat, w io.Writer) error

	// FormatCacheAllConfigResponse formats all cache config response
	FormatCacheAllConfigResponse(resp *CacheAllConfigResponse, format OutputFormat, w io.Writer) error

	// FormatCacheEnableResponse formats a cache enable response
	FormatCacheEnableResponse(resp *CacheEnableResponse, format OutputFormat, w io.Writer) error

	// FormatCacheDisableResponse formats a cache disable response
	FormatCacheDisableResponse(resp *CacheDisableResponse, format OutputFormat, w io.Writer) error

	// FormatCacheWarmResponse formats a cache warm response
	FormatCacheWarmResponse(resp *CacheWarmResponse, format OutputFormat, w io.Writer) error

	// FormatError formats an error message
	FormatError(err error, w io.Writer) error
}

// DefaultFormatter implements the Formatter interface.
type DefaultFormatter struct {
	// ColorEnabled enables/disables color output
	ColorEnabled bool
}

// NewFormatter creates a new default formatter.
func NewFormatter(colorEnabled bool) Formatter {
	return &DefaultFormatter{
		ColorEnabled: colorEnabled,
	}
}

// formatHandler is a function type used for format-specific rendering callbacks.
type formatHandler func(w io.Writer) error

// formatDispatch dispatches formatting based on the output format using map-based lookup for
// JSON and YAML (which are format-agnostic), and callback functions for table and default
// rendering (which are response-specific).
func (f *DefaultFormatter) formatDispatch(
	data interface{},
	format OutputFormat,
	w io.Writer,
	tableHandler formatHandler,
	defaultHandler formatHandler,
) error {
	// Map-based dispatch for serialization formats
	formatHandlers := map[OutputFormat]func() error{
		OutputFormatJSON: func() error { return f.formatJSON(data, w) },
		OutputFormatYAML: func() error { return f.formatYAML(data, w) },
		OutputFormatTable: func() error {
			if tableHandler != nil {
				return tableHandler(w)
			}
			return defaultHandler(w)
		},
	}

	if handler, ok := formatHandlers[format]; ok {
		return handler()
	}

	// Default/raw format uses the response-specific default handler
	return defaultHandler(w)
}

// FormatPutResponse formats a Put operation response.
func (f *DefaultFormatter) FormatPutResponse(resp *PutResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "Key:\t%s\n", resp.Key)
			fmt.Fprintf(tw, "Success:\t%v\n", resp.Success)
			if resp.Message != "" {
				fmt.Fprintf(tw, "Message:\t%s\n", resp.Message)
			}
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
			return tw.Flush()
		},
		func(w io.Writer) error {
			if resp.Success {
				fmt.Fprintf(w, "Successfully stored key: %s\n", resp.Key)
			} else {
				fmt.Fprintf(w, "Failed to store key: %s - %s\n", resp.Key, resp.Message)
			}
			return nil
		},
	)
}

// FormatGetResponse formats a Get operation response.
func (f *DefaultFormatter) FormatGetResponse(resp *GetResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "Key:\t%s\n", resp.Key)
			fmt.Fprintf(tw, "Found:\t%v\n", resp.Found)
			if resp.Found {
				fmt.Fprintf(tw, "Value:\t%s\n", string(resp.Value))
				fmt.Fprintf(tw, "Size:\t%d bytes\n", len(resp.Value))
				fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
			}
			return tw.Flush()
		},
		func(w io.Writer) error {
			if format == OutputFormatRaw {
				if resp.Found {
					_, err := w.Write(resp.Value)
					return err
				}
				return nil
			}
			if resp.Found {
				fmt.Fprintf(w, "%s\n", string(resp.Value))
			} else {
				fmt.Fprintf(w, "Key not found: %s\n", resp.Key)
			}
			return nil
		},
	)
}

// FormatDeleteResponse formats a Delete operation response.
func (f *DefaultFormatter) FormatDeleteResponse(resp *DeleteResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "Key:\t%s\n", resp.Key)
			fmt.Fprintf(tw, "Deleted:\t%v\n", resp.Deleted)
			if resp.Message != "" {
				fmt.Fprintf(tw, "Message:\t%s\n", resp.Message)
			}
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
			return tw.Flush()
		},
		func(w io.Writer) error {
			if resp.Deleted {
				fmt.Fprintf(w, "Successfully deleted key: %s\n", resp.Key)
			} else {
				fmt.Fprintf(w, "Failed to delete key: %s - %s\n", resp.Key, resp.Message)
			}
			return nil
		},
	)
}

// FormatListResponse formats a List operation response.
func (f *DefaultFormatter) FormatListResponse(resp *ListResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "KEY\tSIZE\tTIMESTAMP\n")
			fmt.Fprintf(tw, "%s\t%s\t%s\n", strings.Repeat("-", 40), strings.Repeat("-", 10), strings.Repeat("-", 20))
			for _, item := range resp.Items {
				fmt.Fprintf(tw, "%s\t%d bytes\t%s\n",
					item.Key,
					len(item.Value),
					item.Timestamp.Format(time.RFC3339))
			}
			fmt.Fprintf(tw, "\n")
			fmt.Fprintf(tw, "Total:\t%d items\n", resp.Total)
			if resp.NextCursor != "" {
				fmt.Fprintf(tw, "Next Cursor:\t%s\n", resp.NextCursor)
			}
			return tw.Flush()
		},
		func(w io.Writer) error {
			for _, item := range resp.Items {
				fmt.Fprintf(w, "%s\n", item.Key)
			}
			if resp.NextCursor != "" {
				fmt.Fprintf(w, "\nNext cursor: %s\n", resp.NextCursor)
			}
			return nil
		},
	)
}

// FormatQueryResponse formats a Query operation response.
func (f *DefaultFormatter) FormatQueryResponse(resp *QueryResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "KEY\tINDEXED FIELDS\tSIZE\tTIMESTAMP\n")
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				strings.Repeat("-", 40),
				strings.Repeat("-", 20),
				strings.Repeat("-", 10),
				strings.Repeat("-", 20))
			for _, item := range resp.Items {
				indexedFields := ""
				if len(item.IndexedBy) > 0 {
					fields := make([]string, 0, len(item.IndexedBy))
					for k, v := range item.IndexedBy {
						fields = append(fields, fmt.Sprintf("%s=%v", k, v))
					}
					indexedFields = strings.Join(fields, ", ")
				}
				fmt.Fprintf(tw, "%s\t%s\t%d bytes\t%s\n",
					item.Key,
					indexedFields,
					len(item.Value),
					item.Timestamp.Format(time.RFC3339))
			}
			fmt.Fprintf(tw, "\n")
			fmt.Fprintf(tw, "Total:\t%d items\n", resp.Total)
			return tw.Flush()
		},
		func(w io.Writer) error {
			for _, item := range resp.Items {
				fmt.Fprintf(w, "%s\n", item.Key)
			}
			fmt.Fprintf(w, "\nTotal: %d items\n", resp.Total)
			return nil
		},
	)
}

// FormatClusterStatus formats cluster status information.
func (f *DefaultFormatter) FormatClusterStatus(resp *ClusterStatusResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "CLUSTER STATUS\n")
			fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
			fmt.Fprintf(tw, "Healthy:\t%v\n", resp.Healthy)
			fmt.Fprintf(tw, "Leader ID:\t%d\n", resp.LeaderID)
			fmt.Fprintf(tw, "Nodes:\t%d/%d ready\n", resp.ReadyNodes, resp.NodeCount)
			fmt.Fprintf(tw, "\n")

			if len(resp.Nodes) > 0 {
				fmt.Fprintf(tw, "NODES\n")
				fmt.Fprintf(tw, "%s\n", strings.Repeat("-", 60))
				fmt.Fprintf(tw, "NODE ID\tADDRESS\tLEADER\tHEALTHY\tLAST SEEN\n")
				for _, node := range resp.Nodes {
					fmt.Fprintf(tw, "%d\t%s\t%v\t%v\t%s\n",
						node.NodeID,
						node.Address,
						node.IsLeader,
						node.IsHealthy,
						node.LastSeen.Format(time.RFC3339))
				}
				fmt.Fprintf(tw, "\n")
			}

			if len(resp.RaftGroupInfo) > 0 {
				fmt.Fprintf(tw, "RAFT GROUPS\n")
				fmt.Fprintf(tw, "%s\n", strings.Repeat("-", 60))
				fmt.Fprintf(tw, "GROUP ID\tLEADER\tMEMBERS\tLOG INDEX\tCOMMIT\tAPPLIED\n")
				for _, group := range resp.RaftGroupInfo {
					fmt.Fprintf(tw, "%d\t%d\t%d\t%d\t%d\t%d\n",
						group.GroupID,
						group.LeaderID,
						group.MemberCount,
						group.LogIndex,
						group.CommitIndex,
						group.AppliedIndex)
				}
			}

			return tw.Flush()
		},
		func(w io.Writer) error {
			if resp.Healthy {
				fmt.Fprintf(w, "Cluster is healthy\n")
			} else {
				fmt.Fprintf(w, "Cluster is unhealthy\n")
			}
			fmt.Fprintf(w, "Leader: %d, Nodes: %d/%d ready\n", resp.LeaderID, resp.ReadyNodes, resp.NodeCount)
			return nil
		},
	)
}

// FormatError formats an error message.
func (f *DefaultFormatter) FormatError(err error, w io.Writer) error {
	if f.ColorEnabled {
		fmt.Fprintf(w, "\033[31mError:\033[0m %v\n", err)
	} else {
		fmt.Fprintf(w, "Error: %v\n", err)
	}
	return nil
}

// formatJSON formats data as JSON.
func (f *DefaultFormatter) formatJSON(data interface{}, w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// formatYAML formats data as YAML.
func (f *DefaultFormatter) formatYAML(data interface{}, w io.Writer) error {
	encoder := yaml.NewEncoder(w)
	encoder.SetIndent(2)
	defer encoder.Close()
	return encoder.Encode(data)
}

// FormatBackupCreateResponse formats a backup create response.
func (f *DefaultFormatter) FormatBackupCreateResponse(resp *BackupCreateResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "\nBACKUP CREATED\n")
			fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
			fmt.Fprintf(tw, "Backup ID:\t%s\n", resp.BackupID)
			fmt.Fprintf(tw, "Type:\t%s\n", resp.Type)
			fmt.Fprintf(tw, "Size:\t%s\n", formatBytes(resp.Size))
			fmt.Fprintf(tw, "Compression:\t%s\n", resp.Compression)
			fmt.Fprintf(tw, "Encryption:\t%s\n", resp.Encryption)
			if resp.Duration > 0 {
				fmt.Fprintf(tw, "Duration:\t%s\n", resp.Duration)
			}
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
			if resp.Message != "" {
				fmt.Fprintf(tw, "Message:\t%s\n", resp.Message)
			}
			return tw.Flush()
		},
		func(w io.Writer) error {
			if resp.Success {
				fmt.Fprintf(w, "\nBackup created successfully: %s\n", resp.BackupID)
			} else {
				fmt.Fprintf(w, "\nBackup failed: %s\n", resp.Message)
			}
			return nil
		},
	)
}

// FormatBackupListResponse formats a backup list response.
func (f *DefaultFormatter) FormatBackupListResponse(resp *BackupListResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "\nBACKUPS\n")
			fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 100))
			fmt.Fprintf(tw, "BACKUP ID\tTYPE\tSIZE\tCOMPRESSION\tENCRYPTION\tSHARDS\tKEYS\tTIMESTAMP\n")
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				strings.Repeat("-", 30),
				strings.Repeat("-", 12),
				strings.Repeat("-", 10),
				strings.Repeat("-", 12),
				strings.Repeat("-", 10),
				strings.Repeat("-", 6),
				strings.Repeat("-", 10),
				strings.Repeat("-", 20))
			for _, backup := range resp.Backups {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
					backup.ID,
					backup.Type,
					formatBytes(backup.Size),
					backup.Compression,
					backup.Encryption,
					backup.NumShards,
					backup.NumKeys,
					backup.Timestamp.Format(time.RFC3339))
			}
			fmt.Fprintf(tw, "\n")
			fmt.Fprintf(tw, "Total backups:\t%d\n", resp.TotalCount)
			fmt.Fprintf(tw, "Storage path:\t%s\n", resp.StoragePath)
			return tw.Flush()
		},
		func(w io.Writer) error {
			for _, backup := range resp.Backups {
				fmt.Fprintf(w, "%s (%s, %s)\n", backup.ID, backup.Type, formatBytes(backup.Size))
			}
			fmt.Fprintf(w, "\nTotal: %d backups\n", resp.TotalCount)
			return nil
		},
	)
}

// FormatBackupRestoreResponse formats a backup restore response.
func (f *DefaultFormatter) FormatBackupRestoreResponse(resp *BackupRestoreResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "\nRESTORE COMPLETE\n")
			fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
			fmt.Fprintf(tw, "Backup ID:\t%s\n", resp.BackupID)
			fmt.Fprintf(tw, "Keys restored:\t%d\n", resp.KeysRestored)
			fmt.Fprintf(tw, "Shards restored:\t%d\n", resp.ShardsRestored)
			if resp.Duration > 0 {
				fmt.Fprintf(tw, "Duration:\t%s\n", resp.Duration)
			}
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
			if resp.Message != "" {
				fmt.Fprintf(tw, "Message:\t%s\n", resp.Message)
			}
			return tw.Flush()
		},
		func(w io.Writer) error {
			if resp.Success {
				fmt.Fprintf(w, "\nRestore completed successfully from backup: %s\n", resp.BackupID)
				fmt.Fprintf(w, "Restored %d keys across %d shards\n", resp.KeysRestored, resp.ShardsRestored)
			} else {
				fmt.Fprintf(w, "\nRestore failed: %s\n", resp.Message)
			}
			return nil
		},
	)
}

// FormatBackupDeleteResponse formats a backup delete response.
func (f *DefaultFormatter) FormatBackupDeleteResponse(resp *BackupDeleteResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "Backup ID:\t%s\n", resp.BackupID)
			fmt.Fprintf(tw, "Deleted:\t%v\n", resp.Deleted)
			if resp.Message != "" {
				fmt.Fprintf(tw, "Message:\t%s\n", resp.Message)
			}
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
			return tw.Flush()
		},
		func(w io.Writer) error {
			if resp.Deleted {
				fmt.Fprintf(w, "\nSuccessfully deleted backup: %s\n", resp.BackupID)
			} else {
				fmt.Fprintf(w, "\nFailed to delete backup: %s - %s\n", resp.BackupID, resp.Message)
			}
			return nil
		},
	)
}

// FormatBackupVerifyResponse formats a backup verify response.
func (f *DefaultFormatter) FormatBackupVerifyResponse(resp *BackupVerifyResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "\nBACKUP VERIFICATION\n")
			fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
			fmt.Fprintf(tw, "Backup ID:\t%s\n", resp.BackupID)
			fmt.Fprintf(tw, "Valid:\t%v\n", resp.Valid)
			fmt.Fprintf(tw, "Checksum Valid:\t%v\n", resp.ChecksumValid)
			fmt.Fprintf(tw, "Shards Valid:\t%d\n", resp.ShardsValid)
			if len(resp.Issues) > 0 {
				fmt.Fprintf(tw, "\nIssues:\n")
				for i, issue := range resp.Issues {
					fmt.Fprintf(tw, "  %d.\t%s\n", i+1, issue)
				}
			} else {
				fmt.Fprintf(tw, "Issues:\tNone\n")
			}
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
			return tw.Flush()
		},
		func(w io.Writer) error {
			if resp.Valid {
				fmt.Fprintf(w, "\nBackup is valid: %s\n", resp.BackupID)
				fmt.Fprintf(w, "All checksums verified, %d shards valid\n", resp.ShardsValid)
			} else {
				fmt.Fprintf(w, "\nBackup verification failed: %s\n", resp.BackupID)
				if len(resp.Issues) > 0 {
					fmt.Fprintf(w, "Issues:\n")
					for _, issue := range resp.Issues {
						fmt.Fprintf(w, "  - %s\n", issue)
					}
				}
			}
			return nil
		},
	)
}

// FormatBackupStatusResponse formats a backup status response.
func (f *DefaultFormatter) FormatBackupStatusResponse(resp *BackupStatusResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "\nBACKUP STATUS\n")
			fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))

			if resp.BackupID != "" {
				fmt.Fprintf(tw, "Backup ID:\t%s\n", resp.BackupID)
			}
			fmt.Fprintf(tw, "State:\t%s\n", resp.State)

			switch resp.State {
			case "running":
				fmt.Fprintf(tw, "Progress:\t%d%%\n", resp.Progress)
				if resp.ShardsTotal > 0 {
					fmt.Fprintf(tw, "Shards:\t%d / %d\n", resp.ShardsComplete, resp.ShardsTotal)
				}
				if resp.BytesTotal > 0 {
					fmt.Fprintf(tw, "Bytes:\t%s / %s\n", formatBytes(resp.BytesComplete), formatBytes(resp.BytesTotal))
				}
				if !resp.StartTime.IsZero() {
					fmt.Fprintf(tw, "Started:\t%s\n", resp.StartTime.Format(time.RFC3339))
				}
				if resp.ETA > 0 {
					fmt.Fprintf(tw, "ETA:\t%s\n", resp.ETA)
				}
			case "completed":
				if resp.ShardsTotal > 0 {
					fmt.Fprintf(tw, "Shards:\t%d\n", resp.ShardsTotal)
				}
				if resp.BytesTotal > 0 {
					fmt.Fprintf(tw, "Size:\t%s\n", formatBytes(resp.BytesTotal))
				}
				fmt.Fprintf(tw, "Valid:\t%v\n", resp.Valid)
				fmt.Fprintf(tw, "Checksum Valid:\t%v\n", resp.ChecksumValid)
			}

			if len(resp.Issues) > 0 {
				fmt.Fprintf(tw, "\nIssues:\n")
				for i, issue := range resp.Issues {
					fmt.Fprintf(tw, "  %d.\t%s\n", i+1, issue)
				}
			}

			if resp.Message != "" {
				fmt.Fprintf(tw, "\nMessage:\t%s\n", resp.Message)
			}

			if !resp.Timestamp.IsZero() {
				fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
			}

			return tw.Flush()
		},
		func(w io.Writer) error {
			switch resp.State {
			case "idle":
				fmt.Fprintf(w, "\nNo backup operations in progress\n")
				if resp.Message != "" {
					fmt.Fprintf(w, "%s\n", resp.Message)
				}
			case "running":
				fmt.Fprintf(w, "\nBackup in progress: %s\n", resp.BackupID)
				fmt.Fprintf(w, "Progress: %d%% (%d/%d shards)\n", resp.Progress, resp.ShardsComplete, resp.ShardsTotal)
				if resp.ETA > 0 {
					fmt.Fprintf(w, "ETA: %s\n", resp.ETA)
				}
			case "completed":
				fmt.Fprintf(w, "\nBackup completed: %s\n", resp.BackupID)
				if resp.Valid {
					fmt.Fprintf(w, "Status: Valid, %d shards\n", resp.ShardsTotal)
				} else {
					fmt.Fprintf(w, "Status: Invalid\n")
					for _, issue := range resp.Issues {
						fmt.Fprintf(w, "  - %s\n", issue)
					}
				}
			case "failed":
				fmt.Fprintf(w, "\nBackup failed: %s\n", resp.BackupID)
				if resp.Message != "" {
					fmt.Fprintf(w, "Error: %s\n", resp.Message)
				}
			default:
				if resp.Message != "" {
					fmt.Fprintf(w, "%s\n", resp.Message)
				}
			}
			return nil
		},
	)
}

// formatBytes formats bytes into human-readable format.
func formatBytes(bytes int64) string {
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

// FormatCacheStatsResponse formats cache statistics for a single entity.
func (f *DefaultFormatter) FormatCacheStatsResponse(resp *CacheStatsResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "\nCACHE STATISTICS: %s\n", resp.EntityType)
			fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
			fmt.Fprintf(tw, "Enabled:\t%v\n", resp.Enabled)

			if resp.Enabled {
				fmt.Fprintf(tw, "L1 Size:\t%d / %d entries (%.1f%% full)\n",
					resp.Stats.L1Size,
					resp.Stats.L1Capacity,
					float64(resp.Stats.L1Size)/float64(resp.Stats.L1Capacity)*100)
				fmt.Fprintf(tw, "L1 Hits:\t%d\n", resp.Stats.L1Hits)
				fmt.Fprintf(tw, "L1 Misses:\t%d\n", resp.Stats.L1Misses)
				fmt.Fprintf(tw, "L1 Hit Rate:\t%.2f%%\n", resp.Stats.L1HitRate*100)

				totalRequests := resp.Stats.L1Hits + resp.Stats.L1Misses
				if totalRequests > 0 {
					fmt.Fprintf(tw, "Total Requests:\t%d\n", totalRequests)
				}
			} else {
				fmt.Fprintf(tw, "Status:\tCache disabled for this entity\n")
			}

			return tw.Flush()
		},
		func(w io.Writer) error {
			if resp.Enabled {
				fmt.Fprintf(w, "Entity: %s\n", resp.EntityType)
				fmt.Fprintf(w, "Size: %d/%d, Hit Rate: %.2f%%\n",
					resp.Stats.L1Size,
					resp.Stats.L1Capacity,
					resp.Stats.L1HitRate*100)
			} else {
				fmt.Fprintf(w, "Cache disabled for entity: %s\n", resp.EntityType)
			}
			return nil
		},
	)
}

// FormatCacheAllStatsResponse formats cache statistics for all entities.
func (f *DefaultFormatter) FormatCacheAllStatsResponse(resp *CacheAllStatsResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "\nCACHE STATISTICS (ALL ENTITIES)\n")
			fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 100))
			fmt.Fprintf(tw, "ENTITY\tENABLED\tSIZE\tCAPACITY\tHITS\tMISSES\tHIT RATE\n")
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				strings.Repeat("-", 20),
				strings.Repeat("-", 7),
				strings.Repeat("-", 8),
				strings.Repeat("-", 8),
				strings.Repeat("-", 10),
				strings.Repeat("-", 10),
				strings.Repeat("-", 8))

			for entityType, entityStats := range resp.EntityStats {
				if entityStats.Enabled {
					fmt.Fprintf(tw, "%s\t%v\t%d\t%d\t%d\t%d\t%.2f%%\n",
						entityType,
						entityStats.Enabled,
						entityStats.Stats.L1Size,
						entityStats.Stats.L1Capacity,
						entityStats.Stats.L1Hits,
						entityStats.Stats.L1Misses,
						entityStats.Stats.L1HitRate*100)
				} else {
					fmt.Fprintf(tw, "%s\tdisabled\t-\t-\t-\t-\t-\n", entityType)
				}
			}

			fmt.Fprintf(tw, "\n")
			fmt.Fprintf(tw, "Total entities:\t%d\n", len(resp.EntityStats))

			return tw.Flush()
		},
		func(w io.Writer) error {
			for entityType, entityStats := range resp.EntityStats {
				if entityStats.Enabled {
					fmt.Fprintf(w, "%s: %d/%d entries, %.2f%% hit rate\n",
						entityType,
						entityStats.Stats.L1Size,
						entityStats.Stats.L1Capacity,
						entityStats.Stats.L1HitRate*100)
				} else {
					fmt.Fprintf(w, "%s: disabled\n", entityType)
				}
			}
			return nil
		},
	)
}

// FormatCacheClearResponse formats a cache clear response.
func (f *DefaultFormatter) FormatCacheClearResponse(resp *CacheClearResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "\nCACHE CLEARED\n")
			fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
			if resp.EntityType != "" {
				fmt.Fprintf(tw, "Entity Type:\t%s\n", resp.EntityType)
			} else {
				fmt.Fprintf(tw, "Entity Type:\tAll entities\n")
			}
			fmt.Fprintf(tw, "Entries Cleared:\t%d\n", resp.EntriesCleared)
			fmt.Fprintf(tw, "Duration:\t%s\n", resp.Duration)
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
			if resp.Message != "" {
				fmt.Fprintf(tw, "Message:\t%s\n", resp.Message)
			}
			return tw.Flush()
		},
		func(w io.Writer) error {
			if resp.Success {
				if resp.EntityType != "" {
					fmt.Fprintf(w, "\nCleared %d cache entries for entity: %s\n", resp.EntriesCleared, resp.EntityType)
				} else {
					fmt.Fprintf(w, "\nCleared %d cache entries across all entities\n", resp.EntriesCleared)
				}
			} else {
				fmt.Fprintf(w, "\nFailed to clear cache: %s\n", resp.Message)
			}
			return nil
		},
	)
}

// FormatCacheConfigResponse formats cache configuration for a single entity.
func (f *DefaultFormatter) FormatCacheConfigResponse(resp *CacheConfigResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "\nCACHE CONFIGURATION: %s\n", resp.EntityType)
			fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
			fmt.Fprintf(tw, "Enabled:\t%v\n", resp.Enabled)

			if resp.Enabled {
				fmt.Fprintf(tw, "TTL:\t%s\n", resp.TTL)
				if resp.MaxSize > 0 {
					fmt.Fprintf(tw, "Max Size:\t%d entries\n", resp.MaxSize)
				} else {
					fmt.Fprintf(tw, "Max Size:\tProportional allocation\n")
				}
			}

			return tw.Flush()
		},
		func(w io.Writer) error {
			fmt.Fprintf(w, "Entity: %s, Enabled: %v", resp.EntityType, resp.Enabled)
			if resp.Enabled {
				fmt.Fprintf(w, ", TTL: %s, Max Size: %d\n", resp.TTL, resp.MaxSize)
			} else {
				fmt.Fprintf(w, "\n")
			}
			return nil
		},
	)
}

// FormatCacheAllConfigResponse formats cache configuration for all entities.
func (f *DefaultFormatter) FormatCacheAllConfigResponse(resp *CacheAllConfigResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "\nCACHE CONFIGURATION\n")
			fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 80))
			fmt.Fprintf(tw, "GLOBAL SETTINGS\n")
			fmt.Fprintf(tw, "%s\n", strings.Repeat("-", 80))
			fmt.Fprintf(tw, "Enabled:\t%v\n", resp.GlobalEnabled)
			fmt.Fprintf(tw, "Default TTL:\t%s\n", resp.DefaultTTL)
			fmt.Fprintf(tw, "Max Size:\t%d entries\n", resp.MaxSize)
			fmt.Fprintf(tw, "Cleanup Interval:\t%s\n", resp.CleanupInterval)
			fmt.Fprintf(tw, "\n")

			if len(resp.EntityConfigs) > 0 {
				fmt.Fprintf(tw, "ENTITY-SPECIFIC SETTINGS\n")
				fmt.Fprintf(tw, "%s\n", strings.Repeat("-", 80))
				fmt.Fprintf(tw, "ENTITY\tENABLED\tTTL\tMAX SIZE\n")
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
					strings.Repeat("-", 20),
					strings.Repeat("-", 7),
					strings.Repeat("-", 15),
					strings.Repeat("-", 10))

				for entityType, entityCfg := range resp.EntityConfigs {
					maxSizeStr := "proportional"
					if entityCfg.MaxSize > 0 {
						maxSizeStr = fmt.Sprintf("%d", entityCfg.MaxSize)
					}
					fmt.Fprintf(tw, "%s\t%v\t%s\t%s\n",
						entityType,
						entityCfg.Enabled,
						entityCfg.TTL,
						maxSizeStr)
				}
			} else {
				fmt.Fprintf(tw, "No entity-specific configurations\n")
			}

			return tw.Flush()
		},
		func(w io.Writer) error {
			fmt.Fprintf(w, "Global: Enabled=%v, TTL=%s, MaxSize=%d\n",
				resp.GlobalEnabled, resp.DefaultTTL, resp.MaxSize)
			fmt.Fprintf(w, "Entity-specific configs: %d\n", len(resp.EntityConfigs))
			return nil
		},
	)
}

// FormatCacheEnableResponse formats a cache enable response.
func (f *DefaultFormatter) FormatCacheEnableResponse(resp *CacheEnableResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "Entity Type:\t%s\n", resp.EntityType)
			fmt.Fprintf(tw, "Enabled:\t%v\n", resp.Enabled)
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
			if resp.Message != "" {
				fmt.Fprintf(tw, "Message:\t%s\n", resp.Message)
			}
			return tw.Flush()
		},
		func(w io.Writer) error {
			if resp.Success {
				fmt.Fprintf(w, "%s\n", resp.Message)
			} else {
				fmt.Fprintf(w, "Failed to enable cache: %s\n", resp.Message)
			}
			return nil
		},
	)
}

// FormatCacheDisableResponse formats a cache disable response.
func (f *DefaultFormatter) FormatCacheDisableResponse(resp *CacheDisableResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "Entity Type:\t%s\n", resp.EntityType)
			fmt.Fprintf(tw, "Enabled:\t%v\n", resp.Enabled)
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
			if resp.Message != "" {
				fmt.Fprintf(tw, "Message:\t%s\n", resp.Message)
			}
			return tw.Flush()
		},
		func(w io.Writer) error {
			if resp.Success {
				fmt.Fprintf(w, "%s\n", resp.Message)
			} else {
				fmt.Fprintf(w, "Failed to disable cache: %s\n", resp.Message)
			}
			return nil
		},
	)
}

// FormatCacheWarmResponse formats a cache warm response.
func (f *DefaultFormatter) FormatCacheWarmResponse(resp *CacheWarmResponse, format OutputFormat, w io.Writer) error {
	return f.formatDispatch(resp, format, w,
		func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "\nCACHE WARMING\n")
			fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
			fmt.Fprintf(tw, "State:\t%s\n", resp.State)
			if len(resp.Prefixes) > 0 {
				fmt.Fprintf(tw, "Prefixes:\t%s\n", strings.Join(resp.Prefixes, ", "))
			}
			if resp.CurrentPrefix != "" {
				fmt.Fprintf(tw, "Current Prefix:\t%s\n", resp.CurrentPrefix)
			}
			fmt.Fprintf(tw, "Keys Warmed:\t%d\n", resp.KeysWarmed)
			fmt.Fprintf(tw, "Keys Failed:\t%d\n", resp.KeysFailed)
			if !resp.StartedAt.IsZero() {
				fmt.Fprintf(tw, "Started At:\t%s\n", resp.StartedAt.Format(time.RFC3339))
			}
			if !resp.CompletedAt.IsZero() {
				fmt.Fprintf(tw, "Completed At:\t%s\n", resp.CompletedAt.Format(time.RFC3339))
			}
			if resp.Duration > 0 {
				fmt.Fprintf(tw, "Duration:\t%s\n", resp.Duration)
			}
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
			if resp.Message != "" {
				fmt.Fprintf(tw, "Message:\t%s\n", resp.Message)
			}
			return tw.Flush()
		},
		func(w io.Writer) error {
			switch resp.State {
			case "completed":
				if resp.Success {
					fmt.Fprintf(w, "\nCache warming completed successfully\n")
					fmt.Fprintf(w, "Keys warmed: %d, Keys failed: %d\n", resp.KeysWarmed, resp.KeysFailed)
					if resp.Duration > 0 {
						fmt.Fprintf(w, "Duration: %s\n", resp.Duration)
					}
				} else {
					fmt.Fprintf(w, "\nCache warming completed with errors: %s\n", resp.Message)
					fmt.Fprintf(w, "Keys warmed: %d, Keys failed: %d\n", resp.KeysWarmed, resp.KeysFailed)
				}
			case "warming":
				fmt.Fprintf(w, "\nCache warming in progress...\n")
				if resp.CurrentPrefix != "" {
					fmt.Fprintf(w, "Current prefix: %s\n", resp.CurrentPrefix)
				}
				fmt.Fprintf(w, "Keys warmed: %d\n", resp.KeysWarmed)
			case "failed":
				fmt.Fprintf(w, "\nCache warming failed: %s\n", resp.Message)
				fmt.Fprintf(w, "Keys warmed: %d, Keys failed: %d\n", resp.KeysWarmed, resp.KeysFailed)
			default:
				if resp.Message != "" {
					fmt.Fprintf(w, "%s\n", resp.Message)
				}
			}
			return nil
		},
	)
}

// ParseOutputFormat parses a string into an OutputFormat.
func ParseOutputFormat(s string) (OutputFormat, error) {
	// Handle empty string as default format (raw/simple output)
	if s == "" {
		return OutputFormatRaw, nil
	}

	if format, ok := outputFormatParseMap[strings.ToLower(s)]; ok {
		return format, nil
	}
	return "", &UnknownOutputFormatError{Format: s}
}
