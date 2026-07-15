package client

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

	"github.com/Penny-B1t/hexwar-exporter/internal/config"
)

// 서킷 브레이크를 위한 상태 표현
type circuitState int

const (
	stateClosed   circuitState = iota // 정상 수집
	stateOpen                         // 장애 상황 (네트워크 요청 차단 및 캐시 에러 반환)
	stateHalfOpen                     // 테스트 수집 시도
)

// ServerStats는 HexWar /api/diagnostics/stats의 JSON 응답을 매핑한다.
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

// SampleResult는 1회 폴링 결과(성공 또는 실패)를 담는다.
type SampleResult struct {
	Stats     ServerStats
	Err       error
	Duration  time.Duration
	FetchedAt time.Time
}

type NodePoller interface {
	Target() config.Target
	Last() SampleResult
}

// NodeClient는 단일 노드를 주기적으로 폴링해 최신 결과를 저장한다.
type NodeClient struct {
	target config.Target
	http   *http.Client
	logger *slog.Logger

	mu   sync.RWMutex
	last SampleResult // 가장 최근 폴링 결과

	// 서킷 브레이커 & 백오프 상태 필드
	state           circuitState
	consecutiveFail int
	nextRetryTime   time.Time
	backoffDuration time.Duration
	// 임계치 설정 (설정이나 상수로 정의)
	maxFailures int           // 연속 실패 허용 횟수 (예: 3회)
	minBackoff  time.Duration // 최소 백오프 대기 시간 (예: 5초)
	maxBackoff  time.Duration // 최대 백오프 대기 시간 (예: 60초)
}

// NewNodeClient는 단일 노드 폴링 클라이언트를 만든다.
func NewNodeClient(target config.Target, timeout time.Duration, logger *slog.Logger) *NodeClient {
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
		// 서킷 브레이커 & 백오프 설정
		state:           stateClosed,
		maxFailures:     3,
		minBackoff:      5 * time.Second,
		maxBackoff:      60 * time.Second,
		backoffDuration: 5 * time.Second,
	}
}

// Target은 폴링 대상 정보를 반환한다 (NodePoller 구현).
func (c *NodeClient) Target() config.Target { return c.target }

// ScrapeManager는 등록된 여러 NodeClient의 수집 작업을 
// 워커 풀(Worker Pool) 구조를 통해 병렬적이고 안정적으로 관리한다.
type ScrapeManager struct {
	clients []*NodeClient
	workers int
	queue   chan *NodeClient
	logger  *slog.Logger
}

// NewScrapeManager는 ScrapeManager 인스턴스를 생성한다.
func NewScrapeManager(clients []*NodeClient, workers int, logger *slog.Logger) *ScrapeManager {
	return &ScrapeManager{
		clients: clients,
		workers: workers,
		queue:   make(chan *NodeClient, len(clients)),
		logger:  logger,
	}
}

// Start는 ctx가 취소될 때까지 지정된 워커 개수로 폴링 루프를 실행한다.
func (m *ScrapeManager) Start(ctx context.Context, interval time.Duration) {
	m.logger.Info("ScrapeManager 시작", "workers", m.workers, "targets", len(m.clients))

	// 1. 지정된 워커 수만큼 백그라운드 고루틴(Worker Pool) 기동
	for i := 1; i <= m.workers; i++ {
		go m.worker(ctx, i)
	}

	// 시작 직후 즉시 1회 수집 작업 밀어 넣기 (초기 데이터 즉시 확보)
	m.trigger()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("ScrapeManager 중단")
			return
		case <-ticker.C:
			m.trigger()
		}
	}
}

// trigger는 수집 대기열 큐에 모든 클라이언트를 밀어 넣는다.
func (m *ScrapeManager) trigger() {
	for _, client := range m.clients {
		select {
		case m.queue <- client:
		default:
			// 큐 버퍼가 꽉 찬 경우 (이전 수집 주기의 작업이 아직 안 끝난 노드)
			m.logger.Warn("수집 대기열 큐가 가득 찼습니다. 이전 수집 작업이 지연되고 있을 수 있습니다.", 
				"node", client.target.Name)
		}
	}
}

// worker는 큐에서 작업을 꺼내 실제 HTTP 폴링을 수행한다.
func (m *ScrapeManager) worker(ctx context.Context, id int) {
	m.logger.Debug("워커 고루틴 기동", "worker_id", id)
	for {
		select {
		case <-ctx.Done():
			return
		case client, ok := <-m.queue:
			if !ok {
				return
			}
			client.poll(ctx)
		}
	}
}


// poll은 1회 폴링을 수행하고 결과를 last에 저장한다.
// 에러가 발생해도 last.err에 저장되며, 다른 노드에 영향을 주지 않는다(에러 격리).
func (c *NodeClient) poll(ctx context.Context) {
	c.mu.Lock()
	now := time.Now()

	// 1. 서킷이 Open 상태이고 쿨다운 대기 중인 경우 실제 fetch를 차단
	if c.state == stateOpen {
		if now.Before(c.nextRetryTime) {
			c.last = SampleResult{
				Err:       fmt.Errorf("서킷 오픈 상태: 게임 서버 호출 차단 중 (남은 시간: %v)", c.nextRetryTime.Sub(now).Round(time.Second)),
				FetchedAt: now,
			}
			c.mu.Unlock()
			return
		}
		// 쿨다운 대기 시간이 지난 경우 테스트 수집 상태로 전이
		c.state = stateHalfOpen
		c.logger.Info("서킷 브레이커 Half-Open 진입 (단일 테스트 요청 시도)", "node", c.target.Name)
	}
	c.mu.Unlock()

	// 2. 실제 네트워크 수집 수행
	start := time.Now()
	stats, err := c.fetch(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		c.consecutiveFail++
		c.last = SampleResult{
			Err:       err,
			Duration:  time.Since(start),
			FetchedAt: time.Now(),
		}

		// 실패 시 서킷 오픈 전이
		if c.state == stateClosed && c.consecutiveFail >= c.maxFailures {
			c.state = stateOpen
			// 지수 백오프 대신 고정 백오프(minBackoff = 5s)로 쿨다운 시간 설정
			c.nextRetryTime = time.Now().Add(c.minBackoff)
			c.logger.Warn("서킷 브레이커 OPEN (장애 감지)", "node", c.target.Name, "nextRetry", c.minBackoff.String())
		} else if c.state == stateHalfOpen {
			// 테스트 호출 상태에서 실패 시 다시 Open으로 복귀 및 고정 쿨다운 재설정
			c.state = stateOpen
			c.nextRetryTime = time.Now().Add(c.minBackoff)
			c.logger.Warn("서킷 브레이커 OPEN 유지 (테스트 요청 실패)", "node", c.target.Name, "nextRetry", c.minBackoff.String())
		}
	} else {
		// 성공 시 서킷 닫기 및 카운터 초기화
		c.state = stateClosed
		c.consecutiveFail = 0
		c.last = SampleResult{
			Stats:     stats,
			Duration:  time.Since(start),
			FetchedAt: time.Now(),
		}
		c.logger.Info("서킷 브레이커 CLOSED (정상 복구)", "node", c.target.Name)
	}

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

// Last는 가장 최근 폴링 결과를 반환한다 (스레드 안전, NodePoller 구현).
// collector가 /metrics 스크랩 시점에 호출한다.
func (c *NodeClient) Last() SampleResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.last
}

// 컴파일 타임 인터페이스 구현 보장
var _ NodePoller = (*NodeClient)(nil)
