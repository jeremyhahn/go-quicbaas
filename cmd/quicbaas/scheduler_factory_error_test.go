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

// Package main provides tests covering the schedulerClientFactory error branch
// in each scheduler run command. When the factory returns an error, each command
// should propagate it.
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// factoryError is a helper that injects a failing schedulerClientFactory,
// runs the given command, and asserts the error is propagated.
func testSchedulerFactoryError(t *testing.T, run func() error) {
	t.Helper()
	saved := schedulerClientFactory
	defer func() { schedulerClientFactory = saved }()

	schedulerClientFactory = func() (SchedulerClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "test", "factory failure")
	}

	err := run()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "factory failure")
}

func TestRunSchedulerListCommand_FactoryError(t *testing.T) {
	testSchedulerFactoryError(t, func() error {
		cmd := newSchedulerListCommand()
		return cmd.RunE(cmd, []string{})
	})
}

func TestRunSchedulerGetCommand_FactoryError(t *testing.T) {
	testSchedulerFactoryError(t, func() error {
		cmd := newSchedulerGetCommand()
		return cmd.RunE(cmd, []string{"1"})
	})
}

func TestRunSchedulerDeleteCommand_FactoryError(t *testing.T) {
	testSchedulerFactoryError(t, func() error {
		cmd := newSchedulerDeleteCommand()
		return cmd.RunE(cmd, []string{"1"})
	})
}

func TestRunSchedulerTriggerCommand_FactoryError(t *testing.T) {
	testSchedulerFactoryError(t, func() error {
		cmd := newSchedulerTriggerCommand()
		return cmd.RunE(cmd, []string{"1"})
	})
}

func TestRunSchedulerPauseCommand_FactoryError(t *testing.T) {
	testSchedulerFactoryError(t, func() error {
		cmd := newSchedulerPauseCommand()
		return cmd.RunE(cmd, []string{"1"})
	})
}

func TestRunSchedulerResumeCommand_FactoryError(t *testing.T) {
	testSchedulerFactoryError(t, func() error {
		cmd := newSchedulerResumeCommand()
		return cmd.RunE(cmd, []string{"1"})
	})
}

func TestRunSchedulerCancelCommand_FactoryError(t *testing.T) {
	testSchedulerFactoryError(t, func() error {
		cmd := newSchedulerCancelCommand()
		return cmd.RunE(cmd, []string{"1"})
	})
}

func TestRunSchedulerHistoryCommand_FactoryError(t *testing.T) {
	testSchedulerFactoryError(t, func() error {
		cmd := newSchedulerHistoryCommand()
		return cmd.RunE(cmd, []string{"1"})
	})
}

func TestRunSchedulerStatusCommand_FactoryError(t *testing.T) {
	testSchedulerFactoryError(t, func() error {
		cmd := newSchedulerStatusCommand()
		return cmd.RunE(cmd, []string{})
	})
}

// --------------------------------------------------------------------------
// createSchedulerClient unsupported transport branch
// --------------------------------------------------------------------------

func TestCreateSchedulerClient_UnsupportedTransport(t *testing.T) {
	savedTransport := transport
	defer func() { transport = savedTransport }()

	transport = "unsupported_transport_xyz"

	_, err := createSchedulerClient()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported transport")
}
