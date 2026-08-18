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

// Package security provides enterprise security features including audit logging,
// encryption at rest, and secret management for QuicBaaS.
package security

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/syslog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Compression level constants for audit log compression.
const (
	CompressionLevelNone        = gzip.NoCompression
	CompressionLevelBestSpeed   = gzip.BestSpeed
	CompressionLevelDefault     = gzip.DefaultCompression
	CompressionLevelBestSize    = gzip.BestCompression
	CompressionLevelHuffmanOnly = gzip.HuffmanOnly
)

// Error codes for audit operations.
const (
	ErrCodeAuditConfigNil        = "AUDIT_CONFIG_NIL"
	ErrCodeAuditSignKeyRequired  = "AUDIT_SIGN_KEY_REQUIRED"
	ErrCodeAuditFileOpen         = "AUDIT_FILE_OPEN"
	ErrCodeAuditFileRotate       = "AUDIT_FILE_ROTATE"
	ErrCodeAuditEventSign        = "AUDIT_EVENT_SIGN"
	ErrCodeAuditEventMarshal     = "AUDIT_EVENT_MARSHAL"
	ErrCodeAuditEventWrite       = "AUDIT_EVENT_WRITE"
	ErrCodeAuditCompressionOpen  = "AUDIT_COMPRESSION_OPEN"
	ErrCodeAuditCompressionRead  = "AUDIT_COMPRESSION_READ"
	ErrCodeAuditCompressionWrite = "AUDIT_COMPRESSION_WRITE"
	ErrCodeAuditCompressionClose = "AUDIT_COMPRESSION_CLOSE"
	ErrCodeAuditDirCreate        = "AUDIT_DIR_CREATE"
	ErrCodeAuditBackupList       = "AUDIT_BACKUP_LIST"
	ErrCodeAuditBackupRemove     = "AUDIT_BACKUP_REMOVE"
)

// Error codes for SIEM operations.
const (
	ErrCodeSIEMConfigNil       = "SIEM_CONFIG_NIL"
	ErrCodeSIEMEndpointMissing = "SIEM_ENDPOINT_MISSING"
	ErrCodeSIEMEndpointInvalid = "SIEM_ENDPOINT_INVALID"
	ErrCodeSIEMSchemeInvalid   = "SIEM_SCHEME_INVALID"
	ErrCodeSIEMEventNil        = "SIEM_EVENT_NIL"
	ErrCodeSIEMFormatBatch     = "SIEM_FORMAT_BATCH"
	ErrCodeSIEMFormatUnknown   = "SIEM_FORMAT_UNKNOWN"
	ErrCodeSIEMRequestCreate   = "SIEM_REQUEST_CREATE"
	ErrCodeSIEMRequestFailed   = "SIEM_REQUEST_FAILED"
	ErrCodeSIEMStatusCode      = "SIEM_STATUS_CODE"
	ErrCodeSIEMRetryExhausted  = "SIEM_RETRY_EXHAUSTED"
)

// Error codes for syslog operations.
const (
	ErrCodeSyslogConfigNil    = "SYSLOG_CONFIG_NIL"
	ErrCodeSyslogConnection   = "SYSLOG_CONNECTION"
	ErrCodeSyslogWrite        = "SYSLOG_WRITE"
	ErrCodeSyslogNotConnected = "SYSLOG_NOT_CONNECTED"
)

// AuditError represents an error from audit operations.
type AuditError struct {
	Code    string
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *AuditError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause.
func (e *AuditError) Unwrap() error {
	return e.Cause
}

// SIEMError represents an error from SIEM operations.
type SIEMError struct {
	Code    string
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *SIEMError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause.
func (e *SIEMError) Unwrap() error {
	return e.Cause
}

// SyslogExporterError represents a general syslog exporter error.
type SyslogExporterError struct {
	Op     string
	Reason string
}

func (e *SyslogExporterError) Error() string {
	return fmt.Sprintf("syslog exporter error: %s: %s", e.Op, e.Reason)
}

// SyslogConnectionError represents a syslog connection error.
type SyslogConnectionError struct {
	Address string
	Network string
	Err     error
}

func (e *SyslogConnectionError) Error() string {
	if e.Network == "" {
		return fmt.Sprintf("failed to connect to local syslog: %v", e.Err)
	}
	return fmt.Sprintf("failed to connect to syslog at %s://%s: %v", e.Network, e.Address, e.Err)
}

func (e *SyslogConnectionError) Unwrap() error {
	return e.Err
}

// SyslogWriteError represents a syslog write error.
type SyslogWriteError struct {
	Err error
}

func (e *SyslogWriteError) Error() string {
	return fmt.Sprintf("failed to write to syslog: %v", e.Err)
}

func (e *SyslogWriteError) Unwrap() error {
	return e.Err
}

// SyslogFacility represents syslog facilities.
type SyslogFacility int

// Syslog facility constants matching log/syslog.
const (
	SyslogFacilityKern     SyslogFacility = 0
	SyslogFacilityUser     SyslogFacility = 1
	SyslogFacilityMail     SyslogFacility = 2
	SyslogFacilityDaemon   SyslogFacility = 3
	SyslogFacilityAuth     SyslogFacility = 4
	SyslogFacilitySyslog   SyslogFacility = 5
	SyslogFacilityLPR      SyslogFacility = 6
	SyslogFacilityNews     SyslogFacility = 7
	SyslogFacilityUUCP     SyslogFacility = 8
	SyslogFacilityCron     SyslogFacility = 9
	SyslogFacilityAuthPriv SyslogFacility = 10
	SyslogFacilityFTP      SyslogFacility = 11
	SyslogFacilityLocal0   SyslogFacility = 16
	SyslogFacilityLocal1   SyslogFacility = 17
	SyslogFacilityLocal2   SyslogFacility = 18
	SyslogFacilityLocal3   SyslogFacility = 19
	SyslogFacilityLocal4   SyslogFacility = 20
	SyslogFacilityLocal5   SyslogFacility = 21
	SyslogFacilityLocal6   SyslogFacility = 22
	SyslogFacilityLocal7   SyslogFacility = 23
)

// SyslogSeverity represents syslog severity levels.
type SyslogSeverity int

// Syslog severity constants matching RFC 5424.
const (
	SyslogSeverityEmergency     SyslogSeverity = 0
	SyslogSeverityAlert         SyslogSeverity = 1
	SyslogSeverityCritical      SyslogSeverity = 2
	SyslogSeverityError         SyslogSeverity = 3
	SyslogSeverityWarning       SyslogSeverity = 4
	SyslogSeverityNotice        SyslogSeverity = 5
	SyslogSeverityInformational SyslogSeverity = 6
	SyslogSeverityDebug         SyslogSeverity = 7
)

// SyslogExporter exports audit events to syslog.
// It supports both local syslog (via Unix socket) and remote syslog (via UDP/TCP).
// For remote syslog, messages are formatted according to RFC 5424.
type SyslogExporter struct {
	config     *SyslogExporterConfig
	logger     *logger.Logger
	localConn  *syslog.Writer
	remoteConn net.Conn
	isRemote   bool
	connected  atomic.Bool
	mu         sync.Mutex
}

// SyslogExporterConfig configures the syslog exporter.
type SyslogExporterConfig struct {
	// Address is the syslog server address.
	// For local syslog, this can be empty or a Unix socket path (e.g., "/dev/log").
	// For remote syslog, this should be "host:port" (e.g., "192.168.1.100:514").
	Address string

	// Network specifies the network type: "udp", "tcp", "unix", or "" for local.
	// Empty string uses the default local syslog.
	Network string

	// Facility is the syslog facility to use.
	Facility SyslogFacility

	// Tag is the syslog tag/application name.
	Tag string

	// Hostname is the hostname to include in RFC 5424 messages.
	// If empty, the system hostname is used.
	Hostname string
}

// NewSyslogExporter creates a new syslog exporter.
// It connects to either local or remote syslog based on the configuration.
func NewSyslogExporter(cfg *SyslogExporterConfig, log *logger.Logger) (*SyslogExporter, error) {
	if cfg == nil {
		return nil, &SyslogExporterError{Op: "NewSyslogExporter", Reason: "config cannot be nil"}
	}

	if cfg.Tag == "" {
		cfg.Tag = "quicbaas"
	}

	if cfg.Hostname == "" {
		hostname, err := os.Hostname()
		if err != nil {
			cfg.Hostname = "localhost"
		} else {
			cfg.Hostname = hostname
		}
	}

	exporter := &SyslogExporter{
		config:   cfg,
		logger:   log,
		isRemote: cfg.Network != "" && cfg.Network != "unix",
	}

	if err := exporter.connect(); err != nil {
		return nil, err
	}

	return exporter, nil
}

// connect establishes the syslog connection.
func (s *SyslogExporter) connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If already connected, close first
	if s.connected.Load() {
		_ = s.closeUnlocked() // Ignore close error during reconnect
	}

	// Determine if this is a local or remote connection
	if s.config.Network == "" || s.config.Network == "unix" {
		// Local syslog connection
		priority := syslog.Priority(int(s.config.Facility)*8 + int(SyslogSeverityInformational))
		var err error
		if s.config.Address != "" && s.config.Network == "unix" {
			s.localConn, err = syslog.Dial("unix", s.config.Address, priority, s.config.Tag)
		} else {
			s.localConn, err = syslog.New(priority, s.config.Tag)
		}
		if err != nil {
			return &SyslogConnectionError{Address: s.config.Address, Network: s.config.Network, Err: err}
		}
		s.isRemote = false
	} else {
		// Remote syslog connection (UDP or TCP)
		conn, err := net.DialTimeout(s.config.Network, s.config.Address, 10*time.Second)
		if err != nil {
			return &SyslogConnectionError{Address: s.config.Address, Network: s.config.Network, Err: err}
		}
		s.remoteConn = conn
		s.isRemote = true
	}

	s.connected.Store(true)
	return nil
}

// Export sends an audit event to syslog.
func (s *SyslogExporter) Export(event *AuditEvent) error {
	if !s.connected.Load() {
		if err := s.connect(); err != nil {
			return err
		}
	}

	severity := s.mapEventToSeverity(event)
	message := s.formatMessage(event)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRemote {
		return s.sendRemote(event, severity, message)
	}
	return s.sendLocal(severity, message)
}

// sendLocal sends a message via the local syslog connection.
func (s *SyslogExporter) sendLocal(severity SyslogSeverity, message string) error {
	if s.localConn == nil {
		return &SyslogExporterError{Op: "sendLocal", Reason: "local connection not established"}
	}

	var err error
	switch severity {
	case SyslogSeverityEmergency:
		err = s.localConn.Emerg(message)
	case SyslogSeverityAlert:
		err = s.localConn.Alert(message)
	case SyslogSeverityCritical:
		err = s.localConn.Crit(message)
	case SyslogSeverityError:
		err = s.localConn.Err(message)
	case SyslogSeverityWarning:
		err = s.localConn.Warning(message)
	case SyslogSeverityNotice:
		err = s.localConn.Notice(message)
	case SyslogSeverityInformational:
		err = s.localConn.Info(message)
	case SyslogSeverityDebug:
		err = s.localConn.Debug(message)
	default:
		err = s.localConn.Info(message)
	}

	if err != nil {
		s.connected.Store(false)
		return &SyslogWriteError{Err: err}
	}
	return nil
}

// sendRemote sends a message via remote syslog using RFC 5424 format.
func (s *SyslogExporter) sendRemote(event *AuditEvent, severity SyslogSeverity, message string) error {
	if s.remoteConn == nil {
		return &SyslogExporterError{Op: "sendRemote", Reason: "remote connection not established"}
	}

	rfc5424Message := s.formatRFC5424(event, severity, message)

	// Set write deadline
	if err := s.remoteConn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return &SyslogWriteError{Err: err}
	}

	if _, err := s.remoteConn.Write([]byte(rfc5424Message)); err != nil {
		s.connected.Store(false)
		return &SyslogWriteError{Err: err}
	}
	return nil
}

// formatRFC5424 formats the audit event as an RFC 5424 syslog message.
// RFC 5424 format: <PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID [STRUCTURED-DATA] MSG.
func (s *SyslogExporter) formatRFC5424(event *AuditEvent, severity SyslogSeverity, message string) string {
	// Calculate priority: facility * 8 + severity
	priority := int(s.config.Facility)*8 + int(severity)

	// Format timestamp as RFC 3339
	timestamp := event.Timestamp.Format(time.RFC3339Nano)

	// Process ID
	procID := fmt.Sprintf("%d", os.Getpid())

	// Message ID (using event ID)
	msgID := event.EventID
	if msgID == "" {
		msgID = "-"
	}

	// Build structured data according to RFC 5424
	structuredData := s.buildStructuredData(event)

	// Build the RFC 5424 message
	// Format: <PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID STRUCTURED-DATA MSG
	return fmt.Sprintf("<%d>1 %s %s %s %s %s %s %s\n",
		priority,
		timestamp,
		s.config.Hostname,
		s.config.Tag,
		procID,
		msgID,
		structuredData,
		message,
	)
}

// sdParam renders one RFC 5424 SD-PARAM. The value is escaped per RFC 5424
// (backslash, double quote and closing bracket) and then wrapped in the
// literal quotes the grammar requires — deliberately not %q, which would
// re-escape the backslashes escapeSDParam just added.
func sdParam(name, value string) string {
	return name + `="` + escapeSDParam(value) + `"`
}

// buildStructuredData creates RFC 5424 structured data from the audit event.
func (s *SyslogExporter) buildStructuredData(event *AuditEvent) string {
	var parts []string

	// QuicBaaS audit structured data element
	// SD-ID: audit@quicbaas (using enterprise number placeholder)
	sdParams := []string{
		sdParam("action", event.Action),
		sdParam("resource", event.Resource),
		sdParam("result", event.Result),
	}

	if event.UserID != "" {
		sdParams = append(sdParams, sdParam("userID", event.UserID))
	}
	if event.Username != "" {
		sdParams = append(sdParams, sdParam("username", event.Username))
	}
	if event.IPAddress != "" {
		sdParams = append(sdParams, sdParam("ipAddress", event.IPAddress))
	}
	if event.NodeID != "" {
		sdParams = append(sdParams, sdParam("nodeID", event.NodeID))
	}
	if event.ShardID != 0 {
		sdParams = append(sdParams, sdParam("shardID", strconv.FormatUint(event.ShardID, 10)))
	}
	if event.ErrorMsg != "" {
		sdParams = append(sdParams, sdParam("errorMsg", event.ErrorMsg))
	}

	// Add details as structured data
	for key, value := range event.Details {
		sdParams = append(sdParams, sdParam(escapeSDParam(key), value))
	}

	parts = append(parts, fmt.Sprintf("[audit@quicbaas %s]", strings.Join(sdParams, " ")))

	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "")
}

// escapeSDParam escapes RFC 5424 SD-PARAM values.
func escapeSDParam(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "]", "\\]")
	return s
}

// formatMessage creates a human-readable message from the audit event.
func (s *SyslogExporter) formatMessage(event *AuditEvent) string {
	var msg strings.Builder

	msg.WriteString(fmt.Sprintf("QuicBaaS Audit: action=%s resource=%s result=%s",
		event.Action, event.Resource, event.Result))

	if event.Username != "" {
		msg.WriteString(fmt.Sprintf(" user=%s", event.Username))
	}
	if event.IPAddress != "" {
		msg.WriteString(fmt.Sprintf(" ip=%s", event.IPAddress))
	}
	if event.ErrorMsg != "" {
		msg.WriteString(fmt.Sprintf(" error=%s", event.ErrorMsg))
	}

	return msg.String()
}

// mapEventToSeverity maps an audit event result to a syslog severity level.
func (s *SyslogExporter) mapEventToSeverity(event *AuditEvent) SyslogSeverity {
	// Map based on result
	switch event.Result {
	case ResultSuccess:
		// Success events are informational, but some actions are more notable
		switch event.Action {
		case ActionKeyRotate, ActionKeyGenerate, ActionKeyExport, ActionKeyImport, ActionKeyDelete:
			// Key management operations are notable
			return SyslogSeverityNotice
		case ActionUserCreate, ActionUserDelete, ActionRoleCreate, ActionRoleUpdate:
			// User/role changes are notable
			return SyslogSeverityNotice
		case ActionConfigChange, ActionSchemaChange:
			// Configuration changes are notable
			return SyslogSeverityNotice
		case ActionTenantSwitch, ActionCrossTenantRead, ActionCrossTenantWrite, ActionCrossTenantDelete:
			// Cross-tenant operations are notable
			return SyslogSeverityNotice
		default:
			return SyslogSeverityInformational
		}
	case ResultFailure:
		// Failures indicate potential issues
		switch event.Action {
		case ActionAuthenticate:
			// Failed authentication could indicate an attack
			return SyslogSeverityWarning
		default:
			return SyslogSeverityError
		}
	case ResultDenied:
		// Access denials are security-relevant
		switch event.Action {
		case ActionAuthenticate, ActionAuthorize:
			// Multiple denials could indicate an attack
			return SyslogSeverityWarning
		case ActionTenantAccessDenied:
			// Cross-tenant access attempts are security-critical
			return SyslogSeverityWarning
		default:
			return SyslogSeverityWarning
		}
	default:
		return SyslogSeverityInformational
	}
}

// Close closes the syslog connection.
func (s *SyslogExporter) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeUnlocked()
}

// closeUnlocked closes the connection without acquiring the lock.
func (s *SyslogExporter) closeUnlocked() error {
	var err error
	if s.localConn != nil {
		err = s.localConn.Close()
		s.localConn = nil
	}
	if s.remoteConn != nil {
		closeErr := s.remoteConn.Close()
		if err == nil {
			err = closeErr
		}
		s.remoteConn = nil
	}
	s.connected.Store(false)
	return err
}

// IsConnected returns whether the syslog connection is active.
func (s *SyslogExporter) IsConnected() bool {
	return s.connected.Load()
}

// Reconnect attempts to reconnect to syslog.
func (s *SyslogExporter) Reconnect() error {
	return s.connect()
}

// SIEMFormat represents supported SIEM output formats.
type SIEMFormat string

const (
	// SIEMFormatCEF is Common Event Format for ArcSight and similar.
	SIEMFormatCEF SIEMFormat = "cef"
	// SIEMFormatLEEF is Log Event Extended Format for QRadar.
	SIEMFormatLEEF SIEMFormat = "leef"
	// SIEMFormatJSON is generic JSON format for Splunk, Elasticsearch, etc.
	SIEMFormatJSON SIEMFormat = "json"
)

// SIEMConfig configures SIEM integration settings.
type SIEMConfig struct {
	Enabled       bool              `json:"enabled"`
	Endpoint      string            `json:"endpoint"`
	Format        SIEMFormat        `json:"format"`
	BatchSize     int               `json:"batch_size"`
	FlushInterval time.Duration     `json:"flush_interval"`
	Headers       map[string]string `json:"headers"`
	Timeout       time.Duration     `json:"timeout"`
	MaxRetries    int               `json:"max_retries"`
	RetryDelay    time.Duration     `json:"retry_delay"`

	// CEF-specific configuration
	CEFVendor  string `json:"cef_vendor"`
	CEFProduct string `json:"cef_product"`
	CEFVersion string `json:"cef_version"`

	// LEEF-specific configuration
	LEEFVendor  string `json:"leef_vendor"`
	LEEFProduct string `json:"leef_product"`
	LEEFVersion string `json:"leef_version"`
}

// SIEMExporter handles sending audit events to SIEM systems.
type SIEMExporter struct {
	config     *SIEMConfig
	logger     *logger.Logger
	client     *http.Client
	batch      []*AuditEvent
	batchMu    sync.Mutex
	shutdown   chan struct{}
	wg         sync.WaitGroup
	started    atomic.Bool
	eventsSent atomic.Int64
	errCount   atomic.Int64
}

// SIEMJSONEvent represents a SIEM event in JSON format.
type SIEMJSONEvent struct {
	Timestamp    string            `json:"timestamp"`
	EventID      string            `json:"event_id"`
	EventType    string            `json:"event_type"`
	Source       string            `json:"source"`
	SourceIP     string            `json:"source_ip,omitempty"`
	User         string            `json:"user,omitempty"`
	UserID       string            `json:"user_id,omitempty"`
	Action       string            `json:"action"`
	Resource     string            `json:"resource"`
	Outcome      string            `json:"outcome"`
	NodeID       string            `json:"node_id,omitempty"`
	ShardID      uint64            `json:"shard_id,omitempty"`
	ErrorMessage string            `json:"error_message,omitempty"`
	Details      map[string]string `json:"details,omitempty"`
	Severity     int               `json:"severity"`
}

// NewSIEMExporter creates a new SIEM exporter with the provided configuration.
func NewSIEMExporter(cfg *SIEMConfig, log *logger.Logger) (*SIEMExporter, error) {
	if cfg == nil {
		return nil, &SIEMError{Code: ErrCodeSIEMConfigNil, Message: "config cannot be nil"}
	}

	if !cfg.Enabled {
		return &SIEMExporter{config: cfg, logger: log}, nil
	}

	if cfg.Endpoint == "" {
		return nil, &SIEMError{Code: ErrCodeSIEMEndpointMissing, Message: "endpoint is required when SIEM is enabled"}
	}

	parsedURL, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, &SIEMError{Code: ErrCodeSIEMEndpointInvalid, Message: "invalid endpoint URL", Cause: err}
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, &SIEMError{Code: ErrCodeSIEMSchemeInvalid, Message: "endpoint must use http or https scheme"}
	}

	// Apply defaults
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 10 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 1 * time.Second
	}
	if cfg.Format == "" {
		cfg.Format = SIEMFormatJSON
	}

	// Set CEF defaults
	if cfg.CEFVendor == "" {
		cfg.CEFVendor = "QuicBaaS"
	}
	if cfg.CEFProduct == "" {
		cfg.CEFProduct = "QuicBaaS"
	}
	if cfg.CEFVersion == "" {
		cfg.CEFVersion = "1.0"
	}

	// Set LEEF defaults
	if cfg.LEEFVendor == "" {
		cfg.LEEFVendor = "QuicBaaS"
	}
	if cfg.LEEFProduct == "" {
		cfg.LEEFProduct = "QuicBaaS"
	}
	if cfg.LEEFVersion == "" {
		cfg.LEEFVersion = "1.0"
	}

	// Create HTTP client with hardened TLS
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	exporter := &SIEMExporter{
		config:   cfg,
		logger:   log,
		batch:    make([]*AuditEvent, 0, cfg.BatchSize),
		shutdown: make(chan struct{}),
		client: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		},
	}

	return exporter, nil
}

// Start begins the background flush routine for batched events.
func (s *SIEMExporter) Start() {
	if !s.config.Enabled || !s.started.CompareAndSwap(false, true) {
		return
	}

	s.wg.Add(1)
	go s.flushLoop()
	s.logger.Info("SIEM exporter started")
}

// flushLoop periodically flushes batched events.
func (s *SIEMExporter) flushLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.Flush(); err != nil {
				s.logger.WithError(err).Error("Failed to flush SIEM batch")
			}
		case <-s.shutdown:
			// Final flush on shutdown
			if err := s.Flush(); err != nil {
				s.logger.WithError(err).Error("Failed to flush SIEM batch during shutdown")
			}
			return
		}
	}
}

// Send queues an audit event for sending to the SIEM system.
func (s *SIEMExporter) Send(event *AuditEvent) error {
	if !s.config.Enabled {
		return nil
	}

	if event == nil {
		return &SIEMError{Code: ErrCodeSIEMEventNil, Message: "event cannot be nil"}
	}

	s.batchMu.Lock()
	s.batch = append(s.batch, event)
	shouldFlush := len(s.batch) >= s.config.BatchSize
	s.batchMu.Unlock()

	if shouldFlush {
		return s.Flush()
	}

	return nil
}

// Flush sends all queued events to the SIEM system.
func (s *SIEMExporter) Flush() error {
	if !s.config.Enabled {
		return nil
	}

	s.batchMu.Lock()
	if len(s.batch) == 0 {
		s.batchMu.Unlock()
		return nil
	}

	// Swap batch to avoid holding lock during HTTP call
	events := s.batch
	s.batch = make([]*AuditEvent, 0, s.config.BatchSize)
	s.batchMu.Unlock()

	return s.sendEvents(events)
}

// sendEvents sends a batch of events to the SIEM endpoint with retry logic.
func (s *SIEMExporter) sendEvents(events []*AuditEvent) error {
	payload, contentType, err := s.formatBatch(events)
	if err != nil {
		return &SIEMError{Code: ErrCodeSIEMFormatBatch, Message: "failed to format batch", Cause: err}
	}

	var lastErr error
	for attempt := 0; attempt <= s.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter
			delay := s.config.RetryDelay * time.Duration(1<<uint(attempt-1)) // #nosec G115 -- attempt is bounded by MaxRetries config
			retryTimer := time.NewTimer(delay)
			select {
			case <-retryTimer.C:
			case <-s.shutdown:
				retryTimer.Stop()
				return &SIEMError{Code: ErrCodeSIEMRetryExhausted, Message: "exporter shutting down", Cause: lastErr}
			}
		}

		err = s.doHTTPRequest(payload, contentType)
		if err == nil {
			s.eventsSent.Add(int64(len(events)))
			return nil
		}
		lastErr = err
		s.errCount.Add(1)
		s.logger.WithError(err).Warnf("SIEM send attempt %d failed", attempt+1)
	}

	return &SIEMError{Code: ErrCodeSIEMRetryExhausted, Message: "all retry attempts failed", Cause: lastErr}
}

// doHTTPRequest performs the actual HTTP request to the SIEM endpoint.
func (s *SIEMExporter) doHTTPRequest(payload []byte, contentType string) error {
	req, err := http.NewRequest(http.MethodPost, s.config.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return &SIEMError{Code: ErrCodeSIEMRequestCreate, Message: "failed to create request", Cause: err}
	}

	req.Header.Set("Content-Type", contentType)
	for key, value := range s.config.Headers {
		req.Header.Set(key, value)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return &SIEMError{Code: ErrCodeSIEMRequestFailed, Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	// Read and discard body to enable connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &SIEMError{
			Code:    ErrCodeSIEMStatusCode,
			Message: fmt.Sprintf("unexpected status code: %d", resp.StatusCode),
		}
	}

	return nil
}

// formatBatch formats a batch of events according to the configured format.
func (s *SIEMExporter) formatBatch(events []*AuditEvent) ([]byte, string, error) {
	switch s.config.Format {
	case SIEMFormatCEF:
		return s.formatCEFBatch(events)
	case SIEMFormatLEEF:
		return s.formatLEEFBatch(events)
	case SIEMFormatJSON:
		return s.formatJSONBatch(events)
	default:
		return nil, "", &SIEMError{Code: ErrCodeSIEMFormatUnknown, Message: fmt.Sprintf("unsupported format: %s", s.config.Format)}
	}
}

// formatJSONBatch formats events as newline-delimited JSON.
func (s *SIEMExporter) formatJSONBatch(events []*AuditEvent) ([]byte, string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)

	for _, event := range events {
		siemEvent := s.toSIEMJSONEvent(event)
		if err := encoder.Encode(siemEvent); err != nil {
			return nil, "", &SIEMError{Code: ErrCodeSIEMFormatBatch, Message: "failed to encode event", Cause: err}
		}
	}

	return buf.Bytes(), "application/x-ndjson", nil
}

// toSIEMJSONEvent converts an AuditEvent to a SIEM-optimized JSON structure.
func (s *SIEMExporter) toSIEMJSONEvent(event *AuditEvent) *SIEMJSONEvent {
	return &SIEMJSONEvent{
		Timestamp:    event.Timestamp.UTC().Format(time.RFC3339Nano),
		EventID:      event.EventID,
		EventType:    "audit",
		Source:       "quicbaas",
		SourceIP:     event.IPAddress,
		User:         event.Username,
		UserID:       event.UserID,
		Action:       event.Action,
		Resource:     event.Resource,
		Outcome:      event.Result,
		NodeID:       event.NodeID,
		ShardID:      event.ShardID,
		ErrorMessage: event.ErrorMsg,
		Details:      event.Details,
		Severity:     s.mapSeverity(event),
	}
}

// formatCEFBatch formats events as CEF (Common Event Format).
// CEF format: CEF:Version|Device Vendor|Device Product|Device Version|Signature ID|Name|Severity|Extension.
func (s *SIEMExporter) formatCEFBatch(events []*AuditEvent) ([]byte, string, error) {
	var buf bytes.Buffer

	for _, event := range events {
		cef := s.formatCEFEvent(event)
		buf.WriteString(cef)
		buf.WriteByte('\n')
	}

	return buf.Bytes(), "text/plain", nil
}

// formatCEFEvent formats a single event as CEF.
func (s *SIEMExporter) formatCEFEvent(event *AuditEvent) string {
	// CEF severity: 0-3 Low, 4-6 Medium, 7-8 High, 9-10 Very High
	severity := s.mapCEFSeverity(event)

	// Build signature ID from action
	signatureID := s.mapSignatureID(event.Action)

	// Build event name
	name := s.mapEventName(event)

	// Build extension fields
	extension := s.buildCEFExtension(event)

	return fmt.Sprintf("CEF:0|%s|%s|%s|%s|%s|%d|%s",
		s.escapeCEF(s.config.CEFVendor),
		s.escapeCEF(s.config.CEFProduct),
		s.escapeCEF(s.config.CEFVersion),
		signatureID,
		s.escapeCEF(name),
		severity,
		extension,
	)
}

// buildCEFExtension builds the CEF extension field from an audit event.
func (s *SIEMExporter) buildCEFExtension(event *AuditEvent) string {
	var parts []string

	// Standard CEF extension fields
	parts = append(parts, fmt.Sprintf("rt=%d", event.Timestamp.UnixMilli()))

	if event.IPAddress != "" {
		parts = append(parts, fmt.Sprintf("src=%s", event.IPAddress))
	}

	if event.Username != "" {
		parts = append(parts, fmt.Sprintf("suser=%s", s.escapeCEFValue(event.Username)))
	}

	if event.UserID != "" {
		parts = append(parts, fmt.Sprintf("suid=%s", s.escapeCEFValue(event.UserID)))
	}

	parts = append(parts,
		fmt.Sprintf("act=%s", s.escapeCEFValue(event.Action)),
		fmt.Sprintf("cs1=%s cs1Label=resource", s.escapeCEFValue(event.Resource)),
		fmt.Sprintf("outcome=%s", event.Result),
	)

	if event.NodeID != "" {
		parts = append(parts, fmt.Sprintf("cs2=%s cs2Label=nodeId", s.escapeCEFValue(event.NodeID)))
	}

	if event.ShardID != 0 {
		parts = append(parts, fmt.Sprintf("cs3=%d cs3Label=shardId", event.ShardID))
	}

	if event.ErrorMsg != "" {
		parts = append(parts, fmt.Sprintf("msg=%s", s.escapeCEFValue(event.ErrorMsg)))
	}

	if event.EventID != "" {
		parts = append(parts, fmt.Sprintf("externalId=%s", s.escapeCEFValue(event.EventID)))
	}

	// Add custom details
	detailIndex := 4
	for key, value := range event.Details {
		if detailIndex <= 6 {
			parts = append(parts, fmt.Sprintf("cs%d=%s cs%dLabel=%s",
				detailIndex, s.escapeCEFValue(value), detailIndex, s.escapeCEFValue(key)))
			detailIndex++
		}
	}

	return strings.Join(parts, " ")
}

// escapeCEF escapes special characters in CEF header fields.
func (s *SIEMExporter) escapeCEF(value string) string {
	// Escape pipe and backslash in header fields
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
}

// escapeCEFValue escapes special characters in CEF extension values.
func (s *SIEMExporter) escapeCEFValue(value string) string {
	// Escape backslash, equals, and newline in extension values
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "=", "\\=")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\r", "\\r")
	return value
}

// mapCEFSeverity maps audit event result to CEF severity (0-10).
func (s *SIEMExporter) mapCEFSeverity(event *AuditEvent) int {
	switch event.Result {
	case ResultDenied:
		return 8 // High - access denial
	case ResultFailure:
		// Check for security-sensitive failures
		switch event.Action {
		case ActionAuthenticate:
			return 7 // High - auth failure
		case ActionKeyRotate, ActionKeyExport, ActionKeyGenerate, ActionKeyImport, ActionKeyDelete:
			return 6 // Medium-High - key operation failure
		default:
			return 4 // Medium - general failure
		}
	case ResultSuccess:
		// Security-sensitive successful operations
		switch event.Action {
		case ActionKeyExport:
			return 5 // Medium - key export should be monitored
		case ActionTenantSwitch, ActionCrossTenantRead, ActionCrossTenantWrite, ActionCrossTenantDelete:
			return 5 // Medium - cross-tenant operations
		case ActionUserCreate, ActionUserDelete, ActionRoleCreate, ActionRoleUpdate:
			return 4 // Medium - admin operations
		default:
			return 1 // Low - normal operations
		}
	default:
		return 3
	}
}

// mapSeverity maps audit event to generic severity level (1-10).
func (s *SIEMExporter) mapSeverity(event *AuditEvent) int {
	return s.mapCEFSeverity(event)
}

// mapSignatureID maps action to a signature ID for CEF/LEEF.
func (s *SIEMExporter) mapSignatureID(action string) string {
	signatureMap := map[string]string{
		ActionAuthenticate:       "1001",
		ActionAuthorize:          "1002",
		ActionRead:               "2001",
		ActionWrite:              "2002",
		ActionDelete:             "2003",
		ActionBackup:             "3001",
		ActionRestore:            "3002",
		ActionClusterJoin:        "4001",
		ActionClusterLeave:       "4002",
		ActionUserCreate:         "5001",
		ActionUserDelete:         "5002",
		ActionRoleCreate:         "5003",
		ActionRoleUpdate:         "5004",
		ActionKeyRotate:          "6001",
		ActionKeyGenerate:        "6002",
		ActionKeyExport:          "6003",
		ActionKeyImport:          "6004",
		ActionKeyDelete:          "6005",
		ActionConfigChange:       "7001",
		ActionSchemaChange:       "7002",
		ActionTenantSwitch:       "8001",
		ActionCrossTenantRead:    "8002",
		ActionCrossTenantWrite:   "8003",
		ActionCrossTenantDelete:  "8004",
		ActionTenantDEKCreate:    "8005",
		ActionTenantDEKRotate:    "8006",
		ActionTenantDEKDelete:    "8007",
		ActionTenantAccessDenied: "8008",
	}

	if id, ok := signatureMap[action]; ok {
		return id
	}
	return "9999" // Unknown action
}

// mapEventName generates a human-readable event name.
func (s *SIEMExporter) mapEventName(event *AuditEvent) string {
	nameMap := map[string]string{
		ActionAuthenticate:       "Authentication Attempt",
		ActionAuthorize:          "Authorization Check",
		ActionRead:               "Data Read",
		ActionWrite:              "Data Write",
		ActionDelete:             "Data Delete",
		ActionBackup:             "Backup Operation",
		ActionRestore:            "Restore Operation",
		ActionClusterJoin:        "Cluster Join",
		ActionClusterLeave:       "Cluster Leave",
		ActionUserCreate:         "User Created",
		ActionUserDelete:         "User Deleted",
		ActionRoleCreate:         "Role Created",
		ActionRoleUpdate:         "Role Updated",
		ActionKeyRotate:          "Encryption Key Rotated",
		ActionKeyGenerate:        "Encryption Key Generated",
		ActionKeyExport:          "Encryption Key Exported",
		ActionKeyImport:          "Encryption Key Imported",
		ActionKeyDelete:          "Encryption Key Deleted",
		ActionConfigChange:       "Configuration Changed",
		ActionSchemaChange:       "Schema Changed",
		ActionTenantSwitch:       "Tenant Context Switch",
		ActionCrossTenantRead:    "Cross-Tenant Read",
		ActionCrossTenantWrite:   "Cross-Tenant Write",
		ActionCrossTenantDelete:  "Cross-Tenant Delete",
		ActionTenantDEKCreate:    "Tenant DEK Created",
		ActionTenantDEKRotate:    "Tenant DEK Rotated",
		ActionTenantDEKDelete:    "Tenant DEK Deleted",
		ActionTenantAccessDenied: "Tenant Access Denied",
	}

	if name, ok := nameMap[event.Action]; ok {
		return fmt.Sprintf("%s - %s", name, event.Result)
	}
	return fmt.Sprintf("%s - %s", event.Action, event.Result)
}

// formatLEEFBatch formats events as LEEF (Log Event Extended Format) for QRadar.
// LEEF format: LEEF:Version|Vendor|Product|Version|EventID|Extension.
func (s *SIEMExporter) formatLEEFBatch(events []*AuditEvent) ([]byte, string, error) {
	var buf bytes.Buffer

	for _, event := range events {
		leef := s.formatLEEFEvent(event)
		buf.WriteString(leef)
		buf.WriteByte('\n')
	}

	return buf.Bytes(), "text/plain", nil
}

// formatLEEFEvent formats a single event as LEEF.
func (s *SIEMExporter) formatLEEFEvent(event *AuditEvent) string {
	eventID := s.mapSignatureID(event.Action)
	extension := s.buildLEEFExtension(event)

	return fmt.Sprintf("LEEF:2.0|%s|%s|%s|%s|%s",
		s.escapeLEEF(s.config.LEEFVendor),
		s.escapeLEEF(s.config.LEEFProduct),
		s.escapeLEEF(s.config.LEEFVersion),
		eventID,
		extension,
	)
}

// buildLEEFExtension builds the LEEF extension field from an audit event.
func (s *SIEMExporter) buildLEEFExtension(event *AuditEvent) string {
	var parts []string

	// LEEF uses tab as default delimiter
	parts = append(parts, fmt.Sprintf("devTime=%s", event.Timestamp.UTC().Format(time.RFC3339)))

	if event.IPAddress != "" {
		parts = append(parts, fmt.Sprintf("src=%s", event.IPAddress))
	}

	if event.Username != "" {
		parts = append(parts, fmt.Sprintf("usrName=%s", s.escapeLEEFValue(event.Username)))
	}

	if event.UserID != "" {
		parts = append(parts, fmt.Sprintf("identSrc=%s", s.escapeLEEFValue(event.UserID)))
	}

	parts = append(parts,
		fmt.Sprintf("action=%s", s.escapeLEEFValue(event.Action)),
		fmt.Sprintf("resource=%s", s.escapeLEEFValue(event.Resource)),
		fmt.Sprintf("result=%s", event.Result),
		fmt.Sprintf("sev=%d", s.mapLEEFSeverity(event)),
	)

	if event.NodeID != "" {
		parts = append(parts, fmt.Sprintf("nodeId=%s", s.escapeLEEFValue(event.NodeID)))
	}

	if event.ShardID != 0 {
		parts = append(parts, fmt.Sprintf("shardId=%d", event.ShardID))
	}

	if event.ErrorMsg != "" {
		parts = append(parts, fmt.Sprintf("reason=%s", s.escapeLEEFValue(event.ErrorMsg)))
	}

	if event.EventID != "" {
		parts = append(parts, fmt.Sprintf("eventId=%s", s.escapeLEEFValue(event.EventID)))
	}

	// Add custom details
	for key, value := range event.Details {
		parts = append(parts, fmt.Sprintf("%s=%s", s.escapeLEEFKey(key), s.escapeLEEFValue(value)))
	}

	return strings.Join(parts, "\t")
}

// escapeLEEF escapes special characters in LEEF header fields.
func (s *SIEMExporter) escapeLEEF(value string) string {
	// Escape pipe and backslash in header fields
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
}

// escapeLEEFKey escapes special characters in LEEF extension keys.
func (s *SIEMExporter) escapeLEEFKey(key string) string {
	// Keys should not contain equals or tabs
	key = strings.ReplaceAll(key, "=", "_")
	key = strings.ReplaceAll(key, "\t", "_")
	return key
}

// escapeLEEFValue escapes special characters in LEEF extension values.
func (s *SIEMExporter) escapeLEEFValue(value string) string {
	// Escape tab, equals, and newline in extension values
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\t", "\\t")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\r", "\\r")
	value = strings.ReplaceAll(value, "=", "\\=")
	return value
}

// mapLEEFSeverity maps audit event result to LEEF severity (1-10).
func (s *SIEMExporter) mapLEEFSeverity(event *AuditEvent) int {
	return s.mapCEFSeverity(event)
}

// GetStats returns SIEM exporter statistics.
func (s *SIEMExporter) GetStats() (eventsSent, errors int64) {
	return s.eventsSent.Load(), s.errCount.Load()
}

// IsRunning returns true if the exporter is running.
func (s *SIEMExporter) IsRunning() bool {
	return s.started.Load()
}

// Close shuts down the SIEM exporter gracefully.
func (s *SIEMExporter) Close() error {
	if !s.config.Enabled || !s.started.Load() {
		return nil
	}

	close(s.shutdown)
	s.wg.Wait()
	s.started.Store(false)
	s.logger.Info("SIEM exporter stopped")
	return nil
}

// AuditLogger provides comprehensive audit trail functionality for security events.
type AuditLogger struct {
	writer         io.Writer
	config         *AuditConfig
	logger         *logger.Logger
	file           *os.File
	signKey        []byte
	mu             sync.Mutex
	shutdown       chan struct{}
	siemExporter   *SIEMExporter
	syslogExporter *SyslogExporter

	// rotationWatchers counts the live checkRotation goroutines. A logger owns
	// exactly one for its lifetime, so this stays at 1 no matter how often the
	// log file is rotated and reopened.
	rotationWatchers atomic.Int64
}

// AuditConfig configures audit logging behavior.
type AuditConfig struct {
	Enabled          bool          `json:"enabled"`
	File             string        `json:"file"`
	MaxSize          int64         `json:"max_size"` // MB
	MaxBackups       int           `json:"max_backups"`
	Compress         bool          `json:"compress"`
	CompressionLevel int           `json:"compression_level"` // gzip compression level (0-9, -1 for default, -2 for Huffman only)
	SignLogs         bool          `json:"sign_logs"`         // Tamper-proof logging
	SignKey          string        `json:"sign_key"`          // HMAC key for signatures
	BufferSize       int           `json:"buffer_size"`       // Buffer size for batching
	FlushInterval    time.Duration `json:"flush_interval"`    // Auto-flush interval

	// Legacy output targets (backward compatibility)
	Syslog       bool   `json:"syslog"`
	SyslogAddr   string `json:"syslog_addr"`
	SIEM         bool   `json:"siem"`
	SIEMEndpoint string `json:"siem_endpoint"`

	// Extended SIEM configuration
	SIEMConfig *SIEMConfig `json:"siem_config,omitempty"`

	// Syslog configuration (new, comprehensive)
	SyslogEnabled  bool           `json:"syslog_enabled"`  // Enable syslog output
	SyslogAddress  string         `json:"syslog_address"`  // Address for remote syslog (e.g., "192.168.1.100:514" or "/dev/log")
	SyslogNetwork  string         `json:"syslog_network"`  // Network type: "udp", "tcp", "unix", or "" for local
	SyslogFacility SyslogFacility `json:"syslog_facility"` // Syslog facility (e.g., LOG_LOCAL0)
	SyslogTag      string         `json:"syslog_tag"`      // Syslog tag/app name (default: "quicbaas")
	SyslogHostname string         `json:"syslog_hostname"` // Hostname for RFC 5424 messages
}

// AuditEvent represents a security audit event.
type AuditEvent struct {
	Timestamp time.Time         `json:"timestamp"`
	EventID   string            `json:"event_id"`
	UserID    string            `json:"user_id"`
	Username  string            `json:"username"`
	Action    string            `json:"action"`
	Resource  string            `json:"resource"`
	Result    string            `json:"result"` // success, failure, denied
	Details   map[string]string `json:"details"`
	IPAddress string            `json:"ip_address"`
	NodeID    string            `json:"node_id"`
	ShardID   uint64            `json:"shard_id,omitempty"`
	Signature string            `json:"signature,omitempty"`
	ErrorMsg  string            `json:"error_msg,omitempty"`
}

// Audit action types.
const (
	ActionAuthenticate = "authenticate"
	ActionAuthorize    = "authorize"
	ActionRead         = "read"
	ActionWrite        = "write"
	ActionDelete       = "delete"
	ActionBackup       = "backup"
	ActionRestore      = "restore"
	ActionClusterJoin  = "cluster.join"
	ActionClusterLeave = "cluster.leave"
	ActionUserCreate   = "user.create"
	ActionUserDelete   = "user.delete"
	ActionRoleCreate   = "role.create"
	ActionRoleUpdate   = "role.update"
	ActionKeyRotate    = "key.rotate"
	ActionKeyGenerate  = "key.generate"
	ActionKeyExport    = "key.export"
	ActionKeyImport    = "key.import"
	ActionKeyDelete    = "key.delete"
	ActionConfigChange = "config.change"
	ActionSchemaChange = "schema.change"

	// Tenant-related actions.
	ActionTenantSwitch       = "tenant.switch"
	ActionCrossTenantRead    = "cross_tenant.read"
	ActionCrossTenantWrite   = "cross_tenant.write"
	ActionCrossTenantDelete  = "cross_tenant.delete"
	ActionTenantDEKCreate    = "tenant.dek.create"
	ActionTenantDEKRotate    = "tenant.dek.rotate"
	ActionTenantDEKDelete    = "tenant.dek.delete"
	ActionTenantAccessDenied = "tenant.access.denied"
)

// Audit result types.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
	ResultDenied  = "denied"
)

// NewAuditLogger creates a new audit logger.
func NewAuditLogger(cfg *AuditConfig, log *logger.Logger) (*AuditLogger, error) {
	if cfg == nil {
		return nil, &AuditError{
			Code:    ErrCodeAuditConfigNil,
			Message: "audit config cannot be nil",
		}
	}

	// Set defaults
	if cfg.MaxSize == 0 {
		cfg.MaxSize = 100 // 100 MB
	}
	if cfg.MaxBackups == 0 {
		cfg.MaxBackups = 10
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 1024
	}
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	// Set default compression level if compression is enabled but level not specified
	if cfg.Compress && cfg.CompressionLevel == 0 {
		cfg.CompressionLevel = CompressionLevelDefault
	}

	al := &AuditLogger{
		config:   cfg,
		logger:   log,
		shutdown: make(chan struct{}),
	}

	if !cfg.Enabled {
		log.Info("Audit logging is disabled")
		al.writer = io.Discard
		return al, nil
	}

	// Setup signing key for tamper-proof logging
	if cfg.SignLogs {
		if cfg.SignKey == "" {
			return nil, &AuditError{
				Code:    ErrCodeAuditSignKeyRequired,
				Message: "sign_key is required when sign_logs is enabled",
			}
		}
		al.signKey = []byte(cfg.SignKey)
	}

	// Create audit log file
	if cfg.File != "" {
		if err := al.openLogFile(); err != nil {
			return nil, &AuditError{
				Code:    ErrCodeAuditFileOpen,
				Message: "failed to open audit log file",
				Cause:   err,
			}
		}
		al.writer = al.file
	} else {
		al.writer = os.Stdout
	}

	// Initialize SIEM exporter if enabled
	if cfg.SIEM {
		siemCfg := cfg.SIEMConfig
		if siemCfg == nil {
			// Create config from legacy fields for backward compatibility
			siemCfg = &SIEMConfig{
				Enabled:  true,
				Endpoint: cfg.SIEMEndpoint,
				Format:   SIEMFormatJSON,
			}
		}

		exporter, err := NewSIEMExporter(siemCfg, log)
		if err != nil {
			return nil, &AuditError{
				Code:    ErrCodeAuditFileOpen,
				Message: "failed to create SIEM exporter",
				Cause:   err,
			}
		}
		al.siemExporter = exporter
		exporter.Start()
	}

	// Initialize syslog exporter if enabled
	if cfg.SyslogEnabled {
		syslogCfg := &SyslogExporterConfig{
			Address:  cfg.SyslogAddress,
			Network:  cfg.SyslogNetwork,
			Facility: cfg.SyslogFacility,
			Tag:      cfg.SyslogTag,
			Hostname: cfg.SyslogHostname,
		}
		exporter, err := NewSyslogExporter(syslogCfg, log)
		if err != nil {
			// Log warning but don't fail - syslog is optional
			log.WithError(err).Warn("Failed to initialize syslog exporter, continuing without syslog")
		} else {
			al.syslogExporter = exporter
			log.Infof("Syslog exporter initialized: address=%s network=%s facility=%d tag=%s",
				cfg.SyslogAddress, cfg.SyslogNetwork, cfg.SyslogFacility, cfg.SyslogTag)
		}
	}

	// Start auto-flush routine
	go al.autoFlush()

	// Start the single rotation watcher for the lifetime of this logger.
	go al.checkRotation()

	log.Infof("Audit logger initialized: file=%s sign_logs=%v compress=%v compression_level=%d siem=%v syslog=%v",
		cfg.File, cfg.SignLogs, cfg.Compress, cfg.CompressionLevel, cfg.SIEM, cfg.SyslogEnabled)
	return al, nil
}

// Log records an audit event.
func (al *AuditLogger) Log(ctx context.Context, event *AuditEvent) error {
	if !al.config.Enabled {
		return nil
	}

	al.mu.Lock()
	defer al.mu.Unlock()

	// Set timestamp if not already set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Generate event ID if not set
	if event.EventID == "" {
		event.EventID = generateEventID()
	}

	// Sign the event for tamper-proof logging
	if al.config.SignLogs {
		sig, err := al.signEvent(event)
		if err != nil {
			return &AuditError{
				Code:    ErrCodeAuditEventSign,
				Message: "failed to sign audit event",
				Cause:   err,
			}
		}
		event.Signature = sig
	}

	// Marshal to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return &AuditError{
			Code:    ErrCodeAuditEventMarshal,
			Message: "failed to marshal audit event",
			Cause:   err,
		}
	}

	// Write to audit log
	if _, err := al.writer.Write(append(data, '\n')); err != nil {
		return &AuditError{
			Code:    ErrCodeAuditEventWrite,
			Message: "failed to write audit event",
			Cause:   err,
		}
	}

	// Send to legacy syslog output
	if al.config.Syslog {
		go al.sendToSyslog(event)
	}

	// Send to SIEM exporter if configured
	if al.siemExporter != nil {
		go func(e *AuditEvent) {
			if err := al.siemExporter.Send(e); err != nil {
				al.logger.WithError(err).Error("Failed to send event to SIEM")
			}
		}(event)
	}

	// Send to syslog exporter if configured
	if al.syslogExporter != nil {
		go func(e *AuditEvent) {
			if err := al.syslogExporter.Export(e); err != nil {
				al.logger.WithError(err).Warn("Failed to export audit event to syslog")
			}
		}(event)
	}

	return nil
}

// LogAuthSuccess logs a successful authentication event.
func (al *AuditLogger) LogAuthSuccess(ctx context.Context, username, userID, ipAddr string) error {
	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    ActionAuthenticate,
		Resource:  "auth",
		Result:    ResultSuccess,
		IPAddress: ipAddr,
		Details: map[string]string{
			"method": "jwt",
		},
	})
}

// LogAuthFailure logs a failed authentication event.
func (al *AuditLogger) LogAuthFailure(ctx context.Context, username, ipAddr, reason string) error {
	return al.Log(ctx, &AuditEvent{
		Username:  username,
		Action:    ActionAuthenticate,
		Resource:  "auth",
		Result:    ResultFailure,
		IPAddress: ipAddr,
		ErrorMsg:  reason,
		Details: map[string]string{
			"method": "jwt",
		},
	})
}

// LogAuthzDenied logs an authorization denial.
func (al *AuditLogger) LogAuthzDenied(ctx context.Context, username, userID, resource, action, ipAddr string) error {
	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    ActionAuthorize,
		Resource:  resource,
		Result:    ResultDenied,
		IPAddress: ipAddr,
		Details: map[string]string{
			"requested_action": action,
		},
	})
}

// LogDataAccess logs data access operations.
func (al *AuditLogger) LogDataAccess(ctx context.Context, username, userID, operation, key, ipAddr string, success bool) error {
	result := ResultSuccess
	if !success {
		result = ResultFailure
	}

	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    operation,
		Resource:  "keys",
		Result:    result,
		IPAddress: ipAddr,
		Details: map[string]string{
			"key": key,
		},
	})
}

// LogClusterChange logs cluster membership changes.
func (al *AuditLogger) LogClusterChange(ctx context.Context, action string, nodeID uint64, details map[string]string) error {
	return al.Log(ctx, &AuditEvent{
		Username: "system",
		Action:   action,
		Resource: "cluster",
		Result:   ResultSuccess,
		NodeID:   fmt.Sprintf("%d", nodeID),
		Details:  details,
	})
}

// LogBackupOperation logs backup/restore operations.
func (al *AuditLogger) LogBackupOperation(ctx context.Context, username, userID, operation string, details map[string]string) error {
	return al.Log(ctx, &AuditEvent{
		UserID:   userID,
		Username: username,
		Action:   operation,
		Resource: "backups",
		Result:   ResultSuccess,
		Details:  details,
	})
}

// LogKeyRotation logs encryption key rotation events.
func (al *AuditLogger) LogKeyRotation(ctx context.Context, oldVersion, newVersion int) error {
	return al.Log(ctx, &AuditEvent{
		Username: "system",
		Action:   ActionKeyRotate,
		Resource: "encryption",
		Result:   ResultSuccess,
		Details: map[string]string{
			"old_version": fmt.Sprintf("%d", oldVersion),
			"new_version": fmt.Sprintf("%d", newVersion),
		},
	})
}

// LogKeyExport logs KMS key export operations.
// This is a security-sensitive event that must be audited.
func (al *AuditLogger) LogKeyExport(ctx context.Context, username, userID, keyID, algorithm, wrappingKeyID, wrappingAlgorithm, ipAddr string, success bool) error {
	result := ResultSuccess
	if !success {
		result = ResultFailure
	}

	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    ActionKeyExport,
		Resource:  "kms",
		Result:    result,
		IPAddress: ipAddr,
		Details: map[string]string{
			"key_id":             keyID,
			"algorithm":          algorithm,
			"wrapping_key_id":    wrappingKeyID,
			"wrapping_algorithm": wrappingAlgorithm,
		},
	})
}

// LogKeyGenerate logs KMS key generation operations.
func (al *AuditLogger) LogKeyGenerate(ctx context.Context, username, userID, keyID, keyName, algorithm, ipAddr string, success bool) error {
	result := ResultSuccess
	if !success {
		result = ResultFailure
	}

	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    ActionKeyGenerate,
		Resource:  "kms",
		Result:    result,
		IPAddress: ipAddr,
		Details: map[string]string{
			"key_id":    keyID,
			"key_name":  keyName,
			"algorithm": algorithm,
		},
	})
}

// LogKeyImport logs KMS key import operations.
func (al *AuditLogger) LogKeyImport(ctx context.Context, username, userID, keyID, keyName, algorithm, ipAddr string, success bool) error {
	result := ResultSuccess
	if !success {
		result = ResultFailure
	}

	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    ActionKeyImport,
		Resource:  "kms",
		Result:    result,
		IPAddress: ipAddr,
		Details: map[string]string{
			"key_id":    keyID,
			"key_name":  keyName,
			"algorithm": algorithm,
		},
	})
}

// LogKeyDelete logs KMS key deletion operations.
func (al *AuditLogger) LogKeyDeletion(ctx context.Context, username, userID, keyID, ipAddr string, success bool) error {
	result := ResultSuccess
	if !success {
		result = ResultFailure
	}

	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    ActionKeyDelete,
		Resource:  "kms",
		Result:    result,
		IPAddress: ipAddr,
		Details: map[string]string{
			"key_id": keyID,
		},
	})
}

// LogTenantSwitch logs when a SuperAdmin switches tenant context.
// This is a security-sensitive operation that must be audited.
func (al *AuditLogger) LogTenantSwitch(ctx context.Context, username, userID, originalTenant, targetTenant, justification, assignmentID, ipAddr string) error {
	details := map[string]string{
		"original_tenant": originalTenant,
		"target_tenant":   targetTenant,
		"justification":   justification,
	}
	if assignmentID != "" {
		details["assignment_id"] = assignmentID
	}

	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    ActionTenantSwitch,
		Resource:  "tenant",
		Result:    ResultSuccess,
		IPAddress: ipAddr,
		Details:   details,
	})
}

// LogCrossTenantRead logs when data is read across tenants.
func (al *AuditLogger) LogCrossTenantRead(ctx context.Context, username, userID, originalTenant, targetTenant, resource, justification, ipAddr string) error {
	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    ActionCrossTenantRead,
		Resource:  resource,
		Result:    ResultSuccess,
		IPAddress: ipAddr,
		Details: map[string]string{
			"original_tenant": originalTenant,
			"target_tenant":   targetTenant,
			"justification":   justification,
		},
	})
}

// LogCrossTenantWrite logs when data is written across tenants.
func (al *AuditLogger) LogCrossTenantWrite(ctx context.Context, username, userID, originalTenant, targetTenant, resource, justification, ipAddr string) error {
	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    ActionCrossTenantWrite,
		Resource:  resource,
		Result:    ResultSuccess,
		IPAddress: ipAddr,
		Details: map[string]string{
			"original_tenant": originalTenant,
			"target_tenant":   targetTenant,
			"justification":   justification,
		},
	})
}

// LogCrossTenantDelete logs when data is deleted across tenants.
func (al *AuditLogger) LogCrossTenantDelete(ctx context.Context, username, userID, originalTenant, targetTenant, resource, justification, ipAddr string) error {
	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    ActionCrossTenantDelete,
		Resource:  resource,
		Result:    ResultSuccess,
		IPAddress: ipAddr,
		Details: map[string]string{
			"original_tenant": originalTenant,
			"target_tenant":   targetTenant,
			"justification":   justification,
		},
	})
}

// LogTenantAccessDenied logs when cross-tenant access is denied.
func (al *AuditLogger) LogTenantAccessDenied(ctx context.Context, username, userID, requestingTenant, targetTenant, resource, ipAddr, reason string) error {
	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    ActionTenantAccessDenied,
		Resource:  resource,
		Result:    ResultDenied,
		IPAddress: ipAddr,
		ErrorMsg:  reason,
		Details: map[string]string{
			"requesting_tenant": requestingTenant,
			"target_tenant":     targetTenant,
		},
	})
}

// LogTenantDEKOperation logs tenant DEK operations.
func (al *AuditLogger) LogTenantDEKOperation(ctx context.Context, action, tenantID, username, userID, ipAddr string, success bool) error {
	result := ResultSuccess
	if !success {
		result = ResultFailure
	}

	return al.Log(ctx, &AuditEvent{
		UserID:    userID,
		Username:  username,
		Action:    action,
		Resource:  "tenant.dek",
		Result:    result,
		IPAddress: ipAddr,
		Details: map[string]string{
			"tenant_id": tenantID,
		},
	})
}

// signEvent creates an HMAC signature for the audit event.
func (al *AuditLogger) signEvent(event *AuditEvent) (string, error) {
	// Create canonical representation for signing
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s",
		event.Timestamp.Format(time.RFC3339Nano),
		event.Username,
		event.Action,
		event.Resource,
		event.Result,
		event.IPAddress)

	h := hmac.New(sha256.New, al.signKey)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifySignature verifies the HMAC signature of an audit event.
func (al *AuditLogger) VerifySignature(event *AuditEvent) bool {
	if !al.config.SignLogs || event.Signature == "" {
		return true // Not signing logs
	}

	expectedSig, err := al.signEvent(event)
	if err != nil {
		return false
	}

	return hmac.Equal([]byte(expectedSig), []byte(event.Signature))
}

// openLogFile opens or creates the audit log file.
func (al *AuditLogger) openLogFile() error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(al.config.File)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return &AuditError{
			Code:    ErrCodeAuditDirCreate,
			Message: "failed to create audit log directory",
			Cause:   err,
		}
	}

	// Open file with append mode
	file, err := os.OpenFile(al.config.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return &AuditError{
			Code:    ErrCodeAuditFileOpen,
			Message: "failed to open audit log file",
			Cause:   err,
		}
	}

	al.file = file

	return nil
}

// checkRotation periodically rotates the audit log once it exceeds the
// configured maximum size. It runs until the logger is closed.
//
// Exactly one instance runs per logger; it is started by NewAuditLogger and
// must never be started from openLogFile, which runs again on every rotation.
func (al *AuditLogger) checkRotation() {
	if al.config.File == "" {
		return
	}

	al.rotationWatchers.Add(1)
	defer al.rotationWatchers.Add(-1)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			al.mu.Lock()
			if err := al.rotateIfNeeded(); err != nil {
				al.logger.WithError(err).Error("Failed to rotate audit log")
			}
			al.mu.Unlock()
		case <-al.shutdown:
			return
		}
	}
}

// rotateIfNeeded rotates the log file if it exceeds max size.
func (al *AuditLogger) rotateIfNeeded() error {
	if al.file == nil {
		return nil
	}

	info, err := al.file.Stat()
	if err != nil {
		return err
	}

	maxBytes := al.config.MaxSize * 1024 * 1024
	if info.Size() < maxBytes {
		return nil
	}

	// Close current file
	_ = al.file.Close() // Ignore close error during rotation

	// Rotate files
	timestamp := time.Now().Format("20060102-150405")
	backupFile := fmt.Sprintf("%s.%s", al.config.File, timestamp)

	if err := os.Rename(al.config.File, backupFile); err != nil {
		return &AuditError{
			Code:    ErrCodeAuditFileRotate,
			Message: "failed to rotate audit log",
			Cause:   err,
		}
	}

	// Compress if enabled - runs in background goroutine
	if al.config.Compress {
		go al.compressFileAsync(backupFile, al.config.CompressionLevel)
	}

	// Clean up old backups
	go al.cleanupOldBackups()

	// Open new file
	return al.openLogFile()
}

// compressFileAsync compresses a file using gzip in a background goroutine.
// It creates a new .gz file and removes the original file on success.
func (al *AuditLogger) compressFileAsync(filename string, level int) {
	if err := al.compressFile(filename, level); err != nil {
		al.logger.WithError(err).Errorf("Failed to compress audit log file: %s", filename)
	}
}

// compressFile compresses a file using gzip with the specified compression level.
// It creates a new .gz file and removes the original file on success.
func (al *AuditLogger) compressFile(filename string, level int) error {
	// Validate compression level
	if level < gzip.HuffmanOnly || level > gzip.BestCompression {
		level = gzip.DefaultCompression
	}

	// Open source file for reading
	srcFile, err := os.Open(filename)
	if err != nil {
		return &AuditError{
			Code:    ErrCodeAuditCompressionOpen,
			Message: "failed to open source file for compression",
			Cause:   err,
		}
	}
	defer srcFile.Close()

	// Create destination file with .gz extension
	gzFilename := filename + ".gz"
	dstFile, err := os.OpenFile(gzFilename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return &AuditError{
			Code:    ErrCodeAuditCompressionOpen,
			Message: "failed to create compressed file",
			Cause:   err,
		}
	}

	// Create gzip writer with specified compression level
	gzWriter, err := gzip.NewWriterLevel(dstFile, level)
	if err != nil {
		dstFile.Close()       // #nosec G104 -- cleanup on error path - primary error returned
		os.Remove(gzFilename) // #nosec G104 -- cleanup on error path - primary error returned
		return &AuditError{
			Code:    ErrCodeAuditCompressionWrite,
			Message: "failed to create gzip writer",
			Cause:   err,
		}
	}

	// Set gzip header metadata
	gzWriter.Name = filepath.Base(filename)
	gzWriter.ModTime = time.Now()
	gzWriter.Comment = "QuicBaaS audit log"

	// Copy data through gzip writer using a buffer for efficiency
	buf := make([]byte, 64*1024) // 64KB buffer
	if _, err := io.CopyBuffer(gzWriter, srcFile, buf); err != nil {
		gzWriter.Close()      // #nosec G104 -- cleanup on error path - primary error returned
		dstFile.Close()       // #nosec G104 -- cleanup on error path - primary error returned
		os.Remove(gzFilename) // #nosec G104 -- cleanup on error path - primary error returned
		return &AuditError{
			Code:    ErrCodeAuditCompressionRead,
			Message: "failed to read source file during compression",
			Cause:   err,
		}
	}

	// Close gzip writer to flush remaining data
	if err := gzWriter.Close(); err != nil {
		dstFile.Close()       // #nosec G104 -- cleanup on error path - primary error returned
		os.Remove(gzFilename) // #nosec G104 -- cleanup on error path - primary error returned
		return &AuditError{
			Code:    ErrCodeAuditCompressionClose,
			Message: "failed to close gzip writer",
			Cause:   err,
		}
	}

	// Close destination file
	if err := dstFile.Close(); err != nil {
		os.Remove(gzFilename) // #nosec G104 -- cleanup on error path - primary error returned
		return &AuditError{
			Code:    ErrCodeAuditCompressionClose,
			Message: "failed to close compressed file",
			Cause:   err,
		}
	}

	// Remove original file only after successful compression
	if err := os.Remove(filename); err != nil {
		// Log but don't fail - compressed file was created successfully
		al.logger.WithError(err).Warnf("Failed to remove original file after compression: %s", filename)
	}

	al.logger.Infof("Compressed audit log: %s -> %s", filename, gzFilename)
	return nil
}

// cleanupOldBackups removes old backup files exceeding the configured MaxBackups limit.
// It lists all backup files matching the audit log base name pattern, sorts them by
// modification time (oldest first), and removes the oldest backups that exceed the limit.
// Both uncompressed (.{timestamp}) and compressed (.{timestamp}.gz) backup files are
// considered. Errors are logged but do not halt the cleanup process.
func (al *AuditLogger) cleanupOldBackups() {
	if al.config.MaxBackups <= 0 {
		return
	}

	if al.config.File == "" {
		return
	}

	dir := filepath.Dir(al.config.File)
	base := filepath.Base(al.config.File)

	entries, err := os.ReadDir(dir)
	if err != nil {
		al.logger.WithError(err).Errorf("Failed to list backup directory: %s", dir)
		return
	}

	// Collect backup files that match the pattern: {base}.{timestamp} or {base}.{timestamp}.gz
	type backupEntry struct {
		path    string
		modTime time.Time
	}

	backups := make([]backupEntry, 0, len(entries))
	prefix := base + "."

	for _, entry := range entries {
		name := entry.Name()

		// Skip the current log file itself
		if name == base {
			continue
		}

		// Match backup files: must start with "{base}." and have a timestamp suffix
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		suffix := name[len(prefix):]

		// Validate timestamp format: YYYYMMDD-HHMMSS or YYYYMMDD-HHMMSS.gz
		tsStr := strings.TrimSuffix(suffix, ".gz")
		if len(tsStr) != 15 { // "20060102-150405" is 15 chars
			continue
		}
		if _, parseErr := time.Parse("20060102-150405", tsStr); parseErr != nil {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			al.logger.WithError(infoErr).Warnf("Failed to stat backup file: %s", name)
			continue
		}

		backups = append(backups, backupEntry{
			path:    filepath.Join(dir, name),
			modTime: info.ModTime(),
		})
	}

	// If within the limit, nothing to clean up
	if len(backups) <= al.config.MaxBackups {
		return
	}

	// Sort by modification time ascending (oldest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].modTime.Before(backups[j].modTime)
	})

	// Remove oldest backups exceeding the limit
	removeCount := len(backups) - al.config.MaxBackups
	for i := range removeCount {
		if removeErr := os.Remove(backups[i].path); removeErr != nil {
			al.logger.WithError(removeErr).Warnf("Failed to remove old backup: %s", backups[i].path)
			continue
		}
		al.logger.Infof("Removed old audit backup: %s", backups[i].path)
	}
}

// autoFlush periodically flushes buffered logs.
func (al *AuditLogger) autoFlush() {
	ticker := time.NewTicker(al.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_ = al.Flush() // Ignore flush error during periodic flush
		case <-al.shutdown:
			return
		}
	}
}

// Flush flushes any buffered audit logs.
func (al *AuditLogger) Flush() error {
	al.mu.Lock()
	defer al.mu.Unlock()

	if al.file != nil {
		return al.file.Sync()
	}
	return nil
}

// sendToSyslog sends audit event to syslog using the legacy configuration.
// This method is kept for backward compatibility with the old Syslog/SyslogAddr config.
func (al *AuditLogger) sendToSyslog(event *AuditEvent) {
	// If the new syslog exporter is configured, use it
	if al.syslogExporter != nil {
		if err := al.syslogExporter.Export(event); err != nil {
			al.logger.WithError(err).Warn("Failed to send audit event to syslog")
		}
		return
	}

	// Legacy implementation: try to connect to syslog at configured address
	if al.config.SyslogAddr == "" {
		return
	}

	// Create a temporary syslog connection for legacy support
	priority := syslog.Priority(syslog.LOG_INFO | syslog.LOG_LOCAL0)
	writer, err := syslog.Dial("udp", al.config.SyslogAddr, priority, "quicbaas")
	if err != nil {
		al.logger.WithError(err).Warn("Failed to connect to syslog")
		return
	}
	defer writer.Close()

	msg := fmt.Sprintf("QuicBaaS Audit: action=%s resource=%s result=%s user=%s ip=%s",
		event.Action, event.Resource, event.Result, event.Username, event.IPAddress)

	if err := writer.Info(msg); err != nil {
		al.logger.WithError(err).Warn("Failed to write to syslog")
	}
}

// GetSIEMExporter returns the SIEM exporter for external access.
func (al *AuditLogger) GetSIEMExporter() *SIEMExporter {
	return al.siemExporter
}

// GetSyslogExporter returns the syslog exporter for external access.
func (al *AuditLogger) GetSyslogExporter() *SyslogExporter {
	return al.syslogExporter
}

// Close closes the audit logger and flushes any buffered data.
func (al *AuditLogger) Close() error {
	close(al.shutdown)

	al.mu.Lock()
	defer al.mu.Unlock()

	// Close SIEM exporter
	if al.siemExporter != nil {
		if err := al.siemExporter.Close(); err != nil {
			al.logger.WithError(err).Error("Failed to close SIEM exporter")
		}
	}

	// Close syslog exporter
	if al.syslogExporter != nil {
		if err := al.syslogExporter.Close(); err != nil {
			al.logger.WithError(err).Warn("Failed to close syslog exporter")
		}
	}

	if al.file != nil {
		_ = al.file.Sync() // Ignore sync error during close
		return al.file.Close()
	}

	return nil
}

// generateEventID generates a unique event ID.
func generateEventID() string {
	return fmt.Sprintf("evt-%d", time.Now().UnixNano())
}
