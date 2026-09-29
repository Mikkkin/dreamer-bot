import { useMemo, useState } from 'react'
import type { RecipeTag, TagKind } from '../../api/types'
import { RECIPE_BACKDROP } from '../../lib/categoryStyle'
import { countByTag, tagsOfKind } from '../../lib/recipes'
import { useData } from '../../state/data'
import { useTelegram } from '../../telegram/hooks'
import { Backdrop } from '../../ui/art'
import { IconPlus } from '../../ui/icons'
import { Cell, IconTile, Section } from '../../ui/layout'
import { KIND_COPY, TagSheet } from './TagSheet'

const GROUPS: readonly { kind: TagKind; header: string }[] = [
  { kind: 'cuisine', header: 'Кухни' },
  { kind: 'course', header: 'Типы блюд' },
]

/** The tags manager: cuisines and courses, edited like wish categories. */
export function RecipeTags() {
  const data = useData()
  const tg = useTelegram()
  const counts = useMemo(() => countByTag(data.recipes), [data.recipes])
  const [sheet, setSheet] = useState<{ open: boolean; tag: RecipeTag | null; kind: TagKind }>({
    open: false,
    tag: null,
    kind: 'cuisine',
  })

  const open = (tag: RecipeTag | null, kind: TagKind) => {
    tg.haptic.impact('light')
    setSheet({ open: true, tag, kind })
  }
  const close = () => setSheet((s) => ({ ...s, open: false }))

  return (
    <div className="page">
      <h1 className="page__title">Кухни и типы блюд</h1>
      {GROUPS.map((g, i) => (
        <Section
          key={g.kind}
          header={g.header}
          footer={i === GROUPS.length - 1 ? 'Если удалить тег, рецепты останутся — просто без него.' : undefined}
        >
          <Cell
            before={
              <IconTile tone="accent" style={{ width: 40, height: 40, borderRadius: 12 }}>
                <IconPlus size={22} strokeWidth={2.2} />
              </IconTile>
            }
            title={KIND_COPY[g.kind].create}
            tone="accent"
            onClick={() => open(null, g.kind)}
          />
          {tagsOfKind(data.tags, g.kind).map((t) => (
            <Cell
              key={t.id}
              before={
                <span className="emoji-tile">
                  <Backdrop emoji={t.emoji} {...RECIPE_BACKDROP} size="tile" />
                </span>
              }
              title={t.name}
              after={<span className="num">{counts.get(t.id) ?? 0}</span>}
              chevron
              onClick={() => open(t, t.kind)}
            />
          ))}
        </Section>
      ))}
      <TagSheet open={sheet.open} tag={sheet.tag} kind={sheet.kind} onClose={close} onSaved={close} />
    </div>
  )
}
