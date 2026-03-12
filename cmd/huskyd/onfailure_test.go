package daemoncmd

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/husky-scheduler/husky/internal/config"
	"github.com/husky-scheduler/husky/internal/store"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHandleOnFailure_Stop_DoesNotStopDaemon(t *testing.T) {
	stopped := false
	d := &daemon{
		logger: testLogger(),
		stop: func() {
			stopped = true
		},
	}

	status := d.handleOnFailure(context.Background(), "job", &config.Job{OnFailure: "stop"}, nil)
	assert.False(t, stopped)
	assert.Equal(t, store.StatusFailed, status)
}

func TestHandleOnFailure_Skip_DoesNotCallStop(t *testing.T) {
	stopped := false
	d := &daemon{
		logger: testLogger(),
		stop: func() {
			stopped = true
		},
	}

	status := d.handleOnFailure(context.Background(), "job", &config.Job{OnFailure: "skip"}, nil)
	assert.False(t, stopped)
	assert.Equal(t, store.StatusSkipped, status)
}

func TestHandleOnFailure_Ignore_DoesNotCallStop(t *testing.T) {
	stopped := false
	d := &daemon{
		logger: testLogger(),
		stop: func() {
			stopped = true
		},
	}

	status := d.handleOnFailure(context.Background(), "job", &config.Job{OnFailure: "ignore"}, nil)
	assert.False(t, stopped)
	assert.Equal(t, store.StatusFailed, status)
}

func TestHandleOnFailure_Alert_DoesNotCallStop(t *testing.T) {
	stopped := false
	d := &daemon{
		logger: testLogger(),
		stop: func() {
			stopped = true
		},
	}

	status := d.handleOnFailure(context.Background(), "job", &config.Job{OnFailure: "alert"}, nil)
	assert.False(t, stopped)
	assert.Equal(t, store.StatusFailed, status)
}

func TestHandleOnFailure_CaseInsensitive(t *testing.T) {
	stopped := false
	d := &daemon{
		logger: testLogger(),
		stop: func() {
			stopped = true
		},
	}

	status := d.handleOnFailure(context.Background(), "job", &config.Job{OnFailure: " STOP "}, nil)
	assert.False(t, stopped)
	assert.Equal(t, store.StatusFailed, status)
}
