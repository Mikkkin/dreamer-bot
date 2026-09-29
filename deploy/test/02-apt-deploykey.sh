# apt lock waiting and the deploy-key flow against real GitHub.
# Variables below are read by the sourced setup-vds.sh functions; mocks are
# called indirectly by them.
# shellcheck disable=SC2034,SC2329,SC1091
# shellcheck source=lib.sh
source /s/test/lib.sh
echo 0 > /tmp/calls
apt-get() { calls=$(($(cat /tmp/calls) + 1)); echo "$calls" > /tmp/calls; if ((calls < 3)); then echo "E: Could not get lock /var/lib/apt/lists/lock. It is held by process 3377 (apt-get)"; return 100; fi; echo "done"; return 0; }
sleep() { :; }
out=$(apt_get update 2>&1); rc=$?
t [ $rc -eq 0 ]; t [ "$(cat /tmp/calls)" -eq 3 ]; t grep -q "жду" <<<"$out"
apt-get() { echo "E: Unable to locate package nope"; return 100; }
apt_get install nope >/dev/null 2>&1; rc=$?
t [ $rc -eq 100 ]
unset -f apt-get sleep
useradd -m deploy
ADMIN_USER=deploy APP_DIR=/opt/dreamer-bot ASSUME_YES=1
# A repository that anonymous HTTPS cannot read forces the deploy-key path.
REPO_URL=https://github.com/Mikkkin/no-such-repo-dreamer-test.git
REPO_SSH=git@github.com:Mikkkin/no-such-repo-dreamer-test.git
home=/home/deploy
mkdir -p $home/.ssh && printf 'Host other\n  HostName example.com\n\nHost github-dreamer\n  HostName github.com\n  User git\n' > $home/.ssh/config && chown -R deploy:deploy $home/.ssh
out=$( (clone_repo) 2>&1 ); rc=$?
t [ $rc -ne 0 ]
t grep -q "GitHub не пускает: .*Permission denied (publickey)" <<<"$out"
t grep -q "Deploy keys" <<<"$out"
t [ "$(grep -c '^Host github-dreamer$' $home/.ssh/config)" -eq 1 ]
t grep -q "^Host other$" $home/.ssh/config
t grep -q "HostName ssh.github.com" $home/.ssh/config
t grep -q "^\[ssh.github.com\]:443 ssh-ed25519 " $home/.ssh/known_hosts_github
t [ "$(stat -c '%a %U' $home/.ssh/config)" = "600 deploy" ]
report
