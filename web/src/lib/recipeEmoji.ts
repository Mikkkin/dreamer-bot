import { foldText } from './search'

// A cover sticker for a recipe without photos, picked from keywords in its
// title, so every recipe gets its own art. The first match wins.

/** JavaScript's \b only knows ASCII letters; these guard Cyrillic word edges (no lookbehind: older WebViews lack it). */
const B = '(?:^|[^а-я])'
const E = '(?![а-я])'

const MAP: readonly (readonly [RegExp, string])[] = [
  [/паст|спагет|карбонар|макарон|лазань|феттучин|пенне/, '🍝'],
  [/сырник|блин|олад|панкейк|вафл/, '🥞'],
  [new RegExp(`суп|борщ|${B}щи${E}|солянк|бульон|рамен|${B}фо${E}|${B}уха${E}`), '🍲'],
  [/салат|боул|цезар/, '🥗'],
  [/пицц/, '🍕'],
  [/торт|пирог|кекс|печень|десерт|брауни|чизкейк|тирамису|маффин/, '🍰'],
  [/куриц|курин|цыпл|крыл|индейк/, '🍗'],
  [/стейк|говя|свин|котлет|фарш|мяс|шашлык/, '🥩'],
  [/рыб|лосос|семг|тунец|форел|сибас|дорадо/, '🐟'],
  [/кревет|морепр|мидии|кальмар/, '🍤'],
  [new RegExp(`плов|ризотто|${B}рис(?:а|ом|у)?${E}|суши|роллы`), '🍚'],
  [/омлет|яичниц|шакшук|яйц/, '🍳'],
  [/хлеб|булк|багет|фокачч|круассан/, '🍞'],
  [/бургер|сэндвич|сендвич/, '🍔'],
  [/тако|буррито|шаурм|шаверм/, '🌯'],
  [/смузи|коктейл|лимонад/, '🥤'],
  [/кофе|латте|капучин|какао/, '☕'],
  [/картоф|пюре|драник/, '🥔'],
  [/овощ|рагу|брокк|кабач|баклаж/, '🥦'],
  [/пельмен|вареник|хинкал|дамплинг|манты/, '🥟'],
]

export const DEFAULT_RECIPE_EMOJI = '🍳'

export function recipeEmoji(title: string): string {
  const t = foldText(title)
  for (const [re, emoji] of MAP) if (re.test(t)) return emoji
  return DEFAULT_RECIPE_EMOJI
}
