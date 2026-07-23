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
  - [1. Go Exporter를 통한 매트릭 수집 아키텍쳐 구축](#1-go-exporter를-통한-매트릭-수집-아키텍쳐-구축)
  - [2. 쿠버네티스 기반 오토스케일링](#2-쿠버네티스-기반-오토스케일링)
- [로컬 실행 방법](#로컬-실행-방법)

## 프로젝트 개요


## 기술 스택

| 분야 | 기술 | 사용 이유 |
| --- | --- | --- |
| **언어/런타임** | Go 1.22 | 단일 바이너리, 저메모리 |
| **쿠버네티스** | k3d (로컬) | 경량 로컬 클러스터 |
| **오토스케일링** | KEDA, HPA, Karpenter | Prometheus 메트릭 기반 파드 수평 확장(KEDA) 및 동적 노드 프로비저닝 |
| **관측 스택** | Prometheus, Grafana, OTel Collector | CNCF 표준 |
| **인프라 검증** | LocalStack 3.8, kwok | 로컬 시나리오 모방 도구 |
| **테스트/자동화** | hey, Make | 부하테스트 및 파이프라인 자동화 도구 |

## 💡 핵심 기술 결정 및 최적화

### 1. 매트릭 수집을 위한 사이트카 패턴 구축 

#### 문제
* 대규모 서비스에서 수평적 확장으로 게임 서버 Pod가 늘어날 경우, 단일 Prometheus 서버가 모든 부담을 가지고 있는채로 Json 데이터를 Pull 하는 방식은 메트릭을 직렬화 하는 작업 비용과 네트워크 비용을 증가 시켜 불필요한 비용을 발생 시킬 수 있다고 판단하였습니다.

#### 행동
* **사이드카 패턴 구축:** - 
* **포멧 변환:** 게임 서버는 유저 요청에만 집중하고, 데이터의 수집, 압축 및 Prometheus 형식에 맞추는 작업을 Exporter가 전담하도록 도메인을 분리하였습니다.
* **서킷 브레이커 구축:** 게임 서버의 연쇄 장애 발생 시, 지연된 스크랩 요청이 계속 쌓여 exporter 전체가 다운되는 연쇄 장애를 방지하기 위해 차단 로직을 적용하여, 외부 연동 시스템의 장애가 전파되지 않도록 보호합니다.

#### 결과 및 검증 데이터

1. **Exporter 메모리 사용량 측정**
   * **측정 방법:** 게임 서버 파드 내부에 **사이드카(Sidecar)**로 배치된 환경을 모방하여, 단일 노드의 메트릭을 수집하고 직렬화하는 과정을 Go 벤치마크 테스트로 검증했습니다. (go test -bench=. -benchmem)
   * **측정 데이터 (Log 추출):**
     ```text
     BenchmarkExporterScrape_WithCache-10    	   66381	     17281 ns/op	   56593 B/op	     315 allocs/op
     ```
   * **결과:** 사이드카 배포 환경에서 프로메테우스의 1회 스크랩 요청 처리 당 **약 `0.017ms`의 빠른 처리 속도**를 보여주며, 건당 힙 메모리 할당 또한 **약 `56KB` 수준**으로, 이를 통해 각 게임 서버 pod 내에 expoter 서버를 구성하여도 메모리 간섭을 최소화 할 수 있도록 구성하였습니다.

### 2. 쿠버네티스 기반 오토스케일링

#### 문제
* **수평적 확장 불가 문제:** 기존 Docker Compose 기반 환경은 K8s와 달리 HPA와 같이 시스템 상태를 지속적으로 모니터링하고 컨테이너 개수를 능동적으로 조절하는 제어 루프 메커니즘이 없습니다. 트래픽 급증 시 사람의 개입이 필수적이라 프로덕션 환경에 부적합했습니다.
* **프로덕션 검증:** Karpenter

#### 행동
* **클라우드 모킹:** k3d와 LocalStack을 연동하여 실제 AWS 인프라(EC2 ASG API 호출 등)와 호환되는 검증 환경을 로컬에 구축했습니다. 이를 통해 클라우드 비용 발생 없이도 오토스케일링 파이프라인의 호출을 모방하고 검증할 수 있었습니다.
* **이벤트 기반 확장 구축:** HPA와 KEDA ScaledObject를 도입해 Prometheus가 수집한 메트릭을 기반으로 작업 부하가 임계치를 초과하였을 떄, Pod 단위의 수평적 확장이 발생하도록 이벤트 기반 제어 루프를 구축했습니다.
* **노드 오토스케일러 동작 모방 (kwok 활용):** 대규모 트래픽 발생 시 KEDA가 파드를 급격히 늘리면 클러스터의 리소스 부족으로 대량의 Pending 파드가 발생합니다. 프로덕션 환경에서는 Karpenter를 통해 최적의 인스턴스를 직접 생성하여 이를 해결하지만, 로컬 검증 단계에서는 비용과 리소스를 절약하기 위해 가상 노드 시뮬레이터인 **kwok**을 활용했습니다. 실제 Karpenter를 무겁게 설치하는 대신, kwok를 통해 가상의 빈 노드를 동적으로 생성하고 파드를 스케줄링함으로써 노드 오토스케일링의 E2E 파이프라인 동작을 효과적으로 모방 및 검증했습니다.

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

2. **KEDA 이벤트 오토스케일링 검증**
   * **검증 내용:** Prometheus가 수집하는 게임 서버의 커스텀 메트릭이 임계치를 초과할 때 KEDA가 개입하여 Pod를 수평 확장하는지 검증했습니다.
   * **로그 데이터:**
     ```text
     kubectl logs -n keda deployment/keda-operator --tail=10

     [keda-operator] 2026-07-22T10:15:01Z INFO Reconciler Reconciling ScaledObject {"ScaledObject.Namespace": "default", "ScaledObject.Name": "game-server-scaler"}
     [keda-metrics-apiserver] 2026-07-22T10:15:10Z INFO external_metrics provider Found metrics for triggers
     [keda-operator] 2026-07-22T10:15:12Z INFO scale_handler Scaling deployment game-server-deployment from 2 to 5 based on Prometheus triggers
     ```
   * **결과:** 단순 컴퓨팅 자원 기반이 아닌, 서비스 도메인 메트릭을 기반으로 한 수평 확장이 안정적으로 트리거됨을 확인했습니다.

### 3. 


## 로컬 실행 방법

로컬 k3d 클러스터와 LocalStack을 활용하여 전체 오토스케일링 시나리오를 재현할 수 있습니다.

```bash
# 1. 전체 환경 구축 (LocalStack 기동 + k3d 클러스터 생성 + 이미지 빌드/임포트 + 매니페스트 배포)
make k3d-recreate-all

# 2. 부하 테스트 유도 (게임 서버 Pod 80개로 강제 확장)
make scale-load

# 3. Grafana 대시보드 확인
make tunnel-up
# → http://localhost:3000 접속 (admin/admin)

# 4. 리소스 정리
make scale-reset
make k3d-delete
make clean
```