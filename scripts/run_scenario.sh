#!/usr/bin/env bash
# Runs one load-test scenario once and records everything about it.
#   scripts/run_scenario.sh <baseline|medium|high|ceiling|failure|failure5k|spike> <run-number>
# Needs the compose stack's images built (docker compose build) and the
# load generator image (docker compose --profile load build loadgen).
set -euo pipefail

scenario=${1:?scenario}
run=${2:?run number}
cd "$(dirname "$0")/.."

# Connection count for the node-failure scenario; set it from the
# ceiling-search result.
FAILURE_CONNECTIONS=${FAILURE_CONNECTIONS:-2000}

case $scenario in
  baseline) args=(--connections 1000 --senders 5  --msg-rate 2  --duration 60s) ;;
  medium)   args=(--connections 3000 --senders 10 --msg-rate 5  --duration 120s) ;;
  high)     args=(--connections 5000 --senders 20 --msg-rate 10 --duration 120s) ;;
  ceiling)  args=(--start 1000 --connections 40000 --ramp-step 1000 --ramp-interval 30s
                  --senders 20 --msg-rate 10 --duration 40m --stop-drops-per-min 5 --stop-p99-ms 100) ;;
  # Kill gateway-2 at T=60s. "failure" runs at the ceiling count, where two
  # surviving nodes cannot carry the load; "failure5k" runs with headroom so the
  # recovery itself can be measured.
  failure)  args=(--connections "$FAILURE_CONNECTIONS" --senders 20 --msg-rate 10 --duration 120s) ;;
  failure5k) args=(--connections 5000 --senders 20 --msg-rate 10 --duration 120s) ;;
  spike)    args=(--start 500 --connections 5000 --ramp-step 450 --ramp-interval 500ms
                  --senders 20 --msg-rate 10 --duration 120s) ;;
  *) echo "unknown scenario: $scenario" >&2; exit 2 ;;
esac

mkdir -p results/prometheus results/grafana
tag="$scenario-run$run"

# Fresh gateways every run: clears connections, rate-limit state and counters.
# The limits are raised because every sender publishes faster than 2 msg/s.
RATE_LIMIT_CAPACITY=1000 RATE_LIMIT_REFILL_RATE=1000 \
  docker compose up -d --force-recreate gateway-1 gateway-2 gateway-3 >/dev/null 2>&1
for _ in $(seq 1 60); do
  healthy=$(docker compose ps --format '{{.Service}} {{.Status}}' | grep -c 'gateway-.*healthy' || true)
  [ "$healthy" -eq 3 ] && break
  sleep 1
done
sleep 15 # let Prometheus scrape the new processes

start=$(date +%s)
echo "== $tag started $(date -u +%FT%TZ)"

killer=""
if [[ $scenario == failure* ]]; then
  ( sleep 60; docker kill chatscale-gateway-2-1 >/dev/null; echo "== killed gateway-2 at $(date -u +%FT%TZ)" ) &
  killer=$!
fi

# The load generator runs inside the compose network (see docker-compose.yml).
docker compose run --rm --no-deps -T loadgen \
    --target ws://gateway-1:8080,ws://gateway-2:8080,ws://gateway-3:8080 \
    --prometheus http://prometheus:9090 \
    "${args[@]}" --scenario "$scenario" --run "$run" --out /results/runs.jsonl 2>&1 \
  | grep --line-buffered -v '^result appended' | tee "results/log-$tag.txt"

[ -n "$killer" ] && wait "$killer"
end=$(date +%s)

# Did any gateway die on its own?
for n in 1 2 3; do
  docker inspect -f "$tag gateway-$n oomKilled={{.State.OOMKilled}} exitCode={{.State.ExitCode}} restarts={{.RestartCount}} running={{.State.Running}}" "chatscale-gateway-$n-1"
done | tee -a results/container_status.log

# Raw Prometheus series for the test window.
prom() {
  curl -s --data-urlencode "query=$1" --data-urlencode "start=$((start - 10))" \
    --data-urlencode "end=$((end + 10))" --data-urlencode step=5 localhost:9090/api/v1/query_range
}
jq -n \
  --arg scenario "$scenario" --argjson run "$run" --argjson start "$start" --argjson end "$end" \
  --argjson connections "$(prom 'chatscale_active_connections')" \
  --argjson publishRate "$(prom 'sum(rate(chatscale_messages_total[1m]))')" \
  --argjson fanoutP99Ms "$(prom 'histogram_quantile(0.99, sum by (le, node) (rate(chatscale_fanout_latency_seconds_bucket[1m]))) * 1000')" \
  --argjson dropRate "$(prom 'sum(rate(chatscale_messages_dropped_total[1m]))')" \
  --argjson rssBytes "$(prom 'process_resident_memory_bytes{job="chatscale"}')" \
  '{scenario: $scenario, run: $run, start: $start, end: $end, connections: $connections,
    publishRate: $publishRate, fanoutP99Ms: $fanoutP99Ms, dropRate: $dropRate, rssBytes: $rssBytes}' \
  > "results/prometheus/$tag.json"

# Dashboard picture of the test window.
node scripts/grafana-screenshot.mjs "results/grafana/$tag.png" "$(((start - 30) * 1000))" "$(((end + 30) * 1000))" \
  || echo "screenshot failed (continuing)"
echo "== $tag done"
