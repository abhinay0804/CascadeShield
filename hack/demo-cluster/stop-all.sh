#!/usr/bin/env bash
# ==============================================================================
# CascadeShield — Stop All Demo Microservices
# ==============================================================================

set -euo pipefail

BOLD="\033[1m"
RED="\033[31m"
RESET="\033[0m"

echo -e "${BOLD}${RED}Stopping all CascadeShield demo microservices...${RESET}"

for pidfile in /tmp/cascadeshield-*.pid; do
    if [ -f "$pidfile" ]; then
        pid=$(cat "$pidfile")
        kill "$pid" 2>/dev/null || true
        rm -f "$pidfile"
    fi
done

pkill -f "bin/demo/" 2>/dev/null || true

echo -e "${BOLD}✓ All demo services stopped.${RESET}"
