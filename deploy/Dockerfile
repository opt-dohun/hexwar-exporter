# ── 스테이지 1: 빌드 ──
# exporter 관측 서비스의 기본이 되는 바이너리만 포함된 이미지 생성
# k3s에 등록하여 hexwar-server와 함께 동작

FROM golang:alpine AS builder

# CGO_ENABLED=0: C 라이브러리 의존 없는 순수 정적 바이너리 생성.
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

RUN apk add --no-cache ca-certificates tzdata

# 비루트 사용자로 실행 
RUN adduser -D -u 10000 exporter
USER exporter

WORKDIR /app

COPY --from=builder /build/hexwar-exporter .

EXPOSE 9091

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:9091/healthz || exit 1

ENTRYPOINT ["/app/hexwar-exporter"]
