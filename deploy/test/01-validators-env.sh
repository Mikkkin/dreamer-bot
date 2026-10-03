# Validators and .env generation.
# Variables below are read by the sourced setup-vds.sh functions; mocks are
# called indirectly by them.
# shellcheck disable=SC2034,SC2317,SC2329,SC1091
# shellcheck source=lib.sh
source /s/test/lib.sh
t valid_token "123456789:AAEXAMPLEexampleEXAMPLEexample_-12345"; n valid_token "123:abc"; n valid_token "x"
t valid_ids ""; t valid_ids "111,222"; n valid_ids "111, 222x"; n valid_ids "abc"
t valid_domain "dreams.mooo.com"; t valid_domain "Dreams.Mooo.com"; n valid_domain "localhost"; n valid_domain "a..b.com"; n valid_domain "http://x.com"
t valid_email "me@example.com"; n valid_email "me@"; t valid_user deploy; n valid_user root; n valid_user "Bad User"
t valid_sub ourdreams; n valid_sub "our.dreams"; t valid_duck 01234567-89ab-cdef-0123-456789abcdef; n valid_duck nope
t valid_tz Europe/Amsterdam; n valid_tz "../../etc/passwd"; n valid_tz Nowhere/City; t valid_cur EUR; n valid_cur XYZ
APP_DIR=$(mktemp -d); ADMIN_USER=root; ASSUME_YES=1
BOT_TOKEN=123456789:AAEXAMPLEexampleEXAMPLEexample_-12345 ALLOWED_USER_IDS="111,222" DOMAIN_MODE=domain DOMAIN=Dreams.Mooo.com ACME_EMAIL=me@example.com DEFAULT_CURRENCY=EUR APP_TZ=Europe/Amsterdam
curl() { return 1; }   # no network in the test: getMe "fails"
confirm() { return 0; }
configure_env >/dev/null 2>&1
e=$APP_DIR/.env
t grep -qx "WEBAPP_URL=https://dreams.mooo.com/" "$e"; t grep -qx "COMPOSE_PROFILES=caddy" "$e"; t grep -qx "DOMAIN=dreams.mooo.com" "$e"
t grep -qx "QUICK_TUNNEL_METRICS_URL=" "$e"; t grep -qx "ALLOWED_USER_IDS=111,222" "$e"; t grep -qx "TZ=Europe/Amsterdam" "$e"
t [ "$(stat -c %a "$e")" = 600 ]
t [ "$(env_get DOMAIN)" = dreams.mooo.com ]
FORCE_ENV=1 DOMAIN_MODE=none DOMAIN='' configure_env >/dev/null 2>&1
t grep -qx "COMPOSE_PROFILES=" "$e"; t grep -qx "WEBAPP_URL=" "$e"
# The optional model key: written when given, kept on a re-run together with
# the other LLM_* settings, and validated.
t grep -qx "LLM_API_KEY=" "$e"
printf 'LLM_MODEL=gemini-3.6-flash\n' >> "$e"
FORCE_ENV=1 DOMAIN_MODE=none DOMAIN='' LLM_API_KEY=AQ.ExampleExampleExample_1234 configure_env >/dev/null 2>&1
t grep -qx "LLM_API_KEY=AQ.ExampleExampleExample_1234" "$e"; t grep -qx "LLM_MODEL=gemini-3.6-flash" "$e"
FORCE_ENV=1 DOMAIN_MODE=none DOMAIN='' configure_env >/dev/null 2>&1
t grep -qx "LLM_API_KEY=AQ.ExampleExampleExample_1234" "$e"; t grep -qx "LLM_MODEL=gemini-3.6-flash" "$e"
t valid_llm_key ""; t valid_llm_key "AIzaSyExampleExampleExample1234"; n valid_llm_key "short"; n valid_llm_key "has space in the key here"
# GitHub host keys: from the API, else the published ones (CI runners share
# IPs and hit the unauthenticated rate limit).
kh=$(mktemp)
curl() { return 22; }
github_known_hosts "$kh" 2>/dev/null
t [ "$(wc -l < "$kh")" -eq 6 ]
t grep -qx '\[ssh.github.com\]:443 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl' "$kh"
curl() { printf '{"ssh_keys":["ssh-ed25519 AAAATESTKEY"]}'; }
github_known_hosts "$kh"
t [ "$(cat "$kh")" = "$(printf 'github.com ssh-ed25519 AAAATESTKEY\n[ssh.github.com]:443 ssh-ed25519 AAAATESTKEY')" ]
unset -f curl

report
