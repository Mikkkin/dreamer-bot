# Instagram caption fixtures

Golden test data for the rule-based recipe parser in `internal/recipeimport`. Each fixture is one real, public, Russian-language Instagram post caption plus the parse we expect from it.

There are 35 fixtures:

- 31 recipe captions. 30 have an ingredient list; in 1 the ingredients exist only inside the steps.
- 2 recipe posts whose caption holds no recipe text.
- 2 negative (non-recipe) posts.

## Files

For every fixture `NN-slug`:

- `NN-slug.txt` is the caption exactly as Instagram publishes it in the post's `og:description`. It is UTF-8 with LF newlines and no trailing newline. Invisible characters are preserved: TABs, U+2800 blank lines, U+200C, emoji ZWJ sequences.
- `NN-slug.json` is the expected parse:

```json
{
  "source_url": "https://www.instagram.com/reel/<shortcode>/",
  "title": "Салат коул-слоу",
  "servings": 4,
  "ingredients": [
    {"raw": "1 ч.л. соли (без горки)", "name": "Соль", "amount": "1", "unit": "ч. л.", "section": null}
  ],
  "steps": ["…"],
  "notes": "what is hard or ambiguous here"
}
```

`title` and `servings` may be `null`. `amount` is `null` or a decimal string. `unit` is `null` or one of `г кг мл л шт ст. л. ч. л. стакан щепотка зубчик пучок упаковка по вкусу`. `section` may be `null`.

## How the fixtures were collected (2026-10-03)

1. **Discovery.** Ran 8 Tavily web searches restricted to `instagram.com` with Russian recipe-shaped queries, for example "рецепт ингредиенты приготовление ст. л.", "рецепт ч.л. щепотка по вкусу", "рецепт на 4 порции", "рецепт ½ стакана", "суп рецепт", "выпечка рецепт г муки Начинка:", "рецепт Для соуса: 1/2 ч.л. 1️⃣". `/popular/` aggregate pages were skipped. Candidates were picked from the result snippets to cover as many formatting patterns as possible. Six posts were already known to work.
2. **Fetch.** Sent one `GET https://www.instagram.com/{p|reel}/{shortcode}/` per post with `User-Agent: facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)` and `Accept-Language: ru`, the same request a link-preview bot makes. The script read `<meta property="og:description">`, decoded HTML entities, and stripped the wrapper `N likes, M comments - user Date: "…".` to leave the caption. There were 39 requests to instagram.com in total, sent sequentially 1.5 s apart.
3. **Selection.** 37 captions came back. Three were dropped. `CyIPYFRNpTl` returned no `og:description` on two attempts. `DXHVW-SCpBU` ("Сохраняй рецепт ❤️") and `DaGOqT-Khxk` (a one-line teaser) duplicate the pattern already covered by fixtures 32–33.
4. **Expected parses.** Written by hand following the conventions below. A generator script then checked them mechanically:
   - every `raw` must equal a caption line after trimming;
   - every step must occur in the caption once punctuation and whitespace are ignored;
   - units must come from the vocabulary;
   - amounts must match `^\d+(\.\d{1,2})?$`;
   - names must be capitalised and contain no emoji or parentheses.

`og:image` (the cover picture) is available from the same response, but it is not stored. The CDN URLs are signed and expire.

**Use of the data.** The captions are public post texts that belong to their authors. They are kept here only as parser test data. They are not shipped in the binary, not shown to users, and not used for anything else. Each `.json` records the source URL. No images, comments or profile data are stored. If an author objects, delete that fixture's two files.

## Expected-parse conventions

1. **Amount.** A decimal string with a dot and at most 2 decimals: `½`→`"0.5"`, `⅓`/`1/3`→`"0.33"`, `¼`→`"0.25"`, `¾`→`"0.75"`, `1/5`→`"0.2"`, `1½`→`"1.5"`, `0,5`→`"0.5"`. For a range (`1-2`, `3–4`, `½–1½`) take the lower bound and say so in `notes`. A leading `~` is dropped.
2. **Unit normalisation.**

   | Written | Unit |
   |---|---|
   | `гр`, `гр.`, `грамм`, `г.`, `200г` | `г` |
   | `мл.` | `мл` |
   | `л.` | `л` |
   | `ст. л`, `ст.л.`, `ст л`, `ст/л`, `ст.ложки`, `столовая ложка`, `столовые ложки` | `ст. л.` |
   | `ч. л`, `ч.л`, `ч л`, `2ч.л.`, `чайной ложки` | `ч. л.` |
   | `шт.` | `шт` |
   | `зуб.`, `зубчика`, `зубчиков` | `зубчик` |
   | `пучка`, `пучков` | `пучок` |
   | `стакана` | `стакан` |
   | `по-вкусу` | `по вкусу` |

3. **Bare counts** of countable foods (`2 яйца`, `1 банан`, `1 средняя морковь`, `две небольшие моркови`) get `шт`. A head of onion (`головка`, `луковица`) is one onion, so it also gets `шт`.
4. **Measure words outside the vocabulary** (`кочан`, `головка`/`луковица` of garlic, `горсть`) give `unit: null`. The amount is kept when a number is written (`1 горсть базилика` → `"1"`) and is `null` otherwise (`Шпинат - горсть`). `raw` keeps the original wording.
5. **A unit word without a number** (`Щепотка соли`, `Соль — щепотка`) has amount `"1"`.
6. **`по вкусу`** gives `unit: "по вкусу"` and `amount: null`. `по желанию`, `для подачи`, `для украшения` and lines with no quantity give `amount: null` and `unit: null`.
7. **Name** is in the nominative with a capitalised first letter. Bullets, numbering and emoji are removed, and so are all parentheticals. Also dropped: size words (`средний`, `крупный`), qualifiers (`любой`), and trailing prep or commentary (`, нарезанный на кусочки`, `обжарить.`, `из холодильника`). Product-type adjectives and fat % stay (`Отварная курица`, `Сливки 20%`, `Уксус 9%`). Alternatives joined by `или`/`либо` stay in the name, and the КБЖУ lookup should use the first one. Count-form nouns become the product name (`картофелин`/`картошки` → `Картофель`, `луковица` → `Лук`). Otherwise the author's spelling is kept. The exception is the broken `и`+U+200C, which becomes `й`.
8. **One line can hold several ingredients.** Split on `,`, `и` or `/` outside parentheses when each part is a separate food. A shared amount (`перец, паприка — по 1/2 ч.л.`, `Помидор, огурец - по 15-20 грамм`, `Щепотка соли/перца`, `Соль и перец по вкусу`) is copied to each part, and each part repeats the same `raw`. A slash between synonyms (`Кунжутная паста/тахини`) and comma-joined alternatives (`содовая,тоник`) stay one ingredient.
9. **`raw`** is the original caption line with only its leading and trailing whitespace trimmed. Bullets, emoji, TABs and zero-width characters inside the line are byte-identical to the `.txt`.
10. **`section`** is the sub-header text without its colon or emoji and without an `Ингредиенты (…)` wrapper, first letter capitalised (`Начинка`, `Для соуса`, `Для курочки`, `Крем`). The plain `Ингредиенты:` list header is not a section. A section ends at the next header or at a separator line (the single `.` in fixture 26). Inline label lines (`Специи: соль, паприка, …`, `Добавки: …`) carry their own section.
11. **Steps** are the cooking instructions in caption order. Numbering (`1.`, `1)`, `1️⃣`), bullets (`✔️`, `-`, `🔹`) and edge emoji are removed. Continuation lines are joined with a space and whitespace runs are collapsed. Case and punctuation are kept. Sub-bullets of a numbered step are merged into it. Instructions interleaved with ingredient blocks are steps in caption order (fixture 12). Lines that are both an instruction and an ingredient appear in both lists (fixture 31). Excluded: tips, serving suggestions, variations, CTA, ads, hashtags, `Приятного аппетита`, and placeholders such as `Приготовление на видео`.
12. **Title** is the dish name. Take the first meaningful line after any leading CTA lines. Cut it at the first emoji, at ` Рецепт⤵️`, or at `, которая …`. Strip a leading `Рецепт`, a trailing parenthetical, colon or dot, and surrounding guillemets around a quoted name (`Рецепт самого вкусного коктейля «Апероль Шприц»` → `Апероль Шприц`). Convert ALL CAPS to sentence case. When the header never names the dish (only `Рецепт ⤵️`, only `Ингредиенты`, or a long prose opener), the title is `null`. The importer then falls back to manual entry or the optional LLM.
13. **Servings** is an integer taken only from `порци-` phrases (`на 4 порции`, `(2 порции)`, `Выход :3-4 порции`, `одна порция`). Take the lower bound of a range. A yield in pieces (`(12 шт.)`) or in meat weight (`на 3,3 кг свиной шеи`) gives `null` and a note.
14. **Notes** are free text. Two machine-readable prefixes exist. `NEGATIVE` marks a post that is not a recipe and must not be imported. `NO_RECIPE_TEXT` marks a recipe post whose caption has nothing to import. Both have empty `ingredients` and `steps`.

## Fixture index

| # | File stem | Source | What it exercises |
|---|---|---|---|
| 01 | `marinad-dlya-shashlyka` | `DItfAhKCJ3h` | `- Name — amount` dash list, tips in parentheses, a `-` instruction line after the numbered steps |
| 02 | `yagodnyj-chizkejk` | `DVs8ssPihOG` | clean baseline: 3 sections, `•`, keycap steps, a step continuation line |
| 03 | `morkovka-po-korejski` | `DCjGOmwo84T` | same 🥕 bullet glued to every name, `ст. л` without the final dot, no steps |
| 04 | `cvetnaya-kapusta-v-panirovke` | `DEutP15K8bi` | amount first with a genitive name, `1½`, `½–1½`, `кочан`, `Рецепт:` used as the steps header |
| 05 | `shokoladnyj-keks-ru-el` | `DVc5aleDL04` | title in the second sentence, the whole recipe repeated in Greek |
| 06 | `salat-iz-botvy-rediski` | `DWib3IKjSLr` | whole list on one comma line, shared `по 1/2 ч.л.`, alternative amount inside parentheses |
| 07 | `salat-iz-rediski` | `DZQd20xMAwg` | `ст л`/`ч л` without dots, `200г`, `чеснок головка`, a hashtag line with no leading `#` |
| 08 | `salat-koul-slou` | `DDel-pfoRpF` | pure amount-first list with genitive names |
| 09 | `salat-iz-zelenoj-redki` | `DVD3UGgjDf2` | TAB•TAB bullets, trailing emoji, a note line inside the list, an ad |
| 10 | `keksy-na-kefire` | `DV81PzBDa9c` | CTA before the title, a different emoji per line, `¼`, yield `(12 шт.)` |
| 11 | `tri-recepta-v-odnom-poste` | `C9PFLKoN2wm` | three recipes in one caption, `Ингридиенты`, `Белки-6 шт`, U+200C |
| 12 | `tertyj-pirog-bez-nazvaniya` | `Dcf6mO-tTtp` | steps interleaved with ingredients, `X и Y` as two ingredients on one line, no title |
| 13 | `morkovnoe-pyure-so-speciyami` | `DUTaSAsjR-Q` | no header and no title, `1/5`, ranges, `~`, sub-bullets inside a step |
| 14 | `syrniki-iz-rikotty` | `DdyYHheIQxz` | a parenthetical in the middle of a name, unit first in `щепотка соли` |
| 15 | `kofe-s-medom-i-koricej` | `DZu7T5yIWKA` | brand chit-chat, `;`-terminated list, `⅓ стакана` |
| 16 | `kurica-s-kartofelem-v-duhovke` | `DTKUYF4CBwt` | number words, spelled-out spoons, `~`, prose steps, `на 3-4 порции` |
| 17 | `holodnyj-sup` | `DNyAz9v2FAO` | NUMBERED ingredients (with a duplicate `9.`), commentary after the amount |
| 18 | `pasta-s-zapechennymi-ovoshchami` | `DEk1fnstxQI` | servings and kcal stated, sections named by content, `Добавки:` inline label, `Щепотка соли/перца` |
| 19 | `salat-s-kuricej-i-ogurchikom` | `DdoDhoLArvS` | emoji per line, prep instructions after the amount |
| 20 | `blinchiki-tri-stakana` | `DVBVpfQCGZk` | `·` bullets, `стакан`, `⅓`/`½` + `чайной ложки` |
| 21 | `sup-s-frikadelkami-bez-kolichestv` | `DSVHf5SDViH` | almost no quantities, `Можно грибы добавить` |
| 22 | `solnechnyj-sup-s-frikadelkami` | `DM8A2r3I_he` | `1.5 л.`, amount hidden in `(у меня 800 гр.)`, `Соль/перец/зелень`, `ст/л` |
| 23 | `pp-salat-na-uzhin` | `C-lESd7IwiM` | kcal numbers inside ingredient lines, `Сметана 10% 30 гр.`, `одна порция` |
| 24 | `zapechennyj-cukini` | `C_DwG_wNK0-` | no headers at all, U+2800 block separators, prose steps |
| 25 | `pp-shaurma` | `DHbfDyzoYU5` | sections inside parentheses, `по 15-20 грамм` shared range, КБЖУ stated |
| 26 | `kurinye-bedra-v-soevom-souse` | `Bb09QqEDYER` | `Что нужно:`, `.,` line endings, a `.` line that ends the section |
| 27 | `bulgur-s-kuricej` | `DOpov-mAGHs` | `(2 порции)`, `горсть`, `для подачи`, `Специи: a, b, c`, an ad with a phone number |
| 28 | `omlet-s-dzhusaem` | `DWl4MT4AnbK` | colon separator, servings after the list |
| 29 | `pirog-s-vishnej` | `DUqXnNgiP9d` | `Для теста`/`Начинка`, `Сахар` twice, an oven line that is a step |
| 30 | `gata` | `DXUhx8qDTKI` | emoji-prefixed sections, filling flour mentioned only in prose |
| 31 | `aperol-shpric` | `DGbPVRNOrrJ` | no ingredient list, steps that double as ingredients, the ratio `3:2:1` |
| 32 | `salat-anons-bez-recepta` | `DaUyiY4owc0` | `NO_RECIPE_TEXT`: a prose TV teaser |
| 33 | `salat-iz-redki-tolko-heshtegi` | `DVZbhznjD1H` | `NO_RECIPE_TEXT`: a title glued to `##hashtags` |
| 34 | `negative-igra-gryaz` | `Daz8W5TsNOg` | `NEGATIVE`: a non-food `Рецепт игры` with valid food amounts, plus an ad |
| 35 | `negative-reklama-sbornika` | `DdrKDifIDhK` | `NEGATIVE`: an e-book ad whose bullet list looks like ingredients |

## Taxonomy of observed formatting patterns

Counts were computed from the fixture files. "Ingredient lines" counts the distinct `raw` lines (264) behind the 287 expected ingredients. "Fixtures" counts the files with at least one such line. Patterns overlap, so the columns do not add up.

### Bullet / line prefix

| Pattern | Ingredient lines | Fixtures |
|---|---:|---:|
| no bullet | 128 | 15 |
| emoji bullet (any pictograph first) | 54 | 6 |
| `-` dash (with or without space) | 34 | 5 |
| `•` bullet (incl. TAB•TAB) | 30 | 4 |
| `N.` numbered ingredient | 11 | 1 |
| `·` middle dot | 7 | 1 |

### Order and separator

| Pattern | Ingredient lines | Fixtures |
|---|---:|---:|
| amount first (`200 г муки`, `2 яйца`) | 83 | 11 |
| `Название - 200 г` spaced hyphen | 44 | 7 |
| `Название — 200 г` em dash | 32 | 5 |
| `Название 200 г` no separator | 26 | 7 |
| `Название – 200 г` en dash | 23 | 2 |
| `Название-200 г` hyphen, no spaces | 12 | 2 |
| `название :4 шт` colon | 6 | 1 |

### Units as written

| Pattern | Ingredient lines | Fixtures |
|---|---:|---:|
| `гр` / `гр.` | 30 | 10 |
| `г` (incl. glued `200г`) | 29 | 9 |
| `ч.л.` / `ч.л` (compact, incl. `2ч.л.`) | 29 | 12 |
| no quantity at all | 22 | 15 |
| `шт` / `шт.` | 21 | 10 |
| `по вкусу` / `по-вкусу` | 19 | 13 |
| `мл` / `мл.` | 17 | 13 |
| `ч. л.` / `ч. л` (spaced) | 16 | 8 |
| `ст. л.` / `ст. л` (spaced) | 11 | 5 |
| `стакан/стакана/стаканов` | 8 | 3 |
| `щепотка/щепотки` (incl. unit-first `Щепотка соли`) | 8 | 7 |
| `ст.л.` / `ст.л` (compact) | 7 | 5 |
| `ч л` / `ч л.` (no dot after ч) | 7 | 4 |
| `зубчик/зубчика/зубчиков/зуб.` | 7 | 7 |
| out-of-vocabulary measure (`головка`, `кочан`, `луковица чеснока`, `горсть`) | 7 | 6 |
| `грамм` | 5 | 1 |
| `ст л` (no dots) | 4 | 2 |
| `пучок/пучка/пучков` | 4 | 4 |
| `по желанию` / `можно … добавить` (optional) | 4 | 4 |
| `столовая/столовые ложка(и)` | 3 | 2 |
| `чайной ложки` | 2 | 2 |
| `кг` | 1 | 1 |
| `л` / `л.` (litre) | 1 | 1 |
| `ст/л` | 1 | 1 |
| `ст.ложки` | 1 | 1 |

### Numbers

| Pattern | Ingredient lines | Fixtures |
|---|---:|---:|
| integer only | 184 | 30 |
| range `1-2`, `3–4`, `½–1½` | 17 | 11 |
| ASCII fraction `1/2`, `1/3`, `1/5` | 13 | 6 |
| decimal comma `0,5` | 12 | 4 |
| Unicode fraction `½ ⅓ ¼` | 7 | 4 |
| approximate `~800 мл` | 7 | 4 |
| mixed number `1½` | 3 | 1 |
| number word (`две`, `одна`) | 2 | 1 |
| shared amount `по 1/2 ч.л.` / `по 15-20 грамм` | 2 | 2 |
| decimal dot `1.5` | 1 | 1 |

### Line shape

| Pattern | Ingredient lines | Fixtures |
|---|---:|---:|
| parenthetical (tip / qualifier / alt amount) | 28 | 18 |
| trailing list punctuation `,` `;` `.,` | 14 | 2 |
| one raw line → several ingredients | 13 | 10 |
| TAB inside | 9 | 1 |
| trailing prep instruction or commentary after the amount | 7 | 4 |
| decorative emoji at the END of the name | 7 | 1 |
| percent in the name (`Сливки 20%`, `уксус 9%`) | 5 | 5 |
| zero-width char inside (U+200B–U+200D) | 2 | 1 |

### Fixture-level structure (35 fixtures; 31 with a recipe in the caption)

| Feature | Fixtures |
|---|---:|
| has an `Ингредиенты`-style header (`Ингредиенты`, `ИНГРЕДИЕНТЫ`, `Ингридиенты`, `Что нужно`) | 25 |
| header misspelled `Ингридиенты` | 1 |
| has sections (`Начинка:`, `Для соуса:`, `Крем:` …) | 9 |
| servings stated (`на 4 порции`, `(2 порции)`, `Выход :3-4 порции`, `одна порция`) | 6 |
| yield in pieces instead of servings (`(12 шт.)`) | 1 |
| calories stated in caption (`612 ккал`, `КБЖУ … Б41/Ж6/У44`) | 3 |
| steps present in caption | 21 |
| steps absent — 'на видео' / video only (recipes only) | 10 |
| title null (dish never named in the header) | 13 |
| hashtags | 16 |
| CTA (`сохрани`, `подпишись`, `ссылка в профиле`, `поставь смайлик` …) | 12 |
| author credit `Автор @…` / `Фото:` | 5 |
| blank spacer U+2800 (Braille blank) | 2 |
| '.'-only spacer lines | 2 |
| zero-width characters outside emoji sequences (U+200B/U+200C/U+FEFF) | 1 |
| non-Russian block (Greek, Kyrgyz/Kazakh phrases) | 3 |

Totals: 287 expected ingredients from 264 distinct raw lines; 90 expected steps.

### Step formats (31 recipe captions)

| Format | Fixtures |
|---|---:|
| no steps in the caption (`Приготовление на видео` / video only) | 10 |
| numbered `1.` | 6 |
| prose, one instruction per line, no numbering | 6 |
| keycap emoji `1️⃣ 2️⃣` | 3 |
| prose paragraphs | 3 |
| `✔️` bullets | 1 |
| `-` bullets whose lines are also the ingredients | 1 |
| steps interleaved with ingredient blocks | 1 |

Within these, 2 fixtures (02, 06) have step continuation lines to join. One (13) has `•` sub-bullets inside a numbered step. Six put their steps under a header other than `Приготовление`: `Рецепт:` (2), `Как готовить:`, `ШАГИ`, `Техника приготовления:`, `Тонкости приготовления:`.

### Title patterns (33 non-negative fixtures)

| Pattern | Fixtures |
|---|---:|
| first line, cut at the first emoji or CTA tail | 11 |
| dish never named → `null` | 11 |
| ALL CAPS → sentence case | 4 |
| extracted from a prose sentence (`Этот сливочный булгур с курицей стал…`) | 3 |
| extracted from guillemets or a `Рецепт …` wrapper | 2 |
| second line, after a caps category banner | 1 |
| after leading CTA lines (`Подписывайся…`, `Обязательно сохраняй рецепт`) | 1 |

## Hardest cases

1. **11 `tri-recepta-v-odnom-poste`.** Three recipes run back to back in one caption, and the third has no header at all. The parser has to stop after the first `Приготовление на видео ☝️` or report several recipes. The header is misspelled `Ингридиенты`, and the separator is a hyphen with no spaces (`Белки-6 шт`). `Творожныи‌ сыр` contains U+200C where `й` was meant. In `Сливки-300 мл (33%) холодные` the fat % sits in parentheses after the amount.
2. **06 `salat-iz-botvy-rediski`.** The whole list is a single comma-separated line. Splitting on commas has to respect the parenthesis `(или 70% — 1/3 ч.л.)`, which carries its own amount. `перец, паприка — по 1/2 ч.л.` is a shared amount that spans a comma. Inside one line, entries switch between `name amount` and `name — amount` order. In `ботва от 3 пучков редиски` the quantity sits inside the name.
3. **12 `tertyj-pirog-bez-nazvaniya`.** Steps (`Перетереть в крошку`, `Объединить`, `Уварить до загустения`) are interleaved with ingredient blocks, so "ingredients block, then steps block" splitting fails. `30-40 гр крахмала и 2-3 ст л воды` is two ingredients, each with a range. There is no title.
4. **31 `aperol-shpric`.** There is no ingredient list. The only list sits under `Приготовление:`, and each line is both a step and an ingredient. A sentence that starts with `Ингредиенты` is not a header, `3:2:1` is a ratio rather than amounts, and an amount is glued to a parenthesis: `…Prosecco)~90 мл`.
5. **34 `negative-igra-gryaz`.** It says `Рецепт`, and its prose holds valid food amounts (`1 стакан муки, ½ стакана какао и примерно 1 стакан воды`), yet it is mud for a children's game followed by a detergent ad. A naive parser returns 3 ingredients. Rejecting it takes recipe-detection signals: a list header, at least 2 ingredient lines, and a food title.

Runners-up:

- **22.** The only meatball quantity is hidden in `(у меня 800 гр.)` on a line that also says `по вкусу`.
- **26.** A lone `.` line ends the `Для соуса` section, and the preamble quotes an ingredient name.
- **25.** It has the shared range `по 15-20 грамм` and the alternative-plus-list line `Сушеный чеснок или чесночный перец, сладкая паприка - 0,5 ч.л.`, where the 0.5 may be per item or in total.
- **17.** The ingredients are numbered `1.`–`10.`, with a duplicate `9.`, so they look like steps.
