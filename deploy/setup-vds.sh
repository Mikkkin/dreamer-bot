#!/usr/bin/env bash
# dreamer-bot: one-shot setup of a fresh Ubuntu/Debian VDS.
#
# What it does (idempotent, safe to re-run):
#   - updates the system, adds swap on small servers, sysctl hardening
#   - creates an admin user with your SSH key, key-only SSH, no root login
#   - UFW firewall (SSH + 80/443 only), fail2ban, automatic security updates
#   - installs Docker from the official repository
#   - clones the repository, asks for the .env values and deploys
#   - HTTPS: Caddy obtains and renews a Let's Encrypt certificate by itself;
#     the script checks DNS first and waits until the certificate is live
#
# Usage (as root on the server):
#   bash setup-vds.sh            full setup (default)
#   bash setup-vds.sh update     git pull + rebuild + restart
#   bash setup-vds.sh ids        set ALLOWED_USER_IDS and restart
#   bash setup-vds.sh env        re-enter .env values and redeploy
#   bash setup-vds.sh status     containers, firewall, fail2ban, certificate
#
# Every question can be answered in advance with an environment variable of
# the same name (BOT_TOKEN, ALLOWED_USER_IDS, DOMAIN_MODE=domain|duckdns|none,
# DOMAIN, ACME_EMAIL, DUCKDNS_SUBDOMAIN, DUCKDNS_TOKEN, ADMIN_USER,
# ADMIN_PUBKEY, REPO_URL, UPGRADE_SYSTEM=1|0, ...); ASSUME_YES=1 accepts the defaults.

# The valid_* functions are called indirectly through ask_valid.
# shellcheck disable=SC2329
set -Eeuo pipefail
umask 027

# ---------------------------------------------------------------- settings --
REPO_URL="${REPO_URL:-https://github.com/Mikkkin/dreamer-bot.git}"
REPO_SSH="${REPO_SSH:-git@github.com:Mikkkin/dreamer-bot.git}"
REPO_SETTINGS_KEYS="${REPO_SETTINGS_KEYS:-https://github.com/Mikkkin/dreamer-bot/settings/keys}"
APP_DIR="${APP_DIR:-/opt/dreamer-bot}"
ADMIN_USER="${ADMIN_USER:-}"
ASSUME_YES="${ASSUME_YES:-0}"
SWAP_SIZE_MB="${SWAP_SIZE_MB:-2048}"
LOG_FILE=/var/log/dreamer-setup.log
SELF_INSTALL=/usr/local/sbin/dreamer-vds

# ------------------------------------------------------------------ output --
if [[ -t 1 ]]; then
  C_B=$'\e[1m'; C_G=$'\e[32m'; C_Y=$'\e[33m'; C_R=$'\e[31m'; C_C=$'\e[36m'; C_0=$'\e[0m'
else
  C_B=; C_G=; C_Y=; C_R=; C_C=; C_0=
fi
step() { printf '\n%s==> %s%s\n' "$C_B$C_C" "$*" "$C_0"; }
info() { printf '    %s\n' "$*"; }
ok()   { printf '%s  ✓ %s%s\n' "$C_G" "$*" "$C_0"; }
warn() { printf '%s  ! %s%s\n' "$C_Y" "$*" "$C_0" >&2; }
die()  { printf '%s  ✗ %s%s\n' "$C_R" "$*" "$C_0" >&2; exit 1; }
trap 'printf "%s  ✗ Ошибка в строке %s: %s%s\n" "$C_R" "$LINENO" "$BASH_COMMAND" "$C_0" >&2' ERR

# ----------------------------------------------------------------- prompts --
# Questions read from the terminal even when stdout is piped to a log.
TTY=/dev/tty
interactive() { [[ "$ASSUME_YES" != 1 ]] && [[ -r $TTY ]] && { : <"$TTY"; } 2>/dev/null; }

# ask VAR "question" [default] — keeps a value already set in the environment.
ask() {
  local var=$1 question=$2 default=${3-} answer
  if [[ -n ${!var-} ]]; then return 0; fi
  if ! interactive; then printf -v "$var" '%s' "$default"; return 0; fi
  if [[ -n $default ]]; then
    read -r -p "  $question [$default]: " answer <"$TTY" || true
  else
    read -r -p "  $question: " answer <"$TTY" || true
  fi
  printf -v "$var" '%s' "${answer:-$default}"
}

# ask_secret VAR "question" — input is not echoed.
ask_secret() {
  local var=$1 question=$2 answer
  if [[ -n ${!var-} ]]; then return 0; fi
  interactive || die "$var не задан (неинтерактивный режим)."
  read -r -s -p "  $question: " answer <"$TTY" || true
  printf '\n'
  printf -v "$var" '%s' "$answer"
}

# confirm "question" [y|n] — default answer used in non-interactive mode.
confirm() {
  local question=$1 default=${2:-n} answer hint='[y/N]'
  [[ $default == y ]] && hint='[Y/n]'
  if ! interactive; then [[ $default == y ]]; return; fi
  read -r -p "  $question $hint: " answer <"$TTY" || true
  answer=${answer:-$default}
  [[ $answer =~ ^[YyДд] ]]
}

pause() { interactive && read -r -p "  $1" _ <"$TTY" || true; }

# --------------------------------------------------------------- validators --
# Called indirectly through ask_valid (see the file-level shellcheck directive).
valid_token()  { [[ $1 =~ ^[0-9]{5,20}:[A-Za-z0-9_-]{30,64}$ ]]; }
valid_ids()    { [[ -z $1 || $1 =~ ^[0-9]{1,20}(,[0-9]{1,20})*$ ]]; }
valid_domain() { [[ ${#1} -le 253 && ${1,,} =~ ^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$ ]]; }
valid_email()  { [[ $1 =~ ^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$ ]]; }
valid_user()   { [[ $1 =~ ^[a-z_][a-z0-9_-]{0,31}$ && $1 != root ]]; }
valid_sub()    { [[ $1 =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ ]]; }
valid_duck()   { [[ $1 =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]; }
valid_tz()     { [[ -f /usr/share/zoneinfo/$1 && $1 != *..* ]]; }
valid_cur()    { [[ $1 =~ ^(EUR|USD|RUB|GBP)$ ]]; }

# ask_valid VAR "question" default validator "error" [secret]
ask_valid() {
  local var=$1 question=$2 default=$3 check=$4 error=$5 secret=${6-}
  while :; do
    if [[ -n $secret ]]; then ask_secret "$var" "$question"; else ask "$var" "$question" "$default"; fi
    if "$check" "${!var}"; then return 0; fi
    interactive || die "$error"
    warn "$error"
    printf -v "$var" '%s' ''
  done
}

# ------------------------------------------------------------------ helpers --
# apt_get: apt-get that waits while another package manager holds the apt
# locks (a fresh VDS runs apt-daily / unattended-upgrades right after boot).
# It retries only on lock errors, so real failures still stop the script.
apt_get() {
  local out rc tries=0
  while :; do
    out=$(apt-get -o DPkg::Lock::Timeout=600 "$@" 2>&1) && rc=0 || rc=$?
    if (( rc == 0 )); then
      [[ -z $out ]] || printf '%s\n' "$out"
      return 0
    fi
    if grep -qE 'Could not get lock|Unable to lock|is held by process' <<<"$out" && (( tries < 90 )); then
      (( tries == 0 )) && info "apt занят автообновлением системы — жду (до 15 минут)…"
      tries=$((tries + 1)); sleep 10; continue
    fi
    printf '%s\n' "$out" >&2
    return "$rc"
  done
}

as_admin() { sudo -H -u "$ADMIN_USER" -- "$@"; }
compose()  { (cd "$APP_DIR" && as_admin docker compose "$@"); }
public_ip() { curl -4 -fsS --max-time 10 https://api.ipify.org 2>/dev/null || curl -4 -fsS --max-time 10 https://ifconfig.me 2>/dev/null || true; }

env_get() { # env_get KEY — value from the app's .env (empty if absent)
  [[ -f $APP_DIR/.env ]] || return 0
  sed -n "s/^$1=//p" "$APP_DIR/.env" | tail -n1
}

detect_admin_user() {
  if [[ -z $ADMIN_USER && -f /etc/dreamer-vds.conf ]]; then
    # shellcheck disable=SC1091
    ADMIN_USER=$(sed -n 's/^ADMIN_USER=//p' /etc/dreamer-vds.conf)
  fi
}

# ------------------------------------------------------------------- steps --
preflight() {
  step "Проверка сервера"
  [[ $EUID -eq 0 ]] || die "Запустите от root: sudo bash $0"
  [[ -f /etc/os-release ]] || die "Не найден /etc/os-release"
  # shellcheck disable=SC1091
  . /etc/os-release
  case "${ID:-}" in
    ubuntu|debian) ;;
    *) die "Поддерживаются Ubuntu и Debian (найдено: ${PRETTY_NAME:-unknown})" ;;
  esac
  OS_ID=$ID; OS_CODENAME=${VERSION_CODENAME:-}
  [[ -n $OS_CODENAME ]] || die "Не удалось определить кодовое имя дистрибутива"
  command -v systemctl >/dev/null || die "Нужен systemd"
  ARCH=$(dpkg --print-architecture)
  [[ $ARCH == amd64 || $ARCH == arm64 ]] || die "Поддерживаются amd64 и arm64 (найдено: $ARCH)"
  command -v curl >/dev/null || { apt_get update -qq && apt_get install -y -qq curl ca-certificates >/dev/null; }
  curl -fsS --max-time 15 -o /dev/null https://api.github.com || die "Нет доступа в интернет (api.github.com)"
  ok "${PRETTY_NAME}, $ARCH"
}

install_packages() {
  step "Обновление системы и пакеты"
  export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a
  apt_get update -qq
  # UPGRADE_SYSTEM=1/0 answers the question in advance.
  local upgrade=${UPGRADE_SYSTEM:-}
  if [[ -z $upgrade ]]; then
    if confirm "Обновить установленные пакеты системы (apt upgrade, несколько минут)?" y; then upgrade=1; else upgrade=0; fi
  fi
  if [[ $upgrade == 1 ]]; then
    apt_get -y -qq -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold upgrade >/dev/null
    ok "Система обновлена"
  else
    info "Обновление пакетов пропущено (обновления безопасности всё равно будут ставиться автоматически)"
  fi
  apt_get install -y -qq ca-certificates curl git gnupg sudo ufw fail2ban python3-systemd \
    unattended-upgrades dnsutils openssl tzdata iproute2 psmisc >/dev/null
  ok "Нужные пакеты установлены"
}

setup_swap() {
  step "Swap"
  local mem_mb
  mem_mb=$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo)
  if swapon --noheadings --show | grep -q .; then ok "Swap уже есть"; return; fi
  if (( mem_mb >= 3500 )); then ok "RAM ${mem_mb} MB — swap не нужен"; return; fi
  # The image is built on the server (Go + bun); small VDS need swap for it.
  fallocate -l "${SWAP_SIZE_MB}M" /swapfile 2>/dev/null || dd if=/dev/zero of=/swapfile bs=1M count="$SWAP_SIZE_MB" status=none
  chmod 600 /swapfile
  mkswap /swapfile >/dev/null
  swapon /swapfile
  grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
  printf 'vm.swappiness=10\n' > /etc/sysctl.d/90-dreamer-swap.conf
  ok "Swap ${SWAP_SIZE_MB} MB (RAM ${mem_mb} MB)"
}

harden_sysctl() {
  step "Сетевые настройки ядра"
  cat > /etc/sysctl.d/90-dreamer-hardening.conf <<'EOF'
# dreamer-bot baseline hardening (ip_forward stays on: Docker needs it)
net.ipv4.tcp_syncookies = 1
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
net.ipv6.conf.all.accept_redirects = 0
net.ipv6.conf.default.accept_redirects = 0
net.ipv4.conf.all.send_redirects = 0
net.ipv4.conf.default.send_redirects = 0
net.ipv4.conf.all.accept_source_route = 0
net.ipv6.conf.all.accept_source_route = 0
net.ipv4.icmp_echo_ignore_broadcasts = 1
net.ipv4.icmp_ignore_bogus_error_responses = 1
net.ipv4.conf.all.log_martians = 1
kernel.kptr_restrict = 2
kernel.dmesg_restrict = 1
fs.protected_hardlinks = 1
fs.protected_symlinks = 1
EOF
  sysctl --system >/dev/null 2>&1 || warn "Часть параметров sysctl не применилась (бывает на VPS с общим ядром)"
  ok "sysctl"
}

setup_admin_user() {
  step "Пользователь-администратор"
  local default_user=deploy
  [[ -n ${SUDO_USER:-} && ${SUDO_USER} != root ]] && default_user=$SUDO_USER
  ask_valid ADMIN_USER "Имя пользователя для входа по SSH" "$default_user" valid_user \
    "Имя: латиница в нижнем регистре, цифры, _ и - (не root)"
  if ! id "$ADMIN_USER" >/dev/null 2>&1; then
    adduser --disabled-password --gecos "" "$ADMIN_USER" >/dev/null
    ok "Создан пользователь $ADMIN_USER"
  else
    ok "Пользователь $ADMIN_USER уже есть"
  fi
  usermod -aG sudo "$ADMIN_USER"
  printf 'ADMIN_USER=%s\n' "$ADMIN_USER" > /etc/dreamer-vds.conf

  # sudo: with a password if the user has none yet (interactive), else key-only admin.
  if ! passwd -S "$ADMIN_USER" 2>/dev/null | awk '{exit !($2=="P")}'; then
    if interactive; then
      info "Задайте пароль для sudo (SSH по паролю будет выключен, пароль нужен только для sudo):"
      until passwd "$ADMIN_USER" <"$TTY"; do warn "Попробуйте ещё раз"; done
      rm -f /etc/sudoers.d/90-dreamer-admin
    else
      printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$ADMIN_USER" > /etc/sudoers.d/90-dreamer-admin
      chmod 440 /etc/sudoers.d/90-dreamer-admin
      visudo -cf /etc/sudoers.d/90-dreamer-admin >/dev/null || { rm -f /etc/sudoers.d/90-dreamer-admin; die "sudoers не прошёл проверку"; }
      warn "Пароль не задан: $ADMIN_USER получает sudo без пароля (вход только по ключу)"
    fi
  fi

  # SSH keys: root's existing keys + an optional pasted key.
  local home ssh_dir auth tmp key
  home=$(getent passwd "$ADMIN_USER" | cut -d: -f6)
  ssh_dir=$home/.ssh; auth=$ssh_dir/authorized_keys
  install -d -m 700 -o "$ADMIN_USER" -g "$ADMIN_USER" "$ssh_dir"
  touch "$auth"
  if [[ -s /root/.ssh/authorized_keys ]]; then
    while IFS= read -r key; do
      [[ -n $key && $key != \#* ]] || continue
      grep -qxF "$key" "$auth" || printf '%s\n' "$key" >> "$auth"
    done < /root/.ssh/authorized_keys
  fi
  if ! grep -q . "$auth" || [[ -n ${ADMIN_PUBKEY:-} ]]; then
    [[ -n ${ADMIN_PUBKEY:-} ]] || info "Вставьте ваш публичный SSH-ключ (строка из ~/.ssh/id_ed25519.pub на вашем компьютере)."
    ask ADMIN_PUBKEY "Публичный ключ (Enter — пропустить)" ""
    if [[ -n $ADMIN_PUBKEY ]]; then
      tmp=$(mktemp); printf '%s\n' "$ADMIN_PUBKEY" > "$tmp"
      if ssh-keygen -lf "$tmp" >/dev/null 2>&1; then
        grep -qxF "$ADMIN_PUBKEY" "$auth" || printf '%s\n' "$ADMIN_PUBKEY" >> "$auth"
        ok "Ключ добавлен"
      else
        warn "Это не похоже на публичный SSH-ключ — пропускаю"
      fi
      rm -f "$tmp"
    fi
  fi
  chown "$ADMIN_USER:$ADMIN_USER" "$auth"; chmod 600 "$auth"
  ADMIN_HAS_KEY=0
  ssh-keygen -lf "$auth" >/dev/null 2>&1 && ADMIN_HAS_KEY=1
  if (( ADMIN_HAS_KEY )); then ok "У $ADMIN_USER есть SSH-ключ"; else warn "У $ADMIN_USER нет SSH-ключа"; fi
}

harden_ssh() {
  step "SSH"
  local conf=/etc/ssh/sshd_config.d/00-dreamer-hardening.conf
  if (( ! ADMIN_HAS_KEY )); then
    warn "Пропускаю: без SSH-ключа у $ADMIN_USER отключение паролей заблокирует вход."
    warn "Добавьте ключ и запустите скрипт снова."
    return
  fi
  grep -qE '^\s*Include\s+/etc/ssh/sshd_config.d/' /etc/ssh/sshd_config \
    || sed -i '1i Include /etc/ssh/sshd_config.d/*.conf' /etc/ssh/sshd_config
  local backup=
  [[ -f $conf ]] && backup=$(cat "$conf")
  cat > "$conf" <<'EOF'
# dreamer-bot SSH hardening. First value wins in sshd, so this file (00-)
# overrides cloud-init defaults such as 50-cloud-init.conf.
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitEmptyPasswords no
PubkeyAuthentication yes
AuthenticationMethods publickey
MaxAuthTries 3
LoginGraceTime 30
X11Forwarding no
AllowAgentForwarding no
AllowTcpForwarding local
ClientAliveInterval 300
ClientAliveCountMax 2
EOF
  chmod 644 "$conf"
  if ! sshd -t 2>/dev/null; then
    if [[ -n $backup ]]; then printf '%s\n' "$backup" > "$conf"; else rm -f "$conf"; fi
    warn "Конфигурация sshd не прошла проверку — изменения SSH откатаны"
    return
  fi
  systemctl reload-or-restart ssh 2>/dev/null || systemctl reload-or-restart sshd
  ok "Вход только по ключу, root по SSH запрещён"
  local ip; ip=$(public_ip)
  warn "НЕ закрывайте текущую сессию. В НОВОМ окне проверьте вход:  ssh $ADMIN_USER@${ip:-<IP-сервера>}"
  if interactive && ! confirm "Вход под $ADMIN_USER по ключу работает?" n; then
    rm -f "$conf"
    systemctl reload-or-restart ssh 2>/dev/null || systemctl reload-or-restart sshd
    die "Откатил настройки SSH. Проверьте ключ пользователя $ADMIN_USER и запустите скрипт снова."
  fi
}

setup_firewall() {
  step "Firewall (UFW)"
  local ports port
  ports=$(sshd -T 2>/dev/null | awk '$1=="port"{print $2}' | sort -u)
  [[ -n $ports ]] || ports=22
  ufw default deny incoming >/dev/null
  ufw default allow outgoing >/dev/null
  for port in $ports; do ufw limit "$port/tcp" comment 'SSH' >/dev/null; done
  ufw allow 80/tcp comment 'HTTP (Lets Encrypt)' >/dev/null
  ufw allow 443/tcp comment 'HTTPS' >/dev/null
  ufw allow 443/udp comment 'HTTP/3' >/dev/null
  ufw --force enable >/dev/null
  # Docker publishes ports past UFW; compose binds the bot to 127.0.0.1 only
  # and publishes nothing else besides Caddy's 80/443, so that is fine here.
  ok "Открыты только SSH ($(tr '\n' ' ' <<<"$ports" | sed 's/ $//')), 80 и 443"
}

setup_fail2ban() {
  step "fail2ban"
  local ports
  ports=$(sshd -T 2>/dev/null | awk '$1=="port"{print $2}' | sort -u | paste -sd, -)
  cat > /etc/fail2ban/jail.d/dreamer-sshd.local <<EOF
[sshd]
enabled  = true
port     = ${ports:-ssh}
backend  = systemd
maxretry = 5
findtime = 10m
bantime  = 1h
bantime.increment = true
EOF
  systemctl enable fail2ban >/dev/null 2>&1 || true
  if systemctl restart fail2ban; then
    ok "Бан IP после 5 неудачных входов по SSH"
  else
    warn "fail2ban не запустился — посмотрите: journalctl -u fail2ban"
  fi
}

setup_auto_updates() {
  step "Автоматические обновления безопасности"
  cat > /etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
EOF
  cat > /etc/apt/apt.conf.d/52dreamer-unattended <<'EOF'
Unattended-Upgrade::Remove-Unused-Dependencies "true";
Unattended-Upgrade::Automatic-Reboot "false";
EOF
  systemctl enable --now unattended-upgrades >/dev/null 2>&1 || true
  ok "Включены"
}

install_docker() {
  step "Docker"
  if command -v docker >/dev/null && docker compose version >/dev/null 2>&1; then
    ok "Docker уже установлен ($(docker --version | cut -d, -f1))"
  else
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL "https://download.docker.com/linux/$OS_ID/gpg" -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc
    printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/%s %s stable\n' \
      "$ARCH" "$OS_ID" "$OS_CODENAME" > /etc/apt/sources.list.d/docker.list
    apt_get update -qq
    apt_get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin >/dev/null
    ok "Установлен $(docker --version | cut -d, -f1)"
  fi
  if [[ ! -f /etc/docker/daemon.json ]]; then
    install -d -m 755 /etc/docker
    cat > /etc/docker/daemon.json <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": { "max-size": "10m", "max-file": "3" },
  "live-restore": true,
  "no-new-privileges": true
}
EOF
    systemctl restart docker
  fi
  systemctl enable --now docker >/dev/null 2>&1
  # Membership in "docker" is root-equivalent; the admin already has sudo.
  usermod -aG docker "$ADMIN_USER"
  ok "Docker запущен, $ADMIN_USER в группе docker"
}

github_known_hosts() { # pin GitHub's host keys from its HTTPS API (no TOFU)
  local file=$1 keys key
  keys=$(curl -fsS --max-time 15 https://api.github.com/meta | grep -oE '"(ssh-ed25519|ecdsa-sha2-nistp256|ssh-rsa) [A-Za-z0-9+/=]+"' | tr -d '"')
  [[ -n $keys ]] || die "Не удалось получить ключи GitHub"
  # ssh.github.com:443 serves the same host keys and works where port 22 is blocked.
  while IFS= read -r key; do
    printf 'github.com %s\n[ssh.github.com]:443 %s\n' "$key" "$key"
  done <<<"$keys" > "$file"

}

clone_repo() {
  step "Код бота"
  if [[ -d $APP_DIR/.git ]]; then
    as_admin git -C "$APP_DIR" pull --ff-only --quiet && ok "Обновлён ($APP_DIR)"
    return
  fi
  install -d -m 750 -o "$ADMIN_USER" -g "$ADMIN_USER" "$APP_DIR"
  # sudo resets the environment, so pass the no-prompt flag explicitly:
  # without it git would wait for a GitHub login on a private repository.
  if as_admin env GIT_TERMINAL_PROMPT=0 git ls-remote "$REPO_URL" >/dev/null 2>&1; then
    as_admin git clone --quiet "$REPO_URL" "$APP_DIR"
    ok "Склонирован $REPO_URL"
    return
  fi

  # Private repository: a read-only deploy key for this server.
  info "Репозиторий приватный — нужен deploy key (доступ только на чтение)."
  local home key cfg
  home=$(getent passwd "$ADMIN_USER" | cut -d: -f6)
  key=$home/.ssh/dreamer_deploy_key; cfg=$home/.ssh/config
  if [[ ! -f $key ]]; then
    as_admin ssh-keygen -q -t ed25519 -N '' -C "dreamer-bot deploy $(hostname)" -f "$key"
  fi
  github_known_hosts "$home/.ssh/known_hosts_github"
  chown "$ADMIN_USER:$ADMIN_USER" "$home/.ssh/known_hosts_github"
  # (Re)write our Host block: an earlier run may have left one for port 22.
  local tmp; tmp=$(mktemp)
  if [[ -f $cfg ]]; then
    awk '/^Host github-dreamer$/ {skip=1; next} skip && /^(Host|Match) / {skip=0} !skip' "$cfg" > "$tmp"
  fi
  cat >> "$tmp" <<EOF
Host github-dreamer
  HostName ssh.github.com
  Port 443
  User git
  IdentityFile $key
  IdentitiesOnly yes
  UserKnownHostsFile $home/.ssh/known_hosts_github
  StrictHostKeyChecking yes
  BatchMode yes
  ConnectTimeout 15
EOF
  install -m 600 -o "$ADMIN_USER" -g "$ADMIN_USER" "$tmp" "$cfg"
  rm -f "$tmp"
  local url=${REPO_SSH/git@github.com:/github-dreamer:}
  printf '\n  %sДобавьте этот ключ в GitHub:%s %s\n' "$C_B" "$C_0" "$REPO_SETTINGS_KEYS"
  printf '  (Add deploy key → вставьте строку ниже → галочку «write access» НЕ ставьте)\n\n  %s\n\n' "$(cat "$key.pub")"
  local err
  while :; do
    pause "Нажмите Enter, когда ключ добавлен… "
    if err=$(as_admin env GIT_TERMINAL_PROMPT=0 git ls-remote "$url" 2>&1 >/dev/null); then break; fi
    warn "GitHub не пускает: $(grep -v '^[[:space:]]*$' <<<"$err" | head -n1)"
    case $err in
      *"Permission denied"*) info "Ключ не принят: он должен быть в Deploy keys именно репозитория dreamer-bot (Settings → Deploy keys)" ;;
      *"timed out"*|*"Connection refused"*|*"Could not resolve"*) info "Нет связи с ssh.github.com:443 — проверьте исходящие соединения у хостера" ;;
      *"Host key verification failed"*) info "Не совпал ключ хоста GitHub — запустите скрипт ещё раз" ;;
    esac
    interactive || die "Нет доступа к репозиторию: добавьте deploy key и запустите снова."
  done
  as_admin git clone --quiet "$url" "$APP_DIR"

  ok "Склонирован (deploy key)"
}

configure_env() {
  step "Настройки бота (.env)"
  local env=$APP_DIR/.env
  if [[ -f $env && ${FORCE_ENV:-0} != 1 ]] && ! confirm "Найден .env — ввести настройки заново?" n; then
    ok "Оставляю текущий .env"
    return
  fi

  # The current .env (if any) provides the defaults; secrets are kept on request.
  local old_token old_ids old_cur old_email old_domain old_sub old_duck
  old_token=$(env_get BOT_TOKEN); old_ids=$(env_get ALLOWED_USER_IDS)
  old_cur=$(env_get DEFAULT_CURRENCY); old_email=$(env_get ACME_EMAIL)
  old_domain=$(env_get DOMAIN); old_sub=$(env_get DUCKDNS_SUBDOMAIN); old_duck=$(env_get DUCKDNS_TOKEN)

  if [[ -z ${BOT_TOKEN:-} && -n $old_token ]] && confirm "Оставить текущий токен бота?" y; then
    BOT_TOKEN=$old_token
  fi
  info "Токен бота: @BotFather → /newbot (или /token для существующего)."
  ask_valid BOT_TOKEN "BOT_TOKEN (ввод скрыт)" "" valid_token "Неверный формат токена" secret
  local me
  me=$(printf 'url = "https://api.telegram.org/bot%s/getMe"\n' "$BOT_TOKEN" | curl -fsS --max-time 15 --config - 2>/dev/null || true)
  if [[ $me == *'"ok":true'* ]]; then
    BOT_USERNAME=$(grep -oE '"username":"[^"]+"' <<<"$me" | cut -d'"' -f4)
    ok "Токен рабочий: @$BOT_USERNAME"
  else
    warn "Telegram не подтвердил токен (нет сети или токен неверный)"
    confirm "Продолжить с этим токеном?" n || die "Остановлено"
  fi

  info "Telegram ID вас и партнёра через запятую. Не знаете — оставьте пустым:"
  info "бот ответит на /start вашим ID, потом выполните: sudo $SELF_INSTALL ids"
  ask_valid ALLOWED_USER_IDS "ALLOWED_USER_IDS" "$old_ids" valid_ids "Только числа через запятую, например 111111111,222222222"
  ALLOWED_USER_IDS=${ALLOWED_USER_IDS// /}

  if [[ -z ${DOMAIN_MODE:-} ]]; then
    info "Как открывать Mini App (нужен HTTPS):"
    info "  1) свой или бесплатный домен (FreeDNS и т.п.) с A-записью на этот сервер"
    info "  2) бесплатный DuckDNS (скрипт сам будет обновлять IP)"
    info "  3) без домена — только чат-бот, Mini App выключен"
    local choice; ask choice "Выберите 1, 2 или 3" "1"
    case $choice in 2) DOMAIN_MODE=duckdns ;; 3) DOMAIN_MODE=none ;; *) DOMAIN_MODE=domain ;; esac
  fi

  local profiles='' webapp='' quick_metrics=''
  case $DOMAIN_MODE in
    domain)
      ask_valid DOMAIN "Домен (например dreams.mooo.com)" "$old_domain" valid_domain "Неверный домен"
      DOMAIN=${DOMAIN,,}
      profiles=caddy ;;
    duckdns)
      ask_valid DUCKDNS_SUBDOMAIN "Поддомен DuckDNS (без .duckdns.org)" "$old_sub" valid_sub "Только латиница, цифры и дефис"
      if [[ -z ${DUCKDNS_TOKEN:-} && -n $old_duck ]] && confirm "Оставить текущий токен DuckDNS?" y; then
        DUCKDNS_TOKEN=$old_duck
      fi
      ask_valid DUCKDNS_TOKEN "Токен DuckDNS (ввод скрыт)" "" valid_duck "Токен DuckDNS выглядит как xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" secret
      DOMAIN=$DUCKDNS_SUBDOMAIN.duckdns.org
      profiles=caddy,duckdns ;;
    none) ;;
    *) die "DOMAIN_MODE должен быть domain, duckdns или none" ;;
  esac
  if [[ $DOMAIN_MODE != none ]]; then
    ask_valid ACME_EMAIL "E-mail для Let's Encrypt (уведомления о сертификате)" "$old_email" valid_email "Неверный e-mail"
    webapp="https://$DOMAIN/"
  fi

  ask_valid DEFAULT_CURRENCY "Валюта по умолчанию (EUR, USD, RUB, GBP)" "${old_cur:-EUR}" valid_cur "Одна из: EUR, USD, RUB, GBP"
  local tz_default; tz_default=$(env_get TZ); [[ -n $tz_default ]] || tz_default=Europe/Amsterdam
  ask_valid APP_TZ "Часовой пояс" "$tz_default" valid_tz "Нет такого часового пояса, пример: Europe/Amsterdam"

  local tmp; tmp=$(mktemp "$APP_DIR/.env.XXXXXX")
  {
    printf '# Generated by deploy/setup-vds.sh on %s. Keep this file private.\n' "$(date -u +%Y-%m-%d)"
    printf 'BOT_TOKEN=%s\n' "$BOT_TOKEN"
    printf 'ALLOWED_USER_IDS=%s\n' "$ALLOWED_USER_IDS"
    printf 'WEBAPP_URL=%s\n' "$webapp"
    printf 'QUICK_TUNNEL_METRICS_URL=%s\n' "$quick_metrics"
    printf 'COMPOSE_PROFILES=%s\n' "$profiles"
    printf 'DOMAIN=%s\n' "${DOMAIN:-}"
    printf 'ACME_EMAIL=%s\n' "${ACME_EMAIL:-}"
    printf 'DUCKDNS_SUBDOMAIN=%s\n' "${DUCKDNS_SUBDOMAIN:-}"
    printf 'DUCKDNS_TOKEN=%s\n' "${DUCKDNS_TOKEN:-}"
    printf 'DEFAULT_CURRENCY=%s\n' "$DEFAULT_CURRENCY"
    printf 'TZ=%s\n' "$APP_TZ"
    printf 'LOG_LEVEL=info\n'
  } > "$tmp"
  chown "$ADMIN_USER:$ADMIN_USER" "$tmp"; chmod 600 "$tmp"
  mv -f "$tmp" "$env"
  ok ".env сохранён (права 600)"
}

check_dns() {
  local domain mode ip resolved
  domain=$(env_get DOMAIN); mode=$(env_get COMPOSE_PROFILES)
  [[ $mode == *caddy* && -n $domain ]] || return 0
  step "DNS и порты для Let's Encrypt"
  ip=$(public_ip)
  [[ -n $ip ]] || { warn "Не удалось узнать публичный IP — пропускаю проверку DNS"; return 0; }
  info "Публичный IP сервера: $ip"
  if [[ $mode == *duckdns* ]]; then
    local sub token answer
    sub=$(env_get DUCKDNS_SUBDOMAIN); token=$(env_get DUCKDNS_TOKEN)
    answer=$(printf 'url = "https://www.duckdns.org/update?domains=%s&token=%s&ip="\n' "$sub" "$token" | curl -fsS --max-time 20 --config - 2>/dev/null || true)
    if [[ $answer == OK ]]; then ok "DuckDNS: $domain → этот сервер"; else warn "DuckDNS не принял обновление — проверьте поддомен и токен"; fi
  fi
  local tries=0
  while :; do
    resolved=$( { dig +short A "$domain" @1.1.1.1 2>/dev/null || dig +short A "$domain" 2>/dev/null; } | grep -E '^[0-9.]+$' | tail -n1 || true)
    if [[ $resolved == "$ip" ]]; then ok "$domain → $ip"; break; fi
    warn "$domain указывает на '${resolved:-ничего}', а должен на $ip"
    info "Создайте A-запись: $domain → $ip (в панели FreeDNS/DuckDNS/регистратора)"
    tries=$((tries + 1))
    if ! interactive; then
      (( tries < 10 )) || { warn "Продолжаю без совпадения DNS — сертификат не будет выдан, пока DNS не исправлен"; break; }
      sleep 30; continue
    fi
    confirm "Проверить снова? (n — продолжить без проверки)" y || break
  done
  if ss -ltnpH '( sport = :80 or sport = :443 )' 2>/dev/null | grep -v docker-proxy | grep -q .; then
    warn "Порты 80/443 уже заняты другим сервисом (nginx/apache?) — остановите его, иначе Caddy не получит сертификат:"
    ss -ltnpH '( sport = :80 or sport = :443 )' 2>/dev/null | sed 's/^/      /' >&2 || true
  fi
}

deploy() {
  step "Сборка и запуск (первый раз — несколько минут)"
  compose up -d --build --remove-orphans
  local status=''
  for _ in $(seq 1 60); do
    status=$(docker inspect -f '{{.State.Health.Status}}' "$(compose ps -q bot)" 2>/dev/null || true)
    [[ $status == healthy ]] && break
    sleep 3
  done
  if [[ $status == healthy ]]; then
    ok "Бот запущен"
  else
    compose logs --tail 40 bot >&2 || true
    die "Бот не стал healthy — смотрите логи выше: cd $APP_DIR && docker compose logs bot"
  fi
  docker image prune -f >/dev/null 2>&1 || true
}

wait_certificate() {
  local domain; domain=$(env_get DOMAIN)
  [[ $(env_get COMPOSE_PROFILES) == *caddy* && -n $domain ]] || return 0
  step "HTTPS-сертификат Let's Encrypt"
  info "Caddy получает сертификат автоматически и сам продлевает его заранее."
  for _ in $(seq 1 40); do
    # --resolve checks the real certificate for the domain through this very
    # server, without relying on the hoster supporting hairpin NAT.
    if curl -fsS --max-time 10 -o /dev/null --resolve "$domain:443:127.0.0.1" "https://$domain/healthz" 2>/dev/null; then
      local cert
      cert=$(openssl s_client -connect 127.0.0.1:443 -servername "$domain" </dev/null 2>/dev/null | openssl x509 -noout -issuer -enddate 2>/dev/null || true)
      ok "https://$domain работает"
      if [[ -n $cert ]]; then printf '      %s\n' "${cert//$'\n'/$'\n      '}"; fi
      return 0
    fi
    sleep 6
  done
  warn "Сертификат пока не получен. Частые причины: DNS ещё не указывает на сервер,"
  warn "закрыт порт 80 у хостера (firewall в панели VDS), лимиты Let's Encrypt."
  compose logs --tail 20 caddy >&2 || true
  info "Caddy продолжит попытки сам. Проверка: sudo $SELF_INSTALL status"
}

install_self() {
  local src=$APP_DIR/deploy/setup-vds.sh
  [[ -f $src ]] || src=$(readlink -f "$0")
  install -m 0755 -o root -g root "$src" "$SELF_INSTALL"
}

summary() {
  local ip domain ids
  ip=$(public_ip); domain=$(env_get DOMAIN); ids=$(env_get ALLOWED_USER_IDS)
  step "Готово"
  info "Вход на сервер:      ssh $ADMIN_USER@${ip:-<IP>}"
  info "Код и .env:          $APP_DIR"
  [[ -n $domain && $(env_get COMPOSE_PROFILES) == *caddy* ]] && info "Mini App:            https://$domain/"
  [[ -n ${BOT_USERNAME:-} ]] && info "Бот:                 https://t.me/$BOT_USERNAME"
  if [[ -z $ids ]]; then
    printf '\n'
    warn "Бот в режиме настройки. Напишите ему /start — он пришлёт ваш Telegram ID,"
    warn "то же сделает партнёр. Затем: sudo $SELF_INSTALL ids"
  fi
  printf '\n'
  info "Команды: sudo $SELF_INSTALL update | ids | env | status"
  info "Логи:    cd $APP_DIR && docker compose logs -f bot"
}

cmd_status() {
  detect_admin_user
  [[ -n $ADMIN_USER ]] || die "Сначала выполните полную установку"
  step "Контейнеры"; compose ps
  step "Firewall"; ufw status verbose | sed 's/^/    /'
  step "fail2ban"; fail2ban-client status sshd 2>/dev/null | sed 's/^/    /' || warn "fail2ban не запущен"
  local domain; domain=$(env_get DOMAIN)
  if [[ -n $domain && $(env_get COMPOSE_PROFILES) == *caddy* ]]; then
    step "Сертификат $domain"
    openssl s_client -connect 127.0.0.1:443 -servername "$domain" </dev/null 2>/dev/null \
      | openssl x509 -noout -issuer -enddate 2>/dev/null | sed 's/^/    /' || warn "HTTPS недоступен"
  fi
}

cmd_ids() {
  detect_admin_user
  [[ -f $APP_DIR/.env ]] || die "Нет $APP_DIR/.env — сначала выполните полную установку"
  local ids=${ALLOWED_USER_IDS:-}
  info "Сейчас: ALLOWED_USER_IDS=$(env_get ALLOWED_USER_IDS)"
  ALLOWED_USER_IDS=$ids
  ask_valid ALLOWED_USER_IDS "Telegram ID через запятую" "" valid_ids "Только числа через запятую"
  ALLOWED_USER_IDS=${ALLOWED_USER_IDS// /}
  sed -i "s/^ALLOWED_USER_IDS=.*/ALLOWED_USER_IDS=$ALLOWED_USER_IDS/" "$APP_DIR/.env"
  compose up -d
  ok "Готово. Напишите боту /start — кнопка «✨ Мечты» откроет приложение."
}

cmd_update() {
  detect_admin_user
  [[ -n $ADMIN_USER && -d $APP_DIR/.git ]] || die "Сначала выполните полную установку"
  step "Обновление"
  as_admin git -C "$APP_DIR" pull --ff-only
  install_self
  deploy
  wait_certificate
}

cmd_setup() {
  preflight
  install_packages
  setup_swap
  harden_sysctl
  setup_admin_user
  harden_ssh
  setup_firewall
  setup_fail2ban
  setup_auto_updates
  install_docker
  clone_repo
  configure_env
  check_dns
  deploy
  wait_certificate
  install_self
  summary
}

main() {
  [[ $EUID -eq 0 ]] || { echo "Запустите от root: sudo bash $0 ${1:-}" >&2; exit 1; }
  touch "$LOG_FILE"; chmod 600 "$LOG_FILE"
  local cmd=${1:-setup}
  case $cmd in
    setup)  cmd_setup ;;
    update) cmd_update ;;
    ids)    cmd_ids ;;
    env)    detect_admin_user; preflight; FORCE_ENV=1 configure_env; check_dns; deploy; wait_certificate; summary ;;
    status) cmd_status ;;
    -h|--help|help) sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//' ;;
    *) die "Неизвестная команда: $cmd (setup|update|ids|env|status)" ;;
  esac
}

# Run only when executed, not when sourced (e.g. by tests).
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  main "$@" 2>&1 | tee -a "$LOG_FILE"
  exit "${PIPESTATUS[0]}"
fi
