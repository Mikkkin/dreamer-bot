# Mini App HTTP API

The Mini App talks to the Go service over a small JSON API served from the
same origin as the app. There is no CORS, and there are no cookies or server sessions.

## Authentication

Every `/api/*` request carries the raw Telegram launch data:

```
Authorization: tma <Telegram.WebApp.initData>
```

For each request the server checks:

1. The `hash` is valid. The secret is `HMAC_SHA256(key="WebAppData", msg=BOT_TOKEN)`, compared in constant time.
2. `auth_date` is not older than `INITDATA_MAX_AGE` (24h by default) and not in the future.
3. `user.id` is in `ALLOWED_USER_IDS`.

| Failure | Status | `error.code` |
|---|---|---|
| Header missing, malformed, bad signature, or expired | 401 | `unauthorized` |
| Valid signature but the user is not whitelisted | 403 | `forbidden` |

## Conventions

- JSON bodies only (`Content-Type: application/json`). Unknown fields are rejected, and bodies are capped at 64 KiB.
- Timestamps are RFC 3339 in UTC.
- A money amount is a **decimal string** such as `"1200.50"`, paired with an ISO-4217 `currency`. The server parses it with the same routine the bot uses.
- Errors always have this shape:

```json
{ "error": { "code": "validation", "message": "Название обязательно", "field": "title" } }
```

`code` is one of `validation` (400), `unauthorized` (401), `forbidden` (403),
`not_found` (404), `conflict` (409), `too_large` (413), `unsupported_media` (415),
`limit` (422), `rate_limited` (429), `internal` (500). `message` is in Russian and
safe to show to users. `field` is present only for validation errors.

## Objects

### Wish

```json
{
  "id": 12,
  "title": "Поездка в Токио",
  "note": "Весной, на сакуру",
  "category_id": 2,
  "link": "https://example.com/tour",
  "price": { "amount": "1200.50", "currency": "EUR", "formatted": "1 200,50 €" },
  "status": "want",
  "hot": true,
  "author": { "id": 111, "name": "Дима" },
  "images": [ { "id": 5, "width": 1600, "height": 1200,
                "thumb_url": "/media/5/thumb?exp=1790000000&sig=…",
                "full_url":  "/media/5/full?exp=1790000000&sig=…" } ],
  "created_at": "2026-09-27T13:50:18Z",
  "updated_at": "2026-09-27T13:50:18Z",
  "fulfilled_at": null
}
```

`category_id`, `link`, `price` and `fulfilled_at` may be `null`. `status` is one of
`want` («Хотим»), `progress` («Копим»), or `done` («Сбылось»).

### Category

```json
{ "id": 2, "name": "Путешествия", "emoji": "✈️", "position": 1 }
```

### Recipe («Что приготовить»)

Recipes form a flat list with no categories and no statuses. Every part except
the title is optional: a link to a recipe, a hand-written recipe, screenshots, or
any mix of these.

```json
{
  "id": 4,
  "title": "Паста карбонара",
  "link": "https://example.com/carbonara",
  "body": "1. Сварить пасту\n2. …",
  "author": { "id": 111, "name": "Дима" },
  "images": [ { "id": 9, "width": 1170, "height": 2532, "thumb_url": "…", "full_url": "…" } ],
  "created_at": "2026-09-27T13:50:18Z",
  "updated_at": "2026-09-27T13:50:18Z"
}
```

`link` may be `null`, and `body` may be `""`. The body is plain text with
newlines. Clients must render it as text (`white-space: pre-wrap`), never as HTML.

## Endpoints

| Method & path | Body | Response |
|---|---|---|
| `GET /api/me` | — | `200` [Me](#me) |
| `GET /api/wishes?status=&category=&q=` | — | `200 {"wishes":[Wish…]}`, newest first. `category=none` selects uncategorized wishes. All filters are optional. |
| `POST /api/wishes` | [WishInput](#wishinput) | `201` Wish |
| `GET /api/wishes/{id}` | — | `200` Wish |
| `PATCH /api/wishes/{id}` | partial WishInput. A field set to `null` is cleared, an absent field is untouched. | `200` Wish |
| `PUT /api/wishes/{id}/status` | `{"status":"done"}` | `200` Wish |
| `DELETE /api/wishes/{id}` | — | `204` |
| `POST /api/wishes/{id}/images` | `multipart/form-data`, field `file`: JPEG, PNG, or WebP, ≤ 10 MiB | `201` Image |
| `DELETE /api/wishes/{id}/images/{imageId}` | — | `204` |
| `GET /api/categories` | — | `200 {"categories":[Category…]}` |
| `POST /api/categories` | `{"name":"Книги","emoji":"📚"}` | `201` Category |
| `PATCH /api/categories/{id}` | `{"name":"…","emoji":"…"}` | `200` Category |
| `DELETE /api/categories/{id}` | — | `204`. Wishes in the category become uncategorized. |
| `GET /api/recipes?q=` | — | `200 {"recipes":[Recipe…]}`, newest first. `q` matches the title or the body. |
| `GET /api/recipes/random` | — | `200` Recipe, or `404 not_found` when there are no recipes («🎲 Что приготовить?») |
| `POST /api/recipes` | [RecipeInput](#recipeinput) | `201` Recipe |
| `GET /api/recipes/{id}` | — | `200` Recipe |
| `PATCH /api/recipes/{id}` | partial RecipeInput. A `null` link clears it. | `200` Recipe |
| `DELETE /api/recipes/{id}` | — | `204` |
| `POST /api/recipes/{id}/images` | `multipart/form-data`, field `file` | `201` Image |
| `DELETE /api/recipes/{id}/images/{imageId}` | — | `204` |
| `GET /api/stats` | — | `200` [Stats](#stats) |
| `GET /media/{imageId}/{thumb\|full}?exp=&sig=` | — | `200 image/jpeg`. The signature is required; a bad or expired one returns `403`. |
| `GET /healthz` | — | `200 ok`, no auth |

### WishInput

```json
{
  "title": "Поездка в Токио",
  "note": "",
  "category_id": 2,
  "link": null,
  "price": { "amount": "1200.50", "currency": "EUR" },
  "hot": false
}
```

Only `title` is required. `link` and `price` are opt-in: the UI hides them
behind switches and sends `null` when a switch is off.

### RecipeInput

```json
{ "title": "Паста карбонара", "link": null, "body": "1. Сварить пасту…" }
```

Only `title` is required. The UI puts the link and the text behind switches
(«Ссылка на рецепт», «Написать рецепт»). Screenshots are uploaded afterwards
through the images endpoint.

### Me

```json
{
  "user": { "id": 111, "name": "Дима" },
  "partners": [ { "id": 222, "name": "Аня" } ],
  "currencies": [ { "code": "EUR", "symbol": "€" }, { "code": "USD", "symbol": "$" },
                  { "code": "RUB", "symbol": "₽" }, { "code": "GBP", "symbol": "£" } ],
  "default_currency": "EUR",
  "limits": { "title_max": 120, "note_max": 2000, "link_max": 2048,
              "images_per_wish": 10, "image_max_bytes": 10485760,
              "category_name_max": 32, "recipe_body_max": 10000,
              "images_per_recipe": 10 }
}
```

### Stats

```json
{
  "overall": { "want": { "count": 8, "sums": [ { "amount": "3450", "currency": "EUR", "formatted": "3 450 €" } ] },
               "progress": { "count": 2, "sums": [] },
               "done": { "count": 7, "sums": [] } },
  "categories": [
    { "category": { "id": 2, "name": "Путешествия", "emoji": "✈️", "position": 1 },
      "by_status": { "want": { "count": 3, "sums": [] }, "progress": { "count": 0, "sums": [] }, "done": { "count": 1, "sums": [] } } },
    { "category": null, "by_status": { "…": "…" } }
  ],
  "fulfilled_this_year": 5,
  "recipes": 12
}
```

All three status keys are always present in `overall` and in each `by_status`.

## Media URLs

Image URLs are signed with `HMAC_SHA256(key = HMAC_SHA256("dreamer/media/v1", BOT_TOKEN), msg = "{imageId}/{variant}/{exp}")`
and returned inside Wish objects. `exp` is rounded up to the next hour, and each
URL stays valid for at least 12 hours. Because of the rounding, repeated list loads
return identical URLs, so the browser cache keeps working. Responses carry
`Cache-Control: private, max-age=…` up to `exp`, plus `X-Content-Type-Options: nosniff`.

## Deep links

Bot messages open the app on a specific item with `WEBAPP_URL?wish={id}` or
`WEBAPP_URL?recipe={id}`.
The URL hash is reserved for Telegram's launch parameters, so it is never used
for routing.
