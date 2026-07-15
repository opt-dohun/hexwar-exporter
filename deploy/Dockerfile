# ── 스테이지 1: 빌드 ──
# golang 이미지는 Go 컴파일러와 표준 라이브러리를 포함한 커다란 이미지(~800MB).
# 이 환경에서만 바이너리를 컴파일하고, 실행 환경에는 결과물만 전달한다.
FROM golang:alpine AS builder

# CGO_ENABLED=0: C 라이브러리 의존 없는 순수 정적 바이너리 생성.
# 어느 리눅스 환경에서든 시스템 라이브러리 없이 실행 가능.
RUN CGO_ENABLED=0 GOOS=linux go install -ldflags="-s -w" github.com/prometheus/client_golang/prometheus/promhttp@latest || true

WORKDIR /build

# go.mod, go.sum을 먼저 복사 → 의존성 다운로드 캐시 (소스 변경 시 재다운로드 방지)
COPY go.mod go.sum ./
RUN go mod download

# 소스 복사 후 빌드
COPY cmd/ ./cmd/
COPY internal/ ./internal/
# -s -w: 심벌 테이블·DWARF 정보 제거 → 바이너리 크기 ~30% 감소
# -extldflags "-static": 외부 링커에 정적 링크 지시 (CGO_ENABLED=0과 중복되지만 명시)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o hexwar-exporter ./cmd/hexwar-exporter

# ── 스테이지 2: 실행 ──
# alpine: ~5MB의 경량 리눅스. CA 인증서(HTTPS 폴링용)와 쉘(디버깅용)이 포함됨.
# scratch(0MB) 대신 alpine을 선택한 이유:
#   1. exporter가 hexwar-server에 HTTP 폴링을 하므로 TLS/CA 인증서 필요
#   2. 컨테이너 내부에서 wget/curl로 디버깅 가능
FROM alpine:3.20

# ca-certificates: HTTPS 엔드포인트 폴링 시 TLS 검증용 (평소 HTTP라도 안전망)
# tzdata: 로그 타임스탬프의 표준 시간대 설정용
RUN apk add --no-cache ca-certificates tzdata

# 비루트 사용자로 실행 
RUN adduser -D -u 10000 exporter
USER exporter

WORKDIR /app

# 빌드 스테이지에서 바이너리만 복사 (컴파일러·소스 코드는 포함되지 않음)
COPY --from=builder /build/hexwar-exporter .

# Prometheus가 스크랩하는 포트 (실제로는 Go HTTP 서버가 리슨)
EXPOSE 9091

# 건강 확인: exporter 자체의 /healthz 엔드포인트 사용
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:9091/healthz || exit 1

ENTRYPOINT ["/app/hexwar-exporter"]
