#!/usr/bin/env bash

# ============================================================
# NOOK DOCKER SERVER MONITOR
# ============================================================

REFRESH=2

cleanup() {
    tput cnorm 2>/dev/null
    clear
    echo "Nook monitor stopped."
    exit 0
}

trap cleanup INT TERM

# ------------------------------------------------------------
# Helpers
# ------------------------------------------------------------

bar() {
    local value="$1"
    local width=20
    local filled

    filled=$(awk -v v="$value" -v w="$width" 'BEGIN {
        x = int((v / 100) * w)
        if (x < 0) x = 0
        if (x > w) x = w
        print x
    }')

    printf "%${filled}s" "" | tr ' ' '█'

    local empty=$((width - filled))
    printf "%${empty}s" "" | tr ' ' '░'
}

status_symbol() {
    local status="$1"

    if [[ "$status" == "running" ]]; then
        echo "●"
    else
        echo "○"
    fi
}

# ------------------------------------------------------------
# Hide cursor
# ------------------------------------------------------------

tput civis 2>/dev/null

# ------------------------------------------------------------
# Main loop
# ------------------------------------------------------------

while true; do

    clear

    # ========================================================
    # HEADER
    # ========================================================

    HOSTNAME=$(hostname)
    NOW=$(date '+%Y-%m-%d %H:%M:%S')
    UPTIME=$(uptime -p 2>/dev/null)

    echo "╔════════════════════════════════════════════════════════════════════╗"
    echo "║                         NOOK SERVER MONITOR                       ║"
    echo "╠════════════════════════════════════════════════════════════════════╣"
    printf "║  Host: %-25s  Time: %-19s ║\n" "$HOSTNAME" "$NOW"
    printf "║  Uptime: %-55s ║\n" "$UPTIME"
    echo "╚════════════════════════════════════════════════════════════════════╝"
    echo

    # ========================================================
    # DOCKER STATUS
    # ========================================================

    if docker info >/dev/null 2>&1; then
        DOCKER_STATUS="RUNNING"
    else
        DOCKER_STATUS="NOT AVAILABLE"
    fi

    echo "┌─ DOCKER ──────────────────────────────────────────────────────────┐"

    if [[ "$DOCKER_STATUS" == "RUNNING" ]]; then
        echo "│  ● Docker daemon: RUNNING                                       │"
    else
        echo "│  ○ Docker daemon: NOT AVAILABLE                                │"
    fi

    CONTAINERS_TOTAL=$(docker ps -a -q 2>/dev/null | wc -l)
    CONTAINERS_RUNNING=$(docker ps -q 2>/dev/null | wc -l)
    CONTAINERS_STOPPED=$(docker ps -a -q -f status=exited 2>/dev/null | wc -l)

    printf "│  Containers: %-4s running   %-4s stopped   %-4s total          │\n" \
        "$CONTAINERS_RUNNING" "$CONTAINERS_STOPPED" "$CONTAINERS_TOTAL"

    echo "└───────────────────────────────────────────────────────────────────┘"
    echo

    # ========================================================
    # CONTAINERS
    # ========================================================

    echo "┌─ CONTAINERS ─────────────────────────────────────────────────────┐"

    if [[ "$CONTAINERS_TOTAL" -eq 0 ]]; then
        echo "│  No Docker containers found.                                    │"
    else

        printf "│  %-2s %-25s %-12s %-9s %-20s │\n" \
            "" "NAME" "STATUS" "CPU" "MEMORY"

        echo "│  ─────────────────────────────────────────────────────────────── │"

        docker ps -a \
            --format '{{.Names}}|{{.State}}|{{.Status}}' |
        while IFS='|' read -r NAME STATE STATUS; do

            CPU="--"
            MEM="--"

            if [[ "$STATE" == "running" ]]; then
                STATS=$(docker stats "$NAME" --no-stream \
                    --format '{{.CPUPerc}}|{{.MemUsage}}' 2>/dev/null)

                if [[ -n "$STATS" ]]; then
                    CPU=$(echo "$STATS" | cut -d'|' -f1)
                    MEM=$(echo "$STATS" | cut -d'|' -f2)
                fi
            fi

            SYMBOL=$(status_symbol "$STATE")

            printf "│  %s  %-25.25s %-12.12s %-9s %-20.20s │\n" \
                "$SYMBOL" "$NAME" "$STATE" "$CPU" "$MEM"

        done
    fi

    echo "└───────────────────────────────────────────────────────────────────┘"
    echo

    # ========================================================
    # PORTS
    # ========================================================

    echo "┌─ PORTS ───────────────────────────────────────────────────────────┐"

    PORT_OUTPUT=$(docker ps \
        --format '{{.Names}}|{{.Ports}}' 2>/dev/null)

    if [[ -z "$PORT_OUTPUT" ]]; then
        echo "│  No published Docker ports.                                     │"
    else

        while IFS='|' read -r NAME PORTS; do

            if [[ -n "$PORTS" ]]; then
                printf "│  %-25.25s → %-35.35s │\n" \
                    "$NAME" "$PORTS"
            fi

        done <<< "$PORT_OUTPUT"

    fi

    echo "└───────────────────────────────────────────────────────────────────┘"
    echo

    # ========================================================
    # HOST RESOURCES
    # ========================================================

    echo "┌─ HOST RESOURCES ─────────────────────────────────────────────────┐"

    CPU_USAGE=$(top -bn1 2>/dev/null |
        awk '/Cpu\(s\)/ {
            print 100 - $8
            exit
        }')

    if [[ -z "$CPU_USAGE" ]]; then
        CPU_USAGE="0"
    fi

    MEM_INFO=$(free -m 2>/dev/null)

    if [[ -n "$MEM_INFO" ]]; then

        MEM_TOTAL=$(echo "$MEM_INFO" |
            awk '/Mem:/ {print $2}')

        MEM_USED=$(echo "$MEM_INFO" |
            awk '/Mem:/ {print $3}')

        if [[ "$MEM_TOTAL" -gt 0 ]]; then
            MEM_PERCENT=$(awk \
                -v used="$MEM_USED" \
                -v total="$MEM_TOTAL" \
                'BEGIN { printf "%.1f", (used / total) * 100 }')
        else
            MEM_PERCENT="0"
        fi

    else
        MEM_TOTAL="0"
        MEM_USED="0"
        MEM_PERCENT="0"
    fi

    printf "│  CPU   "
    bar "$CPU_USAGE"
    printf "  %5.1f%%                                      │\n" "$CPU_USAGE"

    printf "│  RAM   "
    bar "$MEM_PERCENT"
    printf "  %5.1f%%  (%s MB / %s MB)                 │\n" \
        "$MEM_PERCENT" "$MEM_USED" "$MEM_TOTAL"

    echo "└───────────────────────────────────────────────────────────────────┘"
    echo

    # ========================================================
    # DOCKER RESOURCE USAGE
    # ========================================================

    echo "┌─ CONTAINER RESOURCE USAGE ───────────────────────────────────────┐"

    if [[ "$CONTAINERS_RUNNING" -gt 0 ]]; then

        docker stats --no-stream \
            --format '│  {{.Name}}: CPU {{.CPUPerc}}  MEM {{.MemUsage}}  NET {{.NetIO}}' \
            2>/dev/null |
        head -10

    else
        echo "│  No running containers.                                          │"
    fi

    echo "└───────────────────────────────────────────────────────────────────┘"
    echo

    # ========================================================
    # RECENT LOG ACTIVITY
    # ========================================================

    echo "┌─ RECENT LOG ACTIVITY ────────────────────────────────────────────┐"

    RUNNING_CONTAINERS=$(docker ps --format '{{.Names}}' 2>/dev/null)

    if [[ -z "$RUNNING_CONTAINERS" ]]; then

        echo "│  No running containers.                                          │"

    else

        while read -r NAME; do

            LAST_LOG=$(docker logs "$NAME" \
                --tail 1 \
                --timestamps \
                2>&1 |
                sed 's/|/ /g' |
                cut -c1-68)

            if [[ -n "$LAST_LOG" ]]; then
                printf "│  %-18.18s  %s │\n" "$NAME" "$LAST_LOG"
            fi

        done <<< "$RUNNING_CONTAINERS"

    fi

    echo "└───────────────────────────────────────────────────────────────────┘"
    echo

    # ========================================================
    # FOOTER
    # ========================================================

    echo "════════════════════════════════════════════════════════════════════"
    printf "  Refresh: %ss    |    Ctrl+C = exit    |    %s\n" \
        "$REFRESH" "$NOW"
    echo "════════════════════════════════════════════════════════════════════"

    sleep "$REFRESH"

done
