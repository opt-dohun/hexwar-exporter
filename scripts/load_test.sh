#!/bin/bash

echo "=============================================="
echo "    HexWar Exporter - /metrics 부하 테스트    "
echo "=============================================="

# hey가 설치되어 있는지 확인
if ! command -v hey &> /dev/null; then
    echo "오류: 'hey' 명령어가 설치되어 있지 않습니다."
    echo "설치 방법:"
    echo "  - Mac: brew install hey"
    echo "  - Linux: go install github.com/rakyll/hey@latest"
    exit 1
fi

TARGET_URL="http://localhost:9091/metrics"
CONCURRENCY=200
TOTAL_REQUESTS=5000

echo "대상 URL: $TARGET_URL"
echo "동시 요청(Concurrency): $CONCURRENCY"
echo "총 요청 수(Total): $TOTAL_REQUESTS"
echo "테스트를 시작합니다..."
echo ""

# hey를 사용하여 부하 발생
hey -n $TOTAL_REQUESTS -c $CONCURRENCY -m GET $TARGET_URL

echo ""
echo "=============================================="
echo "테스트가 완료되었습니다."
echo "캐싱이 올바르게 동작한다면 응답 시간(p99)이 ms 단위로 매우 짧게 나와야 합니다."
echo "=============================================="
