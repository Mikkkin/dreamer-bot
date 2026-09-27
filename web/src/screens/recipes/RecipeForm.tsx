import { useMemo, useState } from 'react'
import { ApiError } from '../../api/errors'
import type { Recipe, RecipeInput } from '../../api/types'
import { linkHost, normalizeLinkInput } from '../../lib/links'
import { usePhotoDraft } from '../../media/usePhotoDraft'
import { useData, useMe } from '../../state/data'
import { useNav } from '../../state/nav'
import { useStillOnTop } from '../../state/screen'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { SwitchRow } from '../../ui/controls'
import { TextArea, TextField, charCount } from '../../ui/fields'
import { IconLink, IconNotebook } from '../../ui/icons'
import { IconTile, Section } from '../../ui/layout'
import { PhotoEditor } from '../../ui/PhotoEditor'
import { dismissKeyboard, fieldOf, uploadFailureText, useLeaveGuard, type FieldErrors } from '../shared/forms'
import { LoadState } from '../shared/LoadState'
import { useEntity } from '../shared/useEntity'

interface Fields {
  title: string
  linkOn: boolean
  link: string
  bodyOn: boolean
  body: string
}

const FIELD_KEYS = ['title', 'link', 'body'] as const
type FieldKey = (typeof FIELD_KEYS)[number]

const ERROR_OF: Readonly<Record<keyof Fields, FieldKey>> = {
  title: 'title',
  linkOn: 'link',
  link: 'link',
  bodyOn: 'body',
  body: 'body',
}

function initialFields(recipe: Recipe | null): Fields {
  return {
    title: recipe?.title ?? '',
    linkOn: recipe?.link != null,
    link: recipe?.link ?? '',
    bodyOn: (recipe?.body ?? '') !== '',
    body: recipe?.body ?? '',
  }
}

export function RecipeForm({ id }: { id?: number }) {
  const data = useData()
  const lookup = useEntity(data.recipes, id ?? null, (recipeId) => data.api.recipe(recipeId))
  if (id === undefined) return <RecipeFormBody recipe={null} />
  if (!lookup.entity) return <LoadState lookup={lookup} missingTitle="Рецепт не найден" missingText="Возможно, его уже удалили." />
  return <RecipeFormBody recipe={lookup.entity} />
}

function RecipeFormBody({ recipe }: { recipe: Recipe | null }) {
  const data = useData()
  const { limits } = useMe()
  const nav = useNav()
  const tg = useTelegram()
  const toast = useToast()

  const [initial] = useState(() => initialFields(recipe))
  const [fields, setFields] = useState(initial)
  const [errors, setErrors] = useState<FieldErrors<FieldKey>>({})
  const [shake, setShake] = useState(0)
  const [saving, setSaving] = useState(false)
  const [savedId, setSavedId] = useState<number | null>(recipe?.id ?? null)
  const photos = usePhotoDraft(recipe?.images ?? [], limits.images_per_recipe)
  const stillOnTop = useStillOnTop()

  const set = <K extends keyof Fields>(key: K, value: Fields[K]) => {
    setFields((f) => ({ ...f, [key]: value }))
    const errorKey = ERROR_OF[key]
    setErrors((e) => (e[errorKey] ? { ...e, [errorKey]: undefined } : e))
  }

  const dirty = useMemo(() => JSON.stringify(fields) !== JSON.stringify(initial), [fields, initial]) || photos.dirty
  useLeaveGuard(dirty, saving)

  const fail = (next: FieldErrors<FieldKey>) => {
    setErrors(next)
    setShake((n) => n + 1)
    tg.haptic.notify('error')
  }

  const validate = (): { input: RecipeInput } | { errors: FieldErrors<FieldKey> } => {
    const errs: FieldErrors<FieldKey> = {}
    const title = fields.title.trim()
    if (title === '') errs.title = 'Название обязательно'
    else if (charCount(title) > limits.title_max) errs.title = `Не длиннее ${limits.title_max} символов`

    let link: string | null = null
    if (fields.linkOn) {
      const r = normalizeLinkInput(fields.link, limits.link_max)
      if (r.ok) link = r.url
      else errs.link = r.message
    }
    const body = fields.bodyOn ? fields.body.trim() : ''
    if (charCount(body) > limits.recipe_body_max) errs.body = `Не длиннее ${limits.recipe_body_max} символов`

    if (Object.keys(errs).length > 0) return { errors: errs }
    return { input: { title, link, body } }
  }

  const save = async () => {
    if (saving) return
    tg.hideKeyboard()
    const result = validate()
    if ('errors' in result) return fail(result.errors)
    setErrors({})
    setSaving(true)
    try {
      const saved = savedId === null ? await data.api.createRecipe(result.input) : await data.api.updateRecipe(savedId, result.input)
      setSavedId(saved.id)
      data.putRecipe(saved)

      const failures = await photos.sync(data.api, 'recipes', saved.id)
      await data.refresh('recipes')
      if (failures.length > 0) {
        tg.haptic.notify('error')
        await tg.alert(uploadFailureText(failures, limits.image_max_bytes))
        return
      }
      tg.haptic.notify('success')
      if (stillOnTop()) nav.pop()
      toast('Сохранено', { tone: 'success' })
    } catch (err) {
      if (!(err instanceof ApiError) || err.isAuth) return
      fail({ [fieldOf(err.field, FIELD_KEYS)]: err.message })
    } finally {
      setSaving(false)
    }
  }

  useMainButton({ text: 'Сохранить', active: fields.title.trim() !== '', progress: saving, onClick: () => void save() })

  const linkCheck = fields.linkOn && fields.link.trim() !== '' ? normalizeLinkInput(fields.link, limits.link_max) : null
  const host = linkCheck?.ok ? linkHost(linkCheck.url) : ''

  return (
    <form className="form" noValidate onSubmit={dismissKeyboard(tg)}>
      <h1 className="form__title">{recipe ? 'Изменить рецепт' : 'Новый рецепт'}</h1>

      <TextField
        label="Название"
        placeholder="Например, паста карбонара"
        value={fields.title}
        onChange={(e) => set('title', e.target.value)}
        error={errors.title}
        shakeKey={shake}
        max={limits.title_max}
        counterFrom={limits.title_max - 20}
        autoComplete="off"
        enterKeyHint="done"
      />

      <PhotoEditor
        label="Скриншоты и фото"
        draft={photos}
        max={limits.images_per_recipe}
        alt={fields.title || 'Рецепт'}
        emptyTitle="Добавить скриншоты"
        emptyHint={`Рецепт из соцсетей или фото блюда · до ${limits.images_per_recipe}`}
        disabled={saving}
      />

      <Section header="Детали">
        <SwitchRow
          label="Ссылка на рецепт"
          before={
            <IconTile tone="accent">
              <IconLink size={18} strokeWidth={2.1} />
            </IconTile>
          }
          checked={fields.linkOn}
          onChange={(on) => set('linkOn', on)}
        >
          <TextField
            label="Ссылка"
            type="url"
            inputMode="url"
            placeholder="https://"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            autoComplete="off"
            value={fields.link}
            onChange={(e) => set('link', e.target.value)}
            error={errors.link}
            shakeKey={shake}
            hint={host ? `Откроется: ${host}` : undefined}
            enterKeyHint="done"
          />
        </SwitchRow>
        <SwitchRow
          label="Написать рецепт"
          before={
            <IconTile tone="orange">
              <IconNotebook size={18} strokeWidth={2.1} />
            </IconTile>
          }
          checked={fields.bodyOn}
          onChange={(on) => set('bodyOn', on)}
        >
          <TextArea
            label="Рецепт"
            placeholder={'Ингредиенты и шаги.\n1. …'}
            value={fields.body}
            onChange={(e) => set('body', e.target.value)}
            error={errors.body}
            shakeKey={shake}
            max={limits.recipe_body_max}
            counterFrom={limits.recipe_body_max - 500}
            minRows={5}
          />
        </SwitchRow>
      </Section>

      {errors.form && (
        <p className="form__error" role="alert">
          {errors.form}
        </p>
      )}
    </form>
  )
}
