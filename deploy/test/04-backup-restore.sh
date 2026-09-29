# Data backup and restore with a faked docker volume.
# Variables below are read by the sourced setup-vds.sh functions; mocks are
# called indirectly by them.
# shellcheck disable=SC2034,SC2329,SC1091
# shellcheck source=lib.sh
source /s/test/lib.sh
VOL=$(mktemp -d); BACKUP_DIR=$(mktemp -d)/backups; BACKUP_KEEP=3; ADMIN_USER=root; ASSUME_YES=1
data_dir() { echo "$VOL"; }
compose() { case "$1 $2" in "ps -q") [[ -f /tmp/running ]] && echo abc ;; "stop bot") rm -f /tmp/running ;; "start bot"|"up -d") touch /tmp/running ;; esac; return 0; }
confirm() { return 0; }
HEALTHY=1
wait_bot_healthy() { [[ $HEALTHY == 1 ]]; }
# restore runs in a subshell: die exits it, and $? is the command's result.
restore() { (cmd_restore "$1") >/tmp/restore.log 2>&1; }
db() { cat "$VOL/dreamer.db"; }

# status before the first backup: nothing listed, no pipefail abort
# shellcheck disable=SC2016 # expanded by the inner shell
t bash -c 'set -euo pipefail; source /s/setup-vds.sh; BACKUP_DIR=/nonexistent; out=$(list_backups); [[ -z $out ]]'

touch /tmp/running
echo v1 > "$VOL/dreamer.db"; mkdir -p "$VOL/images/ab"; echo img > "$VOL/images/ab/x_full.jpg"; chown -R 65532:65532 "$VOL"
backup_data >/dev/null 2>&1; t [ -f /tmp/running ]
f1=$(list_backups | head -1)
t [ -n "$f1" ]; t [ "$f1" = "$LAST_BACKUP" ]; t [ "$(stat -c %a "$f1")" = 600 ]; t grep -q "./dreamer.db" <(tar -tzf "$f1")
t [ -z "$(find "$BACKUP_DIR" -name '.partial.*')" ]

# two backups in the same second get two files; rotation keeps BACKUP_KEEP
backup_data >/dev/null 2>&1; backup_data >/dev/null 2>&1
t [ "$(list_backups | wc -l)" -eq 3 ]
for _ in 1 2; do sleep 1; echo "v$((RANDOM))" > "$VOL/dreamer.db"; backup_data >/dev/null 2>&1; done
t [ "$(list_backups | wc -l)" -eq 3 ]

# restore the OLDEST archive with a full rotation: it must survive the
# pre-restore backup, and the data comes back with the right owner
oldest=$(list_backups | tail -1)
expected=$(tar -xzOf "$oldest" ./dreamer.db)
echo broken > "$VOL/dreamer.db"; rm -rf "$VOL/images"
t restore "$oldest"
t [ -f "$oldest" ]; t [ "$(db)" = "$expected" ]; t [ -f "$VOL/images/ab/x_full.jpg" ]
t [ "$(stat -c %u "$VOL/dreamer.db")" = 65532 ]; t [ -f /tmp/running ]
t [ -z "$(find "$(dirname "$VOL")" -maxdepth 1 \( -name '.restore.*' -o -name '.previous.*' \))" ]
t grep -qx 'broken' <(tar -xzOf "$(list_backups | head -1)" ./dreamer.db)

# the restored data does not start: the previous data goes back, restore fails
echo current > "$VOL/dreamer.db"; HEALTHY=0
n restore "$oldest"
t [ "$(db)" = current ]; t [ -f /tmp/running ]; t grep -q "вернул прежние данные" /tmp/restore.log
HEALTHY=1

# refused archives leave the data alone
tmpd=$(mktemp -d); echo x > "$tmpd/other"; tar -czf /tmp/foreign.tgz -C "$tmpd" .
n restore /tmp/foreign.tgz; t grep -q "не бэкап" /tmp/restore.log
tmpd=$(mktemp -d); echo x > "$tmpd/dreamer.db"; ln -s /etc/shadow "$tmpd/images"; tar -czf /tmp/link.tgz -C "$tmpd" .
n restore /tmp/link.tgz; t grep -q "ссылки" /tmp/restore.log
tmpd=$(mktemp -d); : > "$tmpd/dreamer.db"; tar -czf /tmp/empty.tgz -C "$tmpd" .
n restore /tmp/empty.tgz; t grep -q "текущие данные не тронуты" /tmp/restore.log
printf 'not a gzip' > /tmp/junk.tgz
n restore /tmp/junk.tgz; t grep -q "повреждён" /tmp/restore.log
t [ "$(db)" = current ]; t [ ! -L "$VOL/images" ]
report
