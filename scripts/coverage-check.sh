#!/usr/bin/env bash
# Enforce per-package and total coverage floors.
# Usage: coverage-check.sh [coverage.txt] [.coverage-floors]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COVERAGE_FILE="${1:-$ROOT/coverage.txt}"
FLOORS_FILE="${2:-$ROOT/.coverage-floors}"
MIN_TOTAL="${MIN_COVERAGE:-80}"

if [[ ! -f "$COVERAGE_FILE" ]]; then
  echo "coverage file not found: $COVERAGE_FILE" >&2
  exit 1
fi
if [[ ! -f "$FLOORS_FILE" ]]; then
  echo "floors file not found: $FLOORS_FILE" >&2
  exit 1
fi

awk -v total_min="$MIN_TOTAL" '
FNR == NR {
  if (NF >= 2 && $1 !~ /^#/ && $1 != "") {
    floors[$1] = $2
  }
  next
}
/^total:/ {
  total = $3
  sub(/%/, "", total)
  next
}
/^github\.com\/MChorfa\/ggvalet\// {
  path = $1
  sub(/:[^:]*$/, "", path)
  gsub(/^github\.com\/MChorfa\/ggvalet\//, "", path)
  if (path ~ /\//) {
    sub(/\/[^\/]*$/, "", path)
  } else {
    path = "."
  }
  val = $3
  sub(/%/, "", val)
  sum[path] += val
  cnt[path]++
}
END {
  fail = 0
  for (p in floors) {
    if (!(p in sum)) {
      printf "FAIL: %s not found in coverage report\n", p
      fail = 1
      continue
    }
    avg = sum[p] / cnt[p]
    if (avg + 0 < floors[p] + 0) {
      printf "FAIL: %s coverage %.1f%% < %.1f%% floor\n", p, avg, floors[p]
      fail = 1
    } else {
      printf "OK:   %s coverage %.1f%% >= %.1f%% floor\n", p, avg, floors[p]
    }
  }
  if (total + 0 < total_min + 0) {
    printf "FAIL: total coverage %.1f%% < %s%% floor\n", total, total_min
    fail = 1
  } else {
    printf "OK:   total coverage %.1f%% >= %s%% floor\n", total, total_min
  }
  exit fail
}
' "$FLOORS_FILE" "$COVERAGE_FILE"
