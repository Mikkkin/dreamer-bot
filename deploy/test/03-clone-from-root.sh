# Cloning when the script is started from /root (not readable by the admin).
# Variables below are read by the sourced setup-vds.sh functions; mocks are
# called indirectly by them.
# shellcheck disable=SC2034,SC2317,SC2329,SC1091
# shellcheck source=lib.sh
source /s/test/lib.sh
useradd -m deploy; chmod 700 /root; mkdir -p /root/dreamer-bot/deploy; cd /root/dreamer-bot/deploy || exit 1
ADMIN_USER=deploy ASSUME_YES=1 APP_DIR=/opt/dreamer-bot
# A tiny public repository keeps this test independent of whether the bot's
# own repository is public or private.
REPO_URL=https://github.com/octocat/Hello-World.git
out=$( (clone_repo) 2>&1 ); rc=$?
t [ $rc -eq 0 ]; t [ -f /opt/dreamer-bot/README ]; t [ "$(stat -c %U /opt/dreamer-bot/README)" = deploy ]
# Files the capability-less Caddy container reads must be world-readable
# (644, or 664 where pam_umask gives user-private groups 002).
t [ $(( 8#$(stat -c %a /opt/dreamer-bot/README) & 4 )) -ne 0 ]
out=$( (clone_repo) 2>&1 ); rc=$?
t [ $rc -eq 0 ]; t grep -q "Обновлён" <<<"$out"
APP_DIR=/opt/private-test REPO_URL=https://github.com/Mikkkin/no-such-repo-dreamer-test.git REPO_SSH=git@github.com:Mikkkin/no-such-repo-dreamer-test.git
out=$( (clone_repo) 2>&1 )
t grep -q "Permission denied (publickey)" <<<"$out"; n grep -q "failed to stat" <<<"$out"
report
