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
`limit` (422), `not_a_recipe` (422, [import](#recipe-import) found no recipe), `rate_limited` (429), `internal` (500),
`unavailable` (503, an external service the server called for you did not answer). `message` is in Russian and
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
    { "name": "Спагетти", "amount": "320", "unit": "г", "unit_label": "г", "formatted": "320 г" },
    { "name": "Соль", "amount": null, "unit": "по вкусу", "unit_label": "по вкусу", "formatted": "по вкусу" },
    { "name": "Яйца", "amount": "4", "unit": "шт", "unit_label": "шт", "formatted": "4 шт" },
    { "name": "Сливки", "amount": "0.5", "unit": "стакан", "unit_label": "стакана", "formatted": "½ стакана" },
    { "name": "Гуанчале", "amount": "100", "unit": "г", "unit_label": "г", "formatted": "100 г" }
  ],
  "servings": 4,
  "nutrition": {
    "per_100g":    { "kcal": "150", "protein": "12.5", "fat": "6", "carbs": "10.4" },
    "weight_g": 800,
    "servings": 4,
    "per_dish":    { "kcal": "1200", "protein": "100", "fat": "48", "carbs": "83.2" },
    "per_serving": { "kcal": "300", "protein": "25", "fat": "12", "carbs": "20.8" }
  },
  "nutrition_auto": {
    "per_100g":    { "kcal": "266.7", "protein": "11.3", "fat": "7.1", "carbs": "38.2" },
    "weight_g": 640,
    "per_dish":    { "kcal": "1706.8", "protein": "72.1", "fat": "45.7", "carbs": "244.3" },
    "per_serving": { "kcal": "426.7", "protein": "18", "fat": "11.4", "carbs": "61.1" },
    "coverage": { "counted": 3, "total": 4, "missing": ["Гуанчале"], "no_amount": [], "skipped": ["Соль"] },
    "items": [
      { "name": "Спагетти", "food": "Макароны", "grams": 320, "kcal": "1187.2", "protein": "41.6", "fat": "4.8", "carbs": "239" },
      { "name": "Яйца", "food": "Яйцо куриное", "grams": 220, "kcal": "314.6", "protein": "27.7", "fat": "20.9", "carbs": "1.5" },
      { "name": "Сливки", "food": "Сливки 20%", "grams": 100, "kcal": "205", "protein": "2.8", "fat": "20", "carbs": "3.7" }
    ]
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
  - `link`, `cuisine_id`, `servings` and `nutrition` may be `null`.
  - `body` may be `""`, and `course_ids` and `ingredients` may be `[]`.
  - The body is plain text with newlines. Clients must render it as text (`white-space: pre-wrap`), never as HTML.
- **Servings.** `servings` (1–50, or `null` when unknown) is how many portions the recipe yields. The ingredient amounts and the КБЖУ are for the whole recipe; the Mini App rescales them for another number of portions on the client, and nothing is stored.
- **Nutrition numbers** are decimal strings with at most one decimal place. All four (`kcal`, `protein`, `fat`, `carbs`) are required when `nutrition` is sent — a blank value is unknown, not zero, so write `"0"` for a real zero. `weight_g` may be `null`.
  - `nutrition.servings` always equals the recipe's `servings`; it stays for older clients.
  - `per_dish` is `null` without `weight_g`.
  - `per_serving` is `null` without both `weight_g` and `servings`.
  - The server computes both and rounds to 0.1.
- **Estimated КБЖУ.** `nutrition_auto` is computed on every read from the ingredients and a built-in food table ([`internal/nutrition`](../internal/nutrition)); nothing is stored. It is `null` when no ingredient could be counted, and it is returned even when `nutrition` is set — the manual values win in the UI.
  - `per_dish` is the sum over the counted ingredients, `weight_g` their weight, `per_100g` per 100 g of that weight, and `per_serving` is `per_dish` divided by the recipe's `servings` (`null` without them). All are for the stored servings; the Mini App's servings scaler multiplies `per_dish` itself.
  - `coverage`: `total` counts every ingredient except the `skipped` ones («по вкусу»), so `counted` + `missing` + `no_amount` = `total`. `missing` names the ingredients that match no food; `no_amount` those without an amount or with an amount in a unit that has no known weight for that food («2 шт» of flour). The names are as written in the recipe.
  - `items` lists each counted ingredient, in recipe order, with the food it matched (`food`), its weight in grams and its КБЖУ as decimal strings with at most one decimal.
  - Spoons and cups are counted level; values are estimates, so clients show them with «≈».
- **Cooking.** `cooking.last_cooked_at` and `cooking.rating_avg` are `null` until the first cooking or rating. `rating_avg` is the average of all ratings over all cookings, with one decimal.
- **Ingredient amounts** follow [Quantity](#quantity): `amount`, `unit`, `unit_label` and `formatted`.

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
{ "id": 31, "name": "Молоко", "quantity": { "amount": "750", "unit": "мл", "unit_label": "мл", "formatted": "750 мл" },
  "checked": false, "recipe_id": 4, "added_by": { "id": 111, "name": "Дима" },
  "created_at": "…", "updated_at": "…" }
```

`quantity` (a [Quantity](#quantity)) and `recipe_id` may be `null`. Adding an item whose name (case-insensitive) matches an **unchecked** item with the same unit, or the related one (г↔кг, мл↔л), merges the amounts into that item: 500 мл + 250 мл gives 750 мл, and 200 мл + ½ л gives 700 мл. A mixed sum is shown in the larger unit when it is at least 1 and has at most two decimals there (500 г + 1 кг = 1½ кг), otherwise in the smaller one (333 г + 1 кг = 1333 г). Other units never merge.

The shopping list is a plain shared checklist. The server does not search any store and never sends the list anywhere.

### Quantity

An ingredient line (`ingredients[]`) and a shopping item (`quantity`) carry the same four fields:

```json
{ "amount": "1.5", "unit": "ч. л.", "unit_label": "чайной ложки", "formatted": "1½ чайной ложки" }
```

- **`amount`** is the machine value: a decimal string with at most two decimals (`"0.5"`, `"250"`, `"0.33"`), or `null` when there is none (always for `"по вкусу"`). It is stored in hundredths, so thirds are kept as 0.33 and 0.67.
- **`unit`** is a code from `Me.units` (`г`, `кг`, `мл`, `л`, `шт`, `ст. л.`, `ч. л.`, `стакан`, `щепотка`, `зубчик`, `пучок`, `упаковка`, `по вкусу`), or `null` for a bare number.
- **`unit_label`** is the unit's word declined for the amount, or `null` without a unit. Spoons, cups, pinches, cloves, bunches and packs change with the number: «1 чайная ложка», «2 чайные ложки», «5 чайных ложек», «21 чайная ложка», and every amount that is not whole takes the genitive singular: «½ чайной ложки», «1½ стакана». The abbreviations `г`, `кг`, `мл`, `л`, `шт` and `"по вкусу"` never change. `Me.unit_forms` lists every form.
  - The rule for a whole number n: n ends in 1 but not in 11 → `one`; n ends in 2–4 but not in 12–14 → `few`; otherwise `many`. Any other amount → `fraction`.
- **`formatted`** is the amount and the label for display, joined by a no-break space. Every unit except `г` and `мл`, and a bare number, shows .5, .25, .75, .33 and .67 as ½, ¼, ¾, ⅓ and ⅔ glued to the whole part («½ кг», «1¼ кг», «2⅔»); any other fraction, and every amount in `г` or `мл`, uses a decimal comma («0,3 л», «1,5 г»).
- The test vectors shared by the server and the Mini App are in [`internal/domain/testdata/units.json`](../internal/domain/testdata/units.json) (formatting) and [`amounts.json`](../internal/domain/testdata/amounts.json) (parsing).

**Input.** Clients send `amount` and `unit` as strings:

- `amount` accepts a decimal with a comma or a dot (`"0,5"`, `"1.5"`), a fraction (`"1/2"`, `"3/4"`), a mixed number (`"1 1/2"`) and the characters ½ ⅓ ⅔ ¼ ¾ ⅕, alone or after a whole part (`"½"`, `"1½"`). Fractions may use the denominators 2, 3, 4, 5 and 10. Anything else, such as `"1/8"`, is a 400 with «Укажите дробь вида 1/2, 1/3, 1/4 или десятичную, например 0,5». The amount must be above 0 and at most 100 000.
- `unit` accepts the code and also its forms and common spellings in any letter case: «чайные ложки», «ч.л.», «чл», «ст. ложка», «гр», «граммов», «шт.», «штук», «зуб», «уп» and so on. The server stores and returns the code.

## Endpoints

Removed in iteration 3 together with the store integration: `GET /api/stores`, `POST /api/shopping/vkusvill/match` and `POST /api/shopping/vkusvill/cart` (now 404).

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
| `POST /api/recipes/import` | `{"url":"…"}` or `{"text":"…"}` | `201 {"recipe":Recipe,"import":ImportReport}`, or `200` with the existing recipe when the post was imported before. See [Recipe import](#recipe-import). |
| `GET /api/recipes/{id}` | — | `200` Recipe |
| `PATCH /api/recipes/{id}` | partial RecipeInput. `null` clears `link`, `cuisine_id`, `servings` and `nutrition`. | `200` Recipe |
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
  "ingredients": [ { "name": "Спагетти", "amount": "320", "unit": "г" }, { "name": "Сахар", "amount": "1 1/2", "unit": "ч. л." },
                   { "name": "Соль", "amount": null, "unit": "по вкусу" } ],
  "servings": 4,
  "nutrition": { "kcal": "150", "protein": "12.5", "fat": "6", "carbs": "10.4", "weight_g": 800 }
}
```

- Only `title` is required.
- `servings` is 1–50 or `null`. Older clients send the servings as `nutrition.servings` instead: when the top-level `servings` is absent, a non-null `nutrition.servings` sets the recipe's servings. When both are sent, the top-level value wins. A `nutrition` without `servings` keeps the recipe's servings.
- The UI puts the link, the text, the ingredients and the КБЖУ behind switches, and sends `null` (or `[]`) when a switch is off.
- `nutrition` always holds **per-100 g** values. When the user enters values for the whole dish, the client divides by `weight_g / 100` before sending.
- `ingredients` and `course_ids` replace the stored lists as a whole.
- Screenshots are uploaded afterwards through the images endpoint.

### Recipe import

`POST /api/recipes/import` makes a recipe from an Instagram post or reel, or from pasted text, and saves it right away: the client opens it in the edit form for a check. The partner's notice waits until the recipe has stayed unchanged for three minutes (deleting it cancels the notice).

```json
{ "url": "https://www.instagram.com/reel/DItfAhKCJ3h/?igsh=…" }
{ "text": "Маринад для шашлыка\nЛук — 3 шт\n…" }
```

- Exactly one of `url` (at most 2048 bytes) and `text` (at most 10 000 characters) is sent. `url` may be any form of a post, reel or IGTV link (`instagram.com`, `www.`, `m.`, `/p/`, `/reel/`, `/reels/`, `/tv/`, tracking parameters, text around it); the server rebuilds the canonical `https://www.instagram.com/{p|reel|tv}/{code}/` and fetches only that and Instagram's CDN. The recipe's `link` is the canonical URL.
- The caption is parsed by rules. With a model key configured (`LLM_API_KEY`), captions the rules are unsure about go to the model; with `LLM_PROVIDER=gemini`, a reel whose caption has no usable recipe or no steps is read from its video (speech and on-screen text). The post's cover becomes the recipe's first photo, re-encoded like an upload.
- **Duplicates.** When a recipe already links to the same post (imported, or typed in by hand in any form), nothing is fetched or created: the answer is `200` with that recipe and `"duplicate": true`. Two imports of one post at the same time give one recipe.
- **Time.** Most imports take a few seconds; the video path up to ~90 s. The server gives one import at most 120 s, so clients should wait about 130 s for the answer.
- **Rate limit.** Besides the general limit, imports share the per-user limit for calls to external services: a burst of 6, then one every two seconds (`429 rate_limited`). On top of that, imports from the Mini App and the bot together allow each user a burst of 5, then one every 20 seconds (`429 rate_limited`, «Слишком много импортов подряд — подождите минуту.»), and at most 2 imports run at once on the server; a duplicate of an imported post costs nothing. Each user can have 20 videos read per day; after that the import reads only the caption and says so in `warnings`.

```json
{
  "recipe": { "id": 41, "title": "Маринад для шашлыка", "link": "https://www.instagram.com/reel/DItfAhKCJ3h/", "…": "a Recipe" },
  "import": {
    "source": "instagram",
    "parser": "rules",
    "confidence": 0.95,
    "image": true,
    "warnings": ["1 строка не распознана"],
    "duplicate": false
  }
}
```

- `source`: `instagram` or `text`. `parser`: `rules`, `llm` (the caption read by the model) or `video` (the video read by the model). `confidence`: the parser's score, 0–1. `image`: the recipe got the post's cover. `warnings`: short Russian notes for the user (lines that were not read, a range shortened to its lower bound, the cover or the video that failed, the daily video limit). For a duplicate, `parser` is `""`, `confidence` 0 and `warnings` empty.

| Failure | Status | `error.code` | `message` |
|---|---|---|---|
| Neither or both of `url` and `text`, too long, not a post link | 400 | `validation` (field `url` or `text`) | e.g. «Это не ссылка на пост или рилс в Instagram» |
| No recipe in the caption or the text | 422 | `not_a_recipe` | «Не нашли в тексте рецепт — вставьте текст с ингредиентами» |
| A reel with no recipe in its caption and no model that reads videos | 422 | `not_a_recipe` | «Рецепт, похоже, только в видео — вставьте текст рецепта или подписи» (the server log names the setting that enables video: `LLM_PROVIDER=gemini`) |
| Instagram did not give the post (refused, removed, private, timed out) | 503 | `unavailable` | «Instagram не отдал пост. Скопируйте текст подписи и вставьте его сюда.» |

On `503` the client offers the text field; a model that fails or times out is not an error (the rules' result is kept, with a warning).

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
  "units": ["г", "кг", "мл", "л", "шт", "ст. л.", "ч. л.", "стакан", "щепотка", "зубчик", "пучок", "упаковка", "по вкусу"],
  "unit_forms": {
    "ч. л.":  { "one": "чайная ложка", "few": "чайные ложки", "many": "чайных ложек", "fraction": "чайной ложки" },
    "стакан": { "one": "стакан", "few": "стакана", "many": "стаканов", "fraction": "стакана" },
    "г":      { "one": "г", "few": "г", "many": "г", "fraction": "г" },
    "…": "every code in units"
  }
}
```

`units` lists the unit codes in picker order. `unit_forms` has an entry for every code with the words a [Quantity](#quantity) uses: `one` («1 чайная ложка»), `few` («2 чайные ложки»), `many` («5 чайных ложек») and `fraction` («½ чайной ложки»). Invariant units repeat their code in all four.

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
  - See [Quantity](#quantity) for the accepted amounts and unit spellings.
  - A `unit` without an `amount` is rejected. The only exception is `"по вкусу"`, which never has an amount.
  - In `PATCH /api/shopping/{id}`, `amount` and `unit` are sent **together**. Either may be `null`, but sending only one of them is a 400.
- **Recipe → shopping.** `{}` (or `"positions": null`) adds every ingredient. `"positions": []` is a 400. Repeated indexes count once. There are at most 50 positions, each in 0–49.
- **Cooking.**
  - `POST …/cooks` with `{}` or `"stars": null` records a cooking without a rating.
  - A `comment` without `stars` is a 400 on `stars`.
  - Either partner may delete a cooking or a saving.
- **Recipe input.** `servings` and, in `nutrition`, `weight_g` and `servings` may be `null` (unknown). `null` on `course_ids` or `ingredients` means an empty list. `null` on `title` is a 400. `servings` outside 1–50 is a 400 on `servings`.
- **Savings.**
  - A wish that has come true (`done`) takes no savings: 400 on field `status`.
  - Errors use the fields `amount`, `currency` and `note`.
  - Without `currency` the wish's savings currency is used, falling back to `DEFAULT_CURRENCY`.
  - `saved.count` is filled in single-wish responses and is `null` in lists, which keeps lists at one query.
- **Tags.** `GET /api/recipe-tags` lists cuisines first, then courses, each by `position`. Tag names follow the category rules: at most `limits.category_name_max` characters, and case-insensitively unique within a kind.

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
