import { useState, type AnimationEvent } from 'react'
import { writePref } from '../lib/prefs'
import { shoppingCounts } from '../lib/shopping'
import { everyone, personSlot, useData, useMe } from '../state/data'
import { useNav, type Section } from '../state/nav'
import { SectionTabs } from '../ui/controls'
import { IconCart, IconChart, IconFolderHeart, IconTag } from '../ui/icons'
import { IconButton, RetryBanner, Skeleton } from '../ui/layout'
import { PairAvatars } from '../ui/media'
import { RecipesHome } from './recipes/RecipesHome'
import { WishesHome } from './wishes/WishesHome'

const SECTIONS: readonly { value: Section; label: string }[] = [
  { value: 'wishes', label: 'Мечты' },
  { value: 'recipes', label: 'Рецепты' },
]

export const SECTION_PREF = 'section'

export function Home({ section: initial }: { section: Section }) {
  const [section, setSection] = useState(initial)
  // Only a section the user switches to plays the entrance (not a return from another screen).
  const [entering, setEntering] = useState<Section | null>(null)
  const nav = useNav()
  const data = useData()
  const me = useMe()

  const people = everyone(me).map((p) => ({ name: p.name, slot: personSlot(me, p.id) }))
  const names = people.map((p) => p.name).join(' и ')

  const switchTo = (next: Section) => {
    setSection(next)
    setEntering(next)
    writePref(SECTION_PREF, next)
    window.scrollTo({ top: 0 })
  }

  const endEnter = (e: AnimationEvent<HTMLDivElement>) => {
    if (e.target === e.currentTarget) setEntering(null)
  }
  const sectionClass = (s: Section) => (entering === s ? 'home__section home__section--enter' : 'home__section')

  return (
    <div className="home" data-section={section}>
      <header className="home__header">
        <h1 className="visually-hidden">{section === 'wishes' ? 'Наши мечты' : 'Наши рецепты'}</h1>
        <div className="home__titlebar">
          <div className="couple">
            <PairAvatars people={people} />
            <span className="couple__names">{names}</span>
          </div>
          <div className="home__actions">
            <IconButton label="Статистика" onClick={() => nav.push({ name: 'stats' })}>
              <IconChart size={20} strokeWidth={2.2} />
            </IconButton>
            {section === 'wishes' ? (
              <IconButton label="Категории" onClick={() => nav.push({ name: 'categories' })}>
                <IconFolderHeart size={20} />
              </IconButton>
            ) : (
              <>
                <IconButton label="Кухни и типы блюд" onClick={() => nav.push({ name: 'recipe-tags' })}>
                  <IconTag size={20} />
                </IconButton>
                <IconButton label="Покупки" badge={shoppingCounts(data.shopping).open} onClick={() => nav.push({ name: 'shopping' })}>
                  <IconCart size={20} />
                </IconButton>
              </>
            )}
          </div>
        </div>
        <SectionTabs label="Раздел" value={section} onChange={switchTo} options={SECTIONS} />
      </header>

      {data.error && <RetryBanner error={data.error} onRetry={() => void data.reload()} />}

      {/* Both sections stay mounted so filters and search survive switching. */}
      <div className={sectionClass('wishes')} hidden={section !== 'wishes'} onAnimationEnd={endEnter}>
        <WishesHome active={section === 'wishes'} />
      </div>
      <div className={sectionClass('recipes')} hidden={section !== 'recipes'} onAnimationEnd={endEnter}>
        <RecipesHome active={section === 'recipes'} />
      </div>
    </div>
  )
}

/** First paint while /api/* loads; Telegram's placeholder is dismissed right after it renders. */
export function HomeSkeleton() {
  return (
    <div className="home home--skeleton" aria-busy="true" aria-label="Загрузка">
      <header className="home__header">
        <div className="home__titlebar">
          <div className="couple">
            <Skeleton width="48px" height="28px" radius="999px" />
            <Skeleton width="110px" height="14px" radius="7px" />
          </div>
          <Skeleton width="44px" height="44px" radius="999px" />
        </div>
        <div className="section-tabs section-tabs--skeleton">
          <Skeleton width="96px" height="28px" radius="8px" />
          <Skeleton width="120px" height="28px" radius="8px" className="skeleton--dim" />
        </div>
      </header>
      <Skeleton className="hero-card hero-card--skeleton" height="84px" delay={80} />
      <div className="sticky-bar sticky-bar--static">
        <div className="sticky-bar__seg">
          <Skeleton height="44px" radius="999px" delay={160} />
        </div>
        <div className="chips chips--scroll">
          {[64, 108, 132, 96].map((w, i) => (
            <Skeleton key={w} width={`${w}px`} height="36px" radius="999px" className="chip-skeleton" delay={200 + i * 40} />
          ))}
        </div>
      </div>
      <div className="grid">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="card card--skeleton">
            <Skeleton className="card__cover" delay={240 + i * 80} />
            <span className="card__body">
              <Skeleton width="80%" height="14px" radius="6px" delay={280 + i * 80} />
              <Skeleton width="50%" height="12px" radius="6px" delay={300 + i * 80} />
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}
