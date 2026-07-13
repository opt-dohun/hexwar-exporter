package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// 1. 설정 로드
	cfg, err := LoadConfig()
	if err != nil {
		slog.Error("설정 로드 실패", "err", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("HexWar Exporter 시작",
		"targets", targetNames(cfg.Targets),
		"scrapeInterval", cfg.ScrapeInterval.String(),
		"listen", cfg.ListenAddr,
	)

	// 2. 노드별 폴링 클라이언트 생성
	clients := make([]*NodeClient, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		clients = append(clients, NewNodeClient(t, cfg.ScrapeTimeout, logger))
	}

	// 3. 컨텍스트: SIGINT/SIGTERM으로 전체 폴링 루프를 한 번에 종료
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 4. 각 노드 폴링 루프를 별도 goroutine으로 시작 (동시 폴링)
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(c *NodeClient) {
			defer wg.Done()
			c.Run(ctx, cfg.ScrapeInterval)
		}(client)
	}

	// 5. nodePoller 슬라이스로 변환해 collector에 주입
	pollers := make([]nodePoller, 0, len(clients))
	for _, c := range clients {
		pollers = append(pollers, c)
	}

	// 6. Prometheus collector 등록 (커스텀 레지스트리 사용)
	registry := prometheus.NewRegistry()
	collector := NewCollector(NewMetrics(), pollers, logger)
	registry.MustRegister(collector)
	// exporter 자체 메트릭(go_*, process_*)도 함께 노출
	registry.MustRegister(prometheus.NewGoCollector())
	registry.MustRegister(prometheus.NewProcessCollector(
		prometheus.ProcessCollectorOpts{}))

	// 7. HTTP 서버
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry,
		promhttp.HandlerOpts{EnableOpenMetrics: true}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("HexWar Exporter — /metrics, /healthz\n"))
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// 8. 서버를 별도 goroutine에서 실행
	srvErr := make(chan error, 1)
	go func() {
		logger.Info("HTTP 서버 수신 대기", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
		close(srvErr)
	}()

	// 9. 종료 신호 또는 서버 에러 대기
	select {
	case <-ctx.Done():
		logger.Info("종료 신호 수신, 그레이스풀 셧다운 시작")
	case err := <-srvErr:
		if err != nil {
			logger.Error("HTTP 서버 에러", "err", err)
		}
	}

	// 10. 폴링 루프 중단 후 대기 (진행 중인 HTTP 폴링 요청이 끝나도록)
	stop()
	wg.Wait()

	// 11. /metrics 서버도 타임아웃 내 안전 종료
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("서버 종료 중 에러", "err", err)
	}
	logger.Info("종료 완료")
}

// targetNames는 로깅용으로 타깃 이름만 추출한다.
func targetNames(targets []Target) []string {
	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = t.Name
	}
	return names
}
