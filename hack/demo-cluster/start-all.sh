#!/usr/bin/env bash
# ==============================================================================
# CascadeShield — Start All Demo Microservices
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

BOLD="\033[1m"
GREEN="\033[32m"
CYAN="\033[36m"
RESET="\033[0m"

echo -e "${BOLD}${CYAN}Building demo microservices...${RESET}"
mkdir -p "${ROOT_DIR}/bin/demo"
go build -o "${ROOT_DIR}/bin/demo/gateway" "${SCRIPT_DIR}/gateway"
go build -o "${ROOT_DIR}/bin/demo/auth" "${SCRIPT_DIR}/auth"
go build -o "${ROOT_DIR}/bin/demo/orders" "${SCRIPT_DIR}/orders"
go build -o "${ROOT_DIR}/bin/demo/inventory" "${SCRIPT_DIR}/inventory"
go build -o "${ROOT_DIR}/bin/demo/payments" "${SCRIPT_DIR}/payments"
go build -o "${ROOT_DIR}/bin/demo/analytics" "${SCRIPT_DIR}/analytics"

echo -e "${BOLD}${CYAN}Starting all 6 demo services in background...${RESET}"

# Stop any running instances first
"${SCRIPT_DIR}/stop-all.sh" > /dev/null 2>&1 || true

nohup "${ROOT_DIR}/bin/demo/auth" > /tmp/cascadeshield-auth.log 2>&1 &
echo $! > /tmp/cascadeshield-auth.pid

nohup "${ROOT_DIR}/bin/demo/inventory" > /tmp/cascadeshield-inventory.log 2>&1 &
echo $! > /tmp/cascadeshield-inventory.pid

nohup "${ROOT_DIR}/bin/demo/payments" > /tmp/cascadeshield-payments.log 2>&1 &
echo $! > /tmp/cascadeshield-payments.pid

nohup "${ROOT_DIR}/bin/demo/analytics" > /tmp/cascadeshield-analytics.log 2>&1 &
echo $! > /tmp/cascadeshield-analytics.pid

nohup "${ROOT_DIR}/bin/demo/orders" > /tmp/cascadeshield-orders.log 2>&1 &
echo $! > /tmp/cascadeshield-orders.pid

nohup "${ROOT_DIR}/bin/demo/gateway" > /tmp/cascadeshield-gateway.log 2>&1 &
echo $! > /tmp/cascadeshield-gateway.pid

sleep 1

echo -e "${BOLD}${GREEN}✓ All 6 microservices started successfully!${RESET}"
echo -e "Logs available in /tmp/cascadeshield-*.log"
