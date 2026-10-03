import { useEffect, useRef, useState } from 'react'
import { ApiError } from '../../api/errors'
import { IMPORT_STAGE_MS, IMPORT_TEXT_MAX, checkImport, importFailure, importStages, type ImportMode } from '../../lib/importer'
import { useData } from '../../state/data'
import { useNav } from '../../state/nav'
import { useStillOnTop } from '../../state/screen'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { TextArea, TextField } from '../../ui/fields'
import { IconNotebook, IconPaste } from '../../ui/icons'
import { Cell, IconTile, Section } from '../../ui/layout'
import { Sheet } from '../../ui/Sheet'
import { dismissKeyboard } from '../shared/forms'

type Step = 'choose' | 'import'

/**
 * «+» on the recipes screen: write a recipe by hand, or import one from an
 * Instagram post (a link, or the caption pasted as text).
 */
export function AddRecipeSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const nav = useNav()
  const [session, setSession] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  const [step, setStep] = useState<Step>('choose')
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) {
      setSession((s) => s + 1)
      setStep('choose')
    }
  }
  return (
    <Sheet open={open} onClose={onClose} title={step === 'choose' ? 'Новый рецепт' : 'Рецепт из Instagram'}>
      {step === 'choose' ? (
        <div className="add-recipe">
          <Section>
            <Cell
              before={
                <IconTile tone="orange">
                  <IconNotebook size={18} strokeWidth={2.1} />
                </IconTile>
              }
              title="Написать рецепт"
              subtitle="Ингредиенты, шаги, фото"
              chevron
              onClick={() => {
                onClose()
                nav.push({ name: 'recipe-form' })
              }}
            />
            <Cell
              before={
                <IconTile tone="rose">
                  <IconPaste size={18} strokeWidth={2.1} />
                </IconTile>
              }
              title="Из Instagram"
              subtitle="Ссылка на пост или текст подписи"
              chevron
              onClick={() => setStep('import')}
            />
          </Section>
        </div>
      ) : (
        <ImportForm key={session} active={open} onClose={onClose} />
      )}
    </Sheet>
  )
}

/**
 * Link (or text) → «Разобрать». The recipe is saved by the server right
 * away and opens in the edit form to be checked; the partner hears about it
 * only after a few quiet minutes.
 */
function ImportForm({ active, onClose }: { active: boolean; onClose: () => void }) {
  const data = useData()
  const nav = useNav()
  const tg = useTelegram()
  const toast = useToast()
  const stillOnTop = useStillOnTop()
  const [mode, setMode] = useState<ImportMode>('link')
  const [link, setLink] = useState('')
  const [text, setText] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [stage, setStage] = useState(0)
  const [shake, setShake] = useState(0)
  const timer = useRef(0)
  const value = mode === 'link' ? link : text
  const stages = importStages(mode)

  useEffect(() => () => window.clearInterval(timer.current), [])

  const fail = (message: string) => {
    setError(message)
    setShake((n) => n + 1)
    tg.haptic.notify('error')
  }

  const switchMode = (next: ImportMode) => {
    tg.haptic.selection()
    setMode(next)
    setError(null)
  }

  const submit = async () => {
    if (busy) return
    tg.hideKeyboard()
    const check = checkImport(mode, value)
    if (!check.ok) return fail(check.message)
    setError(null)
    setNotice(null)
    setBusy(true)
    setStage(0)
    window.clearInterval(timer.current)
    timer.current = window.setInterval(() => setStage((n) => Math.min(n + 1, stages.length - 1)), IMPORT_STAGE_MS)
    try {
      const { recipe, import: report } = await data.api.importRecipe(check.input)
      data.putRecipe(recipe)
      tg.haptic.notify('success')
      onClose()
      if (!stillOnTop()) return
      nav.push({ name: 'recipe', id: recipe.id })
      if (report.duplicate) toast(`Уже есть: «${recipe.title}»`)
      else nav.push({ name: 'recipe-form', id: recipe.id, imported: { warnings: report.warnings } })
    } catch (err) {
      if (!(err instanceof ApiError) || err.isAuth) return
      const next = importFailure(err, mode)
      if (next.kind === 'paste-text') {
        tg.haptic.notify('warning')
        setMode('text')
        setNotice(next.message)
      } else fail(next.message)
    } finally {
      window.clearInterval(timer.current)
      setBusy(false)
    }
  }

  useMainButton(active ? { text: 'Разобрать', active: value.trim() !== '', progress: busy, onClick: () => void submit() } : null)

  return (
    <form className="import-sheet" noValidate onSubmit={dismissKeyboard(tg)}>
      {notice && (
        <p className="import-sheet__notice" role="alert">
          {notice}
        </p>
      )}
      {mode === 'link' ? (
        <TextField
          label="Ссылка на пост или рилс"
          type="url"
          inputMode="url"
          placeholder="https://www.instagram.com/reel/…"
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          autoComplete="off"
          enterKeyHint="go"
          value={link}
          disabled={busy}
          error={error ?? undefined}
          shakeKey={shake}
          hint="Разложим название, ингредиенты, шаги и обложку"
          onChange={(e) => {
            setLink(e.target.value)
            setError(null)
          }}
        />
      ) : (
        <TextArea
          label="Текст рецепта"
          placeholder={'Скопируйте подпись к посту: название, ингредиенты с количеством, шаги'}
          value={text}
          disabled={busy}
          error={error ?? undefined}
          shakeKey={shake}
          max={IMPORT_TEXT_MAX}
          counterFrom={IMPORT_TEXT_MAX - 500}
          minRows={6}
          onChange={(e) => {
            setText(e.target.value)
            setError(null)
          }}
        />
      )}
      {busy ? (
        <p className="import-sheet__progress" aria-live="polite">
          <span className="spinner" aria-hidden="true" />
          {stages[stage] ?? stages.at(-1)}
        </p>
      ) : (
        <div className="import-sheet__actions">
          <button type="button" className="text-btn" onClick={() => switchMode(mode === 'link' ? 'text' : 'link')}>
            {mode === 'link' ? 'Вставить текст' : 'Вставить ссылку'}
          </button>
        </div>
      )}
      <p className="import-sheet__note">Рецепт сохранится сразу — потом его можно поправить или удалить.</p>
    </form>
  )
}
