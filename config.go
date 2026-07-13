package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config는 exporter의 실행 설정을 담는다.
type Config struct {
	// Targets는 폴링할 HexWar 서버 노드의 "이름=URL" 목록이다.
	// 예: "server-1=http://hexwar-server-1:5000/api/diagnostics/stats"
	Targets []Target

	// ScrapeInterval은 각 노드를 폴링하는 주기다.
	ScrapeInterval time.Duration

	// ScrapeTimeout은 1회 폴링 HTTP 요청의 타임아웃이다.
	ScrapeTimeout time.Duration

	// ListenAddr은 /metrics 엔드포인트의 수신 주소다.
	ListenAddr string
}

// Target은 폴링 대상 노드 하나를 나타낸다.
// Name은 Prometheus 메트릭의 node 라벨 값이 된다.
type Target struct {
	Name string
	URL  string // 예: http://hexwar-server-1:5000/api/diagnostics/stats
}

// DefaultConfig는 플래그/환경변수가 없을 때의 기본값이다.
func DefaultConfig() Config {
	return Config{
		ScrapeInterval: 5 * time.Second,
		ScrapeTimeout:  3 * time.Second,
		ListenAddr:     ":9091",
	}
}

// LoadConfig는 명령줄 플래그를 파싱한 뒤, 비어있는 항목을 환경변수로 채운다.
// 우선순위: 플래그 > 환경변수 > 기본값
func LoadConfig() (Config, error) {
	cfg := DefaultConfig()

	var (
		targetsRaw string
		interval   time.Duration
		timeout    time.Duration
		listen     string
	)

	flag.StringVar(&targetsRaw, "targets", "",
		`폴링 대상 노드 목록. "이름=URL,이름=URL" 형태 (예: server-1=http://host:5000/api/diagnostics/stats)`)
	flag.DurationVar(&interval, "scrape.interval", 0, "폴링 주기 (예: 5s, 10s)")
	flag.DurationVar(&timeout, "scrape.timeout", 0, "1회 폴링 타임아웃 (예: 3s)")
	flag.StringVar(&listen, "listen", "", "/metrics 수신 주소 (예: :9091)")
	flag.Parse()

	// 플래그가 비어있으면 환경변수로 대체
	if targetsRaw == "" {
		targetsRaw = os.Getenv("HEXWAR_TARGETS")
	}
	if interval == 0 {
		interval = getenvDuration("HEXWAR_SCRAPE_INTERVAL", cfg.ScrapeInterval)
	}
	if timeout == 0 {
		timeout = getenvDuration("HEXWAR_SCRAPE_TIMEOUT", cfg.ScrapeTimeout)
	}
	if listen == "" {
		listen = getenvStr("HEXWAR_LISTEN", cfg.ListenAddr)
	}

	cfg.ScrapeInterval = interval
	cfg.ScrapeTimeout = timeout
	cfg.ListenAddr = listen

	targets, err := parseTargets(targetsRaw)
	if err != nil {
		return Config{}, fmt.Errorf("targets 파싱 실패: %w", err)
	}
	if len(targets) == 0 {
		return Config{}, fmt.Errorf("폴링 대상(targets)이 없습니다. -targets 또는 HEXWAR_TARGETS를 지정하세요")
	}
	cfg.Targets = targets

	return cfg, nil
}

// parseTargets는 "name=url,name=url" 형태의 문자열을 []Target으로 파싱한다.
func parseTargets(raw string) ([]Target, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	targets := make([]Target, 0, len(parts))
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("잘못된 대상 형식 %q (예: name=url)", part)
		}
		name := strings.TrimSpace(kv[0])
		url := strings.TrimSpace(kv[1])
		if name == "" || url == "" {
			return nil, fmt.Errorf("빈 이름 또는 URL: %q", part)
		}
		targets = append(targets, Target{Name: name, URL: url})
	}
	return targets, nil
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func getenvStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
