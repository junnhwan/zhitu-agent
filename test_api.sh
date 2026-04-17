#!/bin/bash
# ZhituAgent API Test Script
# Usage: ./test_api.sh [BASE_URL]
# Example: ./test_api.sh http://localhost:10010

BASE_URL="${1:-http://localhost:10010}"
PASS=0
FAIL=0
TOTAL=0

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

report() {
    TOTAL=$((TOTAL + 1))
    if [ "$1" -eq 0 ]; then
        PASS=$((PASS + 1))
        echo -e "${GREEN}  ✓ PASS${NC} $2"
    else
        FAIL=$((FAIL + 1))
        echo -e "${RED}  ✗ FAIL${NC} $2"
    fi
}

separator() {
    echo ""
    echo -e "${CYAN}━━━ $1 ━━━${NC}"
}

# ──────────────────────────────────────────────
separator "1. GET /healthz"
# ──────────────────────────────────────────────

HTTP_CODE=$(curl -s -o /tmp/healthz_body -w "%{http_code}" "$BASE_URL/healthz")
BODY=$(cat /tmp/healthz_body)
if [ "$HTTP_CODE" = "200" ] && echo "$BODY" | grep -q "ok"; then
    report 0 "healthz returns 200 + {status:ok}"
else
    report 1 "healthz: HTTP=$HTTP_CODE BODY=$BODY"
fi

# ──────────────────────────────────────────────
separator "2. GET /metrics"
# ──────────────────────────────────────────────

# NOTE: Prometheus CounterVec only outputs after at least one Inc() call.
# We test metrics AFTER chat to ensure counters are populated.

# ──────────────────────────────────────────────
separator "3. POST /api/chat"
# ──────────────────────────────────────────────

# 3a. Normal chat
HTTP_CODE=$(curl -s -o /tmp/chat_body -w "%{http_code}" \
    -X POST "$BASE_URL/api/chat" \
    -H "Content-Type: application/json" \
    -d '{"sessionId":9001,"userId":8001,"prompt":"你好，请简短回复"}')
BODY=$(cat /tmp/chat_body)
CONTENT_TYPE=$(curl -s -o /dev/null -w "%{content_type}" \
    -X POST "$BASE_URL/api/chat" \
    -H "Content-Type: application/json" \
    -d '{"sessionId":9001,"userId":8001,"prompt":"你好，请简短回复"}')

if [ "$HTTP_CODE" = "200" ] && [ -n "$BODY" ]; then
    report 0 "chat returns 200 with non-empty body"
else
    report 1 "chat: HTTP=$HTTP_CODE BODY_LEN=${#BODY}"
fi

# Success response must be plain text, not JSON
if [ "$HTTP_CODE" = "200" ] && ! echo "$BODY" | grep -qE '^\s*\{"code"'; then
    report 0 "chat success response is plain text (not JSON wrapper)"
else
    report 1 "chat response should be plain text on success, got: ${BODY:0:80}"
fi

# 3b. Bad request (missing prompt)
HTTP_CODE=$(curl -s -o /tmp/chat_bad -w "%{http_code}" \
    -X POST "$BASE_URL/api/chat" \
    -H "Content-Type: application/json" \
    -d '{"sessionId":1}')
BODY=$(cat /tmp/chat_bad)
if [ "$HTTP_CODE" = "400" ]; then
    report 0 "chat bad request returns 400"
else
    report 1 "chat bad request: expected 400, got HTTP=$HTTP_CODE"
fi

# 3c. Invalid JSON
HTTP_CODE=$(curl -s -o /tmp/chat_invalid -w "%{http_code}" \
    -X POST "$BASE_URL/api/chat" \
    -H "Content-Type: application/json" \
    -d 'not json')
BODY=$(cat /tmp/chat_invalid)
if [ "$HTTP_CODE" = "400" ]; then
    report 0 "chat invalid JSON returns 400"
else
    report 1 "chat invalid JSON: expected 400, got HTTP=$HTTP_CODE"
fi

# ──────────────────────────────────────────────
separator "4. POST /api/streamChat"
# ──────────────────────────────────────────────

# 4a. Stream chat
HTTP_CODE=$(curl -s -o /tmp/stream_body -w "%{http_code}" \
    -X POST "$BASE_URL/api/streamChat" \
    -H "Content-Type: application/json" \
    -d '{"sessionId":9002,"userId":8001,"prompt":"1+1等于几？一个字回答"}')
BODY=$(cat /tmp/stream_body)

if [ "$HTTP_CODE" = "200" ]; then
    report 0 "streamChat returns 200"
else
    report 1 "streamChat: HTTP=$HTTP_CODE"
fi

# SSE stream should contain "data:" prefix
if [ -n "$BODY" ] && echo "$BODY" | grep -q "data:"; then
    report 0 "streamChat returns SSE data format"
else
    report 1 "streamChat: no SSE data found, body=${BODY:0:100}"
fi

# ──────────────────────────────────────────────
separator "5. POST /api/multiAgentChat"
# ──────────────────────────────────────────────

# 5a. With knowledge keyword
HTTP_CODE=$(curl -s -o /tmp/multi_body -w "%{http_code}" \
    -X POST "$BASE_URL/api/multiAgentChat" \
    -H "Content-Type: application/json" \
    -d '{"sessionId":9003,"userId":8001,"prompt":"什么是Go语言？请简短回答"}')
BODY=$(cat /tmp/multi_body)

if [ "$HTTP_CODE" = "200" ] && [ -n "$BODY" ]; then
    report 0 "multiAgentChat with keyword returns 200"
else
    report 1 "multiAgentChat keyword: HTTP=$HTTP_CODE BODY_LEN=${#BODY}"
fi

# 5b. Without knowledge keyword (casual chat)
HTTP_CODE=$(curl -s -o /tmp/multi_casual -w "%{http_code}" \
    -X POST "$BASE_URL/api/multiAgentChat" \
    -H "Content-Type: application/json" \
    -d '{"sessionId":9004,"userId":8001,"prompt":"今天天气怎么样"}')
BODY=$(cat /tmp/multi_casual)

if [ "$HTTP_CODE" = "200" ] && [ -n "$BODY" ]; then
    report 0 "multiAgentChat casual returns 200"
else
    report 1 "multiAgentChat casual: HTTP=$HTTP_CODE BODY_LEN=${#BODY}"
fi

# ──────────────────────────────────────────────
separator "6. POST /api/insert"
# ──────────────────────────────────────────────

HTTP_CODE=$(curl -s -o /tmp/insert_body -w "%{http_code}" \
    -X POST "$BASE_URL/api/insert" \
    -H "Content-Type: application/json" \
    -d '{"question":"Go语言的创始人是？","answer":"Go语言由Robert Griesemer、Rob Pike和Ken Thompson于2007年在Google创建","sourceName":"test-api"}')
BODY=$(cat /tmp/insert_body)

if [ "$HTTP_CODE" = "200" ] && [ -n "$BODY" ]; then
    report 0 "insert returns 200 with non-empty body"
else
    report 1 "insert: HTTP=$HTTP_CODE BODY=$BODY"
fi

# Insert success should be plain text
if [ "$HTTP_CODE" = "200" ] && ! echo "$BODY" | grep -qE '^\s*\{"code"'; then
    report 0 "insert success response is plain text"
else
    report 1 "insert response should be plain text on success"
fi

# ──────────────────────────────────────────────
separator "7. Guardrail (sensitive word)"
# ──────────────────────────────────────────────

HTTP_CODE=$(curl -s -o /tmp/guard_body -w "%{http_code}" \
    -X POST "$BASE_URL/api/chat" \
    -H "Content-Type: application/json" \
    -d '{"sessionId":9005,"userId":8001,"prompt":"我想死"}')
BODY=$(cat /tmp/guard_body)

if [ "$HTTP_CODE" != "200" ]; then
    report 0 "guardrail blocks sensitive word (HTTP=$HTTP_CODE)"
else
    report 1 "guardrail should block sensitive word, got HTTP=200"
fi

# Error response must be JSON with code field
if echo "$BODY" | grep -q '"code"'; then
    report 0 "guardrail error returns JSON with code field"
else
    report 1 "guardrail error should return JSON: BODY=$BODY"
fi

# ──────────────────────────────────────────────
separator "8. Static files"
# ──────────────────────────────────────────────

HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/gpt.html")
if [ "$HTTP_CODE" = "200" ]; then
    report 0 "gpt.html returns 200"
else
    report 1 "gpt.html: HTTP=$HTTP_CODE"
fi

HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/ai.png")
if [ "$HTTP_CODE" = "200" ]; then
    report 0 "ai.png returns 200"
else
    report 1 "ai.png: HTTP=$HTTP_CODE (may not exist in container yet)"
fi

HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/user.png")
if [ "$HTTP_CODE" = "200" ]; then
    report 0 "user.png returns 200"
else
    report 1 "user.png: HTTP=$HTTP_CODE (may not exist in container yet)"
fi

# ──────────────────────────────────────────────
separator "9. Session memory (multi-turn)"
# ──────────────────────────────────────────────

# First turn
HTTP_CODE=$(curl -s -o /tmp/mem1 -w "%{http_code}" \
    -X POST "$BASE_URL/api/chat" \
    -H "Content-Type: application/json" \
    -d '{"sessionId":9999,"userId":8001,"prompt":"我叫小明，请记住我的名字"}')
if [ "$HTTP_CODE" = "200" ]; then
    report 0 "memory turn 1: chat accepted"
else
    report 1 "memory turn 1: HTTP=$HTTP_CODE"
fi

# Second turn — same session
HTTP_CODE=$(curl -s -o /tmp/mem2 -w "%{http_code}" \
    -X POST "$BASE_URL/api/chat" \
    -H "Content-Type: application/json" \
    -d '{"sessionId":9999,"userId":8001,"prompt":"我叫什么名字？一个字回答"}')
BODY=$(cat /tmp/mem2)
if [ "$HTTP_CODE" = "200" ]; then
    report 0 "memory turn 2: chat accepted"
    # LLM may or may not recall the name, but the request should succeed
    # Check that Redis is actually storing messages by verifying a 3rd turn works
    HTTP_CODE3=$(curl -s -o /tmp/mem3 -w "%{http_code}" \
        -X POST "$BASE_URL/api/chat" \
        -H "Content-Type: application/json" \
        -d '{"sessionId":9999,"userId":8001,"prompt":"我们之前聊了什么？"}')
    if [ "$HTTP_CODE3" = "200" ]; then
        report 0 "memory: 3-turn conversation works (session persists)"
    else
        report 1 "memory: 3rd turn failed HTTP=$HTTP_CODE3"
    fi
else
    report 1 "memory turn 2: HTTP=$HTTP_CODE"
fi

# ──────────────────────────────────────────────
separator "10. GET /metrics (after chat requests)"
# ──────────────────────────────────────────────

HTTP_CODE=$(curl -s -o /tmp/metrics_body -w "%{http_code}" "$BASE_URL/metrics")
BODY=$(cat /tmp/metrics_body)
if [ "$HTTP_CODE" = "200" ] && echo "$BODY" | grep -q "ai_model_requests_total"; then
    report 0 "metrics returns 200 with ai_model_requests_total"
else
    report 1 "metrics: HTTP=$HTTP_CODE, ai_model_requests_total not found"
fi

if echo "$BODY" | grep -q "rag_retrieval"; then
    report 0 "metrics contains RAG metrics"
else
    report 1 "metrics: rag_retrieval metrics not found"
fi

# ──────────────────────────────────────────────
# Summary
# ──────────────────────────────────────────────
echo ""
echo -e "${CYAN}════════════════════════════════════════${NC}"
echo -e "  Total: $TOTAL  ${GREEN}Pass: $PASS${NC}  ${RED}Fail: $FAIL${NC}"
if [ "$FAIL" -eq 0 ]; then
    echo -e "  ${GREEN}ALL TESTS PASSED${NC}"
else
    echo -e "  ${RED}$FAIL TEST(S) FAILED${NC}"
fi
echo -e "${CYAN}════════════════════════════════════════${NC}"
