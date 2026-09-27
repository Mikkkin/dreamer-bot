import type { CSSProperties } from 'react'
import type { Recipe } from '../../api/types'
import { RECIPE_BACKDROP } from '../../lib/categoryStyle'
import { linkHost } from '../../lib/links'
import { recipeEmoji } from '../../lib/recipeEmoji'
import { glueShortWords } from '../../lib/typo'
import { Backdrop } from '../../ui/art'
import { IconImages, IconLink } from '../../ui/icons'
import { Img } from '../../ui/media'

function firstLine(recipe: Recipe): string {
  return recipe.body.split('\n').find((l) => l.trim() !== '')?.trim() ?? ''
}

export function RecipeCard({ recipe, index, onOpen }: { recipe: Recipe; index: number; onOpen: () => void }) {
  const cover = recipe.images[0]
  const line = firstLine(recipe)
  const host = !line && recipe.link ? linkHost(recipe.link) : ''
  const style = { '--i': Math.min(index, 8), '--bd-c': RECIPE_BACKDROP.center, '--bd-e': RECIPE_BACKDROP.edge } as CSSProperties
  return (
    <button type="button" className="card card--recipe" style={style} onClick={onOpen}>
      <span className="card__cover">
        {cover ? (
          <Img src={cover.thumb_url} alt={recipe.title} top />
        ) : (
          <Backdrop emoji={recipeEmoji(recipe.title)} {...RECIPE_BACKDROP} seed={recipe.id} />
        )}
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
