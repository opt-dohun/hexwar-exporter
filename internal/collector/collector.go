package collector

import (
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Penny-B1t/hexwar-exporter/internal/client"
)

// Metrics는 exporter가 노출하는 모든 Prometheus 메트릭 디스크립터를 모은다.
// Describe()에서 이 디스크립터들을 Prometheus에 등록한다.
type Metrics struct {
	workingSet       *prometheus.Desc
	privateMemory    *prometheus.Desc
	gcHeap           *prometheus.Desc
	gcCollections    *prometheus.Desc // 라벨: gen (0/1/2)
	sessions         *prometheus.Desc // 라벨: state (total/active/gameover)
	connections      *prometheus.Desc
	memoryPerSession *prometheus.Desc

	// exporter 자체 상태
	exporterUp      *prometheus.Desc
	scrapeDuration  *prometheus.Desc
	scrapeTimestamp *prometheus.Desc
}

// NewMetrics는 메트릭 디스크립터들을 생성한다.
// 모든 메트릭에 node 라벨을 부여해 다중 노드를 구분한다.
func NewMetrics() *Metrics {
	const namespace = "hexwar"
	nodeLabel := []string{"node"}

	return &Metrics{
		workingSet: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "working_set_bytes"),
			"서버 프로세스의 Working Set 메모리(바이트)",
			nodeLabel, nil,
		),
		privateMemory: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "private_memory_bytes"),
			"서버 프로세스의 Private Memory(바이트)",
			nodeLabel, nil,
		),
		gcHeap: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "gc", "heap_bytes"),
			".NET GC 힙 크기(바이트)",
			nodeLabel, nil,
		),
		gcCollections: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "gc", "collections_total"),
			".NET GC 컬렉션 누적 횟수(counter). gen 라벨: 0, 1, 2",
			[]string{"node", "gen"}, nil,
		),
		sessions: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "sessions"),
			"게임 세션 수. state 라벨: total, active, gameover",
			[]string{"node", "state"}, nil,
		),
		connections: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "connections"),
			"활성 WebSocket 연결 수",
			nodeLabel, nil,
		),
		memoryPerSession: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "memory_per_session_bytes"),
			"세션당 평균 메모리(바이트)",
			nodeLabel, nil,
		),
		exporterUp: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "exporter", "up"),
			"노드 폴링 성공 여부(1=성공, 0=실패)",
			nodeLabel, nil,
		),
		scrapeDuration: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "scrape", "duration_seconds"),
			"노드 1회 폴링 소요 시간(초)",
			nodeLabel, nil,
		),
		scrapeTimestamp: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "scrape", "timestamp_seconds"),
			"마지막 폴링 성공 시각(unix epoch 초)",
			nodeLabel, nil,
		),
	}
}

// Collector는 Prometheus의 Collector 인터페이스를 구현한다.
// 여러 client.NodePoller를 등록해 두고, /metrics 스크랩 시점에 각 클라이언트의
// 최신 캐시값을 읽어 메트릭으로 변환한다.
type Collector struct {
	metrics *Metrics
	pollers []client.NodePoller
	logger  *slog.Logger
}

func NewCollector(metrics *Metrics, pollers []client.NodePoller, logger *slog.Logger) *Collector {
	return &Collector{metrics: metrics, pollers: pollers, logger: logger}
}

// Describe는 메트릭 디스크립터를 Prometheus에 등록한다.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.metrics.workingSet
	ch <- c.metrics.privateMemory
	ch <- c.metrics.gcHeap
	ch <- c.metrics.gcCollections
	ch <- c.metrics.sessions
	ch <- c.metrics.connections
	ch <- c.metrics.memoryPerSession
	ch <- c.metrics.exporterUp
	ch <- c.metrics.scrapeDuration
	ch <- c.metrics.scrapeTimestamp
}

// Collect는 실제 메트릭 값을 수집해 Prometheus로 보낸다.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	for _, p := range c.pollers {
		last := p.Last()
		node := p.Target().Name

		if last.Err != nil || last.FetchedAt.IsZero() {
			c.emit(ch, c.metrics.exporterUp, node, 0)
			continue
		}
		c.emit(ch, c.metrics.exporterUp, node, 1)

		s := last.Stats
		c.emit(ch, c.metrics.workingSet, node, mbToBytes(s.WorkingSetMB))
		c.emit(ch, c.metrics.privateMemory, node, mbToBytes(s.PrivateMemoryMB))
		c.emit(ch, c.metrics.gcHeap, node, mbToBytes(s.GCHeapMB))
		c.emit(ch, c.metrics.connections, node, float64(s.TotalConnections))
		c.emit(ch, c.metrics.memoryPerSession, node, kbToBytes(s.EstimatedMemoryPerSessionKB))

		c.emitWithLabels(ch, c.metrics.gcCollections, []string{node, "0"}, float64(s.GCGen0))
		c.emitWithLabels(ch, c.metrics.gcCollections, []string{node, "1"}, float64(s.GCGen1))
		c.emitWithLabels(ch, c.metrics.gcCollections, []string{node, "2"}, float64(s.GCGen2))

		c.emitWithLabels(ch, c.metrics.sessions, []string{node, "total"}, float64(s.TotalSessions))
		c.emitWithLabels(ch, c.metrics.sessions, []string{node, "active"}, float64(s.ActiveSessions))
		c.emitWithLabels(ch, c.metrics.sessions, []string{node, "gameover"}, float64(s.GameOverSessions))

		c.emit(ch, c.metrics.scrapeDuration, node, last.Duration.Seconds())
		c.emit(ch, c.metrics.scrapeTimestamp, node, float64(last.FetchedAt.Unix()))
	}
}

// emit은 단일 라벨(node) 메트릭을 내보낸다.
func (c *Collector) emit(ch chan<- prometheus.Metric, desc *prometheus.Desc, node string, val float64) {
	m, err := prometheus.NewConstMetric(desc, prometheus.GaugeValue, val, node)
	if err != nil {
		c.logger.Error("메트릭 생성 실패", "desc", desc.String(), "err", err)
		return
	}
	ch <- m
}

// emitWithLabels은 다중 라벨 메트릭을 내보낸다.
func (c *Collector) emitWithLabels(ch chan<- prometheus.Metric, desc *prometheus.Desc, labels []string, val float64) {
	m, err := prometheus.NewConstMetric(desc, prometheus.GaugeValue, val, labels...)
	if err != nil {
		c.logger.Error("메트릭 생성 실패", "desc", desc.String(), "err", err)
		return
	}
	ch <- m
}

// mbToBytes는 메가바이트(소수)를 바이트로 변환한다.
func mbToBytes(mb float64) float64 { return mb * 1024 * 1024 }

// kbToBytes는 킬로바이트(소수)를 바이트로 변환한다.
func kbToBytes(kb float64) float64 { return kb * 1024 }

var _ prometheus.Collector = (*Collector)(nil)
