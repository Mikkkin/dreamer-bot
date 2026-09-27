import { useMemo, useState } from 'react'
import type { Category } from '../../api/types'
import { categoryBackdrop } from '../../lib/categoryStyle'
import { useData } from '../../state/data'
import { useTelegram } from '../../telegram/hooks'
import { Backdrop } from '../../ui/art'
import { IconPlus } from '../../ui/icons'
import { Cell, EmptyState, IconTile, Section } from '../../ui/layout'
import { countByCategory } from '../wishes/model'
import { CategorySheet } from './CategorySheet'

export function Categories() {
  const data = useData()
  const tg = useTelegram()
  const counts = useMemo(() => countByCategory(data.wishes), [data.wishes])
  const [sheet, setSheet] = useState<{ open: boolean; category: Category | null }>({ open: false, category: null })

  const open = (category: Category | null) => {
    tg.haptic.impact('light')
    setSheet({ open: true, category })
  }
  const close = () => setSheet((s) => ({ ...s, open: false }))

  return (
    <div className="page">
      <h1 className="page__title">Категории</h1>
      <Section footer="Если удалить категорию, её мечты останутся — просто без категории.">
        <Cell
          before={
            <IconTile tone="accent" style={{ width: 40, height: 40, borderRadius: 12 }}>
              <IconPlus size={22} strokeWidth={2.2} />
            </IconTile>
          }
          title="Новая категория"
          tone="accent"
          onClick={() => open(null)}
        />
        {data.categories.map((c) => (
          <Cell
            key={c.id}
            before={
              <span className="emoji-tile">
                <Backdrop emoji={c.emoji} {...categoryBackdrop(c)} size="tile" />
              </span>
            }
            title={c.name}
            after={<span className="num">{counts.get(c.id) ?? 0}</span>}
            chevron
            onClick={() => open(c)}
          />
        ))}
      </Section>
      {data.categories.length === 0 && (
        <EmptyState emoji="🗂" title="Категорий пока нет" text="Создайте первую — например, «Путешествия» или «Для дома»." />
      )}
      <CategorySheet open={sheet.open} category={sheet.category} onClose={close} onSaved={close} />
    </div>
  )
}
