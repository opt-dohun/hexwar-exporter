package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ServerStats는 HexWar /api/diagnostics/stats의 JSON 응답을 매핑한다.
// ASP.NET Core 기본 직렬화가 camelCase이므로 태그도 camelCase로 맞춘다.
type ServerStats struct {
	Timestamp                   time.Time `json:"timestamp"`
	WorkingSetMB                float64   `json:"workingSetMB"`
	PrivateMemoryMB             float64   `json:"privateMemoryMB"`
	GCHeapMB                    float64   `json:"gcHeapMB"`
	GCGen0                      int       `json:"gcGen0"`
	GCGen1                      int       `json:"gcGen1"`
	GCGen2                      int       `json:"gcGen2"`
	TotalSessions               int       `json:"totalSessions"`
	ActiveSessions              int       `json:"activeSessions"`
	GameOverSessions            int       `json:"gameOverSessions"`
	TotalConnections            int       `json:"totalConnections"`
	EstimatedMemoryPerSessionKB float64   `json:"estimatedMemoryPerSessionKB"`
}

// sampleResult는 1회 폴링 결과(성공 또는 실패)를 담는다.
type sampleResult struct {
	stats     ServerStats
	err       error
	duration  time.Duration
	fetchedAt time.Time
}

// nodePoller는 폴링 대상 노드 하나의 최신 결과를 제공하는 인터페이스다.
// NodeClient만이 구현하며, 테스트에서는 fake 클라이언트로 대체할 수 있다.
// 이 인터페이스 덕분에 collector 테스트가 HTTP 서버 없이 동작한다.
type nodePoller interface {
	Target() Target
	Last() sampleResult
}

// NodeClient는 단일 노드를 주기적으로 폴링해 최신 결과를 저장한다.
type NodeClient struct {
	target Target
	http   *http.Client
	logger *slog.Logger

	mu   sync.RWMutex
	last sampleResult // 가장 최근 폴링 결과
}

// NewNodeClient는 단일 노드 폴링 클라이언트를 만든다.
func NewNodeClient(target Target, timeout time.Duration, logger *slog.Logger) *NodeClient {
	return &NodeClient{
		target: target,
		http: &http.Client{
			Timeout: timeout,
			// HTTP/1.1 커넥션 재사용으로 폴링마다 핸드셰이크 비용 절감
			Transport: &http.Transport{
				MaxIdleConns:        2,
				MaxIdleConnsPerHost: 1,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		logger: logger,
	}
}

// Target은 폴링 대상 정보를 반환한다 (nodePoller 구현).
func (c *NodeClient) Target() Target { return c.target }

// Run은 ctx가 취소될 때까지 interval마다 폴링한다.
// 각 노드마다 별도 goroutine에서 실행된다.
func (c *NodeClient) Run(ctx context.Context, interval time.Duration) {
	c.logger.Info("노드 폴링 시작",
		"node", c.target.Name, "url", c.target.URL, "interval", interval.String())

	// 즉시 1회 폴링(시작 직후 빈 상태 방지)
	c.poll(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("노드 폴링 중단", "node", c.target.Name)
			return
		case <-ticker.C:
			c.poll(ctx)
		}
	}
}

// poll은 1회 폴링을 수행하고 결과를 last에 저장한다.
// 에러가 발생해도 last.err에 저장되며, 다른 노드에 영향을 주지 않는다(에러 격리).
func (c *NodeClient) poll(ctx context.Context) {
	start := time.Now()
	stats, err := c.fetch(ctx)

	c.mu.Lock()
	c.last = sampleResult{
		stats:     stats,
		err:       err,
		duration:  time.Since(start),
		fetchedAt: time.Now(),
	}
	c.mu.Unlock()

	if err != nil {
		c.logger.Warn("폴링 실패", "node", c.target.Name, "err", err)
	} else {
		c.logger.Debug("폴링 성공",
			"node", c.target.Name,
			"sessions", stats.TotalSessions,
			"gcGen2", stats.GCGen2,
			"duration_ms", time.Since(start).Milliseconds())
	}
}

// fetch는 단일 HTTP GET으로 ServerStats를 가져온다.
func (c *NodeClient) fetch(ctx context.Context) (ServerStats, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.target.URL, nil)
	if err != nil {
		return ServerStats{}, fmt.Errorf("요청 생성: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "hexwar-exporter/1.0")

	resp, err := c.http.Do(req)
	if err != nil {
		return ServerStats{}, fmt.Errorf("HTTP 요청: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return ServerStats{}, fmt.Errorf("상태 코드 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var stats ServerStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return ServerStats{}, fmt.Errorf("JSON 디코딩: %w", err)
	}
	return stats, nil
}

// Last는 가장 최근 폴링 결과를 반환한다 (스레드 안전, nodePoller 구현).
// collector가 /metrics 스크랩 시점에 호출한다.
func (c *NodeClient) Last() sampleResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.last
}

// 컴파일 타임 인터페이스 구현 보장
var _ nodePoller = (*NodeClient)(nil)
