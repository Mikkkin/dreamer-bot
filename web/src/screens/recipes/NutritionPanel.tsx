import { useState } from 'react'
import type { Macros, Nutrition } from '../../api/types'
import { SERVING_FORMS, countOf } from '../../lib/format'
import { MACRO_KEYS, MACRO_LABEL, formatApiDecimal } from '../../lib/nutrition'
import { Segmented } from '../../ui/controls'
import { Section } from '../../ui/layout'

type View = 'per100' | 'serving' | 'dish'

const VIEW_LABEL: Readonly<Record<View, string>> = { per100: 'на 100 г', serving: 'порция', dish: 'всё блюдо' }

function valuesFor(n: Nutrition, view: View): Macros | null {
  switch (view) {
    case 'per100':
      return n.per_100g
    case 'serving':
      return n.per_serving
    case 'dish':
      return n.per_dish
  }
}

/** «КБЖУ»: four tiles, switchable between per 100 g, one serving and the whole dish. */
export function NutritionPanel({ nutrition }: { nutrition: Nutrition }) {
  const views = (['per100', 'serving', 'dish'] as const).filter((v) => valuesFor(nutrition, v) !== null)
  const [picked, setView] = useState<View>(nutrition.per_serving ? 'serving' : 'per100')
  const view = views.includes(picked) ? picked : 'per100'
  const values = valuesFor(nutrition, view) ?? nutrition.per_100g

  const facts = [
    nutrition.weight_g ? `Вес блюда ${nutrition.weight_g} г` : '',
    nutrition.servings ? countOf(nutrition.servings, SERVING_FORMS) : '',
  ].filter(Boolean)

  return (
    <Section header={views.length > 1 ? 'КБЖУ' : 'КБЖУ на 100 г'} footer={facts.length > 0 ? facts.join(' · ') : undefined}>
      {views.length > 1 && (
        <div className="section__pad nutrition__switch">
          <Segmented label="КБЖУ" value={view} onChange={setView} options={views.map((v) => ({ value: v, label: VIEW_LABEL[v] }))} />
        </div>
      )}
      <ul className="macros" key={view}>
        {MACRO_KEYS.map((key, i) => {
          const label = MACRO_LABEL[key]
          const value = formatApiDecimal(values[key])
          return (
            <li
              key={key}
              className={`macro macro--${key}`}
              style={{ animationDelay: `${i * 30}ms` }}
              aria-label={`${label.long}: ${value} ${label.unit}`}
            >
              <span className="macro__letter" aria-hidden="true">
                {label.short}
              </span>
              <span className="macro__value num" aria-hidden="true">
                {value}
                {key !== 'kcal' && <span className="macro__unit"> {label.unit}</span>}
              </span>
              <span className="macro__label" aria-hidden="true">
                {label.abbr}
              </span>
            </li>
          )
        })}
      </ul>
    </Section>
  )
}
