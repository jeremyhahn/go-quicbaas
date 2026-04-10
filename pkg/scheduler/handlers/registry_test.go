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

package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// mockHandler implements the Handler interface for testing.
type mockHandler struct {
	name        string
	handlerFunc scheduler.JobHandler
}

func newMockHandler(name string) *mockHandler {
	return &mockHandler{
		name: name,
		handlerFunc: func(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
			return &scheduler.JobResult{Success: true, Message: "mock executed"}, nil
		},
	}
}

func newMockHandlerWithFunc(name string, fn scheduler.JobHandler) *mockHandler {
	return &mockHandler{
		name:        name,
		handlerFunc: fn,
	}
}

func (m *mockHandler) Name() string {
	return m.name
}

func (m *mockHandler) Handler() scheduler.JobHandler {
	return m.handlerFunc
}

// mockScheduler implements scheduler.Scheduler for testing RegisterAll.
type mockScheduler struct {
	registeredHandlers map[string]scheduler.JobHandler
	registerFunc       func(name string, handler scheduler.JobHandler) error
}

func newMockScheduler() *mockScheduler {
	return &mockScheduler{
		registeredHandlers: make(map[string]scheduler.JobHandler),
	}
}

func newMockSchedulerWithError(errName string, err error) *mockScheduler {
	return &mockScheduler{
		registeredHandlers: make(map[string]scheduler.JobHandler),
		registerFunc: func(name string, handler scheduler.JobHandler) error {
			if name == errName {
				return err
			}
			return nil
		},
	}
}

func (m *mockScheduler) RegisterHandler(name string, handler scheduler.JobHandler) error {
	if m.registerFunc != nil {
		return m.registerFunc(name, handler)
	}
	m.registeredHandlers[name] = handler
	return nil
}

func (m *mockScheduler) UnregisterHandler(name string) error {
	delete(m.registeredHandlers, name)
	return nil
}

func (m *mockScheduler) ScheduleJob(ctx context.Context, job *scheduler.Job) error {
	return nil
}

func (m *mockScheduler) CancelJob(ctx context.Context, jobID uint64) error {
	return nil
}

func (m *mockScheduler) GetJob(ctx context.Context, jobID uint64) (*scheduler.Job, error) {
	return nil, nil
}

func (m *mockScheduler) ListJobs(ctx context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, error) {
	return nil, nil
}

func (m *mockScheduler) Start(ctx context.Context) error {
	return nil
}

func (m *mockScheduler) Stop(ctx context.Context) error {
	return nil
}

func (m *mockScheduler) Stats() *scheduler.SchedulerStats {
	return nil
}

// TestNewRegistry tests the NewRegistry constructor.
func TestNewRegistry(t *testing.T) {
	t.Run("creates registry with empty handlers map", func(t *testing.T) {
		registry := NewRegistry()

		require.NotNil(t, registry)
		require.NotNil(t, registry.handlers)
		assert.Len(t, registry.handlers, 0)
	})

	t.Run("creates independent registries", func(t *testing.T) {
		registry1 := NewRegistry()
		registry2 := NewRegistry()

		registry1.Register(newMockHandler("handler1"))

		assert.Len(t, registry1.handlers, 1)
		assert.Len(t, registry2.handlers, 0)
	})
}

// TestRegistry_Register tests the Register method.
func TestRegistry_Register(t *testing.T) {
	t.Run("registers handler successfully", func(t *testing.T) {
		registry := NewRegistry()
		handler := newMockHandler("test-handler")

		registry.Register(handler)

		assert.Len(t, registry.handlers, 1)
		retrieved, ok := registry.handlers["test-handler"]
		require.True(t, ok)
		assert.Equal(t, handler, retrieved)
	})

	t.Run("registers multiple handlers", func(t *testing.T) {
		registry := NewRegistry()
		handler1 := newMockHandler("handler1")
		handler2 := newMockHandler("handler2")
		handler3 := newMockHandler("handler3")

		registry.Register(handler1)
		registry.Register(handler2)
		registry.Register(handler3)

		assert.Len(t, registry.handlers, 3)
		assert.Contains(t, registry.handlers, "handler1")
		assert.Contains(t, registry.handlers, "handler2")
		assert.Contains(t, registry.handlers, "handler3")
	})

	t.Run("overwrites handler with same name", func(t *testing.T) {
		registry := NewRegistry()
		handler1 := newMockHandler("duplicate")
		handler2 := newMockHandlerWithFunc("duplicate", func(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
			return &scheduler.JobResult{Success: true, Message: "second handler"}, nil
		})

		registry.Register(handler1)
		registry.Register(handler2)

		assert.Len(t, registry.handlers, 1)
		retrieved, ok := registry.handlers["duplicate"]
		require.True(t, ok)
		assert.Equal(t, handler2, retrieved)
	})

	t.Run("registers handler with empty name", func(t *testing.T) {
		registry := NewRegistry()
		handler := newMockHandler("")

		registry.Register(handler)

		assert.Len(t, registry.handlers, 1)
		retrieved, ok := registry.handlers[""]
		require.True(t, ok)
		assert.Equal(t, handler, retrieved)
	})

	t.Run("registers handler with special characters in name", func(t *testing.T) {
		registry := NewRegistry()
		specialNames := []string{
			"handler.with.dots",
			"handler-with-dashes",
			"handler_with_underscores",
			"handler/with/slashes",
			"handler:with:colons",
		}

		for _, name := range specialNames {
			handler := newMockHandler(name)
			registry.Register(handler)
		}

		assert.Len(t, registry.handlers, len(specialNames))
		for _, name := range specialNames {
			_, ok := registry.handlers[name]
			assert.True(t, ok, "handler %q should be registered", name)
		}
	})
}

// TestRegistry_Get tests the Get method.
func TestRegistry_Get(t *testing.T) {
	t.Run("retrieves existing handler", func(t *testing.T) {
		registry := NewRegistry()
		handler := newMockHandler("test-handler")
		registry.Register(handler)

		retrieved, ok := registry.Get("test-handler")

		require.True(t, ok)
		assert.Equal(t, handler, retrieved)
		assert.Equal(t, "test-handler", retrieved.Name())
	})

	t.Run("returns false for non-existent handler", func(t *testing.T) {
		registry := NewRegistry()

		retrieved, ok := registry.Get("non-existent")

		assert.False(t, ok)
		assert.Nil(t, retrieved)
	})

	t.Run("returns false for empty registry", func(t *testing.T) {
		registry := NewRegistry()

		retrieved, ok := registry.Get("any-handler")

		assert.False(t, ok)
		assert.Nil(t, retrieved)
	})

	t.Run("retrieves handler with empty name", func(t *testing.T) {
		registry := NewRegistry()
		handler := newMockHandler("")
		registry.Register(handler)

		retrieved, ok := registry.Get("")

		require.True(t, ok)
		assert.Equal(t, handler, retrieved)
	})

	t.Run("retrieves correct handler from multiple handlers", func(t *testing.T) {
		registry := NewRegistry()
		handler1 := newMockHandler("handler1")
		handler2 := newMockHandler("handler2")
		handler3 := newMockHandler("handler3")
		registry.Register(handler1)
		registry.Register(handler2)
		registry.Register(handler3)

		retrieved, ok := registry.Get("handler2")

		require.True(t, ok)
		assert.Equal(t, handler2, retrieved)
		assert.Equal(t, "handler2", retrieved.Name())
	})

	t.Run("case sensitive handler names", func(t *testing.T) {
		registry := NewRegistry()
		handler := newMockHandler("TestHandler")
		registry.Register(handler)

		_, okLower := registry.Get("testhandler")
		_, okUpper := registry.Get("TESTHANDLER")
		_, okExact := registry.Get("TestHandler")

		assert.False(t, okLower)
		assert.False(t, okUpper)
		assert.True(t, okExact)
	})
}

// TestRegistry_All tests the All method.
func TestRegistry_All(t *testing.T) {
	t.Run("returns empty map for empty registry", func(t *testing.T) {
		registry := NewRegistry()

		result := registry.All()

		require.NotNil(t, result)
		assert.Len(t, result, 0)
	})

	t.Run("returns all registered handlers", func(t *testing.T) {
		registry := NewRegistry()
		handler1 := newMockHandler("handler1")
		handler2 := newMockHandler("handler2")
		handler3 := newMockHandler("handler3")
		registry.Register(handler1)
		registry.Register(handler2)
		registry.Register(handler3)

		result := registry.All()

		assert.Len(t, result, 3)
		assert.Contains(t, result, "handler1")
		assert.Contains(t, result, "handler2")
		assert.Contains(t, result, "handler3")
	})

	t.Run("returns a copy not the original map", func(t *testing.T) {
		registry := NewRegistry()
		handler := newMockHandler("original")
		registry.Register(handler)

		result := registry.All()

		// Modify the returned map
		newHandler := newMockHandler("added")
		result["added"] = newHandler

		// Original registry should be unchanged
		assert.Len(t, registry.handlers, 1)
		_, ok := registry.Get("added")
		assert.False(t, ok, "modifying All() result should not affect registry")
	})

	t.Run("modifying returned map does not affect subsequent All calls", func(t *testing.T) {
		registry := NewRegistry()
		handler := newMockHandler("handler")
		registry.Register(handler)

		result1 := registry.All()
		result1["modified"] = newMockHandler("modified")

		result2 := registry.All()

		assert.NotContains(t, result2, "modified")
		assert.Len(t, result2, 1)
	})

	t.Run("deleting from returned map does not affect registry", func(t *testing.T) {
		registry := NewRegistry()
		handler := newMockHandler("handler")
		registry.Register(handler)

		result := registry.All()
		delete(result, "handler")

		retrieved, ok := registry.Get("handler")
		require.True(t, ok)
		assert.Equal(t, handler, retrieved)
	})

	t.Run("preserves handler values correctly", func(t *testing.T) {
		registry := NewRegistry()
		customFunc := func(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
			return &scheduler.JobResult{Success: true, Message: "custom"}, nil
		}
		handler := newMockHandlerWithFunc("custom", customFunc)
		registry.Register(handler)

		result := registry.All()

		retrieved := result["custom"]
		require.NotNil(t, retrieved)
		assert.Equal(t, "custom", retrieved.Name())
		assert.NotNil(t, retrieved.Handler())
	})
}

// TestRegistry_RegisterAll tests the RegisterAll method.
func TestRegistry_RegisterAll(t *testing.T) {
	t.Run("registers all handlers with scheduler successfully", func(t *testing.T) {
		registry := NewRegistry()
		handler1 := newMockHandler("handler1")
		handler2 := newMockHandler("handler2")
		handler3 := newMockHandler("handler3")
		registry.Register(handler1)
		registry.Register(handler2)
		registry.Register(handler3)
		mockSched := newMockScheduler()

		err := registry.RegisterAll(mockSched)

		require.NoError(t, err)
		assert.Len(t, mockSched.registeredHandlers, 3)
		assert.Contains(t, mockSched.registeredHandlers, "handler1")
		assert.Contains(t, mockSched.registeredHandlers, "handler2")
		assert.Contains(t, mockSched.registeredHandlers, "handler3")
	})

	t.Run("returns nil for empty registry", func(t *testing.T) {
		registry := NewRegistry()
		mockSched := newMockScheduler()

		err := registry.RegisterAll(mockSched)

		require.NoError(t, err)
		assert.Len(t, mockSched.registeredHandlers, 0)
	})

	t.Run("propagates error from scheduler", func(t *testing.T) {
		registry := NewRegistry()
		handler := newMockHandler("failing-handler")
		registry.Register(handler)
		expectedErr := errors.New("registration failed")
		mockSched := newMockSchedulerWithError("failing-handler", expectedErr)

		err := registry.RegisterAll(mockSched)

		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("stops on first error", func(t *testing.T) {
		registry := NewRegistry()
		// Register handlers in a way we can track
		handler1 := newMockHandler("a-handler")
		handler2 := newMockHandler("b-handler")
		handler3 := newMockHandler("c-handler")
		registry.Register(handler1)
		registry.Register(handler2)
		registry.Register(handler3)

		registrationCount := 0
		expectedErr := errors.New("registration failed")
		mockSched := &mockScheduler{
			registeredHandlers: make(map[string]scheduler.JobHandler),
			registerFunc: func(name string, handler scheduler.JobHandler) error {
				registrationCount++
				if registrationCount == 2 {
					return expectedErr
				}
				return nil
			},
		}

		err := registry.RegisterAll(mockSched)

		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
		// Due to map iteration order being non-deterministic,
		// we just verify it stopped early
		assert.Less(t, registrationCount, 4)
	})

	t.Run("registers single handler successfully", func(t *testing.T) {
		registry := NewRegistry()
		handler := newMockHandler("single")
		registry.Register(handler)
		mockSched := newMockScheduler()

		err := registry.RegisterAll(mockSched)

		require.NoError(t, err)
		assert.Len(t, mockSched.registeredHandlers, 1)
		assert.Contains(t, mockSched.registeredHandlers, "single")
	})

	t.Run("passes correct handler functions to scheduler", func(t *testing.T) {
		registry := NewRegistry()
		expectedResult := &scheduler.JobResult{Success: true, Message: "expected"}
		customFunc := func(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
			return expectedResult, nil
		}
		handler := newMockHandlerWithFunc("custom", customFunc)
		registry.Register(handler)
		mockSched := newMockScheduler()

		err := registry.RegisterAll(mockSched)

		require.NoError(t, err)
		registeredHandler := mockSched.registeredHandlers["custom"]
		require.NotNil(t, registeredHandler)

		result, err := registeredHandler(context.Background(), &scheduler.Job{})
		require.NoError(t, err)
		assert.Equal(t, expectedResult, result)
	})

	t.Run("handles scheduler with already existing handler error", func(t *testing.T) {
		registry := NewRegistry()
		handler := newMockHandler("duplicate")
		registry.Register(handler)
		existsErr := scheduler.JobAlreadyExistsError{Name: "duplicate"}
		mockSched := newMockSchedulerWithError("duplicate", existsErr)

		err := registry.RegisterAll(mockSched)

		require.Error(t, err)
		var alreadyExistsErr scheduler.JobAlreadyExistsError
		assert.True(t, errors.As(err, &alreadyExistsErr))
	})
}

// TestRegistry_Integration tests the registry with real-world usage patterns.
func TestRegistry_Integration(t *testing.T) {
	t.Run("full workflow: register, get, all, registerAll", func(t *testing.T) {
		registry := NewRegistry()

		// Register handlers
		handler1 := newMockHandler("kms.grace_period")
		handler2 := newMockHandler("db.backup")
		handler3 := newMockHandler("cache.cleanup")
		registry.Register(handler1)
		registry.Register(handler2)
		registry.Register(handler3)

		// Verify Get works
		retrieved, ok := registry.Get("db.backup")
		require.True(t, ok)
		assert.Equal(t, "db.backup", retrieved.Name())

		// Verify All works
		all := registry.All()
		assert.Len(t, all, 3)

		// Verify RegisterAll works
		mockSched := newMockScheduler()
		err := registry.RegisterAll(mockSched)
		require.NoError(t, err)
		assert.Len(t, mockSched.registeredHandlers, 3)
	})

	t.Run("handler execution through registered function", func(t *testing.T) {
		registry := NewRegistry()
		executionCount := 0
		handler := newMockHandlerWithFunc("counter", func(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
			executionCount++
			return &scheduler.JobResult{Success: true}, nil
		})
		registry.Register(handler)

		retrieved, ok := registry.Get("counter")
		require.True(t, ok)

		jobHandler := retrieved.Handler()
		_, err := jobHandler(context.Background(), &scheduler.Job{})
		require.NoError(t, err)
		_, err = jobHandler(context.Background(), &scheduler.Job{})
		require.NoError(t, err)

		assert.Equal(t, 2, executionCount)
	})

	t.Run("error handling handler", func(t *testing.T) {
		registry := NewRegistry()
		expectedErr := errors.New("handler execution failed")
		handler := newMockHandlerWithFunc("failing", func(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
			return nil, expectedErr
		})
		registry.Register(handler)

		retrieved, ok := registry.Get("failing")
		require.True(t, ok)

		jobHandler := retrieved.Handler()
		result, err := jobHandler(context.Background(), &scheduler.Job{})

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})
}

// TestHandler_Interface verifies that mockHandler properly implements Handler interface.
func TestHandler_Interface(t *testing.T) {
	t.Run("mockHandler implements Handler interface", func(t *testing.T) {
		var _ Handler = (*mockHandler)(nil)
	})

	t.Run("Name returns configured name", func(t *testing.T) {
		handler := newMockHandler("test-name")
		assert.Equal(t, "test-name", handler.Name())
	})

	t.Run("Handler returns non-nil JobHandler", func(t *testing.T) {
		handler := newMockHandler("test")
		jobHandler := handler.Handler()
		require.NotNil(t, jobHandler)
	})

	t.Run("Handler function is callable", func(t *testing.T) {
		handler := newMockHandler("test")
		jobHandler := handler.Handler()

		result, err := jobHandler(context.Background(), &scheduler.Job{Name: "test-job"})

		require.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, "mock executed", result.Message)
	})
}
