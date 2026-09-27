// Prints a local Mini App URL with freshly signed launch data, so the app can be
// opened in a plain browser against a locally running service.
//
//   BOT_TOKEN=... bun run scripts/dev-url.ts <telegramUserId> [wish=12 | recipe=4]
//
// DEV_URL overrides the base (default http://localhost:5173/, the Vite dev
// server, which proxies /api and /media to the Go service). The signed data is
// valid for INITDATA_MAX_AGE on the server: treat the printed URL as a secret.
// This is a development tool; the app bundle never imports it.

import { createHmac } from 'node:crypto'

function fail(message: string): never {
  console.error(message)
  console.error('usage: BOT_TOKEN=... bun run scripts/dev-url.ts <telegramUserId> [wish=<id> | recipe=<id>]')
  process.exit(1)
}

const token = process.env.BOT_TOKEN?.trim()
if (!token) fail('BOT_TOKEN is not set')

const userId = process.argv[2] ?? ''
if (!/^[1-9]\d{0,15}$/.test(userId)) fail(`not a Telegram user id: ${JSON.stringify(userId)}`)

const deepLink = process.argv[3]
if (deepLink !== undefined && !/^(wish|recipe)=[1-9]\d{0,14}$/.test(deepLink)) fail(`bad deep link: ${deepLink}`)

const base = new URL(process.env.DEV_URL ?? 'http://localhost:5173/')
if (deepLink) base.search = `?${deepLink}`

const data = new URLSearchParams()
data.set('auth_date', String(Math.floor(Date.now() / 1000)))
data.set('query_id', 'AAHdev0000000000')
data.set('user', JSON.stringify({ id: Number(userId), first_name: process.env.DEV_NAME ?? 'Дима', language_code: 'ru' }))

// Same as the server: all fields except hash, "key=value" sorted and joined with \n.
const checkString = [...data.entries()]
  .map(([k, v]) => `${k}=${v}`)
  .sort()
  .join('\n')
const secret = createHmac('sha256', 'WebAppData').update(token).digest()
data.set('hash', createHmac('sha256', secret).update(checkString).digest('hex'))

const theme = {
  bg_color: '#ffffff',
  secondary_bg_color: '#efeff4',
  section_bg_color: '#ffffff',
  text_color: '#000000',
  hint_color: '#707579',
  link_color: '#2481cc',
  button_color: '#2481cc',
  button_text_color: '#ffffff',
}

const hash = new URLSearchParams({
  tgWebAppData: data.toString(),
  tgWebAppVersion: '10.1',
  tgWebAppPlatform: 'unknown',
  tgWebAppThemeParams: JSON.stringify(theme),
  tgWebAppDebug: '1',
})
console.log(`${base.toString()}#${hash.toString().replaceAll('+', '%20')}`)
