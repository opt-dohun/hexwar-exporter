#!/bin/bash
cd "$(dirname "$0")/.."

echo "=========================================================="
echo "    KEDA 커스텀 메트릭(Prometheus) 오토스케일링 동작 검증   "
echo "=========================================================="

echo "[1/4] KEDA 컨트롤러 설치 중 (약 30초 소요)..."
kubectl apply --server-side -f https://github.com/kedacore/keda/releases/download/v2.14.0/keda-2.14.0.yaml > /dev/null 2>&1
kubectl wait --for=condition=Ready pods -l app=keda-operator -n keda --timeout=90s > /dev/null 2>&1

echo "[2/4] 테스트용 게임 서버(1대) 및 KEDA ScaledObject 배포..."
kubectl apply -f scratch/game-server-kwok-deployment.yaml > /dev/null 2>&1
kubectl apply -f scratch/keda-scaledobject.yaml > /dev/null 2>&1
sleep 5

echo "[3/4] 🚀 500명의 유저가 몰린 상황을 묘사하는 Fake Metric Server 가동!"
# 프로메테우스 설정 갱신 및 가짜 메트릭 서버 배포
kubectl apply -f scratch/k8s-monitoring-stack.yaml > /dev/null 2>&1
kubectl apply -f scratch/fake-metric-server.yaml > /dev/null 2>&1

echo "KEDA가 프로메테우스에서 지표(500명)를 확인하고 스케일 아웃을 결정할 때까지 대기..."
echo "(최대 40초 소요)"
for i in {1..13}; do
  echo -n "."
  sleep 3
done
echo ""

echo "----------------------------------------------------------"
echo "[4/4] 📊 KEDA HPA 매칭 상태 및 파드 스케일 결과"
echo ">> HPA 상태 (타겟 메트릭이 100을 요구하고 현재 500을 인지해야 함):"
kubectl get hpa keda-hpa-hexwar-game-server-scaler -n monitoring
echo ""
echo ">> 게임 서버 파드 상태 (1대 -> 5대로 자동 확장 성공 기대):"
kubectl get pods -l app=hexwar-game -n monitoring
echo "=========================================================="
echo "테스트 완료! 클린업을 원하시면 아래 명령어를 실행하세요:"
echo "kubectl delete -f scratch/fake-metric-server.yaml && kubectl delete -f scratch/keda-scaledobject.yaml"
echo "=========================================================="
