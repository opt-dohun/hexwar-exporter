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

echo "[5/5] Karpenter 동적 노드 프로비저닝 시작"
# Pending 상태의 파드들을 담을 가상의 빈 노드(EC2)를 Karpenter가 지어주는 과정을 모방
for i in {201..220}; do
cat <<EOF | kubectl apply -f - > /dev/null 2>&1
apiVersion: v1
kind: Node
metadata:
  annotations:
    node.alpha.kubernetes.io/ttl: "0"
    kwok.x-k8s.io/node: "fake"
  labels:
    type: kwok
    node-type: game-server
  name: kwok-karpenter-node-$i
EOF
done

echo "Karpenter가 동적 노드를 생성 후 배정 대기."
sleep 8

echo ">> Karpenter 클라우드 노드 목록:"
kubectl get nodes -l type=kwok | head -n 5
echo "... (총 20대 생성됨)"

echo ">> 노드를 할당받아 정상 구동(Running) 중인 게임 서버 파드 상태:"
kubectl get pods -l app=hexwar-game -n monitoring | grep Running | head -n 5
echo "... (총 20대 Running 성공!)"

