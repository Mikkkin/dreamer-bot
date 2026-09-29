import { useMemo, useState } from 'react'
import { ApiError } from '../../api/errors'
import type { Recipe, RecipeInput, RecipeTag, TagKind } from '../../api/types'
import { checkIngredients, emptyRow, ingredientRows, type IngredientRow, type RowErrors } from '../../lib/ingredients'
import { linkHost, normalizeLinkInput } from '../../lib/links'
import { checkNutrition, nutritionDraft, type NutritionDraft, type NutritionErrors } from '../../lib/nutrition'
import { tagMap, tagsOfKind, toggleCourse } from '../../lib/recipes'
import { usePhotoDraft } from '../../media/usePhotoDraft'
import { useData, useMe } from '../../state/data'
import { useNav } from '../../state/nav'
import { useStillOnTop } from '../../state/screen'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { Chip, ChipGroup, SwitchRow } from '../../ui/controls'
import { TextArea, TextField, charCount } from '../../ui/fields'
import { IconClipboardList, IconFlame, IconLink, IconNotebook, IconPlus } from '../../ui/icons'
import { IconTile, Section } from '../../ui/layout'
import { PhotoEditor } from '../../ui/PhotoEditor'
import { dismissKeyboard, uploadFailureText, useLeaveGuard, type FieldErrors } from '../shared/forms'
import { LoadState } from '../shared/LoadState'
import { useEntity } from '../shared/useEntity'
import { IngredientsEditor } from './IngredientsEditor'
import { NutritionEditor } from './NutritionEditor'
import { TagSheet } from './TagSheet'

interface Fields {
  title: string
  linkOn: boolean
  link: string
  bodyOn: boolean
  body: string
  cuisineId: number | null
  courseIds: number[]
  ingredientsOn: boolean
  ingredients: IngredientRow[]
  nutritionOn: boolean
  nutrition: NutritionDraft
}

type FieldKey = 'title' | 'link' | 'body' | 'cuisine_id' | 'course_ids' | 'ingredients' | 'nutrition'

const ERROR_OF: Readonly<Record<keyof Fields, FieldKey>> = {
  title: 'title',
  linkOn: 'link',
  link: 'link',
  bodyOn: 'body',
  body: 'body',
  cuisineId: 'cuisine_id',
  courseIds: 'course_ids',
  ingredientsOn: 'ingredients',
  ingredients: 'ingredients',
  nutritionOn: 'nutrition',
  nutrition: 'nutrition',
}

const NUTRITION_FIELDS = new Set(['nutrition', 'kcal', 'protein', 'fat', 'carbs', 'weight_g', 'servings'])
const INGREDIENT_FIELDS = new Set(['ingredients', 'name', 'amount', 'unit'])

/** Maps the server's error.field onto a block of the form. */
function blockOf(field: string | undefined): FieldKey | 'form' {
  if (field === undefined) return 'form'
  if (NUTRITION_FIELDS.has(field)) return 'nutrition'
  if (INGREDIENT_FIELDS.has(field)) return 'ingredients'
  if (field === 'title' || field === 'link' || field === 'body' || field === 'cuisine_id' || field === 'course_ids') return field
  return 'form'
}

function initialFields(recipe: Recipe | null): Fields {
  return {
    title: recipe?.title ?? '',
    linkOn: recipe?.link != null,
    link: recipe?.link ?? '',
    bodyOn: (recipe?.body ?? '') !== '',
    body: recipe?.body ?? '',
    cuisineId: recipe?.cuisine_id ?? null,
    courseIds: recipe?.course_ids ?? [],
    ingredientsOn: (recipe?.ingredients.length ?? 0) > 0,
    ingredients: ingredientRows(recipe?.ingredients ?? []),
    nutritionOn: recipe?.nutrition != null,
    nutrition: nutritionDraft(recipe?.nutrition ?? null),
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
  const me = useMe()
  const { limits } = me
  const nav = useNav()
  const tg = useTelegram()
  const toast = useToast()

  const [initial] = useState(() => initialFields(recipe))
  const [fields, setFields] = useState(initial)
  const [errors, setErrors] = useState<FieldErrors<FieldKey>>({})
  const [rowErrors, setRowErrors] = useState<RowErrors>({})
  const [nutritionErrors, setNutritionErrors] = useState<NutritionErrors>({})
  const [shake, setShake] = useState(0)
  const [saving, setSaving] = useState(false)
  const [savedId, setSavedId] = useState<number | null>(recipe?.id ?? null)
  const [tagSheet, setTagSheet] = useState<{ open: boolean; kind: TagKind }>({ open: false, kind: 'cuisine' })
  const photos = usePhotoDraft(recipe?.images ?? [], limits.images_per_recipe)
  const stillOnTop = useStillOnTop()

  const byId = useMemo(() => tagMap(data.tags), [data.tags])
  const cuisines = tagsOfKind(data.tags, 'cuisine')
  const courses = tagsOfKind(data.tags, 'course')

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

    // A tag deleted meanwhile (by the partner) is dropped rather than rejected.
    const cuisineId = fields.cuisineId !== null && byId.get(fields.cuisineId)?.kind === 'cuisine' ? fields.cuisineId : null
    const courseIds = fields.courseIds.filter((cid) => byId.get(cid)?.kind === 'course')
    if (courseIds.length > limits.courses_per_recipe) errs.course_ids = `Не больше ${limits.courses_per_recipe} типов блюда`

    let ingredients: RecipeInput['ingredients'] = []
    if (fields.ingredientsOn) {
      const r = checkIngredients(fields.ingredients, me.units, { nameMax: limits.item_name_max, max: limits.ingredients_per_recipe })
      setRowErrors(r.ok ? {} : r.errors)
      if (r.ok) ingredients = r.ingredients
      else errs.ingredients = r.message ?? 'Проверьте ингредиенты'
    } else setRowErrors({})

    let nutrition: RecipeInput['nutrition'] = null
    if (fields.nutritionOn) {
      const r = checkNutrition(fields.nutrition, limits)
      setNutritionErrors(r.ok ? {} : r.errors)
      if (r.ok) nutrition = r.input
      else errs.nutrition = 'Проверьте КБЖУ'
    } else setNutritionErrors({})

    if (Object.keys(errs).length > 0) return { errors: errs }
    return { input: { title, link, body, cuisine_id: cuisineId, course_ids: courseIds, ingredients, nutrition } }
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
      fail({ [blockOf(err.field)]: err.message })
    } finally {
      setSaving(false)
    }
  }

  useMainButton({ text: 'Сохранить', active: fields.title.trim() !== '', progress: saving, onClick: () => void save() })

  const linkCheck = fields.linkOn && fields.link.trim() !== '' ? normalizeLinkInput(fields.link, limits.link_max) : null
  const host = linkCheck?.ok ? linkHost(linkCheck.url) : ''

  const pickCourse = (tagId: number) => {
    const r = toggleCourse(fields.courseIds, tagId, limits.courses_per_recipe)
    if (r.limited) {
      tg.haptic.notify('warning')
      toast(`Не больше ${limits.courses_per_recipe} типов блюда`, { tone: 'error' })
      return
    }
    set('courseIds', r.ids)
  }

  const onTagCreated = (tag: RecipeTag) => {
    if (tag.kind === 'cuisine') set('cuisineId', tag.id)
    else if (!fields.courseIds.includes(tag.id)) pickCourse(tag.id)
    setTagSheet((s) => ({ ...s, open: false }))
  }

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

      <Section header="Кухня" footer={errors.cuisine_id}>
        <div className="section__pad">
          <ChipGroup label="Кухня" wrap>
            <Chip selected={fields.cuisineId === null} onSelect={() => set('cuisineId', null)}>
              Не указана
            </Chip>
            {cuisines.map((t) => (
              <Chip key={t.id} selected={fields.cuisineId === t.id} emoji={t.emoji} onSelect={() => set('cuisineId', t.id)}>
                {t.name}
              </Chip>
            ))}
            <Chip action onSelect={() => setTagSheet({ open: true, kind: 'cuisine' })}>
              <IconPlus size={16} strokeWidth={2.4} />
              Своя
            </Chip>
          </ChipGroup>
        </div>
      </Section>

      <Section
        header={
          <>
            Тип блюда
            {fields.courseIds.length > 0 && (
              <span className="section__count num">
                {fields.courseIds.length}/{limits.courses_per_recipe}
              </span>
            )}
          </>
        }
        footer={errors.course_ids ?? 'Можно выбрать несколько: например, «Ужин» и «Второе».'}
      >
        <div className="section__pad">
          <ChipGroup label="Тип блюда" wrap multi>
            {courses.map((t) => (
              <Chip key={t.id} multi selected={fields.courseIds.includes(t.id)} emoji={t.emoji} onSelect={() => pickCourse(t.id)}>
                {t.name}
              </Chip>
            ))}
            <Chip action onSelect={() => setTagSheet({ open: true, kind: 'course' })}>
              <IconPlus size={16} strokeWidth={2.4} />
              Свой
            </Chip>
          </ChipGroup>
        </div>
      </Section>

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
          label="Ингредиенты"
          before={
            <IconTile tone="green">
              <IconClipboardList size={18} strokeWidth={2.1} />
            </IconTile>
          }
          checked={fields.ingredientsOn}
          onChange={(on) => {
            setFields((f) => ({
              ...f,
              ingredientsOn: on,
              ingredients: on && f.ingredients.length === 0 ? [emptyRow([])] : f.ingredients,
            }))
            setErrors((e) => ({ ...e, ingredients: undefined }))
          }}
        >
          <IngredientsEditor
            rows={fields.ingredients}
            units={me.units}
            max={limits.ingredients_per_recipe}
            nameMax={limits.item_name_max}
            errors={rowErrors}
            error={errors.ingredients}
            onChange={(rows) => {
              set('ingredients', rows)
              setRowErrors({})
            }}
          />
        </SwitchRow>
        <SwitchRow
          label="КБЖУ"
          before={
            <IconTile tone="amber">
              <IconFlame size={18} strokeWidth={2.1} />
            </IconTile>
          }
          checked={fields.nutritionOn}
          onChange={(on) => set('nutritionOn', on)}
        >
          <NutritionEditor
            draft={fields.nutrition}
            limits={limits}
            errors={nutritionErrors}
            error={errors.nutrition}
            onChange={(draft) => {
              set('nutrition', draft)
              setNutritionErrors({})
            }}
          />
        </SwitchRow>
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
          label="Как готовить"
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
            placeholder={'Шаги по порядку.\n1. …'}
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

      <TagSheet
        open={tagSheet.open}
        tag={null}
        kind={tagSheet.kind}
        onClose={() => setTagSheet((s) => ({ ...s, open: false }))}
        onSaved={onTagCreated}
      />
    </form>
  )
}

