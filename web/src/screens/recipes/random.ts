import type { ApiClient } from '../../api/client'
import type { Recipe } from '../../api/types'

const MAX_REROLLS = 3

/** Asks the server for a random recipe, avoiding an immediate repeat when there is a choice. */
export async function pickRandomRecipe(api: ApiClient, avoidId: number | null, total: number): Promise<Recipe> {
  let recipe = await api.randomRecipe()
  for (let i = 0; i < MAX_REROLLS && avoidId !== null && total > 1 && recipe.id === avoidId; i++) {
    recipe = await api.randomRecipe()
  }
  return recipe
}
