import type { CSSProperties } from 'react'
import type { Recipe, RecipeTag } from '../../api/types'
import { RECIPE_BACKDROP } from '../../lib/categoryStyle'
import { TIMES_FORMS, countOf, decimalComma } from '../../lib/format'
import { linkHost } from '../../lib/links'
import { recipeTagLine } from '../../lib/recipes'
import { recipeEmoji } from '../../lib/recipeEmoji'
import { glueShortWords } from '../../lib/typo'
import { Backdrop } from '../../ui/art'
import { IconImages, IconLink, IconStar } from '../../ui/icons'
import { Img } from '../../ui/media'

function firstLine(recipe: Recipe): string {
  return recipe.body.split('\n').find((l) => l.trim() !== '')?.trim() ?? ''
}

/** «⭐ 4,5 · ×3» on the cover once the recipe has been cooked. */
function CookStamp({ recipe }: { recipe: Recipe }) {
  const { count, rating_avg: avg } = recipe.cooking
  if (count === 0 && avg === null) return null
  const label = [avg !== null ? `оценка ${decimalComma(avg)}` : '', count > 0 ? `готовили ${countOf(count, TIMES_FORMS)}` : ''].filter(Boolean).join(', ')
  return (
    <span className="stamp stamp--cook" role="img" aria-label={label}>
      {avg !== null && (
        <>
          <IconStar size={13} strokeWidth={2} className="icon-fill stamp__star" />
          <span className="num" aria-hidden="true">
            {decimalComma(avg)}
          </span>
        </>
      )}
      {avg !== null && count > 0 && (
        <span className="stamp__dot" aria-hidden="true">
          ·
        </span>
      )}
      {count > 0 && (
        <span className="num" aria-hidden="true">
          ×{count}
        </span>
      )}
    </span>
  )
}

export function RecipeCard({
  recipe,
  index,
  byId,
  onOpen,
}: {
  recipe: Recipe
  index: number
  byId: ReadonlyMap<number, RecipeTag>
  onOpen: () => void
}) {
  const cover = recipe.images[0]
  const tagLine = recipeTagLine(recipe, byId)
  const line = tagLine ? '' : firstLine(recipe)
  const host = !tagLine && !line && recipe.link ? linkHost(recipe.link) : ''
  const style = { '--i': Math.min(index, 8), '--bd-c': RECIPE_BACKDROP.center, '--bd-e': RECIPE_BACKDROP.edge } as CSSProperties
  return (
    <button type="button" className="card card--recipe" style={style} onClick={onOpen}>
      <span className="card__cover">
        {cover ? (
          <Img src={cover.thumb_url} alt={recipe.title} top />
        ) : (
          <Backdrop emoji={recipeEmoji(recipe.title)} {...RECIPE_BACKDROP} seed={recipe.id} />
        )}
        <CookStamp recipe={recipe} />
        {recipe.images.length > 1 && (
          <span className="stamp stamp--br" role="img" aria-label={`${recipe.images.length} фото`}>
            <IconImages size={14} strokeWidth={2.2} />
            <span className="num" aria-hidden="true">
              {recipe.images.length}
            </span>
          </span>
        )}
      </span>
      <span className="card__body">
        <span className="card__title">{glueShortWords(recipe.title)}</span>
        {tagLine && <span className="card__tags">{tagLine}</span>}
        {line && <span className="card__excerpt">{line}</span>}
        {host && (
          <span className="card__excerpt card__excerpt--link">
            <IconLink size={14} strokeWidth={2.2} />
            <span>{host}</span>
          </span>
        )}
      </span>
    </button>
  )
}
