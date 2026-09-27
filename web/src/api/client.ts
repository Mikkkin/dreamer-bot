import { ApiError, networkError, parseErrorEnvelope, parseJsonBody } from './errors'
import type {
  ApiImage,
  Category,
  CategoryInput,
  ImageOwner,
  Me,
  Recipe,
  RecipeInput,
  Stats,
  Status,
  Wish,
  WishInput,
} from './types'

const JSON_TIMEOUT_MS = 20_000
const UPLOAD_TIMEOUT_MS = 180_000

export type AuthFailureHandler = (error: ApiError) => void

/**
 * The only module that talks to the server. Every request carries the raw
 * Telegram initData; auth failures are reported once through onAuthFailure so
 * the app can switch to a full-screen state.
 */
export class ApiClient {
  readonly #authorization: string
  readonly #onAuthFailure: AuthFailureHandler

  constructor(initData: string, onAuthFailure: AuthFailureHandler) {
    this.#authorization = `tma ${initData}`
    this.#onAuthFailure = onAuthFailure
  }

  me(): Promise<Me> {
    return this.#json('GET', '/api/me')
  }

  async wishes(): Promise<Wish[]> {
    return (await this.#json<{ wishes: Wish[] }>('GET', '/api/wishes')).wishes
  }

  wish(id: number): Promise<Wish> {
    return this.#json('GET', `/api/wishes/${seg(id)}`)
  }

  createWish(input: WishInput): Promise<Wish> {
    return this.#json('POST', '/api/wishes', input)
  }

  updateWish(id: number, input: Partial<WishInput>): Promise<Wish> {
    return this.#json('PATCH', `/api/wishes/${seg(id)}`, input)
  }

  setWishStatus(id: number, status: Status): Promise<Wish> {
    return this.#json('PUT', `/api/wishes/${seg(id)}/status`, { status })
  }

  deleteWish(id: number): Promise<void> {
    return this.#json('DELETE', `/api/wishes/${seg(id)}`)
  }

  async categories(): Promise<Category[]> {
    return (await this.#json<{ categories: Category[] }>('GET', '/api/categories')).categories
  }

  createCategory(input: CategoryInput): Promise<Category> {
    return this.#json('POST', '/api/categories', input)
  }

  updateCategory(id: number, input: CategoryInput): Promise<Category> {
    return this.#json('PATCH', `/api/categories/${seg(id)}`, input)
  }

  deleteCategory(id: number): Promise<void> {
    return this.#json('DELETE', `/api/categories/${seg(id)}`)
  }

  async recipes(): Promise<Recipe[]> {
    return (await this.#json<{ recipes: Recipe[] }>('GET', '/api/recipes')).recipes
  }

  recipe(id: number): Promise<Recipe> {
    return this.#json('GET', `/api/recipes/${seg(id)}`)
  }

  randomRecipe(): Promise<Recipe> {
    return this.#json('GET', '/api/recipes/random')
  }

  createRecipe(input: RecipeInput): Promise<Recipe> {
    return this.#json('POST', '/api/recipes', input)
  }

  updateRecipe(id: number, input: Partial<RecipeInput>): Promise<Recipe> {
    return this.#json('PATCH', `/api/recipes/${seg(id)}`, input)
  }

  deleteRecipe(id: number): Promise<void> {
    return this.#json('DELETE', `/api/recipes/${seg(id)}`)
  }

  stats(): Promise<Stats> {
    return this.#json('GET', '/api/stats')
  }

  deleteImage(owner: ImageOwner, ownerId: number, imageId: number): Promise<void> {
    return this.#json('DELETE', `/api/${owner}/${seg(ownerId)}/images/${seg(imageId)}`)
  }

  /** Uploads one image as multipart field "file". XHR is used because fetch cannot report upload progress. */
  uploadImage(owner: ImageOwner, ownerId: number, file: Blob, onProgress?: (fraction: number) => void): Promise<ApiImage> {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('POST', `/api/${owner}/${seg(ownerId)}/images`)
      xhr.setRequestHeader('Authorization', this.#authorization)
      xhr.timeout = UPLOAD_TIMEOUT_MS
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable && e.total > 0) onProgress?.(e.loaded / e.total)
      }
      xhr.onload = () => {
        const payload = parseJsonBody(xhr.responseText)
        if (xhr.status >= 200 && xhr.status < 300) resolve(payload as ApiImage)
        else reject(this.#fail(parseErrorEnvelope(xhr.status, payload)))
      }
      xhr.onerror = () => reject(this.#fail(networkError()))
      xhr.ontimeout = xhr.onerror
      const form = new FormData()
      form.append('file', file, 'upload')
      xhr.send(form)
    })
  }

  async #json<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Authorization: this.#authorization, Accept: 'application/json' }
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    let res: Response
    try {
      res = await fetch(path, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        cache: 'no-store',
        credentials: 'omit',
        signal: AbortSignal.timeout(JSON_TIMEOUT_MS),
      })
    } catch {
      throw this.#fail(networkError())
    }
    let payload: unknown = null
    try {
      payload = parseJsonBody(await res.text())
    } catch {
      if (res.ok) throw this.#fail(networkError())
    }
    if (!res.ok) throw this.#fail(parseErrorEnvelope(res.status, payload))
    return payload as T
  }

  #fail(error: ApiError): ApiError {
    if (error.isAuth) this.#onAuthFailure(error)
    return error
  }
}

/** Formats a numeric id as a path segment; ids are always positive integers. */
function seg(id: number): string {
  if (!Number.isSafeInteger(id) || id <= 0) throw new ApiError(400, 'validation', 'Некорректный идентификатор')
  return String(id)
}
