import { useCallback, useLayoutEffect, useState, type ReactNode } from 'react'
import { parseDeepLink } from './lib/deeplink'
import { readPref } from './lib/prefs'
import { Categories } from './screens/categories/Categories'
import { TelegramFatal, FatalScreen } from './screens/Fatal'
import { Home, HomeSkeleton, SECTION_PREF } from './screens/Home'
import { RecipeDetail } from './screens/recipes/RecipeDetail'
import { RecipeForm } from './screens/recipes/RecipeForm'
import { Stats } from './screens/Stats'
import { Viewer } from './screens/Viewer'
import { WishDetail } from './screens/wishes/WishDetail'
import { WishForm } from './screens/wishes/WishForm'
import { DataProvider, useData, type Fatal } from './state/data'
import { Navigator, type Route, type Section } from './state/nav'
import { ToastProvider } from './state/toast'
import type { TelegramBridge } from './telegram/bridge'
import { TelegramContext, useBackVisible, useTelegram } from './telegram/hooks'
import { RetryBanner } from './ui/layout'

export function App({ tg }: { tg: TelegramBridge | null }) {
  if (!tg) return <FatalScreen kind="outside" />
  return (
    <TelegramContext value={tg}>
      <Root tg={tg} />
    </TelegramContext>
  )
}

function Root({ tg }: { tg: TelegramBridge }) {
  const [fatal, setFatal] = useState<Fatal | null>(null)
  const watchActivation = useCallback((handler: () => void) => tg.onActivated(handler), [tg])

  // Runs once the first frame (the skeleton) is in the DOM.
  useLayoutEffect(() => tg.ready(), [tg])

  if (!tg.initData) return <TelegramFatal kind="outside" />
  if (fatal) return <TelegramFatal kind={fatal} />
  return (
    <ToastProvider>
      <DataProvider initData={tg.initData} onFatal={setFatal} watchActivation={watchActivation}>
        <Shell />
      </DataProvider>
    </ToastProvider>
  )
}

function Shell() {
  const data = useData()
  const tg = useTelegram()
  const [initial] = useState(() => bootRoutes(tg.startParam))

  if (data.phase !== 'ready') {
    return (
      <>
        {data.phase === 'failed' && data.error && (
          <div className="boot-banner">
            <RetryBanner error={data.error} onRetry={() => void data.reload()} />
          </div>
        )}
        <HomeSkeleton />
      </>
    )
  }
  return (
    <Navigator initial={initial} renderRoute={renderRoute}>
      <BrowserBack />
    </Navigator>
  )
}

/** The home of the right section, plus the deep-linked item on top so Back returns home. */
function bootRoutes(startParam: string | undefined): Route[] {
  const link = parseDeepLink(window.location.search, startParam)
  const section: Section = link ? (link.kind === 'wish' ? 'wishes' : 'recipes') : readPref(SECTION_PREF) === 'recipes' ? 'recipes' : 'wishes'
  const home: Route = { name: 'home', section }
  if (!link) return [home]
  return [home, link.kind === 'wish' ? { name: 'wish', id: link.id } : { name: 'recipe', id: link.id }]
}

function renderRoute(route: Route): ReactNode {
  switch (route.name) {
    case 'home':
      return <Home section={route.section} />
    case 'wish':
      return <WishDetail id={route.id} />
    case 'wish-form':
      return <WishForm id={route.id} categoryId={route.categoryId} />
    case 'recipe':
      return <RecipeDetail id={route.id} random={route.random} />
    case 'recipe-form':
      return <RecipeForm id={route.id} />
    case 'viewer':
      return <Viewer images={route.images} start={route.start} title={route.title} />
    case 'stats':
      return <Stats />
    case 'categories':
      return <Categories />
  }
}

/** In a plain browser (local development) there is no native BackButton; mirror it in the page. */
function BrowserBack() {
  const tg = useTelegram()
  const visible = useBackVisible()
  if (tg.native || !visible) return null
  return (
    <button type="button" className="browser-back" onClick={() => tg.back.trigger()}>
      ‹ Назад
    </button>
  )
}
