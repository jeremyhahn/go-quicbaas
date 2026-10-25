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
	stderrors "errors"
	"fmt"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Sentinel errors for gRPC client operations.
var (
	// ErrRequestNil indicates a nil request was provided.
	ErrRequestNil = stderrors.New("request cannot be nil")

	// ErrSealNotInitialized indicates the seal manager is not initialized.
	ErrSealNotInitialized = stderrors.New("not initialized")

	// ErrBarrierSealed indicates the barrier is sealed.
	ErrBarrierSealed = stderrors.New("barrier is sealed")

	// ErrPeerCredUnsupported indicates SO_PEERCRED is not supported on this platform.
	ErrPeerCredUnsupported = stderrors.New("SO_PEERCRED is only supported on Linux")
)

// FromGRPCError converts a gRPC status code to a QuicBaaS error code.
func FromGRPCError(err error) (errors.ErrorCode, bool) {
	s, ok := status.FromError(err)
	if !ok {
		return errors.ErrUnknown, false
	}

	return service.StatusErrorCode(s), true
}

// ToGRPCError converts a QuicBaaS error to a gRPC status error.
func ToGRPCError(err error) error {
	if err == nil {
		return nil
	}

	// Check if it's already a gRPC status error
	if _, ok := status.FromError(err); ok {
		return err
	}

	// Extract QuicBaaS error code
	var errCode errors.ErrorCode
	var dbErr *errors.QBError
	if stderrors.As(err, &dbErr) {
		errCode = dbErr.Code
	} else {
		errCode = errors.ErrInternal
	}

	grpcCode := errorCodeToGRPCCode(errCode)
	return status.Error(grpcCode, err.Error())
}

// convertError converts a gRPC status error to a QBError.
// This is used by the client to convert gRPC errors back to application errors.
func convertError(err error) error {
	if err == nil {
		return nil
	}

	s, ok := status.FromError(err)
	if !ok {
		// Not a gRPC error, wrap it
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "grpc",
			Err:  err,
		}
	}

	errCode := service.StatusErrorCode(s)
	return &errors.QBError{
		Code: errCode,
		Op:   "grpc",
		Err:  stderrors.New(s.Message()),
	}
}

// UserIDError reports a wire user id that is not a non-zero decimal uint64.
// Proto messages carry the iam Snowflake user id as a string; this is the
// edge where it becomes the uint64 the identity store keys on.
type UserIDError struct {
	Field string
	Value string
	Err   error
}

func (e *UserIDError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("%s: user id %q must be non-zero", e.Field, e.Value)
	}
	return fmt.Sprintf("%s: user id %q is not a decimal uint64: %v", e.Field, e.Value, e.Err)
}

func (e *UserIDError) Unwrap() error { return e.Err }

// GRPCStatus maps the error to InvalidArgument for gRPC callers.
func (e *UserIDError) GRPCStatus() *status.Status {
	return status.New(codes.InvalidArgument, e.Error())
}

// UnscopedKeyImportError reports a tenant-scoped ImportKey call on a KMS
// server that has no tenant-aware KMS service. Only the service namespaces a
// key under tenant/{tenantID}/, so the import is refused rather than written
// into the shared key namespace.
type UnscopedKeyImportError struct {
	TenantID string
}

func (e *UnscopedKeyImportError) Error() string {
	return fmt.Sprintf("tenant %q: key import requires the tenant-aware KMS service", e.TenantID)
}

// GRPCStatus maps the error to FailedPrecondition for gRPC callers.
func (e *UnscopedKeyImportError) GRPCStatus() *status.Status {
	return status.New(codes.FailedPrecondition, e.Error())
}

// PolicySpecYAMLError reports a policy spec_yaml document that does not parse.
type PolicySpecYAMLError struct {
	Err error
}

func (e *PolicySpecYAMLError) Error() string {
	return fmt.Sprintf("invalid spec_yaml: %v", e.Err)
}

func (e *PolicySpecYAMLError) Unwrap() error { return e.Err }

// GRPCStatus maps the error to InvalidArgument for gRPC callers.
func (e *PolicySpecYAMLError) GRPCStatus() *status.Status {
	return status.New(codes.InvalidArgument, e.Error())
}

// parseUserID converts a wire user id into the iam uint64 id. Zero is
// refused because it means "not created yet" in the iam model.
func parseUserID(field, value string) (uint64, error) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, &UserIDError{Field: field, Value: value, Err: err}
	}
	if id == 0 {
		return 0, &UserIDError{Field: field, Value: value}
	}
	return id, nil
}

// parseOptionalUserID is parseUserID for fields where "" means unset; it
// returns zero for "" and refuses any other non-id value.
func parseOptionalUserID(field, value string) (uint64, error) {
	if value == "" {
		return 0, nil
	}
	return parseUserID(field, value)
}

// formatUserID renders an iam uint64 user id for a proto string field.
// Zero renders as "" so an unset id stays unset on the wire.
func formatUserID(id uint64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}

// errorCodeToGRPCCode maps QuicBaaS error codes to gRPC status codes.
func errorCodeToGRPCCode(code errors.ErrorCode) codes.Code {
	if grpcCode, ok := grpcCodes[code]; ok {
		return grpcCode
	}
	return codes.Internal
}

// grpcCodes maps every pkg/errors code to the gRPC status code a client
// receives. Codes a client can act on follow their REST status (400 to
// InvalidArgument, 403 to PermissionDenied, 429 to ResourceExhausted, ...);
// server-side faults the client cannot correct map to Internal explicitly, so
// that a code missing from this map is a defect TestErrorCodeToGRPCCode
// catches rather than a silent fall-through to Internal.
var grpcCodes = map[errors.ErrorCode]codes.Code{
	errors.ErrUnknown:                 codes.Unknown,
	errors.ErrNotFound:                codes.NotFound,
	errors.ErrConflict:                codes.AlreadyExists,
	errors.ErrTimeout:                 codes.DeadlineExceeded,
	errors.ErrInternal:                codes.Internal,
	errors.ErrInvalidInput:            codes.InvalidArgument,
	errors.ErrUnauthorized:            codes.Unauthenticated,
	errors.ErrForbidden:               codes.PermissionDenied,
	errors.ErrUnavailable:             codes.Unavailable,
	errors.ErrStorage:                 codes.Internal,
	errors.ErrRaft:                    codes.Internal,
	errors.ErrEncryption:              codes.Internal,
	errors.ErrDecryption:              codes.Internal,
	errors.ErrValidation:              codes.InvalidArgument,
	errors.ErrConfiguration:           codes.Internal,
	errors.ErrCanceled:                codes.Canceled,
	errors.ErrDeadlineExceeded:        codes.DeadlineExceeded,
	errors.ErrAlreadyExists:           codes.AlreadyExists,
	errors.ErrResourceExhausted:       codes.ResourceExhausted,
	errors.ErrPreconditionFailed:      codes.FailedPrecondition,
	errors.ErrAborted:                 codes.Aborted,
	errors.ErrOutOfRange:              codes.InvalidArgument,
	errors.ErrUnimplemented:           codes.Unimplemented,
	errors.ErrDataLoss:                codes.DataLoss,
	errors.ErrSnapshot:                codes.Internal,
	errors.ErrReplication:             codes.Internal,
	errors.ErrLeaderNotAvailable:      codes.Unavailable,
	errors.ErrClusterNotReady:         codes.Unavailable,
	errors.ErrQuorumLost:              codes.Internal,
	errors.ErrSplitBrain:              codes.Internal,
	errors.ErrRateLimited:             codes.ResourceExhausted,
	errors.ErrNotImplemented:          codes.Unimplemented,
	errors.ErrNotAllowed:              codes.PermissionDenied,
	errors.ErrSystemKeyspaceViolation: codes.PermissionDenied,
	errors.ErrCrossTenantAccess:       codes.PermissionDenied,
	errors.ErrMissingTenantContext:    codes.InvalidArgument,
	errors.ErrMissingJustification:    codes.InvalidArgument,
	errors.ErrMissingUserContext:      codes.InvalidArgument,
	errors.ErrCircuitOpen:             codes.Unavailable,
}
