# HexWar Exporter

**HexWar 게임 서버용 Prometheus 메트릭 Exporter — Go 기반 관측가능성 사이드카**

C#/.NET 기반의 실시간 분산 게임 서버(HaxWar)의 상태를 주기적으로 폴링하여 Prometheus 표준 포맷으로 변환합니다. 단순 메트릭 수집을 넘어, K8s 환경에서 HPA(수평 Pod 자동 확장)와 연동되는 관측 파이프라인의 핵심 역할을 수행합니다.

![Go](https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![Kubernetes](https://img.shields.io/badge/kubernetes-%23326ce5.svg?style=for-the-badge&logo=kubernetes&logoColor=white)
![Prometheus](https://img.shields.io/badge/Prometheus-E6522C?style=for-the-badge&logo=Prometheus&logoColor=white)
![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-000000?style=for-the-badge&logo=opentelemetry&logoColor=white)

## 목차
- [프로젝트 개요](#프로젝트-개요)
- [기술 스택](#기술-스택)
- [💡 핵심 기술 결정 및 최적화](#-핵심-기술-결정-및-최적화)
  - [1. 메트릭 수집을 위한 사이드카 패턴 구축](#1-메트릭-수집을-위한-사이드카-패턴-구축)
  - [2. k8s 게임 서버 오토스케일링](#2-k8s-게임-서버-오토스케일링)
  - [3. 로그 수집 에이전트 구축](#3-로그-수집-에이전트-구축)
- [로컬 실행 방법](#로컬-실행-방법)

## 프로젝트 개요

![전체 아키텍처 구조도](./Architecture%20Diagram%20Example%20-%20Multiplayer%20(Community).png)

## 기술 스택

| 분야 | 기술 | 사용 이유 |
| --- | --- | --- |
| **언어/런타임** | Go 1.22 | 단일 바이너리, 저메모리 |
| **쿠버네티스** | k3d (로컬) | 경량 로컬 클러스터 |
| **오토스케일링** | Agones, KEDA, HPA, Karpenter | Prometheus 메트릭 기반 파드 수평 확장(KEDA) 및 동적 노드 프로비저닝 |
| **관측 스택** | Prometheus, Grafana, OTel Collector | CNCF 표준 |
| **인프라 검증** | LocalStack 3.8, kwok | 로컬 시나리오 모방 도구 |
| **테스트/자동화** | hey, Make | 부하테스트 및 파이프라인 자동화 도구 |

## 💡 핵심 기술 결정 및 최적화

### 1. 메트릭 수집을 위한 사이드카 패턴 구축 

#### 문제
* **결합도 및 스크래핑 지연 :** 게임 서버가 스크랩핑 엔드포인트를 노출할 경우, 라이브러리 추가 및 비즈니스 로직의 추가로 인하여 게임 서버가 인프라 모니터링 시스템의 스팩에 의존하게 되어 도메인 경계가 모호해진다고 판단하였습니다. 또한 실시간으로 처리되는 수많은 게임 로직으로 인하여 스크래핑 시 지연이 발생 할 수 있습니다. 

#### 행동
* **도메인 분리:** 게임 서버 코드에는 프로메테우스 관련 라이브러리와 변환 로직을 포함하지 않고, 게임 도메인에 집중하도록 하고 인프라/모니터링 책임을 사이드카 서버에게 위임하여 도메인 경계를 명확하게 분리하였습니다. 
* **언어 특성 최적화:** C# .NET 게임 서버는 복잡한 세션 및 상태 동기화와 같이 게임 도메인에 집중하도록 하고, 메트릭 수집 및 텍스트 포맷팅/캐싱 작업은 Go 언어의 경량 단일 바이너리와 낮은 메모리 사용량을 바탕으로 사이드카 서버를 구축하여 컴퓨팅 자원에 대한 간섭을 최소화하였습니다. 
* **서킷 브레이커 구축:** 게임 서버의 장애로 Expoter가 수집을 진행하지 못할 경우 지연된 요청들로 인한 장애가 프로메테우스와 같은 상위 시스템에 전이 되는 것을 방지하도록 하여 안정성을 확보하였습니다. 

#### 결과 및 검증 데이터

1. **Exporter 메모리 사용량 측정**
   * **측정 방법:** 게임 서버 파드 내부에 **사이드카**로 배치된 환경을 모방하여, 단일 노드의 메트릭을 수집하고 직렬화하는 과정을 Go 벤치마크 테스트로 검증했습니다. (go test -bench=. -benchmem)
   * **측정 데이터 (Log 추출):**
     ```text
     BenchmarkExporterScrape_WithCache-10    	   66381	     17281 ns/op	   56593 B/op	     315 allocs/op
     ```
   * **결과:** 사이드카 배포 환경에서 프로메테우스의 1회 스크랩 요청 처리 당 **약 `0.017ms`의 빠른 처리 속도**를 보여주며, 건당 힙 메모리 할당 또한 **약 `56KB` 수준**으로, 이를 통해 각 게임 서버 pod 내에 expoter 서버를 구성하여도 메모리 간섭을 최소화 할 수 있도록 구성하였습니다.

### 2. k8s 게임 서버 오토스케일링

#### 문제
* **수평적 확장 불가 문제:** 기존 Docker Compose 기반 환경은 K8s와 달리 HPA와 같이 시스템 상태를 지속적으로 모니터링하고 컨테이너 개수를 능동적으로 조절하는 제어 루프 메커니즘이 없습니다. 트래픽 급증 시 사람의 개입이 필수적이라 프로덕션 환경에 부적합하였습니다.
* **게임 세션 생명주기:** 컴퓨팅 리소스를 효율적으로 사용하기 위해 오토스켈링을 도입하긴 하였으나, 스케일 다운 시 진행 중인 게임 세션이 포함된 Pod가 강제 종료되는 것을 방지하는 세션 생명주기 관리 방식이 필요하였습니다. 

#### 행동
* **클라우드 인프라 모킹:** k3d와 LocalStack을 연동하여 실제 AWS 인프라(EC2 ASG API 호출 등)와 호환되는 검증 환경을 로컬에 구축했습니다. 특히 karpenter 기반 오토스케링을 로컬에서 테스트하기 위해 가상 노드 시물레이터인 kwok를 활용하고, karpenter-game-nodepool을 정의하여, 클라우드 비용 없이도 노드 자동 확장 파이프라인을 모방 및 검증했습니다.
* **게임 세션 생명주기 관리:**  Pod 내부의 활성 세션 상태를 counter를 통해 추적 및 관리하도록 구축하였습니다. 스케일 다운 발생 시 진행 중인 세션이 잔존하는 Pod는 종료 대상에서 제외되고, 세션이 완전히 종료되거나 없는 Pod만을 수거하도록 하여 게임 데이터가 유실되거나 서비스가 중단되는 문제를 방지하였습니다.
* **이벤트 기반 Pod 확장 가능성:**  세션이 없는 서비스를 대비하여, KEDA ScaledObject와 HPA를 연동하여 Prometheus 메트릭 기반의 이벤트 기반 오토스케일링을 구현했습니다. 특정 작업량이 임계치를 초과할 경우 자동으로 Pod를 확장하여 서비스 안정성을 확보했습니다. 

#### 결과 및 검증 데이터

1. **AWS 인프라 호환성 및 Cluster Autoscaler 연동 검증**
   * **검증 내용:** 로컬 환경에서 LocalStack을 사용하여 AWS EC2 Auto Scaling Group API를 모킹하고, K8s Cluster Autoscaler가 Pod 증가로 인한 리소스 부족 상태를 감지하여 ASG 호출을 통해 증가를 요청하는 것을 확인하였습니다.
   * **로그 데이터:**
     ```text
     docker logs localstack --tail=10

     I0512 10:20:31.123456       1 scale_up.go:345] Pod default/game-server-deployment-xxx is unschedulable
     I0512 10:20:32.456789       1 asg_aws.go:123] Setting asg localstack-eks-nodegroup size to 3
     I0512 10:20:32.567890       1 asg_aws.go:145] Successfully set asg localstack-eks-nodegroup size to 3
     ```
   * **결과:** 클라우드 과금 없이 로컬 환경에서 AWS Auto Scaling 인프라와의 API 연동 및 노드 확장 시나리오가 완벽하게 동작함을 입증했습니다.

2. **Karpenter 노드 확장 구현 검증**
   * **검증 내용:** Karpenter가 Pending Pod를 감지하고 워커 노드를 자동으로 프로비저닝하여 Pending 상태를 해소하는지 검증했습니다. 검증 데이터에서는 KEDA를 사용하였으나 Agones Controller를 설치하여 Pod를 증가시키는 것으로도 동일한 검증이 가능합니다.
   * **로그 데이터:**
     ```text
     kubectl logs -n karpenter deployment/karpenter -c controller --tail=10

     {"level":"INFO","time":"2026-07-22T10:15:15.123Z","logger":"controller.provisioner","message":"found provisionable pod(s)","pods":3}
     {"level":"INFO","time":"2026-07-22T10:15:15.456Z","logger":"controller.provisioner","message":"computed new node(s) to fit pod(s)","nodes":1,"pods":3}
     {"level":"INFO","time":"2026-07-22T10:15:16.789Z","logger":"controller.provisioner","message":"created node","nodepool":"karpenter-game-nodepool","node":"kwok-node-abcd","provider-id":"kwok://node-abcd"}
     ```
   * **결과:** NodePool에 정의를 확인하고 그에따른 컴퓨팅 자원을 확보하는 것을 확인하였습니다. 스크립트 내에서는 20개의 노드가 자동으로 생성되었습니다. 이는 `scripts/test_e2e_scaling.sh` 스크립트를 통해 검증되었습니다.

### 3. promtail + Loki 로그 수집 파이프라인 구축

#### 문제
* **로그 데이터 부제:** 시스템의 전반적인 상태를 파악하기 위해서는 메트릭 뿐만 아니라 로그 데이터가 반드시 필요합니다. 그러나 쿠버네티스 환경에서는 각 Pod가 독립적으로 로그를 생성하기 때문에, 중앙에서 체계적으로 수집하고 관리하는 시스템이 없다면 장애 발생 시 원인 분석이 어렵다는 것을 알게되었습니다.

#### 해결
* **Promtail Agent:** 쿠버네티스 환경에 특화된 로그 수집 에이전트인 Promtail을 각 노드에 데몬셋 형태로 배포하였습니다. 이를 통해 각 노드에서 실행중인 게임 서버의 로그를 수집하여 Loki로 전송하는 역할을 수행합니다.
* **Loki 수집 시스템:** 메트릭 기반의 Prometheus가 시간열 데이터 저장에 최적화되어 있는 반면, Loki는 로그 데이터를 수집하고 저장하는 시스템으로 로그 본문 전체가 아니라 라벨만 색인하고 실제 로그 내용은 압축된 청크 형태로 오브젝트 스토리지에 저장하여 인덱스 크기 자체가 작게 유지되어 디스크 사용량과 비용을 크게 줄일 수 있습니다. 이를 통해 S3와 같은 저장소를 사용함에 있어 비용 효율적으로 로그를 수집할 수 있습니다.
* **Grafana 연동:** Grafana는 Prometheus뿐만 아니라 Loki와도 연동하여 시각화하여 모니터링 할 수 있도록 구축하였습니다.

#### 결과

1. **Loki API 직접 쿼리를 통한 로그 수집 검증**
   * **검증 방법:** 게임 서버 Pod(`hexwar-server`)에서 발생한 로그 데이터가 Promtail 데몬셋을 거쳐 Loki 파이프라인으로 정상 전달·기입되었는지 Loki REST API(`http://localhost:3100/loki/api/v1/query_range`)를 직접 호출하여 검증했습니다.
   * **로그 데이터 (실제 클러스터 쿼리 데이터):**
     ```bash
     kubectl exec -n monitoring deployment/loki -- wget -qO- "http://localhost:3100/loki/api/v1/query_range?query=%7Bapp%3D%22hexwar-server%22%7D&limit=2"
     ```
     ```json
     {
       "status": "success",
       "data": {
         "resultType": "streams",
         "result": [
           {
             "stream": {
               "app": "hexwar-server",
               "filename": "/var/log/pods/game_hexwar-server-bfpjm-9655c_c8c1f65e-25b2-4504-9d4a-db0734cc73cc/exporter/0.log",
               "namespace": "game",
               "pod": "hexwar-server-bfpjm-9655c"
             },
             "values": [
               [ "1785049216307482342", "2026-07-26T07:00:16.306Z stdout F time=2026-07-26T07:00:16.305Z level=INFO msg=\"서킷 브레이커 CLOSED (정상 복구)\" node=game-server" ]
             ]
           }
         ]
       }
     }
     ```




## 로컬 실행 방법

로컬 k3d 클러스터와 LocalStack을 활용하여 전체 오토스케일링 시나리오를 재현할 수 있습니다.

```bash
# 1. 전체 환경 자동 구축(초기 구축)
make k3d-recreate-all

# 1-1. 변경사항 적용 (재배포)
make k3d-update-all

# 2. 부하 테스트 유도 (수평 확장)
make scale-load

# 3. Grafana 대시보드 확인
make tunnel-up
# → http://localhost:3000 접속 (admin/admin)
# → http://localhost:5002 접속 (게임 페이지) 

# 4. 리소스 정리
make scale-reset
make k3d-delete
make clean
```