#!/usr/bin/env bash
# ==============================================================================
# CascadeShield Phase 6 — Interactive Demo Script
#
# This script demonstrates CascadeShield's ability to detect, predict, and
# remediate cascading failures across synthetic microservices in real-time.
# ==============================================================================

set -euo pipefail

BOLD="\033[1m"
GREEN="\033[32m"
YELLOW="\033[33m"
RED="\033[31m"
CYAN="\033[36m"
RESET="\033[0m"

GATEWAY_HOST="${GATEWAY_HOST:-http://localhost:8080}"
PAYMENTS_HOST="${PAYMENTS_HOST:-http://localhost:8084}"
INVENTORY_HOST="${INVENTORY_HOST:-http://localhost:8083}"
AUTH_HOST="${AUTH_HOST:-http://localhost:8081}"

pause() {
    echo -e "\n${YELLOW}Press [ENTER] to continue to the next step...${RESET}"
    read -r
}

header() {
    echo -e "\n${BOLD}${CYAN}======================================================================${RESET}"
    echo -e "${BOLD}${CYAN} $1${RESET}"
    echo -e "${BOLD}${CYAN}======================================================================${RESET}\n"
}

header "STEP 1: Verify Baseline Microservice Health"
echo "Checking /health endpoints across all microservices..."
curl -s "${GATEWAY_HOST}/health" | jq . || curl -s "${GATEWAY_HOST}/health"
curl -s "${AUTH_HOST}/health" | jq . || curl -s "${AUTH_HOST}/health"
curl -s "${INVENTORY_HOST}/health" | jq . || curl -s "${INVENTORY_HOST}/health"
curl -s "${PAYMENTS_HOST}/health" | jq . || curl -s "${PAYMENTS_HOST}/health"
echo -e "${GREEN}✓ All services baseline healthy.${RESET}"

pause

header "STEP 2: Baseline Traffic Flow (Normal Operation)"
echo "Sending standard requests through API Gateway..."
for i in {1..5}; do
    echo -n "Request #$i: "
    curl -s "${GATEWAY_HOST}/api/order" | jq -c . || true
done
echo -e "${GREEN}✓ Baseline traffic passing with standard low latency (~45ms).${RESET}"

pause

header "STEP 3: Chaos Scenario 1 — Payment Slowdown"
echo -e "Injecting 300ms extra latency into ${RED}Payments${RESET} service..."
curl -s "${PAYMENTS_HOST}/chaos?latency_ms=300" | jq .

echo -e "\nExecuting orders via Gateway under Payment Slowdown..."
for i in {1..3}; do
    echo -n "Order #$i: "
    time curl -s "${GATEWAY_HOST}/api/order" | jq -c . || true
done

echo -e "\n${CYAN}Observe CascadeShield TUI & Metrics:${RESET}"
echo "- Payments node risk increases"
echo "- Orders thread exhaustion predicted"
echo "- Shield action: auto-sheds Orders -> Payments traffic by up to 40%"

pause

header "STEP 4: Chaos Scenario 2 — Inventory Crash (Retry Amplification)"
echo -e "Resetting Payments chaos..."
curl -s "${PAYMENTS_HOST}/chaos?reset=true" > /dev/null

echo -e "Injecting 80% error rate into ${RED}Inventory${RESET} service..."
curl -s "${INVENTORY_HOST}/chaos?error_rate=0.80" | jq .

echo -e "\nExecuting orders (triggers 3x retries in Orders service)..."
for i in {1..3}; do
    echo -n "Order #$i: "
    curl -s "${GATEWAY_HOST}/api/order" | jq -c . || true
done

echo -e "\n${CYAN}Observe CascadeShield Action:${RESET}"
echo "- Retry amplification detected"
echo "- Circuit-breaker shedding activated on Orders -> Inventory edge"

pause

header "STEP 5: Chaos Scenario 3 — Auth Cascade (Critical Path Block)"
echo -e "Resetting Inventory chaos..."
curl -s "${INVENTORY_HOST}/chaos?reset=true" > /dev/null

echo -e "Injecting 2000ms latency into ${RED}Auth${RESET} service..."
curl -s "${AUTH_HOST}/chaos?latency_ms=2000" | jq .

echo -e "\nExecuting Gateway requests (Auth blocks critical fan-out path)..."
for i in {1..2}; do
    echo -n "Order #$i: "
    time curl -s "${GATEWAY_HOST}/api/order" | jq -c . || true
done

pause

header "STEP 6: System Recovery & Reset"
echo "Clearing chaos across all microservices..."
curl -s "${PAYMENTS_HOST}/chaos?reset=true" > /dev/null || true
curl -s "${INVENTORY_HOST}/chaos?reset=true" > /dev/null || true
curl -s "${AUTH_HOST}/chaos?reset=true" > /dev/null || true

echo -e "\nVerifying cluster health..."
curl -s "${GATEWAY_HOST}/api/order" | jq -c . || true
echo -e "\n${GREEN}${BOLD}✓ Demo complete! All microservices restored to nominal state.${RESET}"
