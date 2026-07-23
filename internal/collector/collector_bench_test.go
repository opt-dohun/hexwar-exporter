package collector_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Penny-B1t/hexwar-exporter/internal/client"
	"github.com/Penny-B1t/hexwar-exporter/internal/collector"
	"github.com/Penny-B1t/hexwar-exporter/internal/config"
)

// mockPoller는 캐싱 로직(NodePoller 인터페이스)을 모방합니다.
type mockPoller struct {
	target config.Target
	last   client.SampleResult
}

func (m *mockPoller) Target() config.Target         { return m.target }
func (m *mockPoller) Last() client.SampleResult     { return m.last }
func (m *mockPoller) ConsecutiveFailures() int      { return 0 }
func (m *mockPoller) LastSuccessfulTime() time.Time { return time.Time{} }

func BenchmarkExporterScrape_WithCache(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	metrics := collector.NewMetrics()

	var pollers []client.NodePoller
	for i := 0; i < 1; i++ {
		pollers = append(pollers, &mockPoller{
			target: config.Target{Name: fmt.Sprintf("hexwar-server-mock-%d", i)},
			last: client.SampleResult{
				Stats: client.ServerStats{
					WorkingSetMB:                45.5,
					PrivateMemoryMB:             40.0,
					GCHeapMB:                    20.0,
					TotalConnections:            1000,
					EstimatedMemoryPerSessionKB: 10.5,
					GCGen0:                      100,
					GCGen1:                      50,
					GCGen2:                      10,
					TotalSessions:               200,
					ActiveSessions:              150,
					GameOverSessions:            50,
				},
				FetchedAt: time.Now(),
				Duration:  5 * time.Millisecond,
			},
		})
	}

	col := collector.NewCollector(metrics, pollers, logger)
	registry := prometheus.NewRegistry()
	registry.MustRegister(col)

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorLog:      &loggerErrorAdapter{logger: logger},
		ErrorHandling: promhttp.ContinueOnError,
	})

	// 메모리 할당량 및 벤치마크 시간 초기화
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/metrics", nil)
		rr := httptest.NewRecorder()

		// 캐시된 데이터를 기반으로 Prometheus 형식으로 렌더링 (비동기 폴링의 효과)
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			b.Fatalf("expected status 200, got %d", rr.Code)
		}
	}
}

// loggerErrorAdapter는 promhttp 에러 로깅을 위해 인터페이스를 맞춥니다.
type loggerErrorAdapter struct {
	logger *slog.Logger
}

func (l *loggerErrorAdapter) Println(v ...interface{}) {
	l.logger.Error("prometheus error", "err", v)
}
