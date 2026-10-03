import { useMemo, useState, type CSSProperties } from 'react'
import type { Nutrition, NutritionAuto, NutritionCoverage } from '../../api/types'
import { cx } from '../../lib/cx'
import { forServings } from '../../lib/format'
import {
  MACRO_LABEL,
  MACRO_PARTS,
  apiTenths,
  calorieShares,
  coverageLine,
  dailyPercent,
  donutArcs,
  formatKcal,
  formatTenths,
  nutritionSource,
  scaleTenths,
  topContributors,
  type MacroPart,
  type NutritionSource,
  type Tenths,
} from '../../lib/nutrition'
import { useTelegram } from '../../telegram/hooks'
import { Segmented } from '../../ui/controls'
import { IconChevron } from '../../ui/icons'
import { Section } from '../../ui/layout'
import { Sheet } from '../../ui/Sheet'
import { useCountUp } from '../../ui/useCountUp'

type View = 'serving' | 'dish' | 'per100'

const VIEW_LABEL: Readonly<Record<View, string>> = { serving: 'Порция', dish: 'Всё блюдо', per100: '100 г' }

const SIZE = 120
const STROKE = 12
const RADIUS = (SIZE - STROKE) / 2
const CIRCUMFERENCE = 2 * Math.PI * RADIUS

interface NutritionCardProps {
  nutrition: Nutrition | null
  auto: NutritionAuto | null | undefined
  /** The recipe's servings; null when unknown. */
  base: number | null
  /** The portions chosen in the scaler (equals base until changed). */
  chosen: number | null
}

/**
 * «КБЖУ»: a donut of where the calories come from, the three macros against
 * the daily reference, and a switch between one portion, the whole dish
 * (which follows the servings scaler) and 100 g. КБЖУ typed by hand wins;
 * otherwise the server's estimate from the ingredients is shown with «≈».
 */
export function NutritionCard({ nutrition, auto, base, chosen }: NutritionCardProps) {
  const source = useMemo(() => nutritionSource(nutrition, auto), [nutrition, auto])
  if (!source) return null
  return <Card source={source} base={base} chosen={chosen} />
}

function valuesFor(source: NutritionSource, view: View, base: number | null, chosen: number | null): Tenths | null {
  switch (view) {
    case 'serving':
      return source.serving
    case 'dish':
      return source.dish && base && chosen ? scaleTenths(source.dish, chosen, base) : source.dish
    case 'per100':
      return source.per100
  }
}

/** «1,2 кг» from 1200 g, «750 г» below a kilogram. */
function formatWeight(grams: number): string {
  if (grams < 1000) return `${Math.round(grams)} г`
  return `${formatTenths(Math.round(grams / 100))} кг`
}

function caption(source: NutritionSource, view: View, base: number | null, chosen: number | null): string {
  const weight = source.weightG
  switch (view) {
    case 'serving':
      return weight && base ? `1 порция · ${formatWeight(weight / base)}` : '1 порция'
    case 'dish': {
      const portions = base && chosen ? forServings(chosen) : 'всё блюдо'
      const grams = weight ? (base && chosen ? (weight * chosen) / base : weight) : null
      return grams ? `${portions} · ${formatWeight(grams)}` : portions
    }
    case 'per100':
      return 'на 100 г'
  }
}

function Card({ source, base, chosen }: { source: NutritionSource; base: number | null; chosen: number | null }) {
  const tg = useTelegram()
  const views = (['serving', 'dish', 'per100'] as const).filter((v) => valuesFor(source, v, base, chosen) !== null)
  const [picked, setPicked] = useState<View>(source.serving ? 'serving' : 'per100')
  const [focus, setFocus] = useState<MacroPart | null>(null)
  const [coverageOpen, setCoverageOpen] = useState(false)
  const view = views.includes(picked) ? picked : (views[0] ?? 'per100')
  const values = valuesFor(source, view, base, chosen) ?? source.per100
  const auto = source.kind === 'auto'
  const canExplain = auto && source.items.length > 0

  const shares = calorieShares(values)
  const arcs = donutArcs(shares, CIRCUMFERENCE, STROKE)
  const kcal = useCountUp(Number(formatKcal(values.kcal)), 400)
  const kcalDay = dailyPercent(values, 'kcal')
  const contributors = focus && canExplain ? topContributors(source.items, focus) : null

  const explain = (key: MacroPart) => {
    if (!canExplain) return
    tg.haptic.selection()
    setFocus((f) => (f === key ? null : key))
  }

  const summary = [
    `КБЖУ, ${VIEW_LABEL[view].toLocaleLowerCase('ru')}${auto ? ', примерно' : ''}`,
    `${formatKcal(values.kcal)} ккал, ${kcalDay} % дневной нормы`,
    ...MACRO_PARTS.map((k) => `${MACRO_LABEL[k].long.toLocaleLowerCase('ru')} ${formatTenths(values[k])} г`),
  ].join('; ')

  return (
    <Section
      header={
        <>
          КБЖУ
          <span className={cx('ncard__badge', auto && 'ncard__badge--auto')}>{auto ? '≈ по ингредиентам' : 'своё'}</span>
        </>
      }
    >
      {views.length > 1 && (
        <div className="section__pad ncard__switch">
          <Segmented
            label="КБЖУ на"
            value={view}
            onChange={(v) => {
              setPicked(v)
              setFocus(null)
            }}
            options={views.map((v) => ({ value: v, label: VIEW_LABEL[v] }))}
          />
        </div>
      )}
      <p className="ncard__caption">{caption(source, view, base, chosen)}</p>
      <div className="ncard" role="group" aria-label={summary}>
        <div className="ncard__donut" aria-hidden="true">
          <svg width={SIZE} height={SIZE} viewBox={`0 0 ${SIZE} ${SIZE}`} focusable="false">
            <circle className="ncard__track" cx={SIZE / 2} cy={SIZE / 2} r={RADIUS} strokeWidth={STROKE} />
            {arcs.map((arc, i) => (
              <circle
                key={arc.key}
                className={cx('ncard__arc', `ncard__arc--${arc.key}`, focus && focus !== arc.key && 'ncard__arc--dim')}
                cx={SIZE / 2}
                cy={SIZE / 2}
                r={RADIUS}
                strokeWidth={STROKE}
                strokeDasharray={`${arc.length} ${CIRCUMFERENCE}`}
                transform={`rotate(${-90 + (arc.start / CIRCUMFERENCE) * 360} ${SIZE / 2} ${SIZE / 2})`}
                style={{ '--len': `${arc.length}px`, animationDelay: `${i * 60}ms` } as CSSProperties}
                onClick={() => explain(arc.key)}
              />
            ))}
          </svg>
          <span className="ncard__center">
            <span className="ncard__kcal num">{kcal}</span>
            <span className="ncard__unit">ккал</span>
            <span className="ncard__day num">{kcalDay} % дня</span>
          </span>
        </div>
        <ul className="ncard__rows">
          {MACRO_PARTS.map((key) => {
            const pct = dailyPercent(values, key)
            const label = MACRO_LABEL[key]
            const content = (
              <>
                <span className="ncard__row-top">
                  <span className={`ncard__letter ncard__letter--${key}`} aria-hidden="true">
                    {label.short}
                  </span>
                  <span className="ncard__name">{label.long}</span>
                  <span className="ncard__grams num">{formatTenths(values[key])} г</span>
                  <span className="ncard__pct num">{pct} %</span>
                </span>
                <span className={cx('ncard__bar', pct > 100 && 'ncard__bar--over')} aria-hidden="true">
                  <span className={`ncard__fill ncard__fill--${key}`} style={{ '--p': Math.min(pct, 100) / 100 } as CSSProperties} />
                </span>
              </>
            )
            return (
              <li key={key}>
                {canExplain ? (
                  <button
                    type="button"
                    className={cx('ncard__row', focus === key && 'ncard__row--focus')}
                    aria-expanded={focus === key}
                    aria-label={`${label.long}: ${formatTenths(values[key])} г, ${pct} % дневной нормы. Показать, откуда`}
                    onClick={() => explain(key)}
                  >
                    {content}
                  </button>
                ) : (
                  <div className="ncard__row" aria-label={`${label.long}: ${formatTenths(values[key])} г, ${pct} % дневной нормы`}>
                    {content}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      </div>
      {contributors && focus && (
        <div className="ncard__why" aria-live="polite">
          <p className="ncard__why-title">
            {contributors.by === focus ? `Больше всего ${GENITIVE[focus]} дают` : 'Больше всего калорий дают'}
          </p>
          {contributors.list.length > 0 ? (
            <ol className="ncard__why-list">
              {contributors.list.map((c) => (
                <li key={c.name}>
                  <span className="ncard__why-name">{c.name}</span>
                  <span className="num">{c.percent} %</span>
                </li>
              ))}
            </ol>
          ) : (
            <p className="ncard__why-none">В посчитанных ингредиентах этого почти нет</p>
          )}
        </div>
      )}
      <p className="ncard__ref">% — доля суточной нормы: 2500 ккал, Б 75 г, Ж 83 г, У 365 г (ТР ТС 022/2011)</p>
      {source.coverage && (
        <>
          <button
            type="button"
            className="ncard__coverage"
            onClick={() => {
              tg.haptic.impact('light')
              setCoverageOpen(true)
            }}
          >
            <span>{coverageLine(source.coverage)}</span>
            <IconChevron size={16} strokeWidth={2.2} />
          </button>
          <CoverageSheet open={coverageOpen} source={source} coverage={source.coverage} onClose={() => setCoverageOpen(false)} />
        </>
      )}
    </Section>
  )
}

const GENITIVE: Readonly<Record<MacroPart, string>> = { protein: 'белков', fat: 'жиров', carbs: 'углеводов' }

/** What went into the estimate: each counted ingredient with grams and kcal, then what was left out. */
function CoverageSheet({
  open,
  source,
  coverage,
  onClose,
}: {
  open: boolean
  source: NutritionSource
  coverage: NutritionCoverage
  onClose: () => void
}) {
  const groups = [
    { title: 'Нет в таблице продуктов', names: coverage.missing },
    { title: 'Без количества или веса', names: coverage.no_amount },
    { title: 'Не влияют — «по вкусу»', names: coverage.skipped },
  ].filter((g) => g.names.length > 0)
  return (
    <Sheet open={open} onClose={onClose} title="Как посчитали КБЖУ">
      <div className="coverage-sheet">
        <p className="coverage-sheet__lead">
          Оценка по таблице продуктов: {coverage.counted} из {coverage.total} ингредиентов. Ложки и стаканы считаются без горки.
        </p>
        {source.items.length > 0 && (
          <Section header="Учтено">
            <ul className="coverage-list">
              {source.items.map((it, i) => (
                <li key={`${it.name}-${i}`} className="coverage-item">
                  <span className="coverage-item__main">
                    <span className="coverage-item__name">{it.name}</span>
                    {it.food && it.food !== it.name && <span className="coverage-item__food">как «{it.food}»</span>}
                  </span>
                  <span className="coverage-item__nums num">
                    {it.grams} г · {formatKcal(apiTenths(it.kcal) ?? 0)} ккал
                  </span>
                </li>
              ))}
            </ul>
          </Section>
        )}
        {groups.map((g) => (
          <Section key={g.title} header={g.title}>
            <ul className="coverage-list">
              {g.names.map((name, i) => (
                <li key={`${name}-${i}`} className="coverage-item coverage-item--muted">
                  <span className="coverage-item__name">{name}</span>
                </li>
              ))}
            </ul>
          </Section>
        ))}
      </div>
    </Sheet>
  )
}
