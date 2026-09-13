#!/usr/bin/env bash
# ==============================================================================
# CascadeShield — Health Check All Microservices
# ==============================================================================

set -euo pipefail

BOLD="\033[1m"
GREEN="\033[32m"
RED="\033[31m"
CYAN="\033[36m"
RESET="\033[0m"

echo -e "${BOLD}${CYAN}Checking health across all CascadeShield microservices...${RESET}\n"

services=(
    "Gateway:http://localhost:8080/health"
    "Auth:http://localhost:8081/health"
    "Orders:http://localhost:8082/health"
    "Inventory:http://localhost:8083/health"
    "Payments:http://localhost:8084/health"
    "Analytics:http://localhost:8085/health"
)

all_healthy=true

for entry in "${services[@]}"; do
    name="${entry%%:*}"
    url="${entry#*:}"
    
    if res=$(curl -s --max-time 2 "$url"); then
        echo -e "  [${GREEN}ONLINE${RESET}] ${BOLD}${name}${RESET} ($url) -> $res"
    else
        echo -e "  [${RED}OFFLINE${RESET}] ${BOLD}${name}${RESET} ($url)"
        all_healthy=false
    fi
done

echo ""
if [ "$all_healthy" = true ]; then
    echo -e "${BOLD}${GREEN}✓ All 6 microservices are HEALTHY and ready!${RESET}"
else
    echo -e "${BOLD}${RED}✗ Some services are offline. Run './hack/demo-cluster/start-all.sh' to launch them.${RESET}"
fi
