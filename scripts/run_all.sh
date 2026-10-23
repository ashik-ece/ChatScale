#!/usr/bin/env bash
# Runs every load-test scenario three times, in the order the blueprint needs:
# the node-failure scenario runs at the connection count found by the ceiling
# search, so the ceiling runs come first.
#   scripts/run_all.sh [runs-per-scenario]
set -euo pipefail
cd "$(dirname "$0")/.."

runs=${1:-3}

# Skips runs already recorded, so an interrupted batch can be resumed.
recorded() {
  [ -f results/runs.jsonl ] && jq -e --arg s "$1" --argjson r "$2" \
    'select(.config.Scenario == $s and .config.Run == $r)' results/runs.jsonl >/dev/null 2>&1
}
run_once() {
  if recorded "$1" "$2"; then echo "== $1-run$2 already recorded, skipping"; else ./scripts/run_scenario.sh "$1" "$2"; fi
}

docker compose --profile load build loadgen >/dev/null
docker compose build >/dev/null

for scenario in baseline medium high ceiling; do
  for run in $(seq 1 "$runs"); do
    run_once "$scenario" "$run"
  done
done

# Median of the last stable connection count across the ceiling runs.
export FAILURE_CONNECTIONS
FAILURE_CONNECTIONS=$(jq -s '[.[] | select(.config.Scenario == "ceiling") | .stable.connections] | sort | .[length / 2 | floor]' results/runs.jsonl)
echo "== node-failure scenario will run at $FAILURE_CONNECTIONS connections (median ceiling)"

for scenario in failure spike; do
  for run in $(seq 1 "$runs"); do
    run_once "$scenario" "$run"
  done
done
echo "== all scenarios done"
