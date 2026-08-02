package client

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Penny-B1t/hexwar-exporter/internal/config"
)

func TestCircuitBreaker_ExponentialBackoff(t *testing.T) {
	var requestCount int32
	var fail bool = true

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		if fail {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"totalSessions": 10}`))
	}))
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := NewNodeClient(config.Target{Name: "test-node", URL: ts.URL}, 1*time.Second, logger)
	client.minBackoff = 50 * time.Millisecond
	client.maxBackoff = 200 * time.Millisecond
	client.backoffDuration = 50 * time.Millisecond
	client.maxFailures = 2

	ctx := context.Background()

	// 1. Initial State: Closed
	if client.state != stateClosed {
		t.Fatalf("expected stateClosed, got %v", client.state)
	}

	// Failure 1
	client.poll(ctx)
	if client.consecutiveFail != 1 || client.state != stateClosed {
		t.Fatalf("expected 1 failure and stateClosed, got fail=%d state=%v", client.consecutiveFail, client.state)
	}

	// Failure 2 -> Trigger Circuit OPEN
	client.poll(ctx)
	if client.state != stateOpen {
		t.Fatalf("expected stateOpen after 2 failures, got %v", client.state)
	}
	if client.backoffDuration != 50*time.Millisecond {
		t.Fatalf("expected backoff 50ms, got %v", client.backoffDuration)
	}

	// 2. Poll while OPEN before cooldown expires -> Request should be short-circuited (no HTTP call)
	reqBefore := atomic.LoadInt32(&requestCount)
	client.poll(ctx)
	reqAfter := atomic.LoadInt32(&requestCount)
	if reqBefore != reqAfter {
		t.Fatalf("expected HTTP call to be blocked during OPEN, but count increased from %d to %d", reqBefore, reqAfter)
	}

	// Wait for 50ms cooldown to expire
	time.Sleep(60 * time.Millisecond)

	// 3. Poll after cooldown -> Transitions to HalfOpen and makes 1 Probe HTTP call (which fails)
	client.poll(ctx)
	if client.state != stateOpen {
		t.Fatalf("expected stateOpen after HalfOpen failure, got %v", client.state)
	}
	// Exponential Backoff: 50ms * 2 = 100ms
	if client.backoffDuration != 100*time.Millisecond {
		t.Fatalf("expected backoffDuration 100ms, got %v", client.backoffDuration)
	}

	// 4. Poll again during 100ms cooldown -> Blocked
	reqBefore = atomic.LoadInt32(&requestCount)
	client.poll(ctx)
	reqAfter = atomic.LoadInt32(&requestCount)
	if reqBefore != reqAfter {
		t.Fatalf("expected HTTP call blocked during 100ms cooldown: before=%d, after=%d", reqBefore, reqAfter)
	}

	// Wait for 100ms cooldown
	time.Sleep(110 * time.Millisecond)

	// 5. Now server recovers!
	fail = false
	client.poll(ctx)
	if client.state != stateClosed {
		t.Fatalf("expected stateClosed after successful Probe, got %v", client.state)
	}
	if client.consecutiveFail != 0 {
		t.Fatalf("expected consecutiveFail 0, got %d", client.consecutiveFail)
	}
	if client.backoffDuration != 50*time.Millisecond {
		t.Fatalf("expected backoffDuration reset to 50ms, got %v", client.backoffDuration)
	}
}
