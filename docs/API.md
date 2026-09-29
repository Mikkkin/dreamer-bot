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
`limit` (422), `rate_limited` (429), `internal` (500), `unavailable` (503, an external
service such as ВкусВилл did not answer). `message` is in Russian and
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
  "saved": { "total": { "amount": "12000", "currency": "RUB", "formatted": "12 000 ₽" }, "percent": 26, "count": 3 },
  "author": { "id": 111, "name": "Дима" },
  "images": [ { "id": 5, "width": 1600, "height": 1200,
                "thumb_url": "/media/5/thumb?exp=1790000000&sig=…",
                "full_url":  "/media/5/full?exp=1790000000&sig=…" } ],
  "created_at": "2026-09-27T13:50:18Z",
  "updated_at": "2026-09-27T13:50:18Z",
  "fulfilled_at": null
}
```

`category_id`, `link`, `price`, `saved` and `fulfilled_at` may be `null`. `saved.percent` is 0–100 of the price, or `null` without a price. `status` is one of
`want` («Хотим»), `progress` («Копим»), or `done` («Сбылось»).

### Category

```json
{ "id": 2, "name": "Путешествия", "emoji": "✈️", "position": 1 }
```

### Recipe («Что приготовить»)

Every part except the title is optional. That covers a link, a hand-written recipe, screenshots, cuisine and course tags, ingredients and КБЖУ. Cooking a recipe adds a record to its history but **never removes it from the list**.

```json
{
  "id": 4,
  "title": "Паста карбонара",
  "link": "https://example.com/carbonara",
  "body": "1. Сварить пасту\n2. …",
  "cuisine_id": 3,
  "course_ids": [9, 11],
  "ingredients": [
    { "name": "Спагетти", "amount": "320", "unit": "г", "formatted": "320 г" },
    { "name": "Соль", "amount": null, "unit": "по вкусу", "formatted": "по вкусу" },
    { "name": "Яйца", "amount": "4", "unit": "шт", "formatted": "4 шт" }
  ],
  "nutrition": {
    "per_100g":    { "kcal": "150", "protein": "12.5", "fat": "6", "carbs": "10.4" },
    "weight_g": 800,
    "servings": 4,
    "per_dish":    { "kcal": "1200", "protein": "100", "fat": "48", "carbs": "83.2" },
    "per_serving": { "kcal": "300", "protein": "25", "fat": "12", "carbs": "20.8" }
  },
  "cooking": { "count": 3, "last_cooked_at": "2026-09-20T18:02:00Z", "rating_avg": "4.5", "rating_count": 4 },
  "author": { "id": 111, "name": "Дима" },
  "images": [ { "id": 9, "width": 1170, "height": 2532, "thumb_url": "…", "full_url": "…" } ],
  "created_at": "2026-09-27T13:50:18Z",
  "updated_at": "2026-09-27T13:50:18Z"
}
```

Field rules:

- **Nullable or empty fields.**
  - `link`, `cuisine_id` and `nutrition` may be `null`.
  - `body` may be `""`, and `course_ids` and `ingredients` may be `[]`.
  - The body is plain text with newlines. Clients must render it as text (`white-space: pre-wrap`), never as HTML.
- **Nutrition numbers** are decimal strings with at most one decimal place. All four (`kcal`, `protein`, `fat`, `carbs`) are required when `nutrition` is sent — a blank value is unknown, not zero, so write `"0"` for a real zero. `weight_g` and `servings` may be `null`.
  - `per_dish` is `null` without `weight_g`.
  - `per_serving` is `null` without both `weight_g` and `servings`.
  - The server computes both and rounds to 0.1.
- **Cooking.** `cooking.last_cooked_at` and `cooking.rating_avg` are `null` until the first cooking or rating. `rating_avg` is the average of all ratings over all cookings, with one decimal.
- **Ingredient amounts.** `amount` is `null` when not given. `unit` is one of `Me.units` or `null`.

### RecipeTag

Recipes are grouped along two independent axes. Both lists come with defaults, and users can add, rename and delete their own tags.

```json
{ "id": 3, "kind": "cuisine", "name": "Азиатская", "emoji": "🍜", "position": 3 }
```

- `kind: "cuisine"`: Русская, Европейская, Итальянская, Азиатская, Кавказская, Мексиканская. A recipe has **at most one**.
- `kind: "course"`: Завтрак, Обед, Ужин, Первое, Второе, Салат, Перекус, Десерт, Напиток. A recipe may have **up to 8**.

### Cook and Rating

```json
{
  "id": 17,
  "recipe_id": 4,
  "cooked_by": { "id": 111, "name": "Дима" },
  "cooked_at": "2026-09-20T18:02:00Z",
  "ratings": [
    { "user": { "id": 111, "name": "Дима" }, "stars": 5, "comment": "Идеально", "rated_at": "…" },
    { "user": { "id": 222, "name": "Аня" }, "stars": 4, "comment": "", "rated_at": "…" }
  ]
}
```

Each person has at most one rating per cooking. Rating again replaces it.

### Saving («Копим»)

```json
{ "id": 5, "wish_id": 12, "amount": { "amount": "5000", "currency": "RUB", "formatted": "5 000 ₽" },
  "user": { "id": 222, "name": "Аня" }, "note": "с зарплаты", "created_at": "…" }
```

All savings of a wish use one currency: the price currency when there is a price, otherwise the currency of the first saving.

### ShoppingItem

```json
{ "id": 31, "name": "Молоко", "quantity": { "amount": "750", "unit": "мл", "formatted": "750 мл" },
  "checked": false, "recipe_id": 4, "added_by": { "id": 111, "name": "Дима" },
  "created_at": "…", "updated_at": "…" }
```

`quantity` and `recipe_id` may be `null`. Adding an item whose name (case-insensitive) matches an **unchecked** item with the same unit, or the related one (г↔кг, мл↔л), merges the amounts into that item: 500 мл + 250 мл gives 750 мл, and 200 мл + 0,5 л gives 700 мл. A mixed sum is shown in the larger unit when it is at least 1 and has at most two decimals there (500 г + 1 кг = 1,5 кг), otherwise in the smaller one (333 г + 1 кг = 1333 г). Other units never merge.

### Store

```json
{ "id": "vkusvill", "name": "ВкусВилл", "emoji": "🥬", "search_url_template": "https://vkusvill.ru/search/?q={q}", "opens_app": false, "cart": true }
```

- **Search links.** The client replaces `{q}` with `encodeURIComponent(name)`, using the product name only, without the quantity. It opens the result with `Telegram.WebApp.openLink`, after a user tap: Telegram allows `openLink` only from a gesture. The templates are fixed on the server.
- **`opens_app`** is a hint: whether iOS opens the store app for such links.
- **`cart: true`** marks the one store with a real cart integration, ВкусВилл, described below. For every other store the server fetches nothing: their sites sit behind anti-bot challenges (Servicepipe, Qrator, NGENIX), and scraping them would break their terms.

### ВкусВилл cart («Собрать корзину во ВкусВилле»)

ВкусВилл publishes an official, experimental MCP server at `https://mcp.vkusvill.ru/mcp`, announced on its Habr blog on 2025-12-30. The Go server calls it on the user's tap. Only this fixed host is contacted, the only input sent is product names, and results are cached. The flow:

1. `POST /api/shopping/vkusvill/match` returns up to 3 candidate products with prices for every unchecked list item.
2. The user confirms or swaps the candidates and sets the quantities.
3. `POST /api/shopping/vkusvill/cart` creates a shared basket and returns its link. The client opens it with `openLink`: the basket appears on vkusvill.ru, and the user logs in there to order.

```json
// POST /api/shopping/vkusvill/match   body: {"item_ids":[31,32]}   (optional; default: all unchecked, at most 30)
{ "matches": [
    { "item_id": 31, "query": "Молоко",
      "candidates": [ { "xml_id": 173, "name": "Молоко 3,2%, 1 л",
                        "price": { "amount": "93", "currency": "RUB", "formatted": "93 ₽" },
                        "unit": "шт", "weight": "1 л" } ] } ] }

// POST /api/shopping/vkusvill/cart    body: {"lines":[{"xml_id":173,"quantity":"2"}]}   (1–30 lines, quantity 0.01–40)
{ "url": "https://vkusvill.ru/?share_basket=2063312749",
  "estimated_total": { "amount": "186", "currency": "RUB", "formatted": "186 ₽" } }
```

- When ВкусВилл is unreachable, slow (over 8 s) or disabled (`VKUSVILL_ENABLED=false`), both endpoints answer `503` with `error.code: "unavailable"`. The client then falls back to the search links.
- The server checks that the returned basket URL is `https://vkusvill.ru/…` before passing it on.
- `estimated_total` is approximate, because prices change. It is `null` when a price is missing.

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
| `GET /api/recipes?q=&cuisine=&course=` | — | `200 {"recipes":[Recipe…]}`, newest first. `q` matches the title or the body; `cuisine` / `course` are tag ids. |
| `GET /api/recipes/random` | — | `200` Recipe, or `404 not_found` when there are no recipes («🎲 Что приготовить?») |
| `POST /api/recipes` | [RecipeInput](#recipeinput) | `201` Recipe |
| `GET /api/recipes/{id}` | — | `200` Recipe |
| `PATCH /api/recipes/{id}` | partial RecipeInput. `null` clears `link`, `cuisine_id` and `nutrition`. | `200` Recipe |
| `DELETE /api/recipes/{id}` | — | `204` |
| `POST /api/recipes/{id}/images` | `multipart/form-data`, field `file` | `201` Image |
| `DELETE /api/recipes/{id}/images/{imageId}` | — | `204` |
| `GET /api/wishes/{id}/savings` | — | `200 {"savings":[Saving…]}`, newest first |
| `POST /api/wishes/{id}/savings` | `{"amount":{"amount":"5000","currency":"RUB"},"note":""}` | `201` Saving. A wish still in `want` moves to `progress`. |
| `DELETE /api/wishes/{id}/savings/{savingId}` | — | `204` |
| `GET /api/recipe-tags` | — | `200 {"tags":[RecipeTag…]}`, ordered by kind, then position |
| `POST /api/recipe-tags` | `{"kind":"cuisine","name":"Грузинская","emoji":"🍢"}` | `201` RecipeTag |
| `PATCH /api/recipe-tags/{id}` | `{"name"?, "emoji"?}` | `200` RecipeTag |
| `DELETE /api/recipe-tags/{id}` | — | `204`. Recipes lose the tag and stay. |
| `POST /api/recipes/{id}/cooks` | `{"stars":5,"comment":""}`, or `{}` / `{"stars":null}` to cook without rating | `201` Cook |
| `GET /api/recipes/{id}/cooks` | — | `200 {"cooks":[Cook…]}`, newest first |
| `PUT /api/recipes/{id}/cooks/{cookId}/rating` | `{"stars":4,"comment":""}` | `200` Cook, with the caller's rating set |
| `DELETE /api/recipes/{id}/cooks/{cookId}` | — | `204` |
| `POST /api/recipes/{id}/shopping` | `{"positions":[0,2]}` (indexes into `ingredients`), or `{}` for all | `201 {"items":[ShoppingItem…]}`: the added or merged items |
| `GET /api/shopping` | — | `200 {"items":[ShoppingItem…]}`: unchecked first, then checked, oldest first |
| `POST /api/shopping` | `{"items":[{"name":"Хлеб","amount":null,"unit":null}]}` | `201 {"items":[ShoppingItem…]}` |
| `PATCH /api/shopping/{id}` | `{"name"?, "amount"?, "unit"?, "checked"?}` | `200` ShoppingItem |
| `DELETE /api/shopping/{id}` | — | `204` |
| `POST /api/shopping/clear-checked` | — | `200 {"removed":3}` |
| `GET /api/stores` | — | `200 {"stores":[Store…]}` |
| `POST /api/shopping/vkusvill/match` | `{"item_ids":[…]}` or `{}` | `200 {"matches":[…]}`, see [ВкусВилл cart](#вкусвилл-cart-собрать-корзину-во-вкусвилле); `503 unavailable` |
| `POST /api/shopping/vkusvill/cart` | `{"lines":[{"xml_id":173,"quantity":"2"}]}` | `200 {"url":…,"estimated_total":…}`; `503 unavailable` |
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
{
  "title": "Паста карбонара",
  "link": null,
  "body": "1. Сварить пасту…",
  "cuisine_id": 3,
  "course_ids": [9, 11],
  "ingredients": [ { "name": "Спагетти", "amount": "320", "unit": "г" }, { "name": "Соль", "amount": null, "unit": "по вкусу" } ],
  "nutrition": { "kcal": "150", "protein": "12.5", "fat": "6", "carbs": "10.4", "weight_g": 800, "servings": 4 }
}
```

- Only `title` is required.
- The UI puts the link, the text, the ingredients and the КБЖУ behind switches, and sends `null` (or `[]`) when a switch is off.
- `nutrition` always holds **per-100 g** values. When the user enters values for the whole dish, the client divides by `weight_g / 100` before sending.
- `ingredients` and `course_ids` replace the stored lists as a whole.
- Screenshots are uploaded afterwards through the images endpoint.

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
              "images_per_recipe": 10, "ingredients_per_recipe": 50,
              "courses_per_recipe": 8, "shopping_items_max": 300,
              "item_name_max": 80, "rating_comment_max": 280,
              "saving_note_max": 140, "dish_weight_max_g": 20000, "servings_max": 50 },
  "units": ["г", "кг", "мл", "л", "шт", "ст. л.", "ч. л.", "стакан", "щепотка", "зубчик", "пучок", "упаковка", "по вкусу"]
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
  "recipes": 12,
  "recipes_cooked": 9,
  "saved": [ { "amount": "17000", "currency": "RUB", "formatted": "17 000 ₽" } ]
}
```

All three status keys are always present in `overall` and in each `by_status`.

### Edge cases (normative)

- **Formatted strings** (`formatted` in Money and Quantity) use a no-break space (U+00A0) between the number and the unit or currency. Display them as they are and never compare them as strings.
- **Nested `field` paths** carry indexes for JSON type errors, for example `ingredients.0.amount`, `course_ids.1`, `items.0.name` or `positions.0`. A line whose name or quantity is invalid is reported on the list field (`ingredients` or `items`), and the message names the line («Позиция №3: …»). `POST /api/shopping` takes at most 50 lines per request.
- **Limits.** At most 30 cuisines and 30 courses, 200 contributions per wish and 500 cookings per recipe; beyond that the API answers `422 limit`.
- **Quantities.**
  - A `unit` without an `amount` is rejected. The only exception is `"по вкусу"`, which never has an amount.
  - In `PATCH /api/shopping/{id}`, `amount` and `unit` are sent **together**. Either may be `null`, but sending only one of them is a 400.
- **Recipe → shopping.** `{}` (or `"positions": null`) adds every ingredient. `"positions": []` is a 400. Repeated indexes count once. There are at most 50 positions, each in 0–49.
- **Cooking.**
  - `POST …/cooks` with `{}` or `"stars": null` records a cooking without a rating.
  - A `comment` without `stars` is a 400 on `stars`.
  - Either partner may delete a cooking or a saving.
- **Recipe input.** In `nutrition`, `weight_g` and `servings` may be `null` (unknown). `null` on `course_ids` or `ingredients` means an empty list. `null` on `title` is a 400.
- **Savings.**
  - A wish that has come true (`done`) takes no savings: 400 on field `status`.
  - Errors use the fields `amount`, `currency` and `note`.
  - Without `currency` the wish's savings currency is used, falling back to `DEFAULT_CURRENCY`.
  - `saved.count` is filled in single-wish responses and is `null` in lists, which keeps lists at one query.
- **Tags.** `GET /api/recipe-tags` lists cuisines first, then courses, each by `position`. Tag names follow the category rules: at most `limits.category_name_max` characters, and case-insensitively unique within a kind.
- **ВкусВилл.** A candidate's `price`, `unit` and `weight` may be `null`, so show `—`. The client builds cart lines only from candidates the match returned.

## Media URLs

Image URLs are signed with `HMAC_SHA256(key = HMAC_SHA256("dreamer/media/v1", BOT_TOKEN), msg = "{imageId}/{variant}/{exp}")`
and returned inside Wish objects. `exp` is rounded up to the next hour, and each
URL stays valid for at least 12 hours. Because of the rounding, repeated list loads
return identical URLs, so the browser cache keeps working. Responses carry
`Cache-Control: private, max-age=…` up to `exp`, plus `X-Content-Type-Options: nosniff`.

## Deep links

Bot messages open the app on a specific item with `WEBAPP_URL?wish={id}` or
`WEBAPP_URL?recipe={id}`, and on the shopping list with `WEBAPP_URL?shopping=1`.
The URL hash is reserved for Telegram's launch parameters, so it is never used
for routing.
