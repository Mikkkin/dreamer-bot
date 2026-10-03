// nutritiongen — builds internal/nutrition/data/foods.json from the USDA
// FoodData Central "SR Legacy" CSV release plus the curated mapping.json.
//
// Usage:
//   bun tools/nutritiongen/gen.ts <path-to-csv-dir> [--mapping <file>] [--out <file>] [--check]
//
// <path-to-csv-dir> is the unzipped FoodData_Central_sr_legacy_food_csv_2018-04
// directory (or its parent). --check regenerates in memory and exits 1 when the
// committed output differs, without writing anything.
//
// The output is deterministic: no timestamps, foods sorted by id, aliases sorted
// by their normalized form, fixed key order, fixed number formatting.

import { existsSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

// ---------------------------------------------------------------------------
// Conventions (copied verbatim into the output so consumers can read them)
// ---------------------------------------------------------------------------

const NUTRIENT = { kcal: "1008", protein: "1003", fat: "1004", carbs: "1005" } as const;

const SPOON_ML = { "ч. л.": 5, "ст. л.": 15 } as const;
const GLASS_ML = 200;
const HEAPED_FACTOR = 1.5;

// Output order of unit keys inside "grams".
const UNIT_ORDER = [
  "мл",
  "ч. л.",
  "ст. л.",
  "стакан",
  "шт",
  "зубчик",
  "головка",
  "пучок",
  "веточка",
  "лист",
  "долька",
  "ломтик",
  "см",
  "щепотка",
  "пакетик",
  "упаковка",
  "банка",
  "плитка",
  "пласт",
] as const;

const CONVENTIONS = {
  per: "все значения per100 — на 100 г съедобной части продукта в том виде, в каком он указывается в рецепте (крупы, макароны, бобовые — сухие; мясо, рыба, овощи — сырые)",
  energy:
    "kcal: для source=usda-sr-legacy — нутриент FDC 1008 (Energy, KCAL; nutrient_nbr 208) как есть, со специфическими коэффициентами Этуотера USDA; для source=label-typical — энергетическая ценность из типовой российской маркировки/справочника",
  macros:
    "protein = FDC 1003, fat = FDC 1004 (Total lipid), carbs = FDC 1005 (Carbohydrate, by difference — включает пищевые волокна); у label-typical углеводы как в маркировке (обычно без волокон)",
  rounding: "per100 — 1 знак после запятой; ложки — до 0,5 г; стакан — до 1 г; мл — до 0,01 г",
  blend: "blend — взвешенная смесь нескольких продуктов USDA (доли в сумме = 1)",
  volumes_ml: { "ч. л.": SPOON_ML["ч. л."], "ст. л.": SPOON_ML["ст. л."], стакан: GLASS_ML },
  spoons:
    "ложки ровные, без горки: масса = объём × плотность (для сыпучих — насыпная плотность). Для вязких продуктов (сметана, мёд, майонез, сгущёнка, творог, паста и т. п.), которые на практике набирают с горкой, ложки умножены на heaped_factor",
  heaped_factor: HEAPED_FACTOR,
  glass: "стакан = гранёный 200 мл (до риски); для сыпучих — насыпная масса",
  piece: "шт — средний экземпляр, съедобная часть (без кожуры, скорлупы, косточки, кости), если в note не сказано иное",
  units: {
    "мл": "граммов в 1 мл (плотность); литр = 1000 мл",
    "ч. л.": "чайная ложка 5 мл, ровная (с горкой для вязких)",
    "ст. л.": "столовая ложка 15 мл, ровная (с горкой для вязких)",
    "стакан": "гранёный стакан 200 мл",
    "шт": "средний экземпляр",
    "зубчик": "зубчик чеснока",
    "головка": "головка чеснока",
    "пучок": "магазинный пучок зелени (~30 г, если не указано иное)",
    "веточка": "веточка зелени",
    "лист": "лист (салата, капусты, лавровый, желатина)",
    "долька": "долька (лимона, плитки шоколада)",
    "ломтик": "ломтик (хлеба, сыра, колбасы, бекона)",
    "см": "сантиметр корня (имбирь)",
    "щепотка": "щепотка ~0,5 г",
    "пакетик": "стандартный пакетик (разрыхлитель 10 г, сухие дрожжи 11 г, желатин 10 г, ванильный сахар 8 г, крупа в варочном пакете 80 г)",
    "упаковка": "стандартная упаковка (масло сливочное 180 г, творог 200 г …)",
    "банка": "стандартная банка; для консервов в заливке — масса без жидкости, если в note не сказано иное",
    "плитка": "плитка шоколада 100 г",
    "пласт": "пласт слоёного теста",
  },
  missing_unit: "если единицы нет в grams — она для продукта неприменима или неизвестна; значений null в grams нет",
  name_matching:
    "нормализация для поиска: нижний регистр, ё→е, пробелы схлопнуты; имена и синонимы уникальны после нормализации",
} as const;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Grams = Record<string, number | null>;

interface Category {
  name: string;
  density?: number | null;
  grams?: Grams;
}

interface LabelValues {
  kcal: number;
  protein: number;
  fat: number;
  carbs: number;
  ref: string;
}

interface MappingFood {
  id: string;
  name: string;
  aliases: string[];
  category: string;
  fdc_id?: number;
  blend?: { fdc_id: number; share: number }[];
  label?: LabelValues;
  density?: number | null;
  heaped?: boolean;
  grams?: Grams;
  note?: string;
}

interface Mapping {
  version: number;
  categories: Record<string, Category>;
  foods: MappingFood[];
}

interface Per100 {
  kcal: number;
  protein: number;
  fat: number;
  carbs: number;
}

interface FdcFood {
  description: string;
  dataType: string;
  nutrients: Partial<Record<keyof Per100, number>>;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function fail(msg: string): never {
  console.error(`nutritiongen: ${msg}`);
  process.exit(2);
}

function parseCsvLine(line: string): string[] {
  const out: string[] = [];
  let cur = "";
  let quoted = false;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (quoted) {
      if (c === '"') {
        if (line[i + 1] === '"') {
          cur += '"';
          i++;
        } else quoted = false;
      } else cur += c;
    } else if (c === '"') quoted = true;
    else if (c === ",") {
      out.push(cur);
      cur = "";
    } else cur += c;
  }
  out.push(cur);
  return out;
}

async function readCsv(path: string, onRow: (row: Record<string, string>) => void): Promise<void> {
  const text = await Bun.file(path).text();
  const lines = text.split(/\r?\n/);
  const header = parseCsvLine(lines[0]);
  for (let i = 1; i < lines.length; i++) {
    const line = lines[i];
    if (!line) continue;
    const cells = parseCsvLine(line);
    const row: Record<string, string> = {};
    for (let j = 0; j < header.length; j++) row[header[j]] = cells[j] ?? "";
    onRow(row);
  }
}

function findCsvDir(dir: string): string {
  const abs = resolve(dir);
  if (existsSync(join(abs, "food.csv"))) return abs;
  if (existsSync(abs) && statSync(abs).isDirectory()) {
    for (const entry of readdirSync(abs).sort()) {
      const sub = join(abs, entry);
      if (statSync(sub).isDirectory() && existsSync(join(sub, "food.csv"))) return sub;
    }
  }
  fail(`no food.csv in ${abs} (pass the unzipped SR Legacy CSV directory)`);
}

const round1 = (x: number): number => Math.round(x * 10) / 10;
const roundHalf = (x: number): number => Math.round(x * 2) / 2;
const round2 = (x: number): number => Math.round(x * 100) / 100;

export function normalizeName(s: string): string {
  return s.toLowerCase().replaceAll("ё", "е").replace(/\s+/g, " ").trim();
}

// ---------------------------------------------------------------------------
// USDA loading
// ---------------------------------------------------------------------------

async function loadFdc(csvDir: string, wanted: Set<number>): Promise<Map<number, FdcFood>> {
  // Sanity-check nutrient ids against nutrient.csv so a schema change is loud.
  const expected: Record<string, string> = {
    [NUTRIENT.kcal]: "Energy|KCAL",
    [NUTRIENT.protein]: "Protein|G",
    [NUTRIENT.fat]: "Total lipid (fat)|G",
    [NUTRIENT.carbs]: "Carbohydrate, by difference|G",
  };
  const seen = new Set<string>();
  await readCsv(join(csvDir, "nutrient.csv"), (r) => {
    if (expected[r.id] !== undefined) {
      const got = `${r.name}|${r.unit_name}`;
      if (got !== expected[r.id]) fail(`nutrient ${r.id} is "${got}", expected "${expected[r.id]}"`);
      seen.add(r.id);
    }
  });
  for (const id of Object.keys(expected)) if (!seen.has(id)) fail(`nutrient ${id} missing from nutrient.csv`);

  const foods = new Map<number, FdcFood>();
  await readCsv(join(csvDir, "food.csv"), (r) => {
    const id = Number(r.fdc_id);
    if (wanted.has(id)) foods.set(id, { description: r.description, dataType: r.data_type, nutrients: {} });
  });
  for (const id of wanted) if (!foods.has(id)) fail(`fdc_id ${id} not found in food.csv`);

  const byNutrient: Record<string, keyof Per100> = {
    [NUTRIENT.kcal]: "kcal",
    [NUTRIENT.protein]: "protein",
    [NUTRIENT.fat]: "fat",
    [NUTRIENT.carbs]: "carbs",
  };
  await readCsv(join(csvDir, "food_nutrient.csv"), (r) => {
    const key = byNutrient[r.nutrient_id];
    if (!key) return;
    const food = foods.get(Number(r.fdc_id));
    if (!food) return;
    const v = Number(r.amount);
    if (!Number.isFinite(v)) fail(`bad amount for fdc ${r.fdc_id} nutrient ${r.nutrient_id}: "${r.amount}"`);
    food.nutrients[key] = v;
  });
  return foods;
}

function fdcPer100(id: number, fdc: Map<number, FdcFood>, warnings: string[]): Per100 {
  const f = fdc.get(id)!;
  if (f.dataType !== "sr_legacy_food") fail(`fdc_id ${id} is ${f.dataType}, expected sr_legacy_food`);
  if (f.nutrients.kcal === undefined) fail(`fdc_id ${id} (${f.description}) has no energy (1008)`);
  const out: Per100 = { kcal: f.nutrients.kcal, protein: 0, fat: 0, carbs: 0 };
  for (const k of ["protein", "fat", "carbs"] as const) {
    const v = f.nutrients[k];
    if (v === undefined) warnings.push(`fdc_id ${id} (${f.description}): no ${k} row, using 0`);
    else out[k] = v;
  }
  return out;
}

// ---------------------------------------------------------------------------
// Validation and resolution
// ---------------------------------------------------------------------------

const ID_RE = /^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$/;

function validate(m: Mapping): void {
  if (m.version !== 1) fail(`unsupported mapping version ${m.version}`);
  const ids = new Set<string>();
  const names = new Map<string, string>(); // normalized name/alias -> food id
  for (const [cid, c] of Object.entries(m.categories)) {
    if (!ID_RE.test(cid)) fail(`category id "${cid}" is not ascii snake_case`);
    for (const u of Object.keys(c.grams ?? {})) if (!(UNIT_ORDER as readonly string[]).includes(u)) fail(`category ${cid}: unknown unit "${u}"`);
  }
  for (const f of m.foods) {
    const where = `food ${f.id ?? "?"}`;
    if (!ID_RE.test(f.id)) fail(`${where}: id is not ascii snake_case`);
    if (ids.has(f.id)) fail(`${where}: duplicate id`);
    ids.add(f.id);
    if (!f.name) fail(`${where}: empty name`);
    if (!m.categories[f.category]) fail(`${where}: unknown category "${f.category}"`);
    const kinds = [f.fdc_id !== undefined, f.blend !== undefined, f.label !== undefined].filter(Boolean).length;
    if (kinds !== 1) fail(`${where}: needs exactly one of fdc_id / blend / label`);
    if (f.blend) {
      const sum = f.blend.reduce((s, b) => s + b.share, 0);
      if (Math.abs(sum - 1) > 1e-9) fail(`${where}: blend shares sum to ${sum}`);
    }
    if (f.label) {
      for (const k of ["kcal", "protein", "fat", "carbs"] as const)
        if (typeof f.label[k] !== "number" || f.label[k] < 0) fail(`${where}: label.${k} must be a non-negative number`);
      if (!f.label.ref) fail(`${where}: label.ref is required`);
    }
    for (const u of Object.keys(f.grams ?? {})) if (!(UNIT_ORDER as readonly string[]).includes(u)) fail(`${where}: unknown unit "${u}"`);
    // Names and aliases must be unique across foods after normalization.
    for (const n of [f.name, ...f.aliases]) {
      const key = normalizeName(n);
      if (!key) fail(`${where}: empty alias`);
      const owner = names.get(key);
      if (owner !== undefined && owner !== f.id) fail(`${where}: "${n}" collides with food ${owner}`);
      names.set(key, f.id);
    }
  }
}

function computeGrams(density: number | null | undefined, heaped: boolean, explicit: Grams[]): Record<string, number> {
  const g: Grams = {};
  if (typeof density === "number") {
    const k = heaped ? HEAPED_FACTOR : 1;
    g["мл"] = round2(density);
    g["ч. л."] = roundHalf(SPOON_ML["ч. л."] * density * k);
    g["ст. л."] = roundHalf(SPOON_ML["ст. л."] * density * k);
    g["стакан"] = Math.round(GLASS_ML * density);
  }
  for (const layer of explicit) Object.assign(g, layer);
  const out: Record<string, number> = {};
  for (const u of UNIT_ORDER) {
    const v = g[u];
    if (typeof v === "number") {
      if (!(v > 0)) fail(`non-positive grams for unit ${u}`);
      out[u] = v;
    }
  }
  return out;
}

// ---------------------------------------------------------------------------
// Serialization (fixed formatting so the output diffs cleanly)
// ---------------------------------------------------------------------------

class Fixed1 {
  constructor(readonly v: number) {}
}

function ser(v: unknown): string {
  if (v instanceof Fixed1) return v.v.toFixed(1);
  if (v === null) return "null";
  if (typeof v === "number") {
    if (!Number.isFinite(v)) fail(`non-finite number in output`);
    return String(v);
  }
  if (typeof v === "string" || typeof v === "boolean") return JSON.stringify(v);
  if (Array.isArray(v)) return `[${v.map(ser).join(", ")}]`;
  if (typeof v === "object") {
    const parts = Object.entries(v as Record<string, unknown>)
      .filter(([, x]) => x !== undefined)
      .map(([k, x]) => `${JSON.stringify(k)}: ${ser(x)}`);
    return `{${parts.join(", ")}}`;
  }
  fail(`cannot serialize ${typeof v}`);
}

function pretty(v: unknown, indent: string): string {
  return JSON.stringify(v, null, 2).replace(/\n/g, `\n${indent}`);
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  const args = process.argv.slice(2);
  let csvArg: string | undefined;
  let mappingPath = join(import.meta.dir, "mapping.json");
  let outPath = join(import.meta.dir, "..", "..", "internal", "nutrition", "data", "foods.json");
  let check = false;
  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (a === "--mapping") mappingPath = args[++i] ?? fail("--mapping needs a value");
    else if (a === "--out") outPath = args[++i] ?? fail("--out needs a value");
    else if (a === "--check") check = true;
    else if (a.startsWith("--")) fail(`unknown flag ${a}`);
    else if (csvArg === undefined) csvArg = a;
    else fail(`unexpected argument ${a}`);
  }
  if (!csvArg) fail("usage: bun tools/nutritiongen/gen.ts <path-to-csv-dir> [--mapping f] [--out f] [--check]");

  const csvDir = findCsvDir(csvArg);
  const mapping = (await Bun.file(mappingPath).json()) as Mapping;
  validate(mapping);

  const wanted = new Set<number>();
  for (const f of mapping.foods) {
    if (f.fdc_id !== undefined) wanted.add(f.fdc_id);
    for (const b of f.blend ?? []) wanted.add(b.fdc_id);
  }
  const fdc = await loadFdc(csvDir, wanted);
  const warnings: string[] = [];

  const foods = mapping.foods
    .map((f) => {
      const cat = mapping.categories[f.category];
      let per100: Per100;
      let source: string;
      let fdcDescription: string | undefined;
      let sourceNote: string | undefined;
      let blend: { fdc_id: number; share: number; description: string }[] | undefined;

      if (f.fdc_id !== undefined) {
        per100 = fdcPer100(f.fdc_id, fdc, warnings);
        source = "usda-sr-legacy";
        fdcDescription = fdc.get(f.fdc_id)!.description;
      } else if (f.blend) {
        per100 = { kcal: 0, protein: 0, fat: 0, carbs: 0 };
        for (const b of f.blend) {
          const p = fdcPer100(b.fdc_id, fdc, warnings);
          for (const k of ["kcal", "protein", "fat", "carbs"] as const) per100[k] += p[k] * b.share;
        }
        source = "usda-sr-legacy";
        blend = f.blend.map((b) => ({ fdc_id: b.fdc_id, share: b.share, description: fdc.get(b.fdc_id)!.description }));
      } else {
        const l = f.label!;
        per100 = { kcal: l.kcal, protein: l.protein, fat: l.fat, carbs: l.carbs };
        source = "label-typical";
        sourceNote = l.ref;
      }

      // Typo guard for hand-entered label values: Russian labels use 4/9/4
      // factors, so kcal should be close to the macro estimate. USDA values are
      // authoritative (specific Atwater factors, fibre inside carbs, alcohol),
      // so they are not checked.
      if (f.label) {
        const atwater = 4 * per100.protein + 9 * per100.fat + 4 * per100.carbs;
        if (Math.abs(per100.kcal - atwater) / Math.max(per100.kcal, 1) > 0.15)
          warnings.push(`${f.id}: label kcal ${per100.kcal} vs 4/9/4 estimate ${round1(atwater)}`);
      }

      const density = f.density !== undefined ? f.density : cat.density;
      const grams = computeGrams(density, f.heaped === true, [cat.grams ?? {}, f.grams ?? {}]);

      // Aliases: drop ones equal to the name or to each other after normalization, sort stably.
      const seen = new Set<string>([normalizeName(f.name)]);
      const aliases: string[] = [];
      for (const a of f.aliases) {
        const k = normalizeName(a);
        if (seen.has(k)) continue;
        seen.add(k);
        aliases.push(a);
      }
      aliases.sort((x, y) => {
        const a = normalizeName(x);
        const b = normalizeName(y);
        return a < b ? -1 : a > b ? 1 : x < y ? -1 : x > y ? 1 : 0;
      });

      return {
        id: f.id,
        name: f.name,
        aliases,
        category: f.category,
        source,
        fdc_id: f.fdc_id ?? null,
        fdc_description: fdcDescription,
        blend,
        source_note: sourceNote,
        note: f.note,
        per100: {
          kcal: new Fixed1(round1(per100.kcal)),
          protein: new Fixed1(round1(per100.protein)),
          fat: new Fixed1(round1(per100.fat)),
          carbs: new Fixed1(round1(per100.carbs)),
        },
        grams,
      };
    })
    .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));

  const names = new Set<string>();
  for (const f of foods) {
    if (names.has(f.name)) fail(`duplicate name ${f.name}`);
    names.add(f.name);
  }

  const categories: Record<string, string> = {};
  const categoryGrams: Record<string, Record<string, number>> = {};
  for (const [cid, c] of Object.entries(mapping.categories)) {
    categories[cid] = c.name;
    const g = computeGrams(c.density, false, [c.grams ?? {}]);
    if (Object.keys(g).length > 0) categoryGrams[cid] = g;
  }

  const counts = { total: foods.length, "usda-sr-legacy": 0, "label-typical": 0 };
  for (const f of foods) counts[f.source as "usda-sr-legacy" | "label-typical"]++;

  const header = {
    version: 1,
    source:
      "USDA FoodData Central SR Legacy (April 2018, CC0 1.0) + типовые значения российской маркировки/справочников (label-typical)",
    dataset: {
      name: "USDA FoodData Central — SR Legacy",
      release: "2018-04",
      url: "https://fdc.nal.usda.gov/fdc-datasets/FoodData_Central_sr_legacy_food_csv_2018-04.zip",
      license: "CC0 1.0 (public domain)",
      citation:
        "U.S. Department of Agriculture, Agricultural Research Service. FoodData Central, 2019. fdc.nal.usda.gov",
    },
    generator: "tools/nutritiongen/gen.ts (curated mapping: tools/nutritiongen/mapping.json)",
    counts,
    conventions: CONVENTIONS,
    categories,
    category_grams: categoryGrams,
  };

  let out = "{\n";
  for (const [k, v] of Object.entries(header)) out += `  ${JSON.stringify(k)}: ${pretty(v, "  ")},\n`;
  out += `  "foods": [\n`;
  out += foods.map((f) => `    ${ser(f)}`).join(",\n");
  out += `\n  ]\n}\n`;

  for (const w of warnings) console.error(`warning: ${w}`);

  if (check) {
    const current = existsSync(outPath) ? await Bun.file(outPath).text() : "";
    if (current !== out) {
      console.error(`nutritiongen: ${outPath} is out of date; rerun without --check`);
      process.exit(1);
    }
    console.error(`nutritiongen: ${outPath} is up to date (${counts.total} foods)`);
    return;
  }
  await Bun.write(outPath, out);
  console.error(
    `nutritiongen: wrote ${outPath}: ${counts.total} foods (${counts["usda-sr-legacy"]} usda-sr-legacy, ${counts["label-typical"]} label-typical)`,
  );
}

await main();
