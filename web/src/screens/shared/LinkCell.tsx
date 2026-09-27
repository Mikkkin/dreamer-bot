import { isSafeHttpUrl, linkHost } from '../../lib/links'
import { useTelegram } from '../../telegram/hooks'
import { IconExternal, IconLink } from '../../ui/icons'
import { Cell, IconTile, Section } from '../../ui/layout'

/** Shows a user link as text; it opens only through Telegram's in-app browser after a scheme check. */
export function LinkCell({ url, title }: { url: string; title: string }) {
  const tg = useTelegram()
  const safe = isSafeHttpUrl(url)
  const open = () => {
    tg.haptic.impact('light')
    tg.openLink(url)
  }
  return (
    <Section>
      <Cell
        before={
          <IconTile>
            <IconLink size={18} strokeWidth={2.1} />
          </IconTile>
        }
        title={title}
        subtitle={linkHost(url) || url}
        chevron={safe ? <IconExternal size={18} strokeWidth={2.1} /> : false}
        onClick={safe ? open : undefined}
      />
    </Section>
  )
}
