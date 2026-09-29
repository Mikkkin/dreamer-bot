# Shared helpers for the setup-vds.sh test suites (sourced, never run).
# shellcheck shell=bash
set -u
# shellcheck source=../setup-vds.sh disable=SC1091
source /s/setup-vds.sh
trap - ERR
set +e
pass=0
fail=0
t() { if "$@"; then pass=$((pass + 1)); else fail=$((fail + 1)); echo "FAIL: $*"; fi; }
n() { if "$@"; then fail=$((fail + 1)); echo "FAIL (should reject): $*"; else pass=$((pass + 1)); fi; }
report() { echo "passed=$pass failed=$fail"; [[ $fail -eq 0 ]]; }
