package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/Penny-B1t/hexwar-exporter/internal/config"
)

func TestServerStatsJSONDecode(t *testing.T) {
	// HexWar /api/diagnostics/stats의 실제 응답 형태(camelCase)
	raw := `{
		"timestamp": "2026-07-13T10:30:00Z",
		"workingSetMB": 156.34,
		"privateMemoryMB": 182.51,
		"gcHeapMB": 97.89,
		"gcGen0": 6,
		"gcGen1": 2,
		"gcGen2": 1,
		"totalSessions": 1000,
		"activeSessions": 980,
		"gameOverSessions": 20,
		"totalConnections": 2000,
		"estimatedMemoryPerSessionKB": 50.12
	}`

	var stats ServerStats
	if err := json.Unmarshal([]byte(raw), &stats); err != nil {
		t.Fatalf("디코딩 실패: %v", err)
	}

	// GC 최적화 핵심 지표 — 잘못된 태그면 0이 됨
	if stats.GCGen2 != 1 {
		t.Errorf("GCGen2 = %d, want 1", stats.GCGen2)
	}
	if stats.GCGen1 != 2 {
		t.Errorf("GCGen1 = %d, want 2", stats.GCGen1)
	}
	if stats.GCGen0 != 6 {
		t.Errorf("GCGen0 = %d, want 6", stats.GCGen0)
	}
	if stats.GCHeapMB != 97.89 {
		t.Errorf("GCHeapMB = %f, want 97.89", stats.GCHeapMB)
	}
	if stats.WorkingSetMB != 156.34 {
		t.Errorf("WorkingSetMB = %f, want 156.34", stats.WorkingSetMB)
	}
	if stats.TotalSessions != 1000 {
		t.Errorf("TotalSessions = %d, want 1000", stats.TotalSessions)
	}
	if stats.ActiveSessions != 980 {
		t.Errorf("ActiveSessions = %d, want 980", stats.ActiveSessions)
	}
	if stats.GameOverSessions != 20 {
		t.Errorf("GameOverSessions = %d, want 20", stats.GameOverSessions)
	}
	if stats.TotalConnections != 2000 {
		t.Errorf("TotalConnections = %d, want 2000", stats.TotalConnections)
	}
	if stats.EstimatedMemoryPerSessionKB != 50.12 {
		t.Errorf("EstimatedMemoryPerSessionKB = %f, want 50.12", stats.EstimatedMemoryPerSessionKB)
	}
}

// mockTransport는 지정된 지연시간만큼 대기한 후 가짜 ServerStats JSON을 반환한다.
type mockTransport struct {
	delay time.Duration
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	time.Sleep(m.delay)

	stats := ServerStats{
		Timestamp:    time.Now(),
		WorkingSetMB: 120.5,
		GCHeapMB:     64.2,
	}
	bodyBytes, _ := json.Marshal(stats)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(bodyBytes)),
		Header:     make(http.Header),
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}

type bytesReaderWrapper struct {
	*bytes.Reader
}

func TestScrapeManager_Scale(t *testing.T) {
	numNodes := 1000
	numWorkers := 50
	nodeDelay := 50 * time.Millisecond // 노드당 50ms 지연

	clients := make([]*NodeClient, numNodes)
	mockTrans := &mockTransport{delay: nodeDelay}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	for i := 0; i < numNodes; i++ {
		target := config.Target{
			Name: fmt.Sprintf("server-%d", i),
			URL:  "http://mock-server/api/diagnostics/stats",
		}
		c := NewNodeClient(target, 1*time.Second, logger)
		c.http.Transport = mockTrans
		clients[i] = c
	}

	manager := NewScrapeManager(clients, numWorkers, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 수집 시작
	start := time.Now()
	go manager.Start(ctx, 10*time.Second)

	// 모든 클라이언트가 최소 1회 수집 완료할 때까지 대기
	// 대기 한계 시간: 2.5초 (이론상 (1000 * 50ms)/50 = 1000ms이나 스케줄링 오버헤드 감안)
	maxWait := 2500 * time.Millisecond
	checkInterval := 50 * time.Millisecond

	completed := false
	for time.Since(start) < maxWait {
		allDone := true
		for _, c := range clients {
			c.mu.RLock()
			fetched := !c.last.FetchedAt.IsZero()
			err := c.last.Err
			c.mu.RUnlock()
			if !fetched || err != nil {
				allDone = false
				break
			}
		}
		if allDone {
			completed = true
			break
		}
		time.Sleep(checkInterval)
	}

	duration := time.Since(start)
	if !completed {
		t.Errorf("1000개 노드 수집이 %v 내에 완료되지 못했습니다. (실제 경과시간: %v)", maxWait, duration)
	} else {
		t.Logf("1000개 노드 수집 성공! 소요 시간: %v (목표: < %v)", duration.Round(time.Millisecond), maxWait)
	}
}

