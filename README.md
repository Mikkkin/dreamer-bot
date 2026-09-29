# dreamer-bot

A private Telegram bot and Mini App where two people keep a shared list of wishes (things to buy, trips, experiences) and a list of recipes they want to cook. Only the Telegram accounts you whitelist can use it. Everyone else gets no reply at all.

[![CI](https://github.com/Mikkkin/dreamer-bot/actions/workflows/ci.yml/badge.svg)](https://github.com/Mikkkin/dreamer-bot/actions/workflows/ci.yml)

🇷🇺 [Русская версия](README.ru.md)

<p align="center">
  <img src="docs/screenshots/home.png" width="260" alt="Wishes board">
  <img src="docs/screenshots/recipes.png" width="260" alt="Recipes">
  <img src="docs/screenshots/stats-dark.png" width="260" alt="Statistics in dark theme">
</p>

## What it does

- **Wishes.** Each wish gets categories, photos and one of three statuses: *Хотим* (want), *Копим* (saving up), *Сбылось ✨* (came true). Price and link are **optional**: switches in the form reveal them only when you want them. In *Копим* you log what each of you put aside, and a progress bar shows how much of the price is saved.
- **Recipes («Что приготовить»).**
  - Add a link, write the recipe yourself, attach screenshots, or combine them.
  - Group recipes by cuisine (Русская, Азиатская…) and by meal or course (Завтрак, Ужин, Первое…). Both lists come with defaults, and you can add your own.
  - List ingredients and enter КБЖУ per 100 g; the values for the whole dish and per serving are calculated.
  - After cooking, tap «🍳 Приготовили» and each of you rates the dish from 1 to 5 stars. The recipe stays in the list with its history, average rating and how many times you've cooked it.
  - «🎲 Что приготовить?» picks a random recipe.
- **Mini App.** The main interface. It follows each person's Telegram theme (light or dark) and uses the native Telegram buttons and haptic feedback.
- **Chat bot.** Send it text, a link or photos and a draft card appears: choose wish or recipe, a category, a price or link, then save. `/list`, `/recipes`, `/cook`, `/shop` and `/stats` work right in the chat.
- **Shopping list.**
  - Add a recipe's ingredients in one tap. Same products merge: 500 мл + 250 мл becomes 750 мл, and 200 мл + 0,5 л becomes 700 мл.
  - Tick items off as you buy them.
  - Search any item in ВкусВилл, Перекрёсток, Магнит, Пятёрочка, Лавка, Самокат, Купер, Лента, Ашан, METRO or Азбука вкуса.
  - **«Собрать корзину во ВкусВилле»** matches the whole list to real products with prices, using ВкусВилл's official MCP server, and opens a ready cart. The other stores protect their sites against bots, so for them the bot offers search links only.
- **Partner notifications.** The other person gets a message with the photo and an «Открыть ✨» button when one of you:
  - adds a wish or a recipe;
  - edits a recipe (a burst of edits becomes one message);
  - cooks and rates something, with star buttons to rate it too;
  - puts money aside;
  - marks a wish as fulfilled.
- **Statistics.** Totals per category and per currency (currencies are never mixed or converted), how much is saved, how many dreams came true this year, and how often you cook.

## One-command VDS setup

[`deploy/setup-vds.sh`](deploy/setup-vds.sh) turns a fresh Ubuntu 22.04/24.04 or Debian 12/13 server (amd64 or arm64) into a ready deployment. It asks only what it needs and does the following:
- updates the system and adds swap on small servers;
- creates an admin user with your SSH key, then switches SSH to key-only login with root login disabled (it checks that your key works before applying this);
- sets up UFW (SSH, 80 and 443 only), fail2ban and automatic security updates;
- installs Docker;
- clones the repo, using a read-only deploy key if the repo is private;
- fills in `.env`, checks that DNS points at the server, deploys, and waits until the **Let's Encrypt** certificate is live. Caddy renews it automatically.

```bash
scp deploy/setup-vds.sh root@<server-ip>:
ssh -t root@<server-ip> 'bash setup-vds.sh'   # -t: the script asks questions
```

It then installs itself as `dreamer-vds`: `sudo dreamer-vds update | ids | env | status | backup | restore`.

## Quick start (VPS + free domain)

You need:
- a server with a public IP and ports 80 and 443 open, for example any small amd64 or arm64 VPS;
- Docker with Compose v2;
- a bot token from [@BotFather](https://t.me/BotFather) (`/newbot`).

A domain can be free:
- [FreeDNS](https://freedns.afraid.org): add a subdomain such as `dreams.mooo.com` with an **A record** pointing at the VPS IP.
- [DuckDNS](https://www.duckdns.org): see [Deployment options](#deployment-options).

```bash
git clone https://github.com/Mikkkin/dreamer-bot.git && cd dreamer-bot
cp .env.example .env && chmod 600 .env
```

Fill in `.env`:

```dotenv
BOT_TOKEN=123456789:AA...            # from @BotFather
DOMAIN=dreams.mooo.com               # your (free) domain
WEBAPP_URL=https://dreams.mooo.com/
ACME_EMAIL=you@example.com           # for the certificate authority
COMPOSE_PROFILES=caddy
```

```bash
docker compose up -d --build
```

Caddy gets a free HTTPS certificate by itself. It uses Let's Encrypt and falls back to ZeroSSL if Let's Encrypt's limits for a shared free domain are used up. Then:

1. Send `/start` to your bot. `ALLOWED_USER_IDS` is still empty, so the bot is in **setup mode** and replies with your Telegram ID. Ask your partner to do the same.
2. Put both IDs into `.env`, for example `ALLOWED_USER_IDS=111111111,222222222`, then run `docker compose up -d`.
3. Send `/start` again. The menu button **✨ Мечты** now opens the Mini App.

## Deployment options

> **Do you need a domain at all?** Not for the chat bot. It only makes outbound connections to Telegram (long polling), so it works anywhere, even on a laptop behind NAT, and `docker compose up -d` without any profile is enough. The **Mini App** is different: Telegram opens it only from a public `https://` address with a valid certificate, so for the app you need one of the options below.

Every option runs the same image, built for **linux/amd64 and linux/arm64**: an x86 VPS, a Raspberry Pi or an Apple Silicon Mac. Choose profiles with `COMPOSE_PROFILES` in `.env`, or pass `--profile` to each command.

| Profiles | When | What you need | `.env` |
|---|---|---|---|
| `caddy` | Server with a static public IP | any domain (own, or free on FreeDNS) with an A/AAAA record pointing at it; ports 80 and 443 open | `DOMAIN`, `WEBAPP_URL`, `ACME_EMAIL` |
| `caddy,duckdns` | Home server with a changing public IP | a free DuckDNS subdomain; ports 80 and 443 forwarded on the router; a real public IP, not carrier-grade NAT | the above plus `DUCKDNS_SUBDOMAIN`, `DUCKDNS_TOKEN` |
| `tunnel` | No open ports at all | a domain on Cloudflare | `CLOUDFLARE_TUNNEL_TOKEN`, `WEBAPP_URL` |
| `quick` | A quick try-out, no domain | a network that allows Cloudflare tunnels (outbound port 7844) | leave `WEBAPP_URL` empty |
| *(none)* | Behind your own reverse proxy | a proxy that terminates HTTPS and forwards to `127.0.0.1:8080` | `WEBAPP_URL` |

**DuckDNS (`caddy,duckdns`)**
1. Sign in at duckdns.org, add a subdomain (e.g. `ourdreams`) and copy the token.
2. Set `DOMAIN=ourdreams.duckdns.org`, `WEBAPP_URL=https://ourdreams.duckdns.org/`, `DUCKDNS_SUBDOMAIN=ourdreams` and `DUCKDNS_TOKEN=<token>`.
3. Run `docker compose up -d --build` with `COMPOSE_PROFILES=caddy,duckdns`.

The `duckdns` service updates the IP every 5 minutes. The token goes only to duckdns.org over HTTPS and never appears in logs.

**Cloudflare named tunnel (`tunnel`)**
1. In Cloudflare Zero Trust, open *Networks → Tunnels* and create a tunnel. Choose the Docker connector and copy the token.
2. Add a public hostname, for example `dreams.example.com`, with the service `HTTP` → `bot:8080`.
3. Set `CLOUDFLARE_TUNNEL_TOKEN=<token>`, `WEBAPP_URL=https://dreams.example.com/` and `COMPOSE_PROFILES=tunnel`.

**Quick tunnel (`quick`)** gives the Mini App a temporary `https://*.trycloudflare.com` address. Leave `WEBAPP_URL` empty: the bot discovers the address and sets the menu button itself. The address changes on every restart, so use it only for trying things out. Some networks block Cloudflare tunnels. If the logs show `failed to dial to edge` or `TLS handshake with edge error`, use a domain instead.

To build on one machine and run on another, build for both platforms with `make image-multiarch`. To push to your registry, run `make image-multiarch IMAGE=ghcr.io/you/dreamer-bot PUSH=1`.

## Configuration

Every setting is an environment variable, read from `.env` by Docker Compose. [`.env.example`](.env.example) documents each one.

| Variable | Default | Meaning |
|---|---|---|
| `BOT_TOKEN` | — (required) | Token from @BotFather |
| `ALLOWED_USER_IDS` | empty = setup mode | Comma-separated Telegram user IDs allowed to use the bot |
| `WEBAPP_URL` | empty | Public HTTPS address of the Mini App. Leave empty with the `quick` profile |
| `DEFAULT_CURRENCY` | `EUR` | `EUR`, `USD`, `RUB` or `GBP` |
| `INITDATA_MAX_AGE` | `24h` | How long one Mini App launch stays valid (`1m`–`168h`) |
| `MAX_IMAGE_MB` | `10` | Upload limit for a single photo (1–50) |
| `TZ` | `UTC` | Time zone for dates and the "this year" statistic, for example `Europe/Amsterdam` |
| `VKUSVILL_ENABLED` | `true` | «Собрать корзину во ВкусВилле». Set `false` to keep the server from contacting ВкусВилл at all; the search links still work |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `HTTP_PORT` | `8080` | Host port on `127.0.0.1` where the Mini App is also reachable |
| `COMPOSE_PROFILES` | — | Profiles to run, e.g. `caddy` or `caddy,duckdns` |
| `DOMAIN`, `ACME_EMAIL` | — | `caddy` profile: the domain and the e-mail for the certificate authority (both required) |
| `DUCKDNS_SUBDOMAIN`, `DUCKDNS_TOKEN` | — | `duckdns` profile only |
| `CLOUDFLARE_TUNNEL_TOKEN` | — | `tunnel` profile only |

## Bot commands

| Command | What it does |
|---|---|
| `/start` | Welcome message and the button that opens the app |
| `/list` | Wishes by status, with pages. Tap one to see its card |
| `/recipes` | All recipes |
| `/cook` | A random recipe for today |
| `/shop` | The shopping list: tick items off or clear the bought ones |
| `/stats` | Totals per category and currency |
| `/help` | How to use the bot |
| `/cancel` | Cancel the current input or draft |

You can also send the bot plain text, a link, a photo or an album without any command. It turns what you sent into a draft card with buttons.

## Security model

- **One root secret.** The bot token authenticates the bot. The Mini App login check and the image URL signatures use keys derived from it (each for a different purpose), so there are no other secrets to manage. The token never appears in logs: every log line passes through a redacting handler, and HTTP client errors, which contain the token in their URL, are scrubbed.
- **Whitelist first.** Updates from users who aren't whitelisted are dropped without a reply and without an acknowledgement. Only the user ID is logged. Messages from groups and channels are ignored, and the bot leaves any group it gets added to.
- **Mini App authentication.** Every API request carries Telegram's signed launch data (`Authorization: tma …`). The server checks:
  - the HMAC-SHA256 signature, in constant time;
  - its age;
  - that no parameter appears twice, which blocks a known parameter-injection trick;
  - that the user ID is on the whitelist.

  There are no cookies and no sessions.
- **Images.** Uploads are size-limited. The format is checked from the file contents (JPEG, PNG or WebP only), and huge dimensions are rejected before decoding. Every image is re-encoded, which removes EXIF and GPS metadata. Files are stored under random server-generated names. They are served only through HMAC-signed URLs that expire.
- **No fetching of user links.** Links are validated (http/https only) and shown to you, but the server never downloads them, so there is nothing to exploit with SSRF. The only outbound call the server makes, besides Telegram, goes to ВкусВилл's official MCP server for the cart. The address is fixed in the code, redirects are not followed, and replies are capped in size and time. Only product names from your shopping list are sent, never who you are. `VKUSVILL_ENABLED=false` turns it off.
- **Web hardening.**
  - A strict Content-Security-Policy. The only external script is Telegram's `telegram-web-app.js`, and only `web.telegram.org` may frame the app.
  - `nosniff` and `no-referrer` headers.
  - Per-user rate limits, request body limits and server timeouts.
  - Bot updates use long polling, so there is no public webhook to attack.
- **Container.** A distroless image with no shell. The process runs as a non-root user (UID 65532) on a read-only root filesystem, with all Linux capabilities dropped and `no-new-privileges` set. The Cloudflare token never reaches the bot container.

Want to report a vulnerability? Please open a private security advisory on GitHub rather than a public issue.

## Updating without losing data

All data lives in the `dreamer-data` Docker volume: the SQLite database and the processed images. The volume survives rebuilds and container re-creation. Schema changes ship as new migrations, which run automatically on start, each in its own transaction.

```bash
sudo dreamer-vds update     # git pull → build the new version → back up the data → restart
```

The update builds the new version while the old one keeps running. It then archives the data to `/var/backups/dreamer/`, which stops the bot for a few seconds, keeps the last 10 archives, and restarts the bot. If something goes wrong, restoring takes one command:

```bash
sudo dreamer-vds backup                                  # an archive right now
sudo dreamer-vds restore                                 # list the archives
sudo dreamer-vds restore /var/backups/dreamer/<file>     # restore one (the current data is archived first)
```

Never run `docker compose down -v`: `-v` deletes the data volume. A newer schema cannot be opened by an older binary, so going back to an old version means restoring the backup made before the update.

## How it is built

```
Telegram ──long polling──▶ bot (go-telegram/bot) ─┐
                                                  ├─▶ services ─▶ SQLite (modernc, WAL) + image store
Mini App ──HTTPS /api───▶ HTTP API (net/http) ────┘
   ▲ React 19 + Vite, embedded into the Go binary
```

- **Backend:** Go 1.27, [go-telegram/bot](https://github.com/go-telegram/bot) (Bot API 10.3), `modernc.org/sqlite` (pure Go, so the binary is static and needs no CGO), `disintegration/imaging` for image processing.
- **Frontend:** React 19, Vite 8, TypeScript 7, built with bun. The components are custom and styled with Telegram theme variables, with no UI kit.
- **Layout:** `internal/domain` holds the rules and validation, `internal/service` the use cases, and `internal/storage/sqlite` and `internal/media` the adapters. `internal/httpapi` and `internal/bot` are thin delivery layers on top. The full HTTP contract is in [`docs/API.md`](docs/API.md).

## Development

You need Go ≥ 1.27 and bun ≥ 1.4. The Makefile runs everything CI runs.

```bash
make check          # gofmt, go vet, staticcheck, go test -race, govulncheck, frontend typecheck and tests
make web build run  # build the Mini App, embed it, run the binary with ./data and your .env
```

To work on the Mini App in a normal browser:

```bash
cd web && DREAMER_BACKEND_URL=http://127.0.0.1:8080 bun run dev          # Vite on :5173
BOT_TOKEN=... DEV_URL=http://localhost:5173/ bun run dev-url <your-telegram-id>
```

`dev-url` prints a signed launch URL that includes Telegram's debug bottom bar. Treat that URL like a password until `INITDATA_MAX_AGE` passes.

## Troubleshooting

- **The bot doesn't answer at all.** Check whether your ID is in `ALLOWED_USER_IDS`. The log shows each dropped update with the sender's ID: `docker compose logs bot | grep dropped`.
- **`409 Conflict` in the logs.** Two copies of the bot are polling with the same token. Stop the other one.
- **The container exits with "check BOT_TOKEN".** Telegram rejected the token. Get a new one with `/revoke` in @BotFather.
- **The menu button doesn't open the app.** Make sure `WEBAPP_URL` is an `https://` address that you can reach from your phone. With the `quick` profile, the log line `quick tunnel hostname discovered` shows the current address.
- **Caddy exits with "set DOMAIN and ACME_EMAIL".** Fill both in `.env`.
- **No HTTPS certificate.** The domain must resolve to this server (`dig +short your.domain`), and ports 80 and 443 must be reachable from the internet. Caddy retries automatically; see `docker compose logs caddy`.
