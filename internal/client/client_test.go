package client

import (
	"encoding/json"
	"testing"
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
