#!/bin/bash
cd "$(dirname "$0")/.."

echo "[1/5] 환경 초기화"
kubectl delete nodes -l type=kwok > /dev/null 2>&1
kubectl delete pods -l app=keda-operator -n keda --force --grace-period=0 > /dev/null 2>&1
kubectl apply --server-side -f https://github.com/kedacore/keda/releases/download/v2.14.0/keda-2.14.0.yaml > /dev/null 2>&1
kubectl wait --for=condition=Ready pods -l app=keda-operator -n keda --timeout=90s > /dev/null 2>&1

echo "[2/5] 무거운 자원(CPU 500m)을 요구하는 게임 서버 1대 배포"
kubectl apply -f scratch/game-server-kwok-deployment.yaml > /dev/null 2>&1
kubectl apply -f scratch/keda-scaledobject.yaml > /dev/null 2>&1
sleep 5

echo "[3/5] KEDA 파드 확장 시작"
# Fake Metric(2000명)으로 20대(2000/100) 확장을 유도
sed -i.bak 's/500/2000/g' scratch/fake-metric-server.yaml
kubectl apply -f scratch/k8s-monitoring-stack.yaml > /dev/null 2>&1
kubectl apply -f scratch/fake-metric-server.yaml > /dev/null 2>&1
mv scratch/fake-metric-server.yaml.bak scratch/fake-metric-server.yaml # 원복

echo "KEDA가 프로메테우스를 통해 2000명을 감지하고 20대의 파드를 띄울 때까지 대기..."
for i in {1..12}; do echo -n "."; sleep 3; done
echo ""

echo "[4/5] Pending 사태 발생"
kubectl get pods -l app=hexwar-game -n monitoring | grep Pending | head -n 5
echo "... (대다수 파드 Pending)"
echo "----------------------------------------------------------"
sleep 3

echo "[5/5] Karpenter 동적 노드 프로비저닝 시작 (KWOK Provider 연동)"

# 1. Karpenter 저장소 클론 및 kwok provider 설치
echo "Karpenter 소스코드 다운로드 및 설치 중..."
if [ ! -d "scratch/karpenter" ]; then
  git clone --depth 1 https://github.com/kubernetes-sigs/karpenter.git scratch/karpenter > /dev/null 2>&1
fi

echo "빌드 툴(ko) 확인 및 설치..."
export PATH=$PATH:$(go env GOPATH)/bin
if ! command -v ko &> /dev/null; then
    go install github.com/google/ko@latest > /dev/null 2>&1
fi

cd scratch/karpenter
make install-kwok > /dev/null 2>&1

echo "KWOK Provider 컨트롤러 빌드 및 k3d 이미지 업로드 중 (수 분 소요)..."
export KO_DOCKER_REPO=ko.local
export KWOK_REPO=ko.local
IMG=$(ko build -B sigs.k8s.io/karpenter/kwok 2>/dev/null)
k3d image import $IMG -c hexwar-cluster > /dev/null 2>&1

echo "Karpenter Helm 배포 중..."
make apply IMG_DIGEST="" > /dev/null 2>&1
cd ../../

# 2. Karpenter NodePool 및 KwokNodeClass 적용
echo "Karpenter NodePool(game) 및 KwokNodeClass 적용 중..."
kubectl apply -f scratch/karpenter-game-nodepool.yaml

echo "Karpenter가 Pending 파드를 감지하고 동적 노드를 생성할 때까지 대기..."

# Wait up to 60 seconds
EXPECTED=20
RUNNING=0
i=1
while [ $i -le 12 ]; do
  RUNNING=$(kubectl get pods -l app=hexwar-game -n monitoring --field-selector=status.phase=Running -o name | wc -l | tr -d ' ')
  if [ "$RUNNING" -ge "$EXPECTED" ]; then
    break
  fi
  wait_time=$((i*5))
  echo "현재 Running 파드 수: $RUNNING / $EXPECTED ... 대기 중 (${wait_time}초)"
  sleep 5
  i=$((i+1))
done

echo ">> Karpenter의 NodeClaim 현황 (프로비저닝 시도 확인):"
kubectl get nodeclaims -A

echo ">> Karpenter 컨트롤러 로그 요약 (최근 스케일링 판단 로직):"
kubectl logs -n kube-system -l app.kubernetes.io/name=karpenter --tail=15

echo ">> Karpenter 클라우드 노드 목록:"
kubectl get nodes -l type=kwok | head -n 5
echo "... (동적 생성 노드 상태 확인)"

if [ "$RUNNING" -ge "$EXPECTED" ]; then
  echo "✅ ${RUNNING}/${EXPECTED} Running - 테스트 성공"
else
  echo "❌ ${RUNNING}/${EXPECTED} Running - 테스트 실패"
  exit 1
fi
