package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func initMeterProvider(ctx context.Context, collectorAddr string) (*sdkmetric.MeterProvider, error) {
	log.Printf("OTel Collector 연결 시도: %s", collectorAddr)

	// 1. OTLP gRPC 메트릭 익스포터 생성 (보안연결 미사용, 로컬 테스트용)
	exporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithEndpoint(collectorAddr),
	)
	if err != nil {
		return nil, fmt.Errorf("OTLP 익스포터 생성 실패: %w", err)
	}

	// 2. 리소스 정의 (메트릭의 공통 속성 부여)
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("hexwar-game-server"),
			attribute.String("node", "server-sim-1"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("리소스 정의 실패: %w", err)
	}

	// 3. PeriodicReader를 사용하여 5초 주기로 Push하도록 설정
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(5*time.Second))),
	)

	otel.SetMeterProvider(mp)

	return mp, nil
}

func main() {
	log.Println("가상 게임 서버 시뮬레이터 시작...")

	// 환경 변수에서 OTel Collector 주소 획득 (기본값: localhost:4317)
	collectorAddr := os.Getenv("OTEL_COLLECTOR_ADDR")
	if collectorAddr == "" {
		collectorAddr = "localhost:4317"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// OTel MeterProvider 초기화
	mp, err := initMeterProvider(ctx, collectorAddr)
	if err != nil {
		log.Fatalf("MeterProvider 초기화 에러: %v", err)
	}
	defer func() {
		// 종료 시 버퍼링된 데이터를 Flush하고 자원 반환
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mp.Shutdown(shutdownCtx); err != nil {
			log.Printf("MeterProvider 종료 중 에러: %v", err)
		}
	}()

	meter := otel.Meter("hexwar-diagnostics")

	// 메트릭 악기(Instrument) 정의
	workingSetGauge, _ := meter.Int64Gauge("hexwar.working_set_bytes", metric.WithDescription("서버 프로세스의 Working Set 메모리(바이트)"))
	privateMemGauge, _ := meter.Int64Gauge("hexwar.private_memory_bytes", metric.WithDescription("서버 프로세스의 Private Memory(바이트)"))
	gcHeapGauge, _ := meter.Int64Gauge("hexwar.gc.heap_bytes", metric.WithDescription(".NET GC 힙 크기(바이트)"))
	connectionsGauge, _ := meter.Int64Gauge("hexwar.connections", metric.WithDescription("활성 WebSocket 연결 수"))
	memPerSessionGauge, _ := meter.Int64Gauge("hexwar.memory_per_session_bytes", metric.WithDescription("세션당 평균 메모리(바이트)"))

	gcCollectionsCounter, _ := meter.Int64Counter("hexwar.gc.collections_total", metric.WithDescription(".NET GC 컬렉션 누적 횟수"))
	sessionsGauge, _ := meter.Int64Gauge("hexwar.sessions", metric.WithDescription("게임 세션 수"))

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var gcGen0, gcGen1, gcGen2 int64
	rand.Seed(time.Now().UnixNano())

	log.Println("메트릭 루프 시작. 5초마다 OTel Collector로 Push를 수행합니다.")

	for {
		select {
		case <-ctx.Done():
			log.Println("시뮬레이터 종료 중...")
			return
		case <-ticker.C:
			// 가상 지표 생성
			activeSessions := int64(rand.Intn(100) + 100)
			gameOverSessions := int64(rand.Intn(20))
			totalSessions := activeSessions + gameOverSessions
			connections := activeSessions * 2

			workingSet := int64(200*1024*1024 + rand.Int63n(50*1024*1024))
			privateMem := workingSet + int64(30*1024*1024)
			gcHeap := workingSet / 2

			memPerSession := int64(0)
			if totalSessions > 0 {
				memPerSession = workingSet / totalSessions
			}

			// 누적 증가형 시뮬레이션 (Counter)
			gcGen0 += int64(rand.Intn(3))
			gcGen1 += int64(rand.Intn(2))
			if rand.Float64() < 0.1 {
				gcGen2 += 1
			}

			// 속성 정의 (node=server-sim-1)
			opts := metric.WithAttributes(attribute.String("node", "server-sim-1"))

			// 메트릭 Push 기록
			workingSetGauge.Record(ctx, workingSet, opts)
			privateMemGauge.Record(ctx, privateMem, opts)
			gcHeapGauge.Record(ctx, gcHeap, opts)
			connectionsGauge.Record(ctx, connections, opts)
			memPerSessionGauge.Record(ctx, memPerSession, opts)

			gcCollectionsCounter.Add(ctx, gcGen0, metric.WithAttributes(attribute.String("node", "server-sim-1"), attribute.String("gen", "0")))
			gcCollectionsCounter.Add(ctx, gcGen1, metric.WithAttributes(attribute.String("node", "server-sim-1"), attribute.String("gen", "1")))
			gcCollectionsCounter.Add(ctx, gcGen2, metric.WithAttributes(attribute.String("node", "server-sim-1"), attribute.String("gen", "2")))

			sessionsGauge.Record(ctx, totalSessions, metric.WithAttributes(attribute.String("node", "server-sim-1"), attribute.String("state", "total")))
			sessionsGauge.Record(ctx, activeSessions, metric.WithAttributes(attribute.String("node", "server-sim-1"), attribute.String("state", "active")))
			sessionsGauge.Record(ctx, gameOverSessions, metric.WithAttributes(attribute.String("node", "server-sim-1"), attribute.String("state", "gameover")))

			log.Printf("[server-sim-1] Push 완료: WorkingSet: %d MB | Active Sessions: %d | WS Connections: %d\n",
				workingSet/1024/1024, activeSessions, connections)
		}
	}
}
