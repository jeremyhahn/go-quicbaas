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

package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatabaseNotFoundError(t *testing.T) {
	err := &DatabaseNotFoundError{Name: "mydb"}
	assert.Equal(t, `service: database not found: "mydb"`, err.Error())
}

func TestDatabaseNotFoundError_EmptyName(t *testing.T) {
	err := &DatabaseNotFoundError{Name: ""}
	assert.Equal(t, `service: database not found: ""`, err.Error())
}

func TestDatabaseCreateError(t *testing.T) {
	cause := &InvalidRequestError{Detail: "bad config"}
	err := &DatabaseCreateError{Name: "testdb", Err: cause}
	assert.Equal(t, `service: failed to create database "testdb": service: invalid request: bad config`, err.Error())
}

func TestDatabaseCreateError_Unwrap(t *testing.T) {
	cause := &InvalidRequestError{Detail: "nil"}
	err := &DatabaseCreateError{Name: "db", Err: cause}
	var reqErr *InvalidRequestError
	require.True(t, errors.As(err, &reqErr))
	assert.Equal(t, "nil", reqErr.Detail)
}

func TestDatabaseDropError(t *testing.T) {
	cause := &DatabaseNotFoundError{Name: "gone"}
	err := &DatabaseDropError{Name: "gone", Err: cause}
	assert.Contains(t, err.Error(), "failed to drop database")
	assert.Contains(t, err.Error(), `"gone"`)
}

func TestDatabaseDropError_Unwrap(t *testing.T) {
	cause := &DatabaseNotFoundError{Name: "x"}
	err := &DatabaseDropError{Name: "x", Err: cause}
	var nfErr *DatabaseNotFoundError
	require.True(t, errors.As(err, &nfErr))
	assert.Equal(t, "x", nfErr.Name)
}

func TestDatabaseUpdateError(t *testing.T) {
	cause := &DatabaseNotFoundError{Name: "db"}
	err := &DatabaseUpdateError{Name: "db", Operation: "raft", Err: cause}
	assert.Contains(t, err.Error(), "failed to update raft")
	assert.Contains(t, err.Error(), `"db"`)
}

func TestDatabaseUpdateError_Unwrap(t *testing.T) {
	cause := &DatabaseNotFoundError{Name: "db"}
	err := &DatabaseUpdateError{Name: "db", Operation: "protocol", Err: cause}
	var nfErr *DatabaseNotFoundError
	require.True(t, errors.As(err, &nfErr))
}

func TestDatabaseServiceNilError(t *testing.T) {
	err := &DatabaseServiceNilError{}
	assert.Equal(t, "service: qrdb database servicer is nil", err.Error())
}

func TestInvalidRequestError(t *testing.T) {
	err := &InvalidRequestError{Detail: "request is nil"}
	assert.Equal(t, "service: invalid request: request is nil", err.Error())
}

func TestInvalidRequestError_EmptyDetail(t *testing.T) {
	err := &InvalidRequestError{Detail: ""}
	assert.Equal(t, "service: invalid request: ", err.Error())
}
