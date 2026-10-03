package recipeimport

import (
	"strings"
	"testing"
)

// These lines and captions are not in the golden corpus: they check that
// the rules generalise beyond it.

func TestParseIngredientLines(t *testing.T) {
	tests := []struct {
		line string
		want []string // "Name|amount|unit"
	}{
		{"Мука — 200 г", []string{"Мука|200|г"}},
		{"200 г муки", []string{"Мука|200|г"}},
		{"2 ст.л. сахара", []string{"Сахар|2|ст. л."}},
		{"Сахар 2 столовые ложки", []string{"Сахар|2|ст. л."}},
		{"1 1/2 стакана молока", []string{"Молоко|1.5|стакан"}},
		{"½ ч. л. соли", []string{"Соль|0.5|ч. л."}},
		{"полстакана сметаны", []string{"Сметана|0.5|стакан"}},
		{"полтора стакана кефира", []string{"Кефир|1.5|стакан"}},
		{"Яйца - 3", []string{"Яйца|3|шт"}},
		{"две луковицы", []string{"Лук|2|шт"}},
		{"4 больших помидора", []string{"Помидор|4|шт"}},
		{"Картофель 1/1,2 кг", []string{"Картофель|1|кг"}},
		{"Соль, перец — по вкусу", []string{"Соль||по вкусу", "Перец||по вкусу"}},
		{"Сахар и соль - по 1 ч.л.", []string{"Сахар|1|ч. л.", "Соль|1|ч. л."}},
		{"Сливки 33% - 200 мл", []string{"Сливки 33%|200|мл"}},
		{"Масло сливочное 82,5% — 100 г", []string{"Масло сливочное 82,5%|100|г"}},
		{"Зубчик чеснока", []string{"Чеснок|1|зубчик"}},
		{"Пучок укропа", []string{"Укроп|1|пучок"}},
		{"1 пачка творога", []string{"Творог|1|упаковка"}},
		{"Мёд по-вкусу", []string{"Мёд||по вкусу"}},
		{"Лимон — ½ шт", []string{"Лимон|0.5|шт"}},
		{"Вода 1,5 л", []string{"Вода|1.5|л"}},
		{"Куриное филе – 500 гр (или индейка)", []string{"Куриное филе|500|г"}},
		{"~300 мл тёплой воды", []string{"Тёплая вода|300|мл"}},
		{"3 яйца", []string{"Яйца|3|шт"}},
		{"2 желтка", []string{"Желток|2|шт"}},
		{"100 г сливочного масла комнатной температуры", []string{"Сливочное масло|100|г"}},
		{"Петрушка — для подачи", []string{"Петрушка||"}},
		{"Орехи (по желанию)", []string{"Орехи||"}},
		{"Шпинат - горсть", []string{"Шпинат||"}},
		{"2 горсти шпината", []string{"Шпинат|2|"}},
		{"1 головка лука", []string{"Лук|1|шт"}},
		{"Стебель сельдерея — 100 г", []string{"Стебель сельдерея|100|г"}},
		{"Оссобуко — 2 стейка", []string{"Оссобуко|2|"}},
		{"Лук репчатый — 1 крупный", []string{"Лук репчатый|1|шт"}},
		{"Перец душистый — несколько горошин", []string{"Перец душистый||"}},
		{"Кефир — 1 и 1/2 стакана", []string{"Кефир|1.5|стакан"}},
		{"1 и 1/2 стакана муки", []string{"Мука|1.5|стакан"}},
		{"2 яйца и 1/2 стакана молока", []string{"Яйца|2|шт", "Молоко|0.5|стакан"}},
		{"Сахар 1/2 стак.", []string{"Сахар|0.5|стакан"}},
		{"Мука 2 стак", []string{"Мука|2|стакан"}},
		{"2 стак. кефира", []string{"Кефир|2|стакан"}},
		// «ст.» without «л» is a cup or a tablespoon: the number stays, the
		// unit is not guessed, the name is clean.
		{"2 ст. молока", []string{"Молоко|2|"}},
		{"Мука 1 ст", []string{"Мука|1|"}},
		{"Сахар — 1 ст.", []string{"Сахар|1|"}},
		{"Время — 30 минут", nil},
		{"Цедра 1 лимона", nil},
		{"Духовка 180 градусов - 30 минут", nil},
	}
	for _, tt := range tests {
		var got []string
		for _, it := range parseLine(tt.line) {
			if !it.q.hasQuantity() && !validName(it.name) {
				continue
			}
			q := it.q.quantity()
			amount, unit := amountOf(q)
			got = append(got, it.name+"|"+amount+"|"+unit)
		}
		if tt.want == nil {
			if items, ok := (&parser{lastWasIngredient: true}).readIngredients(prepareLine(tt.line), true); ok {
				t.Errorf("%q: read as ingredients %+v", tt.line, items)
			}
			continue
		}
		if strings.Join(got, "; ") != strings.Join(tt.want, "; ") {
			t.Errorf("%q = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestParseHeldOutCaption(t *testing.T) {
	caption := `Паста карбонара на 2 порции 🍝

Что нужно:
- спагетти 200 г
- гуанчале 100 г
- 2 желтка
- пармезан 50 г
- перец по вкусу

Как готовить:
1) Отварить пасту в подсоленной воде.
2) Обжарить гуанчале до хруста.
3) Смешать желтки с сыром, соединить с пастой и гуанчале.

Подписывайтесь, чтобы не пропустить новые рецепты!
#паста #рецепт #карбонара`
	p := Parse(caption)
	if p.Title != "Паста карбонара" || p.Servings != 2 {
		t.Errorf("title/servings = %q/%d", p.Title, p.Servings)
	}
	var names []string
	for _, ing := range p.Ingredients {
		amount, unit := amountOf(ing.Quantity)
		names = append(names, ing.Name+"|"+amount+"|"+unit)
	}
	want := "Спагетти|200|г; Гуанчале|100|г; Желток|2|шт; Пармезан|50|г; Перец||по вкусу"
	if strings.Join(names, "; ") != want {
		t.Errorf("ingredients = %q", names)
	}
	if len(p.Steps) != 3 || p.Steps[0] != "Отварить пасту в подсоленной воде." {
		t.Errorf("steps = %q", p.Steps)
	}
	if p.Confidence < 0.9 {
		t.Errorf("confidence = %v", p.Confidence)
	}
}

func TestParseLowConfidenceAndRejections(t *testing.T) {
	prose := "Сегодня варила борщ: свекла, капуста, морковь и немного мяса. Варила часа два, получилось очень вкусно, всем советую!"
	if p := Parse(prose); p.Confidence >= DefaultThreshold {
		t.Errorf("prose recipe: confidence %v, want below %v (it needs the LLM)", p.Confidence, DefaultThreshold)
	}
	for _, text := range []string{
		"",
		"   \n\n ",
		"#рецепт #ужин #вкусно",
		"Привет всем! Сегодня был отличный день, гуляли в парке 🌳",
		"Chocolate cake: 200 g flour, 2 eggs, bake 30 minutes.",
		"Розыгрыш! Подпишись и поставь лайк, чтобы выиграть сборник из 100 рецептов 🎁",
	} {
		if p := Parse(text); !p.notARecipe() {
			t.Errorf("%q: not rejected (%d ingredients, %d steps, confidence %v)", text, len(p.Ingredients), len(p.Steps), p.Confidence)
		}
	}
}

func TestParseDropsForeignHalfAndHashtagTitle(t *testing.T) {
	caption := "Блины на молоке\nИнгредиенты:\nМолоко — 500 мл\nЯйца — 2 шт\nМука — 200 г\n\nIngredients:\nMilk — 500 ml\nEggs — 2\n#блины"
	p := Parse(caption)
	if p.Title != "Блины на молоке" || len(p.Ingredients) != 3 {
		t.Errorf("parsed = %q, %+v", p.Title, p.Ingredients)
	}
}

func TestParseCapsManyIngredients(t *testing.T) {
	var b strings.Builder
	b.WriteString("Ингредиенты:\n")
	for i := 0; i < 60; i++ {
		b.WriteString("Специя — 1 ч. л.\n")
	}
	p := Parse(b.String())
	if len(p.Ingredients) != 50 || len(p.Warnings) == 0 {
		t.Errorf("%d ingredients, warnings %q", len(p.Ingredients), p.Warnings)
	}
}

func TestParseCallsToActionVersusCookingVerbs(t *testing.T) {
	p := &parser{cyrillic: true}
	for text, junk := range map[string]bool{
		"Сохрани рецепт, чтобы не потерять":                   true,
		"📌 Сохраните, чтобы не потерять":                      true,
		"Обязательно сохраняйте и готовьте на выходных 😋":     true,
		"📩 Отправьте подруге — пусть тоже попробует":          true,
		"Отправляйте тому, кто не знает, что приготовить":     true,
		"Жми на ссылку в профиле":                             true,
		"Накройте крышкой и отправьте в духовку на 30 минут.": false,
		"Отправляем в разогретую до 190 градусов духовку":     false,
		"Готовое блюдо сохраните в холодильнике до 3 дней.":   false,
		"Отожми сок из лимона и переходим к соусу":            false,
	} {
		if got := p.isJunk(prepareLine(text)); got != junk {
			t.Errorf("isJunk(%q) = %v, want %v", text, got, junk)
		}
	}
}

func TestParseLabelledListBulletListAndHeadingTitle(t *testing.T) {
	p := Parse("Ингредиенты:\nКартофель — 300 г\nСоус: 50 г сметаны, 50 г майонеза, 1 ч.л. горчицы")
	var names []string
	for _, ing := range p.Ingredients {
		names = append(names, ing.Name)
	}
	if strings.Join(names, ", ") != "Картофель, Сметана, Майонез, Горчица" {
		t.Errorf("labelled list = %q", names)
	}

	p = Parse("ужин в одной форме 👇\n\nГраммовки не пишу, смотрите по форме\n\n•филе куриное\n•картофель\n•помидор\n•сыр\n\nФиле отбейте и выложите в форму.\nСверху выложите картофель и помидор.\nЗапекайте 40 минут при 180 градусах.")
	if len(p.Ingredients) != 4 || len(p.Steps) != 3 || p.Confidence < minConfidence || p.Confidence >= DefaultThreshold {
		t.Errorf("bullet list: %d ingredients, %d steps, confidence %v", len(p.Ingredients), len(p.Steps), p.Confidence)
	}

	p = Parse("Рецепты ⤵️\n\nВсе блюда простые и вкусные, готовятся в духовке\n\nЖаркое с курицей 🔥\n\nИНГРЕДИЕНТЫ:\nКурица — 600 г\nКартофель — 5 шт")
	if p.Title != "Жаркое с курицей" {
		t.Errorf("heading title = %q", p.Title)
	}

	if p := Parse("Ужин за 30 минут — без граммовок!\n\nКогда не хочется стоять у плиты, беру любые овощи, курицу, специи — и в духовку.\n\n💾 Сохраняйте рецепт, чтобы не потерять"); !p.notARecipe() {
		t.Errorf("a vague post was accepted: %+v", p)
	}
}

// «Ингредиенты: …» and «Приготовление: …» with their section on the same
// line, and one-line lists without a header.
func TestParseInlineHeaders(t *testing.T) {
	want := []string{"Филе бедра|600|г", "Соевый соус|4|ст. л.", "Мёд|2|ст. л.", "Чеснок|2|зубчик"}
	for name, caption := range map[string]string{
		"inline headers": "Курица терияки\nИнгредиенты: филе бедра 600 г, соевый соус 4 ст.л., мёд 2 ст.л., чеснок 2 зубчика.\n" +
			"Приготовление: обжарить курицу 10 минут, залить соусом и тушить 5 минут.",
		"no headers": "Курица терияки\nфиле бедра 600 г, соевый соус 4 ст.л., мёд 2 ст.л., чеснок 2 зубчика.\n" +
			"Обжарить курицу 10 минут, залить соусом и тушить 5 минут.",
		"header with servings": "Курица терияки\nИнгредиенты (на 2 порции): филе бедра 600 г, соевый соус 4 ст.л., мёд 2 ст.л., чеснок 2 зубчика\n" +
			"Приготовление: обжарить курицу 10 минут, залить соусом и тушить 5 минут.",
	} {
		p := Parse(caption)
		if got := ingredientRows(p); strings.Join(got, "; ") != strings.Join(want, "; ") {
			t.Errorf("%s: ingredients %q, want %q", name, got, want)
		}
		if len(p.Steps) != 1 || p.Steps[0] != "Обжарить курицу 10 минут, залить соусом и тушить 5 минут." {
			t.Errorf("%s: steps %q", name, p.Steps)
		}
		if p.Title != "Курица терияки" || p.Confidence < DefaultThreshold {
			t.Errorf("%s: title %q, confidence %v", name, p.Title, p.Confidence)
		}
	}
	// A header word in a sentence, or with one food after the colon, is
	// not an inline list.
	for _, text := range []string{
		"Ингредиенты: самые простые, всё есть дома",
		"Состав: мука 200 г",
		"Рецепт: обжарить и подать",
	} {
		if head, _, ok := inlineHeader(prepareLine(text)); ok {
			t.Errorf("%q: read as an inline header %q", text, head.text)
		}
	}
}

// A list may start with foods counted without a unit: they belong to it,
// and none of them is the dish name.
func TestParseBareCountsBeforeList(t *testing.T) {
	tests := []struct {
		caption string
		want    []string
	}{
		{"Салат с тунцом\n2 яйца\n1 огурец\n2 ст.л. греческого йогурта\nВсё нарезать и смешать.",
			[]string{"Яйца|2|шт", "Огурец|1|шт", "Греческий йогурт|2|ст. л."}},
		{"Салат с тунцом\n2 яйца\n1 огурец\nГорсть листьев салата\n1 банка тунца\n2 ст.л. греческого йогурта\nВсё нарезать и смешать.",
			[]string{"Яйца|2|шт", "Огурец|1|шт", "Листья салата||", "Тунец|1|", "Греческий йогурт|2|ст. л."}},
	}
	for _, tt := range tests {
		p := Parse(tt.caption)
		if got := ingredientRows(p); strings.Join(got, "; ") != strings.Join(tt.want, "; ") {
			t.Errorf("%q: ingredients %q, want %q", tt.caption, got, tt.want)
		}
		if p.Title != "Салат с тунцом" || len(p.Steps) != 1 {
			t.Errorf("%q: title %q, steps %q", tt.caption, p.Title, p.Steps)
		}
	}
	// Without a dish name the title stays empty rather than naming a food.
	if p := Parse("2 яйца\n1 огурец\nГорсть орехов\n2 ст.л. йогурта\nВсё смешать."); p.Title != "" {
		t.Errorf("title = %q, want none", p.Title)
	}
	for _, seg := range []string{"2 яйца", "1 огурец", "Горсть орехов", "Соль по вкусу"} {
		if got := titleFrom(seg); got != "" {
			t.Errorf("titleFrom(%q) = %q, want none", seg, got)
		}
	}
	for seg, want := range map[string]string{"Пицца 4 сыра": "Пицца 4 сыра", "Салат за 5 минут": "Салат за 5 минут"} {
		if got := titleFrom(seg); got != want {
			t.Errorf("titleFrom(%q) = %q, want %q", seg, got, want)
		}
	}
}

// «Духовка 180°, 12-15 минут» inside a list is an instruction.
func TestParseTimeAndTemperatureLineInListIsStep(t *testing.T) {
	p := Parse("Сырники\nИнгредиенты:\nТворог 500 г\nЯйца 2 шт\nДуховка 180°, 12-15 минут\nЯйцо 1 (отварить 10 минут)\nПриготовление:\nСмешать всё.\nЗапекать.")
	want := []string{"Творог|500|г", "Яйца|2|шт", "Яйцо|1|шт"}
	if got := ingredientRows(p); strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("ingredients %q, want %q", got, want)
	}
	if len(p.Steps) != 3 || p.Steps[0] != "Духовка 180°, 12-15 минут" {
		t.Errorf("steps %q", p.Steps)
	}
	for _, w := range p.Warnings {
		if strings.Contains(w, "Минут") {
			t.Errorf("a time became an ingredient: %q", w)
		}
	}
}

func TestCapWarnings(t *testing.T) {
	many := make([]string, 25)
	for i := range many {
		many[i] = "w"
	}
	got := capWarnings(many)
	if len(got) != maxWarnings || got[maxWarnings-1] != "…и ещё 16" {
		t.Errorf("capWarnings(25) = %d warnings ending %q", len(got), got[len(got)-1])
	}
	if got := capWarnings(many[:maxWarnings]); len(got) != maxWarnings || got[maxWarnings-1] != "w" {
		t.Errorf("exactly %d warnings must stay as they are, got %q", maxWarnings, got)
	}
}

func ingredientRows(p Parsed) []string {
	var rows []string
	for _, ing := range p.Ingredients {
		amount, unit := amountOf(ing.Quantity)
		rows = append(rows, ing.Name+"|"+amount+"|"+unit)
	}
	return rows
}
