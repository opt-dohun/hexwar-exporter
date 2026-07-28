.PHONY: setup-all create-vpc create-subnet create-lc create-asg status clean \
        localstack-up localstack-down \
        obs-up obs-down \
        obs-merged-up obs-merged-down \
        k3d-create k3d-start k3d-delete \
        helm-repo helm-install helm-uninstall helm-upgrade \
        k8s-pods k3d-import-server k3d-deploy-exporter exporter-restart \
		kafka-install kafka-uninstall \
		quickwit-install quickwit-uninstall \
		vector-daemonset vector-daemonset-uninstall

# LocalStack 호스트 주소가 필요한 경우 환경 변수 설정 (기본값: http://localhost:4566)
AWS_ENDPOINT_URL ?= http://localhost:4566

# LocalStack에서 생성된 VPC ID 동적 쿼리
VPC_ID = $(shell awslocal ec2 describe-vpcs --filters "Name=cidr,Values=10.0.0.0/16" --query "Vpcs[0].VpcId" --output text 2>/dev/null)

# LocalStack에서 생성된 Subnet ID 동적 쿼리
SUBNET_ID = $(shell awslocal ec2 describe-subnets --filters "Name=vpc-id,Values=$(VPC_ID)" --query "Subnets[0].SubnetId" --output text 2>/dev/null)

setup-all: create-vpc create-subnet create-lc create-asg status
	@echo "=== [성공] 모든 로컬 인프라 구성이 완료되었습니다. ==="

create-vpc:
	@if [ "$(VPC_ID)" = "None" ] || [ -z "$(VPC_ID)" ]; then \
		echo "=== 1. 가상 VPC 생성 시작 ==="; \
		VPC=$$(awslocal ec2 create-vpc --cidr-block 10.0.0.0/16 --query 'Vpc.VpcId' --output text); \
		echo "VPC 생성 완료: $$VPC"; \
	else \
		echo "=== 1. 가상 VPC 가 이미 존재합니다: $(VPC_ID) ==="; \
	fi

create-subnet: create-vpc
	@# VPC ID가 갱신될 수 있도록 서브쉘 또는 실시간 쿼리를 수행합니다.
	@$(eval VPC_ID_CURRENT := $(shell awslocal ec2 describe-vpcs --filters "Name=cidr,Values=10.0.0.0/16" --query "Vpcs[0].VpcId" --output text 2>/dev/null))
	@if [ "$(SUBNET_ID)" = "None" ] || [ -z "$(SUBNET_ID)" ]; then \
		echo "=== 2. 가상 서브넷 생성 시작 (VPC: $(VPC_ID_CURRENT)) ==="; \
		SUB=$$(awslocal ec2 create-subnet --vpc-id "$(VPC_ID_CURRENT)" --cidr-block 10.0.1.0/24 --query 'Subnet.SubnetId' --output text); \
		echo "서브넷 생성 완료: $$SUB"; \
	else \
		echo "=== 2. 가상 서브넷이 이미 존재합니다: $(SUBNET_ID) ==="; \
	fi

create-lc:
	@echo "=== 3. 가상 시작 구성(Launch Configuration) 생성 시작 ==="
	-@if ! awslocal autoscaling describe-launch-configurations --launch-configuration-names hexwar-lc --query "LaunchConfigurations[0].LaunchConfigurationName" --output text 2>/dev/null | grep -q "hexwar-lc"; then \
		awslocal autoscaling create-launch-configuration \
			--launch-configuration-name hexwar-lc \
			--image-id ami-12345678 \
			--instance-type t3.medium; \
		echo "시작 구성 생성 완료: hexwar-lc"; \
	else \
		echo "시작 구성이 이미 존재합니다: hexwar-lc"; \
	fi

create-asg: create-subnet create-lc
	@$(eval SUBNET_ID_CURRENT := $(shell awslocal ec2 describe-subnets --filters "Name=vpc-id,Values=$(VPC_ID)" --query "Subnets[0].SubnetId" --output text 2>/dev/null))
	@echo "=== 4. 가상 오토스케일링 그룹(ASG) 생성 시작 ==="
	-@if ! awslocal autoscaling describe-auto-scaling-groups --auto-scaling-group-names hexwar-node-group --query "AutoScalingGroups[0].AutoScalingGroupName" --output text 2>/dev/null | grep -q "hexwar-node-group"; then \
		awslocal autoscaling create-auto-scaling-group \
			--auto-scaling-group-name hexwar-node-group \
			--launch-configuration-name hexwar-lc \
			--min-size 1 \
			--max-size 5 \
			--desired-capacity 1 \
			--vpc-zone-identifier "$(SUBNET_ID_CURRENT)"; \
			echo "오토스케일링 그룹 생성 완료: hexwar-node-group"; \
	else \
		echo "오토스케일링 그룹이 이미 존재합니다: hexwar-node-group"; \
	fi

status:
	@echo "\n=============================================="
	@echo "           현재 LocalStack 리소스 상태"
	@echo "=============================================="
	@echo "\n[VPCs]"
	@awslocal ec2 describe-vpcs --query "Vpcs[*].{Id:VpcId,Cidr:CidrBlock,State:State}" --output table
	@echo "\n[Subnets]"
	@awslocal ec2 describe-subnets --query "Subnets[*].{Id:SubnetId,Vpc:VpcId,Cidr:CidrBlock}" --output table
	@echo "\n[Launch Configurations]"
	-@awslocal autoscaling describe-launch-configurations --query "LaunchConfigurations[*].{Name:LaunchConfigurationName,Image:ImageId,Type:InstanceType}" --output table
	@echo "\n[Auto Scaling Groups]"
	-@awslocal autoscaling describe-auto-scaling-groups --query "AutoScalingGroups[*].{Name:AutoScalingGroupName,LC:LaunchConfigurationName,Desired:DesiredCapacity,Min:MinSize,Max:MaxSize}" --output table


clean:
	@echo "=== LocalStack 리소스 정리 시작 ==="
	-awslocal autoscaling delete-auto-scaling-group --auto-scaling-group-name hexwar-node-group --force-delete
	-awslocal autoscaling delete-launch-configuration --launch-configuration-name hexwar-lc
	-if [ -n "$(SUBNET_ID)" ] && [ "$(SUBNET_ID)" != "None" ]; then awslocal ec2 delete-subnet --subnet-id "$(SUBNET_ID)"; fi
	-if [ -n "$(VPC_ID)" ] && [ "$(VPC_ID)" != "None" ]; then awslocal ec2 delete-vpc --vpc-id "$(VPC_ID)"; fi
	@echo "정리가 완료되었습니다."

-include .env

# ── Docker Compose 제어 명령어 ──

# 메인 프로젝트(HaxWar) 경로 및 docker-compose.yml 경로 설정
MAIN_PROJECT_DIR ?= ./third_party/HaxWar
MAIN_COMPOSE_FILE ?= $(MAIN_PROJECT_DIR)/docker-compose.yml
SERVER_REGISTRY_IMAGE ?= ghcr.io/opt-dohun/haxwar:latest

# LocalStack 컨테이너 기동 및 중지
localstack-up:
	docker compose -f deploy/docker-compose.localstack.yml up -d

localstack-down:
	docker compose -f deploy/docker-compose.localstack.yml down

# Observability 스택 단독 실행 및 중지
obs-up:
	docker compose -f deploy/docker-compose.observability.yml up -d

obs-down:
	docker compose -f deploy/docker-compose.observability.yml down

# 메인 프로젝트의 docker-compose.yml과 병합하여 실행 및 중지
obs-merged-up:
	@if [ -f "$(MAIN_COMPOSE_FILE)" ]; then \
		echo "메인 프로젝트의 docker-compose.yml과 병합하여 실행합니다."; \
		docker compose -f "$(MAIN_COMPOSE_FILE)" -f deploy/docker-compose.observability.yml up -d; \
	else \
		echo "오류: 메인 프로젝트 docker-compose.yml을 찾을 수 없습니다: $(MAIN_COMPOSE_FILE)"; \
		echo "대신 deploy/docker-compose.observability.yml 단독 실행합니다."; \
		docker compose -f deploy/docker-compose.observability.yml up -d; \
	fi

obs-merged-down:
	@if [ -f "$(MAIN_COMPOSE_FILE)" ]; then \
		docker compose -f "$(MAIN_COMPOSE_FILE)" -f deploy/docker-compose.observability.yml down; \
	else \
		docker compose -f deploy/docker-compose.observability.yml down; \
	fi

# ── k3d 클러스터 제어 명령어 ──

# 기본 변수 설정
K3D_CLUSTER_NAME ?= hexwar-cluster
K3D_SERVERS ?= 1
K3D_OPTS ?= 

# k3d 클러스터 생성 (추가 옵션을 줄 수 있음)
# 예: make k3d-create K3D_OPTS="--port 8080:80@loadbalancer"
k3d-create:
	k3d cluster create $(K3D_CLUSTER_NAME) --servers $(K3D_SERVERS) $(K3D_OPTS)

k3d-start:
	k3d cluster start $(K3D_CLUSTER_NAME)

# k3d 클러스터 삭제
k3d-delete:
	k3d cluster delete $(K3D_CLUSTER_NAME)

# ── Helm 차트 제어 명령어 ──

# 기본 변수 설정
HELM_NAMESPACE ?= monitoring
HELM_VALUES_FILE ?= deploy/autoscaler-values.yaml

# Helm 레포지토리 추가 및 업데이트
helm-repo:
	helm repo add autoscaler https://kubernetes.github.io/autoscaler || true
	helm repo update

# cluster-autoscaler 설치
helm-install: helm-repo
	helm install cluster-autoscaler autoscaler/cluster-autoscaler \
		-f $(HELM_VALUES_FILE) \
		-n $(HELM_NAMESPACE) \
		--create-namespace

# cluster-autoscaler 삭제
helm-uninstall:
	helm uninstall cluster-autoscaler -n $(HELM_NAMESPACE)

# cluster-autoscaler 업그레이드
helm-upgrade:
	helm upgrade cluster-autoscaler autoscaler/cluster-autoscaler \
		-f $(HELM_VALUES_FILE) \
		-n $(HELM_NAMESPACE)

# Agones 설치 및 대기
agones-install:
	@echo "=== Agones 시스템 설치 시작 ==="
	helm repo add agones https://agones.dev/chart/stable || true
	helm repo update
	helm install my-release --namespace agones-system --create-namespace agones/agones
	@echo "=== Agones 컨트롤러 기동 대기 (최대 120초) ==="
	kubectl wait --for=condition=Ready pods --all -n agones-system --timeout=120s
	@echo "=== Agones 시스템 설치 완료 ==="

# kafka 클러스터 구성 및 대기
kafka-install:
	@echo "=== kafka 클러스터 구성 및 설치 시작 ==="
	kubectl create namespace observability || true
	kubectl apply -f deploy/logging/kafka-dev.yaml
	@echo "=== kafka 기동 대기 ==="
	sleep 5
	kubectl wait --for=condition=Ready pod/kafka-0 -n observability --timeout=120s
	@echo "=== game-logs 토픽 사전 생성 ==="
	kubectl exec -n observability kafka-0 -- /opt/kafka/bin/kafka-topics.sh --create --topic game-logs --bootstrap-server localhost:9092 --partitions 1 --replication-factor 1 --if-not-exists || true
	@echo "=== kafka 클러스터 설치 완료 ==="

# kafka 클러스터 삭제
kafka-uninstall:
	@echo "=== kafka 클러스터 삭제 ==="
	kubectl delete -f deploy/logging/kafka-dev.yaml || true
# minio 구성 및 버킷 생성
minio-install:
	@echo "=== MinIO 배포 및 버킷 사전 생성 시작 ==="
	kubectl apply -f deploy/logging/minio.yaml
	@echo "=== MinIO 기동 대기 ==="
	kubectl wait --for=condition=Ready pod -l app=minio -n observability --timeout=120s
	@echo "=== MinIO 버킷 생성 (quickwit-indexes) ==="
	kubectl exec -n observability deploy/minio -- mkdir -p /data/quickwit-indexes || true
	@echo "=== MinIO 구성 완료 ==="

minio-uninstall:
	@echo "=== MinIO 삭제 ==="
	kubectl delete -f deploy/logging/minio.yaml || true

# Quickwit 구성 및 대기
quickwit-install:
	@echo "=== Quickwit 클러스터 구성 및 설치 시작 ==="
	helm repo add quickwit https://quickwit-hub.github.io/helm-charts || true
	helm repo update
	helm install quickwit quickwit/quickwit -n observability -f deploy/logging/quickwit-values.yaml
	@echo "=== Quickwit 클러스터 기동 대기 (최대 300초) ==="
	kubectl wait --for=condition=Ready pods --all -n observability --timeout=300s
	@echo "=== Quickwit 클러스터 설치 완료 ==="
	@echo "=== Quickwit 인덱스 생성 시작 ==="
	kubectl cp deploy/logging/quickwit-index-config.yaml observability/quickwit-searcher-0:/tmp/config.yaml
	kubectl exec -n observability -it quickwit-searcher-0 -- quickwit index create --index-config /tmp/config.yaml
	@echo "=== Quickwit 인덱스 생성 완료 ==="
	@echo "=== Quickwit 소스 생성 시작 ==="
	kubectl cp deploy/logging/quickwit-source-config.yaml observability/quickwit-searcher-0:/tmp/config.yaml
	kubectl exec -n observability -it quickwit-searcher-0 -- quickwit source create --index game-logs --source-config /tmp/config.yaml
	@echo "=== Quickwit 소스 생성 완료 ==="
	@echo "=== Quickwit 클러스터 설치 완료 ==="

# Vector 데몬셋 배포
vector-daemonset:
	@echo "=== Vector 데몬셋 배포 시작 ==="
	kubectl apply -f deploy/logging/vector-configmap.yaml
	kubectl apply -f deploy/logging/vector-daemonset.yaml
	@echo "=== Vector 데몬셋 배포 완료 ==="

# Vector 데몬셋 삭제
vector-daemonset-uninstall:
	@echo "=== Vector 데몬셋 삭제 시작 ==="
	kubectl delete -f deploy/logging/vector-daemonset.yaml
	kubectl delete -f deploy/logging/vector-configmap.yaml
	@echo "=== Vector 데몬셋 삭제 완료 ==="


# Quickwit 삭제
quickwit-uninstall:
	@echo "=== Quickwit 클러스터 삭제 ==="
	helm uninstall quickwit -n observability
	

# Agones 삭제
agones-uninstall:
	@echo "=== Agones 시스템 삭제 ==="
	helm uninstall my-release -n agones-system

# ── Kubernetes 제어 명령어 ──

# 서브모듈 초기화 및 업데이트
init-submodule:
	@echo "=== HaxWar 서브모듈 초기화 및 업데이트 ==="
	git submodule update --init --recursive

# 메인 프로젝트의 docker 이미지를 로컬(서브모듈)에서 빌드하여 k3d 클러스터에 로드
k3d-import-server: init-submodule
	@echo "=== HaxWar 프로젝트 빌드 및 k3d 업로드 시작 ==="
	cd $(MAIN_PROJECT_DIR) && \
	docker build -t hexwar-server-1:latest -f src/HexWar.Server/Dockerfile . && \
	k3d image import hexwar-server-1:latest -c $(K3D_CLUSTER_NAME)

# 미리 빌드된 레지스트리 이미지를 가져와 k3d 클러스터에 로드
k3d-pull-server:
	@echo "=== 원격 레지스트리에서 서버 이미지 Pull 및 k3d 업로드 시작 ==="
	docker pull $(SERVER_REGISTRY_IMAGE)
	docker tag $(SERVER_REGISTRY_IMAGE) hexwar-server-1:latest
	k3d image import hexwar-server-1:latest -c $(K3D_CLUSTER_NAME)

# ── Exporter 및 K8s 모니터링 이식 제어 명령어 ──

# exporter 이미지 빌드 및 k3d 임포트
k3d-import-exporter:
	@echo "=== hexwar-exporter 이미지 빌드 및 k3d 업로드 시작 ==="
	docker build -t hexwar-exporter:latest -f deploy/Dockerfile .
	k3d image import hexwar-exporter:latest -c $(K3D_CLUSTER_NAME)

# exporter 디플로이먼트 재시작 및 파드 상태 조회
exporter-restart:
	kubectl rollout restart deployment hexwar-exporter -n $(HELM_NAMESPACE)
	sleep 5
	kubectl get pods -n $(HELM_NAMESPACE)

# K8s 내부 이식 모니터링 스택 (Prometheus, Grafana, OTel) 배포 및 중지
k8s-monitoring-up:
	@echo "=== K8s 내부 모니터링 스택(Prometheus, Grafana, OTel Collector) 배포 ==="
	kubectl apply -f scratch/k8s-monitoring-stack.yaml

k8s-monitoring-down:
	@echo "=== K8s 내부 모니터링 스택 삭제 ==="
	kubectl delete -f scratch/k8s-monitoring-stack.yaml

# C# 게임 서버 K8s 배포 및 중지
k8s-server-up:
	@echo "=== C# 게임 서버 배포 ==="
	kubectl apply -f scratch/hexwar-server-deployment.yaml

k8s-server-down:
	@echo "=== C# 게임 서버 삭제 ==="
	kubectl delete -f scratch/hexwar-server-deployment.yaml

# hexwar-exporter K8s 배포 및 중지
k8s-exporter-up:
	@echo "=== hexwar-exporter 배포 ==="
	kubectl apply -f scratch/k8s-deployment.yaml

k8s-exporter-down:
	@echo "=== hexwar-exporter 삭제 ==="
	kubectl delete -f scratch/k8s-deployment.yaml

# Redis 클러스터 K8s 배포 및 중지 (순서 대기 포함)
k8s-redis-up:
	@echo "=== Redis 클러스터 인프라 배포 ==="
	kubectl apply -f scratch/redis-cluster-deployment.yaml
	@echo "=== Pod 리소스 생성 대기... ==="
	sleep 3
	@echo "=== Redis 노드 기동 대기 (최대 120초) ==="
	kubectl wait --namespace=hexwar-infra --for=condition=ready pod -l app=redis-cluster --timeout=120s

k8s-redis-down:
	@echo "=== Redis 클러스터 인프라 삭제 ==="
	kubectl delete -f scratch/redis-cluster-deployment.yaml

# 브라우저 뷰어용 Grafana 포트포워딩 실행
tunnel-up:
	@echo "=== Grafana 뷰어 포트포워딩 시작 (http://localhost:3000) ==="
	kubectl port-forward service/grafana -n $(HELM_NAMESPACE) 3000:3000 --address=0.0.0.0 &
	@echo "=== hexwar-server 포트포워딩 시작 (http://localhost:5002) ==="
	kubectl port-forward service/hexwar-server -n game 5002:5000 --address=0.0.0.0

# 모니터링 포트포워딩 중지
tunnel-down:
	@echo "=== 포트포워딩 종료 ==="
	@PIDS=$$(lsof -t -i:3000 -i:5002 2>/dev/null); \
	if [ -n "$$PIDS" ]; then \
		kill -9 $$PIDS; \
		echo "포트포워딩 프로세스($$PIDS)를 종료했습니다."; \
	else \
		echo "종료할 포트포워딩 프로세스가 없습니다."; \
	fi



# ── 부하 스케일 테스트 제어 ──
scale-load:
	@echo "=== 부하 테스트 유도 (게임 서버 Fleet 80개로 확장) ==="
	kubectl scale fleet hexwar-server -n game --replicas=80

scale-reset:
	@echo "=== 부하 테스트 종료 (게임 서버 Fleet 1개로 원복) ==="
	kubectl scale fleet hexwar-server -n game --replicas=1

# ── 엣지 케이스 복구: 전체 인프라 완전 초기화 및 재기동 ──
k3d-recreate-all: k3d-delete clean
	@echo "=== [초기화] 전체 가상 환경을 파괴하고 처음부터 완전히 재구축합니다. ==="
	$(MAKE) localstack-up
	# 1. LocalStack 기동 대기
	@until curl -s http://localhost:4566/_localstack/health > /dev/null; do \
		echo "Waiting for LocalStack to be Ready..."; \
		sleep 3; \
	done
	# 2. 가상 AWS ASG 인프라 재생성
	$(MAKE) setup-all
	# 3. K3d 클러스터 재생성
	$(MAKE) k3d-create
	# 4. 소스코드 이미지 빌드(또는 Pull) 및 K3s 클러스터 내부 적재
	$(MAKE) k3d-pull-server
	$(MAKE) k3d-import-exporter
	# 5. 오토스케일러 Helm 차트 배포
	$(MAKE) helm-install
	# 5.5 Agones 컨트롤러 배포
	$(MAKE) agones-install
	# 6. K8s 모니터링 스택 배포
	$(MAKE) k8s-monitoring-up
	# 6.5 hexwar-exporter 배포
	$(MAKE) k8s-exporter-up
	# 7. Redis 클러스터 기동 및 동기화 완료 대기
	$(MAKE) k8s-redis-up
	# 8. Agones SDK ServiceAccount 권한 부여 및 게임 서버 배포
	@echo "=== Agones SDK ServiceAccount 생성 및 권한 부여 ==="
	kubectl create serviceaccount agones-sdk -n game || true
	kubectl create rolebinding agones-sdk --clusterrole=agones-sdk --serviceaccount=game:agones-sdk -n game || true
	@echo "kafka 클러스터 생성"
	$(MAKE) kafka-install
	@echo "minio 클러스터 생성"
	$(MAKE) minio-install
	@echo "quickwit 클러스터 생성"
	$(MAKE) quickwit-install
	@echo "vector 수집 에이전트 구성"
	$(MAKE) vector-daemonset
	@echo "hexwar game 서버 배포"
	$(MAKE) k8s-server-up
	@echo "=== [성공] 모든 시스템이 완전 재기동 및 복구되었습니다. ==="

# ── 기존 클러스터 환경 유지 및 소스/설정만 빠르고 가볍게 재배포 ──
k3d-update-all:
	@echo "=== [업데이트] 클러스터를 파괴하지 않고 코드와 설정만 최신으로 덮어씁니다. ==="
	# 1. 소스코드 변경사항 빌드(또는 Pull) 및 k3d 업로드
	$(MAKE) k3d-pull-server
	$(MAKE) k3d-import-exporter
	# 2. 모니터링 및 인프라 리소스 업데이트 (Apply)
	$(MAKE) k8s-monitoring-up
	$(MAKE) k8s-exporter-up
	$(MAKE) k8s-redis-up
	# 3. Agones 권한 부여 및 게임 서버 Fleet 업데이트
	kubectl create serviceaccount agones-sdk -n game || true
	kubectl create rolebinding agones-sdk --clusterrole=agones-sdk --serviceaccount=game:agones-sdk -n game || true
	$(MAKE) k8s-server-up
	# 4. 새 이미지가 반영되도록 기존 파드(GameServer 및 Exporter) 강제 재시작
	@echo "=== 변경된 이미지 반영을 위해 파드를 재시작합니다 ==="
	kubectl rollout restart deployment hexwar-exporter -n monitoring || true
	kubectl rollout restart deployment grafana -n monitoring || true
	kubectl delete gs --all -n game || true
	@echo "=== [성공] 코드 및 설정 업데이트가 완료되었습니다. ==="



