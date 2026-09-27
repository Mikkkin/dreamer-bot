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

export interface Wish {
  id: number
  title: string
  note: string
  category_id: number | null
  link: string | null
  price: Price | null
  status: Status
  hot: boolean
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

export interface Recipe {
  id: number
  title: string
  link: string | null
  body: string
  author: Person
  images: ApiImage[]
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
}

export interface Me {
  user: Person
  partners: Person[]
  currencies: CurrencyInfo[]
  default_currency: string
  limits: Limits
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
}

export interface WishInput {
  title: string
  note: string
  category_id: number | null
  link: string | null
  price: { amount: string; currency: string } | null
  hot: boolean
}

export interface RecipeInput {
  title: string
  link: string | null
  body: string
}

export interface CategoryInput {
  name: string
  emoji: string
}

/** The owner collection of an image, as it appears in API paths. */
export type ImageOwner = 'wishes' | 'recipes'
