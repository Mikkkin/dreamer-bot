# Ingredient nutrition table (`foods.json`)

This is the built-in food table behind auto-КБЖУ. It has about 365 ingredients that come up often in Russian home cooking. For each one it stores energy and macros per 100 g, Russian names and synonyms, and gram weights for household measures (ч. л., ст. л., стакан, шт, зубчик, пучок, банка and so on).

`foods.json` is **generated**. Edit `tools/nutritiongen/mapping.json` instead, then regenerate.

## Provenance

| `source` | Meaning | Count |
|---|---|---|
| `usda-sr-legacy` | Values taken as-is from **USDA FoodData Central, SR Legacy** (April 2018 release). Each entry carries `fdc_id` and the original `fdc_description`. A few entries are a `blend`, which is a weighted mix of several FDC foods (for example "фарш домашний" = 50% pork mince + 50% beef 80/20). | 314 |
| `label-typical` | Typical values from Russian retail labels or the Skurikhin reference tables (Скурихин, «Химический состав российских пищевых продуктов»). These are used only where USDA has no fitting food, mostly Russian dairy fat levels (молоко/кефир/сметана/сливки/творог), Russian cheeses, майонез 67%, сгущёнка, манка, Russian bread, колбасы and Russian canned fish. Each entry has a `source_note`. | 51 |

The USDA FoodData Central data is in the public domain under **CC0 1.0**. Cite it as: U.S. Department of Agriculture, Agricultural Research Service. *FoodData Central*, 2019. fdc.nal.usda.gov.

These are the USDA nutrient ids used:

- Energy (kcal): `1008` (`nutrient_nbr` 208)
- Protein: `1003`
- Total lipid (fat): `1004`
- Carbohydrate, by difference: `1005`

The input archive is `FoodData_Central_sr_legacy_food_csv_2018-04.zip`:

- Source: <https://fdc.nal.usda.gov/fdc-datasets/FoodData_Central_sr_legacy_food_csv_2018-04.zip>. If the file moves, check the index at <https://fdc.nal.usda.gov/download-datasets>.
- SHA-256: `b80817294b8850530aaedf2e515c02593b1824f763a0ff356e5c2081643e6fd0`

## Conventions

The full machine-readable conventions are in `foods.json` → `conventions`. In summary:

- **`per100`**: values for 100 g of the edible part, in the state the recipe names it. Grains, pasta and legumes are **dry**. Meat, fish and vegetables are **raw**. Canned goods are drained unless the `note` says otherwise. Values are rounded to 1 decimal.
- **Energy**: USDA kcal is used unchanged, since USDA applies specific Atwater factors. Label entries use the label kcal. `gen.ts` warns if a label's kcal differs from 4P + 9F + 4C by more than 15%, which catches typos.
- **Carbs**: USDA "by difference" **includes dietary fibre**. Russian labels usually exclude it, so USDA vegetables, greens and spices show somewhat higher carbs than a Russian label would.
- **Volumes**: ч. л. = 5 мл, ст. л. = 15 мл, стакан = гранёный 200 мл.
  - Liquids are converted by density: water and milk ≈ 1.0–1.03 g/ml, oil 0.92.
  - Dry goods use bulk density taken from the Russian 200 ml glass table (мука 130 г, сахар 180 г, рис 180 г, гречка 166 г …).
- **Spoons are level** (без горки). Thick products are normally scooped with a heap (сметана, мёд, майонез, сгущёнка, творог, томатная паста, сливочное масло, nut pastes, soft cheeses). For these the spoon weights are multiplied by `heaped_factor` = 1.5, which lines up with the Russian tables (ст. л. сметаны ≈ 23–25 г, мёда ≈ 32 г).
- **`grams`** gives the grams in 1 unit:
  - `мл` is the density. For litres, multiply by 1000.
  - `шт` is an average piece, edible part only. Some examples: яйцо С1 55 г without the shell, луковица 100, морковь 80, картофелина 100, помидор 120, огурец 100, банан 120 (flesh), яблоко 150, лимон 100, зубчик чеснока 5.
  - Other units: пучок зелени 30 г, щепотка 0.5 г.
  - `упаковка` / `пакетик` / `банка` appear only where a standard pack exists (масло 180 г, творог 200 г, разрыхлитель 10 г, сухие дрожжи 11 г, кукуруза 340 г → 210 г drained …).
  - **If a unit is missing, it is unknown or does not apply to that food.** The file never contains `null` grams. `category_grams` lists the per-category defaults that were already merged into each food.
- **Matching names**: normalize by lowercasing, replacing ё→е and collapsing whitespace. After normalization, names and aliases are unique across the whole table, and the generator enforces this. A few bare words are deliberately mapped to the most common food:

  | Word | Maps to |
  |---|---|
  | «молоко» | 2,5% |
  | «кефир» | 2,5% |
  | «сметана» | 15% |
  | «сливки» | 20% |
  | «творог» | 5% |
  | «сыр» | твёрдый |
  | «кукуруза» | консервированная |
  | «горошек» | консервированный |
  | «фарш» | домашний |
  | «сельдь» | солёная |
  | «масло» | сливочное 82,5% (unqualified «масло» in a Russian ingredient list is butter; vegetable oils are named: «растительное», «подсолнечное», «для жарки») |

  «перец» is left ambiguous on purpose, because it could be sweet pepper or black pepper.

## Regenerating

You need [bun](https://bun.sh). Do not use npm or npx.

```sh
# 1. download and unzip the dataset anywhere outside the repo
curl -LO https://fdc.nal.usda.gov/fdc-datasets/FoodData_Central_sr_legacy_food_csv_2018-04.zip
unzip FoodData_Central_sr_legacy_food_csv_2018-04.zip -d /tmp/sr_legacy

# 2. regenerate (accepts the CSV directory or its parent)
bun tools/nutritiongen/gen.ts /tmp/sr_legacy

# CI / review: verify the committed file matches mapping.json + dataset
bun tools/nutritiongen/gen.ts /tmp/sr_legacy --check
```

The output is deterministic. There are no timestamps, foods are sorted by `id` (one food per line, so diffs stay clean) and aliases are sorted.

### Adding or fixing a food

Add a line to `tools/nutritiongen/mapping.json` with:

- `id`: ASCII snake_case.
- `name`: the Russian name in the nominative case.
- `aliases`
- `category`
- **Exactly one** of the following:
  - `fdc_id`, an SR Legacy id. Look it up in `food.csv`.
  - `blend`, a list of `{fdc_id, share}` entries whose shares add up to 1.
  - `label`, written as `{kcal, protein, fat, carbs, ref}`, where `ref` says where the numbers come from.
- Optional fields:
  - `density` in g/ml. This produces `мл`, `ч. л.`, `ст. л.` and `стакан`. Set it to `null` to drop a category default.
  - `heaped`
  - `grams`, which holds explicit units and overrides computed ones. Setting a unit to `null` removes it.
  - `note`

The generator rejects the following:

- duplicate ids, names or aliases
- unknown categories or units
- missing FDC ids
- non-SR-Legacy records
- blends whose shares do not add up to 1

## Known caveats

- **Sugar** is 387 kcal, from USDA's 3.87 kcal/g factor. Russian labels print 399 kcal (4 kcal/g). That is a 3% difference and was kept as is for provenance.
- **Butter 82,5%** is label-typical at 748 kcal. USDA butter (81% fat) is 717.
- **Гречка** uses USDA roasted buckwheat groats (ядрица-like) at 346 kcal. Russian labels range from 308 to 343 kcal because they exclude fibre.
- **Spread-out label values**: сельдь солёная (fat varies 8–20% by season), лаваш (236–277 kcal), слоёное тесто, сервелат, плавленый сыр and дижонская горчица vary a lot between brands. Treat them as typical values, not exact ones.
- **Proxies**: свиная шея uses USDA Boston butt, which is leaner than typical Russian шея. Хек uses USDA whiting. Судак uses walleye. Уксус 9% uses USDA distilled vinegar, which is 5%. Its energy is negligible in either case.
