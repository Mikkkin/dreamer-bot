import { useMainButton, useTelegram } from '../telegram/hooks'
import { EmptyState } from '../ui/layout'

export type FatalKind = 'outside' | 'unauthorized' | 'forbidden'

const COPY: Record<FatalKind, { emoji: string; title: string; text: string }> = {
  outside: {
    emoji: '📱',
    title: 'Откройте через Telegram',
    text: 'Это мини-приложение работает только внутри Telegram — откройте его из чата с ботом.',
  },
  unauthorized: {
    emoji: '⏳',
    title: 'Сессия устарела',
    text: 'Откройте приложение заново из чата с ботом.',
  },
  forbidden: {
    emoji: '🔒',
    title: 'Это приватное приложение',
    text: 'Доступ есть только у двоих 💞',
  },
}

/** A full-screen state without Telegram context (the SDK did not load). */
export function FatalScreen({ kind }: { kind: FatalKind }) {
  const copy = COPY[kind]
  return (
    <main className="fatal">
      <EmptyState emoji={copy.emoji} title={copy.title} text={copy.text} />
    </main>
  )
}

/** A full-screen state inside Telegram; an expired session offers to close the app. */
export function TelegramFatal({ kind }: { kind: FatalKind }) {
  const tg = useTelegram()
  useMainButton(kind === 'unauthorized' ? { text: 'Закрыть', onClick: () => tg.close() } : null)
  return <FatalScreen kind={kind} />
}
