# HexWar Exporter

HexWar 게임 서버용 Prometheus 메트릭 Exporter입니다. Go 기반의 관측가능성 사이드카로 HexWar 게임 서버의 성능 메트릭과 상태 정보를 수집하여 Prometheus 형식으로 노출합니다.

## 개요

HexWar Exporter는 게임 서버의 실시간 메트릭을 모니터링하고 관측가능성(Observability)을 제공하는 도구입니다. Prometheus와 호환되어 Grafana 등의 시각화 도구와 함께 사용할 수 있습니다.

## 기능

- 🎮 HexWar 게임 서버 메트릭 수집
- 📊 Prometheus 형식 메트릭 노출
- 🔍 실시간 성능 모니터링
- 🐳 Docker 지원
- ⚙️ 경량의 사이드카 아키텍처

## 기술 스택

- **Go** (76.1%): 메인 프로그램
- **Makefile** (19.8%): 빌드 자동화
- **Dockerfile** (4.1%): 컨테이너화

## 설치

### 소스코드에서 빌드

```bash
make build
```

### Docker를 사용한 설치

```bash
docker build -t hexwar-exporter .
docker run -d -p 9090:9090 hexwar-exporter
```

## 사용법

### 기본 실행

```bash
./hexwar-exporter
```

### 설정

환경 변수를 통해 설정할 수 있습니다:

- `HEXWAR_SERVER_HOST`: HexWar 게임 서버 주소 (기본값: localhost)
- `HEXWAR_SERVER_PORT`: HexWar 게임 서버 포트 (기본값: 8080)
- `EXPORTER_PORT`: Exporter 메트릭 포트 (기본값: 9090)

### Prometheus 설정

`prometheus.yml`에 다음과 같이 추가하세요:

```yaml
scrape_configs:
  - job_name: 'hexwar'
    static_configs:
      - targets: ['localhost:9090']
```

## 메트릭

주요 노출 메트릭:

- `hexwar_server_up`: 게임 서버 상태 (1=정상, 0=다운)
- `hexwar_active_players`: 현재 접속 플레이어 수
- `hexwar_server_cpu`: 게임 서버 CPU 사용률
- `hexwar_server_memory`: 게임 서버 메모리 사용량
- `hexwar_requests_total`: 총 요청 수
- `hexwar_request_duration_seconds`: 요청 처리 시간

## 개발

### 프로젝트 구조

```
hexwar-exporter/
├── main.go              # 메인 애플리케이션
├── exporter/            # Exporter 로직
├── go.mod               # Go 모듈 정의
├── go.sum               # 의존성 체크섬
├── Makefile             # 빌드 스크립트
└── Dockerfile           # Docker 이미지 정의
```

### 빌드 및 테스트

```bash
# 빌드
make build

# 테스트
make test

# 린트
make lint

# 정리
make clean
```

## 라이선스

[라이선스 정보를 추가하세요]

## 기여

버그 리포트 및 기능 요청은 [Issues](../../issues)를 통해 제출해주세요.

## 지원

문제가 발생하면 [Issues](../../issues)에 자세한 설명과 함께 보고해주세요.

---

**작성일**: 2026-07-21  
**버전**: 1.0.0
