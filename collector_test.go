package main

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// --- config 테스트 ---

func TestParseTargets(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"단일 노드", "server-1=http://host:5000/api/diagnostics/stats", 1, false},
		{"다중 노드", "server-1=http://a:5000/x,server-2=http://b:5000/x", 2, false},
		{"빈 문자열", "", 0, false},
		{"잘못된 형식(= 없음)", "server-1", 0, true},
		{"빈 URL", "server-1=", 0, true},
		{"공백 포함", " server-1 = http://a:5000/x ", 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTargets(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if len(got) != tt.want {
				t.Fatalf("개수 = %d, want = %d", len(got), tt.want)
			}
		})
	}
}

// --- ServerStats JSON 매핑 테스트 (핵심) ---
// ASP.NET Core의 camelCase 직렬화와 정확히 매칭되는지 검증한다.
// 태그가 하나라도 틀리면 값이 0이 되므로 즉시 발견된다.

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

// --- 단위 변환 테스트 ---

func TestUnitConversions(t *testing.T) {
	// 97.89MB → 바이트
	got := mbToBytes(97.89)
	want := 97.89 * 1024 * 1024
	if got != want {
		t.Errorf("mbToBytes(97.89) = %f, want %f", got, want)
	}
	// 50.12KB → 바이트
	gotKB := kbToBytes(50.12)
	wantKB := 50.12 * 1024
	if gotKB != wantKB {
		t.Errorf("kbToBytes(50.12) = %f, want %f", gotKB, wantKB)
	}
}

// --- collector 테스트 (핵심) ---
// nodePoller 인터페이스를 통해 HTTP 없이 fake 클라이언트로 검증한다.

// fakePoller는 nodePoller 인터페이스를 구현하는 테스트 더블이다.
type fakePoller struct {
	target Target
	last   sampleResult
}

func (f *fakePoller) Target() Target     { return f.target }
func (f *fakePoller) Last() sampleResult { return f.last }

func newCollectorWithFakes(t *testing.T, pollers []nodePoller) (*Collector, chan prometheus.Metric) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(&discardWriter{}, nil))
	c := NewCollector(NewMetrics(), pollers, logger)
	// collector의 Collect가 보낸 메트릭을 모두 받아온다
	ch := make(chan prometheus.Metric, 100)
	c.Collect(ch)
	close(ch)
	return c, ch
}

func TestCollector_SuccessfulNode(t *testing.T) {
	stats := ServerStats{
		WorkingSetMB:                156.34,
		PrivateMemoryMB:             182.51,
		GCHeapMB:                    97.89,
		GCGen0:                      6,
		GCGen1:                      2,
		GCGen2:                      1,
		TotalSessions:               1000,
		ActiveSessions:              980,
		GameOverSessions:            20,
		TotalConnections:            2000,
		EstimatedMemoryPerSessionKB: 50.12,
	}
	pollers := []nodePoller{
		&fakePoller{
			target: Target{Name: "server-1"},
			last:   sampleResult{stats: stats, fetchedAt: time.Now()},
		},
	}

	_, ch := newCollectorWithFakes(t, pollers)

	metrics := collectMetrics(t, ch)

	// 검증 1: exporter_up == 1
	assertGaugeValue(t, metrics, "hexwar_exporter_up", map[string]string{"node": "server-1"}, 1)

	// 검증 2: GC 힙이 MB→bytes로 정확히 변환됨
	assertGaugeValue(t, metrics, "hexwar_gc_heap_bytes", map[string]string{"node": "server-1"}, 97.89*1024*1024)

	// 검증 3: GC 컬렉션이 gen 라벨로 3개 시계열 노출
	assertGaugeValue(t, metrics, "hexwar_gc_collections_total", map[string]string{"node": "server-1", "gen": "0"}, 6)
	assertGaugeValue(t, metrics, "hexwar_gc_collections_total", map[string]string{"node": "server-1", "gen": "1"}, 2)
	assertGaugeValue(t, metrics, "hexwar_gc_collections_total", map[string]string{"node": "server-1", "gen": "2"}, 1)

	// 검증 4: 세션이 state 라벨로 3개 시계열 노출
	assertGaugeValue(t, metrics, "hexwar_sessions", map[string]string{"node": "server-1", "state": "total"}, 1000)
	assertGaugeValue(t, metrics, "hexwar_sessions", map[string]string{"node": "server-1", "state": "active"}, 980)
	assertGaugeValue(t, metrics, "hexwar_sessions", map[string]string{"node": "server-1", "state": "gameover"}, 20)

	// 검증 5: 커넥션 수
	assertGaugeValue(t, metrics, "hexwar_connections", map[string]string{"node": "server-1"}, 2000)

	// 검증 6: 세션당 메모리 KB→bytes 변환
	assertGaugeValue(t, metrics, "hexwar_memory_per_session_bytes", map[string]string{"node": "server-1"}, 50.12*1024)
}

func TestCollector_FailedNodeIsolation(t *testing.T) {
	// server-1은 폴링 실패, server-2는 정상
	// 실패 노드는 exporter_up=0만 노출, 다른 노드 메트릭은 정상
	pollers := []nodePoller{
		&fakePoller{
			target: Target{Name: "server-1"},
			last:   sampleResult{err: errDummy, fetchedAt: time.Now()},
		},
		&fakePoller{
			target: Target{Name: "server-2"},
			last:   sampleResult{stats: ServerStats{TotalConnections: 500}, fetchedAt: time.Now()},
		},
	}

	_, ch := newCollectorWithFakes(t, pollers)
	metrics := collectMetrics(t, ch)

	// 실패 노드: up=0
	assertGaugeValue(t, metrics, "hexwar_exporter_up", map[string]string{"node": "server-1"}, 0)
	// 실패 노드의 다른 메트릭은 노출되지 않아야 함
	if _, ok := lookupMetric(metrics, "hexwar_connections", map[string]string{"node": "server-1"}); ok {
		t.Error("실패 노드는 값 메트릭을 노출하지 않아야 함")
	}

	// 정상 노드: up=1, 커넥션 정상 노출 (에러 격리 검증)
	assertGaugeValue(t, metrics, "hexwar_exporter_up", map[string]string{"node": "server-2"}, 1)
	assertGaugeValue(t, metrics, "hexwar_connections", map[string]string{"node": "server-2"}, 500)
}

func TestCollector_FreshPollingNotYetDone(t *testing.T) {
	// 아직 폴링 전(fetchedAt 제로값): up=0만 노출, 패닉 없음
	pollers := []nodePoller{
		&fakePoller{target: Target{Name: "server-1"}},
	}
	_, ch := newCollectorWithFakes(t, pollers)
	metrics := collectMetrics(t, ch)
	assertGaugeValue(t, metrics, "hexwar_exporter_up", map[string]string{"node": "server-1"}, 0)
}

// --- 테스트 보조 함수 ---

var errDummy = newDummyError()

type dummyError struct{ msg string }

func (e *dummyError) Error() string { return e.msg }
func newDummyError() error          { return &dummyError{msg: "connection refused"} }

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// metricKey는 메트릭 이름+라벨 조합의 식별자다.
type metricKey struct {
	name   string
	labels string // "k=v,k=v" 정렬된 형태
}

type collectedMetric struct {
	name   string
	labels map[string]string
	value  float64
}

// collectMetrics는 채널에서 모든 메트릭을 꺼내 파싱한다.
func collectMetrics(t *testing.T, ch <-chan prometheus.Metric) []collectedMetric {
	t.Helper()
	var out []collectedMetric
	for m := range ch {
		pb := &dto.Metric{}
		if err := m.Write(pb); err != nil {
			t.Fatalf("메트릭 직렬화 실패: %v", err)
		}
		name := extractName(t, m)
		labels := map[string]string{}
		for _, l := range pb.Label {
			labels[*l.Name] = *l.Value
		}
		val := pb.GetGauge().GetValue()
		out = append(out, collectedMetric{name: name, labels: labels, value: val})
	}
	return out
}

func lookupMetric(metrics []collectedMetric, name string, labels map[string]string) (float64, bool) {
	for _, m := range metrics {
		if m.name != name {
			continue
		}
		if labelsMatch(m.labels, labels) {
			return m.value, true
		}
	}
	return 0, false
}

func assertGaugeValue(t *testing.T, metrics []collectedMetric, name string, labels map[string]string, want float64) {
	t.Helper()
	got, ok := lookupMetric(metrics, name, labels)
	if !ok {
		t.Errorf("메트릭 %s{%v}를 찾을 수 없음", name, labels)
		return
	}
	if got != want {
		t.Errorf("메트릭 %s{%v} = %f, want %f", name, labels, got, want)
	}
}

func labelsMatch(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

// extractName은 메트릭의 Desc()에서 이름을 추출한다.
// Desc.String() 형태: "Desc{fqName: \"hexwar_gc_heap_bytes\", help: \"...\", constLabels: {}, variableLabels: [node]}"
func extractName(t *testing.T, m prometheus.Metric) string {
	t.Helper()
	descStr := m.Desc().String()
	start := indexOf(descStr, "fqName: \"")
	if start < 0 {
		t.Fatalf("fqName을 찾을 수 없음: %s", descStr)
	}
	start += len("fqName: \"")
	end := indexOf(descStr[start:], "\"")
	if end < 0 {
		t.Fatalf("fqName 종료 인용부호를 찾을 수 없음: %s", descStr)
	}
	return descStr[start : start+end]
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
