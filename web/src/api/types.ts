// Wire types of the Mini App API (docs/API.md). Field names follow the JSON.

export type Status = 'want' | 'progress' | 'done'

export const STATUSES: readonly Status[] = ['want', 'progress', 'done']

export interface Person {
  id: number
  name: string
}

/** A stored image. Both URLs are pre-signed by the server; never build them on the client. */
export interface ApiImage {
  id: number
  width: number
  height: number
  thumb_url: string
  full_url: string
}

export interface Price {
  amount: string
  currency: string
  formatted: string
}

/** The sum of a wish's savings («Копим»). */
export interface WishSaved {
  total: Price
  /** 0–100 of the price, or null without a price. */
  percent: number | null
  /** Number of contributions; null in lists (filled for a single wish). */
  count: number | null
}

export interface Wish {
  id: number
  title: string
  note: string
  category_id: number | null
  link: string | null
  price: Price | null
  status: Status
  hot: boolean
  /** null until the first saving. */
  saved: WishSaved | null
  author: Person
  images: ApiImage[]
  created_at: string
  updated_at: string
  fulfilled_at: string | null
}

export interface Category {
  id: number
  name: string
  emoji: string
  position: number
}

export interface Ingredient {
  name: string
  /** A canonical decimal such as "1.5" or "0.33" (thirds are stored rounded), or null when not given. */
  amount: string | null
  /** A unit code from Me.units, or null. */
  unit: string | null
  /** The unit's word declined for the amount («чайные ложки»), or null without a unit. */
  unit_label?: string | null
  /** «1½ чайной ложки»: amount and label joined by a no-break space. */
  formatted: string
}

/** One КБЖУ set as decimal strings with at most one decimal place. */
export interface Macros {
  kcal: string
  protein: string
  fat: string
  carbs: string
}

export interface Nutrition {
  per_100g: Macros
  weight_g: number | null
  /** Mirrors Recipe.servings (kept for older clients). */
  servings: number | null
  /** null without weight_g. */
  per_dish: Macros | null
  /** null without both weight_g and servings. */
  per_serving: Macros | null
}

/** One ingredient counted into the automatic КБЖУ, for the whole recipe. */
export interface NutritionAutoItem {
  name: string
  /** The food-table entry it matched, e.g. «Макароны сухие». */
  food: string
  grams: number
  kcal: string
  protein?: string
  fat?: string
  carbs?: string
}

export interface NutritionCoverage {
  /** Ingredients that went into the total. */
  counted: number
  /** All ingredients except the skipped ones («по вкусу»). */
  total: number
  /** Not found in the food table. */
  missing: string[]
  /** Found, but without an amount or a gram measure for its unit. */
  no_amount: string[]
  /** «по вкусу»: never counted and never missing. */
  skipped: string[]
}

/** КБЖУ estimated by the server from the ingredients and its food table; shown with «≈». */
export interface NutritionAuto {
  per_100g: Macros
  weight_g: number
  per_dish: Macros
  /** null without the recipe's servings. */
  per_serving: Macros | null
  coverage: NutritionCoverage
  items: NutritionAutoItem[]
}

export interface CookingSummary {
  count: number
  last_cooked_at: string | null
  /** The average of all ratings with one decimal, e.g. "4.5"; null before the first rating. */
  rating_avg: string | null
  rating_count: number
}

export interface Recipe {
  id: number
  title: string
  link: string | null
  body: string
  cuisine_id: number | null
  course_ids: number[]
  ingredients: Ingredient[]
  /** How many portions the recipe yields (1–50); null when unknown. Amounts and КБЖУ are for all of them. */
  servings: number | null
  /** КБЖУ typed by hand; wins over nutrition_auto in the UI. */
  nutrition: Nutrition | null
  /** null when nothing could be counted; absent from older servers. */
  nutrition_auto?: NutritionAuto | null
  cooking: CookingSummary
  author: Person
  images: ApiImage[]
  created_at: string
  updated_at: string
}

export type TagKind = 'cuisine' | 'course'

export interface RecipeTag {
  id: number
  kind: TagKind
  name: string
  emoji: string
  position: number
}

export interface Rating {
  user: Person
  stars: number
  comment: string
  rated_at: string
}

/** One time a recipe was cooked, with the ratings each person gave. */
export interface Cook {
  id: number
  recipe_id: number
  cooked_by: Person
  cooked_at: string
  ratings: Rating[]
}

export interface Saving {
  id: number
  wish_id: number
  amount: Price
  user: Person
  note: string
  created_at: string
}

export interface Quantity {
  amount: string | null
  unit: string | null
  unit_label?: string | null
  formatted: string
}

export interface ShoppingItem {
  id: number
  name: string
  quantity: Quantity | null
  checked: boolean
  recipe_id: number | null
  added_by: Person
  created_at: string
  updated_at: string
}

export interface CurrencyInfo {
  code: string
  symbol: string
}

export interface Limits {
  title_max: number
  note_max: number
  link_max: number
  images_per_wish: number
  image_max_bytes: number
  category_name_max: number
  recipe_body_max: number
  images_per_recipe: number
  ingredients_per_recipe: number
  courses_per_recipe: number
  shopping_items_max: number
  item_name_max: number
  rating_comment_max: number
  saving_note_max: number
  dish_weight_max_g: number
  servings_max: number
}

export interface Me {
  user: Person
  partners: Person[]
  currencies: CurrencyInfo[]
  default_currency: string
  limits: Limits
  /** Kitchen unit codes in picker order, e.g. "г", "ч. л.", "по вкусу". */
  units: string[]
  /** The words of every unit code; absent from older servers (units.ts has the same table built in). */
  unit_forms?: Record<string, UnitFormsJSON>
}

/** «1 чайная ложка», «2 чайные ложки», «5 чайных ложек», «½ чайной ложки»; invariant units repeat the code. */
export interface UnitFormsJSON {
  one: string
  few: string
  many: string
  fraction: string
}

export interface MoneySum {
  amount: string
  currency: string
  formatted: string
}

export interface StatusTotals {
  count: number
  sums: MoneySum[]
}

export type ByStatus = Record<Status, StatusTotals>

export interface CategoryStats {
  category: Category | null
  by_status: ByStatus
}

export interface Stats {
  overall: ByStatus
  categories: CategoryStats[]
  fulfilled_this_year: number
  recipes: number
  /** Every time any recipe was cooked. */
  recipes_cooked: number
  /** Money put aside for wishes that have not come true yet, one entry per currency. */
  saved: MoneySum[]
}

export interface MoneyInput {
  amount: string
  currency: string
}

export interface WishInput {
  title: string
  note: string
  category_id: number | null
  link: string | null
  price: MoneyInput | null
  hot: boolean
}

export interface IngredientInput {
  name: string
  amount: string | null
  unit: string | null
}

/**
 * Always per-100 g values (decimal strings); the client converts whole-dish
 * input before sending. The servings travel as RecipeInput.servings.
 */
export interface NutritionInput {
  kcal: string
  protein: string
  fat: string
  carbs: string
  weight_g: number | null
}

export interface RecipeInput {
  title: string
  link: string | null
  body: string
  cuisine_id: number | null
  course_ids: number[]
  ingredients: IngredientInput[]
  /** 1–50, or null for unknown. */
  servings: number | null
  nutrition: NutritionInput | null
}

/** POST /api/recipes/import: exactly one of the two. */
export type ImportInput = { url: string } | { text: string }

export interface ImportReport {
  source: 'instagram' | 'text'
  /** '' on a duplicate: nothing was parsed. */
  parser: 'rules' | 'llm' | 'video' | ''
  /** 0..1 */
  confidence: number
  /** Whether the cover image was attached. */
  image: boolean
  /** Lines the parser could not read, ranges it shortened, … — in Russian, for people. */
  warnings: string[]
  /** The same post was imported before: the recipe is the existing one (HTTP 200). */
  duplicate?: boolean
}

export interface RecipeImport {
  recipe: Recipe
  import: ImportReport
}

export interface CategoryInput {
  name: string
  emoji: string
}

export interface RecipeTagInput {
  kind: TagKind
  name: string
  emoji: string
}

export interface RatingInput {
  stars: number
  comment: string
}

export interface SavingInput {
  amount: MoneyInput
  note: string
}

export interface ShoppingItemInput {
  name: string
  amount: string | null
  unit: string | null
}

export interface ShoppingPatch {
  name?: string
  amount?: string | null
  unit?: string | null
  checked?: boolean
}

/** The owner collection of an image, as it appears in API paths. */
export type ImageOwner = 'wishes' | 'recipes'
