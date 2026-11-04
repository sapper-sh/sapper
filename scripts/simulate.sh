#!/bin/sh
# Run the disaster simulation campaign (internal/sim) against real restic and print
# a scoreboard. Exits 1 if any scenario was missed.
set -eu
cd "$(dirname "$0")/.."
command -v restic >/dev/null || { echo "simulate: restic not installed" >&2; exit 5; }
out=$(mktemp)
trap 'rm -f "$out"' EXIT
echo "restic: $(restic version | head -n1)"
echo "running scenarios (a few minutes)..."
status=0
SAPPER_SIM_ALLOW_MISS=1 SAPPER_SIM_OUT="$out" go test -count=1 -timeout 20m ./internal/sim/ >/dev/null || status=$?
printf '\n%-4s %-8s %s\n' ID RESULT SCENARIO
printf '%-4s %-8s %s\n' ---- -------- --------
awk -F'\t' '{ printf "%-4s %-8s %s\n", $1, $2, $3 }' "$out"
echo
awk -F'\t' '{ n[$2]++; t++ } END { printf "caught %d, partial %d, missed %d, known gaps %d, of %d\n", n["CAUGHT"], n["PARTIAL"], n["MISSED"], n["GAP"], t }' "$out"
if [ "$status" -ne 0 ]; then
  echo "simulate: go test failed (exit $status); rerun with: go test -v ./internal/sim/" >&2
  exit 1
fi
if grep -q "	MISSED	" "$out"; then exit 1; fi
