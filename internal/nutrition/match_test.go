package nutrition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

type matchCase struct {
	name string
	want string // food id; "" = no match
}

func runMatch(t *testing.T, cases []matchCase) {
	t.Helper()
	tb := Default()
	for _, c := range cases {
		got, ok := tb.Match(c.name)
		switch {
		case c.want == "" && ok:
			t.Errorf("Match(%q) = %s (%s), want no match", c.name, got.ID, got.Name)
		case c.want != "" && !ok:
			t.Errorf("Match(%q) = no match, want %s", c.name, c.want)
		case c.want != "" && got.ID != c.want:
			t.Errorf("Match(%q) = %s (%s), want %s", c.name, got.ID, got.Name, c.want)
		}
	}
}

// Ingredient names as the caption fixtures in
// internal/recipeimport/testdata/captions store them (fixture number in the
// comment), so the matcher is tested on real recipe wording.
func TestMatchCorpusNames(t *testing.T) {
	runMatch(t, []matchCase{
		// 01
		{"Соль поваренная", "salt"},
		{"Соль нитритная", "salt"},
		{"Перец чёрный молотый", "black_pepper"},
		{"Базилик", "basil"},
		{"Лук репчатый", "onion"},
		{"Растительное масло", "oil_sunflower"},
		{"Сахар", "sugar"},
		// 02
		{"Миндаль", "almonds"},
		{"Финики", "dates"},
		{"Кокосовое масло", "oil_coconut"},
		{"Ягоды", ""},
		{"Кокосовое молоко", "coconut_milk"},
		{"Кукурузный крахмал", "starch_corn"},
		{"Кленовый сироп или мёд", "maple_syrup"}, // the first alternative
		{"Сок лимона", "lemon_juice"},
		{"Ванильный сахар", "vanilla_sugar"},
		{"Кокосовая стружка", "coconut_flakes"},
		// 03
		{"Морковь", "carrot"},
		{"Молотый кориандр", "coriander_seed"},
		{"Черный перец", "black_pepper"},
		{"Паприка", "paprika"},
		{"Острый красный перец", ""}, // fresh chili or ground cayenne: ambiguous
		{"Уксус рисовый", "vinegar"},
		{"Чеснок", "garlic"},
		// 04
		{"Панировочные сухари", "breadcrumbs"},
		{"Чесночный порошок", "garlic_powder"},
		{"Луковый порошок", "onion_powder"},
		{"Молотый перец", "black_pepper"},
		{"Газированная вода", "water"},
		{"Универсальная мука", "flour_wheat"},
		{"Цветная капуста", "cauliflower"},
		// 05
		{"Банан", "banana"},
		{"Какао", "cocoa"},
		{"Арахисовая паста", "peanut_butter"},
		{"Разрыхлитель", "baking_powder"},
		// 06
		{"Ботва редиски", ""}, // not radish
		{"Уксус 9%", "vinegar"},
		{"Соевый соус", "soy_sauce"},
		{"Кунжутное масло", "oil_sesame"},
		{"Перец", ""},
		{"Зелёный лук", "green_onion"},
		// 07
		{"Редис", "radish"},
		{"Огурец", "cucumber"},
		{"Кинза", "cilantro"},
		{"Петрушка", "parsley"},
		{"Масло растительное", "oil_sunflower"},
		// 08
		{"Белокочанная капуста", "cabbage"},
		{"Красная капуста", "red_cabbage"},
		{"Измельчённая петрушка", "parsley"},
		{"Майонез", "mayonnaise"},
		{"Сметана", "smetana_15"},
		{"Горчица", "mustard"},
		{"Яблочный уксус", "vinegar_apple"},
		// 09
		{"Зелёная редька", "daikon"},
		{"Лук", "onion"},
		{"Уксус 80%", "vinegar"},
		{"Красный острый перец", ""},
		{"Кориандр", "coriander_seed"},
		// 10
		{"Яйца", "egg"},
		{"Кефир", "kefir_2_5"},
		{"Мука", "flour_wheat"},
		{"Крахмал", "starch_potato"},
		{"Сода", "baking_soda"},
		{"Ваниль", ""},
		// 11
		{"Белки", "egg_white"},
		{"Лимонный сок", "lemon_juice"},
		{"Сливки", "cream_20"},
		{"Сахарная пудра", "powdered_sugar"},
		{"Творожный сыр", "curd_cheese"},
		{"Клубника", "strawberry"},
		// 12
		{"Сливочное масло", "butter"},
		{"Ванилин", ""},
		{"Ягоды или фрукты", ""},
		// 13
		{"Кунжутная паста/тахини", "tahini"},
		{"Масло авокадо или гхи", "ghee"}, // «масло авокадо» is unknown, so the alternative
		{"Бульон", ""},
		{"Семена кориандра", "coriander_seed"},
		{"Семена фенхеля", ""},
		{"Семена тмина", "caraway"},
		{"Молотый имбирь", "ginger_ground"},
		{"Корица", "cinnamon"},
		{"Красный перец", "red_pepper"},
		// 14, 15
		{"Рикотта", "ricotta"},
		{"Яйцо", "egg"},
		{"Эспрессо", "coffee"},
		{"Кардамон", "cardamom"},
		{"Мёд", "honey"},
		{"Горячее молоко", "milk_2_5"},
		// 16
		{"Картофель", "potato"},
		{"Куриные голени", "chicken_drumstick"},
		{"Кипяток", "water"},
		{"Специи", ""},
		{"Зелень свежая", "dill"},
		{"Плавленный сыр", "processed_cheese"}, // misspelt «-нн-»
		{"Куркума", "turmeric"},
		// 17
		{"Огурцы", "cucumber"},
		{"Помидоры", "tomato"},
		{"Мясо", ""},
		{"Хлеб", "bread_white"},
		{"Зелень лука и укропа", "dill"},
		{"Хрен", "horseradish"},
		{"Минеральная вода", "water"},
		// 18
		{"Лук-шалот", "shallot"},
		{"Помидоры черри", "cherry_tomato"},
		{"Макароны", "pasta"},
		{"Веганский сливочный сыр", ""},
		{"Итальянские травы", "herbs_provence"},
		{"Веганский пармезан", ""},
		// 19, 20, 21, 22
		{"Отварная курица", "chicken_whole"},
		{"Плавленный сырок", "processed_cheese"},
		{"Сахарный песок", "sugar"},
		{"Фарш", "mince_mixed"},
		{"Грибы", "champignon"},
		{"Зелень", "dill"},
		{"Фрикадельки", ""},
		{"Макароны либо рис", "pasta"},
		// 23
		{"Капуста свежая", "cabbage"},
		{"Морковь сырая", "carrot"}, // «сырая» is not «сыр»
		{"Ветчина", "ham"},
		{"Кукуруза консервированная", "corn_canned"},
		{"Сметана 10%", "smetana_10"},
		// 24
		{"Цукини", "zucchini"},
		{"Орегано сушеный", "oregano"},
		{"Базилик сушеный", "basil_dried"},
		{"Чеснок сушеный", "garlic_powder"},
		{"Пармезан", "parmesan"},
		{"Перец черный молотый", "black_pepper"},
		// 25
		{"Куриное филе", "chicken_breast"},
		{"Сушеный чеснок или чесночный перец", "garlic_powder"},
		{"Сладкая паприка", "paprika"},
		{"Греческий йогурт", "yogurt_greek"},
		{"Зерненая горчица", "mustard"},
		{"Лаваш тонкий", "lavash"},
		{"Помидор", "tomato"},
		{"Болгарский перец", "bell_pepper"},
		// 26
		{"Мед жидкий", "honey"},
		{"Базилик сухой", "basil_dried"},
		{"Орегано сухой", "oregano"},
		{"Перец чили в хлопьях", "red_pepper"},
		{"Куриные бедра", "chicken_thigh"},
		{"Брокколи свежая", "broccoli"},
		{"Кунжут белый", "sesame"},
		{"Петрушка свежая", "parsley"},
		// 27
		{"Булгур", "bulgur"},
		{"Куриная грудка", "chicken_breast"},
		{"Вяленые томаты", ""}, // dried tomatoes are not fresh ones
		{"Сливки 20%", "cream_20"},
		{"Шпинат", "spinach"},
		{"Твердый сыр", "cheese_hard"},
		{"Приправа для курицы", ""},
		// 28 – 31
		{"Джусай", ""},
		{"Чёрный перец", "black_pepper"},
		{"Масло для жарки", "oil_sunflower"},
		{"Вишня", "cherry_sour"},
		{"Молоко", "milk_2_5"},
		{"Дрожжи", "yeast_dry"},
		{"Масло", "butter"},
		{"Шампанское", ""},
		{"Содовая, тоник", ""},
		{"Апельсин", "orange"},
	})
}

// Whole caption lines (the fixtures' "raw"), names with amounts, units,
// emoji, parentheticals and the broken «и» + U+200C spelling of «й».
func TestMatchRawLines(t *testing.T) {
	runMatch(t, []matchCase{
		{"1 ст.л. измельчённой петрушки", "parsley"},
		{"· ½ чайной ложки соды", "baking_soda"},
		{"3 стакана панировочных сухарей", "breadcrumbs"},
		{"1½ ч. л. чесночного порошка", "garlic_powder"},
		{"Сливки-300 мл (33%) холодные", "cream_33"},
		{"Сахар(мелкии\u200c)-260 гр", "sugar"},
		{"Творожныи\u200c сыр -180 гр", "curd_cheese"},
		{"180 гр сливочного масла холодного", "butter"},
		{"2 столовые ложки плавленного сыра (но можно и без него)", "processed_cheese"},
		{"~800 мл. кипятка (или бульона)", "water"},
		{"20 помидоров черри", "cherry_tomato"},
		{"3 лука-шалота", "shallot"},
		{"· ⅓ стакана сахарного песка", "sugar"},
		{"🥕Черный перец 0,5 ч. л", "black_pepper"},
		{"• рикотта - 250 гр", "ricotta"},
		{"🍗Отварная курица-400 гр.", "chicken_whole"},
		{"- 400 гр вишни (можно любые ягоды)", "cherry_sour"},
		{"📌масло для жарки :50 мл", "oil_sunflower"},
		{"4-5 средних картофелин", "potato"},
		{"4-5 куриных голеней", "chicken_drumstick"},
		{"•\t3 зубчика чеснока 🧄", "garlic"},
		{"Сок 1 лимона", "lemon_juice"},
	})
}

// The forms the brief names: cases, fat levels, grades, descriptors.
func TestMatchNoise(t *testing.T) {
	runMatch(t, []matchCase{
		{"сахара", "sugar"},
		{"сахарного песка", "sugar"},
		{"Яйца С1", "egg"},
		{"лук репчатый (крупный)", "onion"},
		{"мука пшеничная в/с", "flour_wheat"},
		{"молоко 3,2%", "milk_3_2"},
		{"масло сливочное 82,5%", "butter"},
		{"2 ст. л. оливкового масла", "oil_olive"},
		{"Лук репчатый, нарезанный полукольцами", "onion"},
		{"Сахар по желанию", "sugar"},
		{"Сыр для подачи", "cheese_hard"},
		{"Клубника для украшения", "strawberry"},
		{"Вода для пасты", "water"}, // «паста» after «для» does not compete
		{"cметана", "smetana_15"},   // Latin «c»
		{"ЯЙЦА", "egg"},
		{"Свёкла", "beet"},
		{"Свекла", "beet"},
		{"Фарш из индейки", "turkey_mince"},
		{"Перец сладкий", "bell_pepper"},
		{"Вареные яйца", "egg"},
		{"Сырые яйца", "egg"},
		{"Сода, гашенная уксусом", "baking_soda"},
		{"Сода гашеная уксусом", "baking_soda"},
		{"Зубчик чеснока", "garlic"},
		{"Масло гхи", "ghee"},
		{"Масло нерафинированное", "oil_sunflower"},
		{"Огурчики маринованные", "pickled_cucumber"},
	})
}

// Different foods that share letters must never meet, and the guards keep
// a product made from a food from matching the food itself.
func TestMatchNegatives(t *testing.T) {
	runMatch(t, []matchCase{
		{"соль", "salt"},
		{"солод", ""},
		{"Солодовый экстракт", ""},
		{"Солёная карамель", ""},
		{"Солёные огурцы", "pickled_cucumber"},
		{"перец", ""},          // sweet or black: deliberately unmapped
		{"Перец горошком", ""}, // not «горошек»
		{"Зелёный перец", ""},  // «зелёный» is not «зелень»
		{"Печень", ""},         // liver of what? and never «печенье»
		{"Печенье", "cookies"},
		{"Сало", "salo"},
		{"Салями", "salami"},
		{"Мороженая рыба", ""}, // not ice cream
		{"Мороженое", "ice_cream"},
		{"Лимонная кислота", ""},
		{"Сок апельсина", ""},     // juice, not the fruit
		{"Масло авокадо", ""},     // oil, not the fruit
		{"Масло виноградное", ""}, // oil of another plant, not butter
		{"Масло кукурузное", ""},
		{"Хлеб кукурузный", ""},
		{"Рисовое молоко", ""},
		{"Кокосовые сливки", ""},
		{"Сливки растительные", ""},
		{"Мука овсяная", ""},
		{"Грибы сушеные", ""},
		{"Капуста морская", ""},
		{"Цедра лимона", ""},
		{"Курица и грибы", ""}, // two foods in one line
		{"Мясо соевое", ""},
		{"Гуанчале", ""},
	})
}

// A tie between foods goes to the aliases that cover more of the main
// (pre-preposition) words; an even tie stays no match.
func TestMatchTieBreak(t *testing.T) {
	runMatch(t, []matchCase{
		{"Масло оливковое для жарки", "oil_olive"}, // not the «масло для жарки» alias of sunflower oil
		{"Оливковое масло для жарки", "oil_olive"},
		{"Масло оливковое для салата", "oil_olive"},
		{"Масло для жарки", "oil_sunflower"},
		{"Масло оливковое", "oil_olive"},
		{"Масло кукурузное для жарки", ""}, // oil of another plant
		{"перец", ""},
		{"Перец для жарки", ""},
	})
}

// A fat percentage picks the closest variant; a bare name picks the
// documented default.
func TestMatchFatVariants(t *testing.T) {
	runMatch(t, []matchCase{
		{"Молоко", "milk_2_5"},
		{"Молоко 3,2%", "milk_3_2"},
		{"молоко 1,5 %", "milk_1_5"},
		{"Молоко 1%", "milk_1_5"},
		{"Молоко 6%", "milk_3_2"},
		{"Молоко обезжиренное", "milk_1_5"},
		{"Сливки 10%", "cream_10"},
		{"Сливки 35%", "cream_33"},
		{"Сметана 25%", "smetana_20"},
		{"Творог", "tvorog_5"},
		{"Творог 2%", "tvorog_0"},
		{"Творог 9%", "tvorog_9"},
		{"Кефир 1%", "kefir_1"},
		{"Кефир 2%", "kefir_2_5"},
		{"Масло 82,5%", "butter"},
		{"Масло сливочное 72,5%", "butter_72"},
		{"Масло 72%", "butter_72"},
	})
}

// Every name and alias of the table must find its own food: a regression
// net for the normaliser (two foods whose aliases stem alike would fail).
func TestMatchEveryAlias(t *testing.T) {
	var raw struct {
		Foods []struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Aliases []string `json:"aliases"`
		} `json:"foods"`
	}
	if err := json.Unmarshal(foodsJSON, &raw); err != nil {
		t.Fatal(err)
	}
	tb := Default()
	n := 0
	for _, f := range raw.Foods {
		for _, a := range append([]string{f.Name}, f.Aliases...) {
			n++
			got, ok := tb.Match(a)
			if !ok || got.ID != f.ID {
				t.Errorf("Match(%q) = %q, %v; want %s", a, got.ID, ok, f.ID)
			}
		}
	}
	if n < 1000 {
		t.Fatalf("only %d names and aliases checked", n)
	}
}

func TestStem(t *testing.T) {
	same := [][2]string{
		{"сахар", "сахара"}, {"песок", "песка"}, {"огурец", "огурцы"}, {"чеснок", "чеснока"},
		{"соль", "соли"}, {"соль", "солью"}, {"хлопья", "хлопьях"}, {"листья", "листьев"},
		{"плавленый", "плавленный"}, {"яйцо", "яиц"}, {"морковь", "моркови"}, {"плов", "плова"},
		{"сухой", "сушеный"}, {"вяленые", "сушеные"},
	}
	for _, p := range same {
		if a, b := stem(p[0]), stem(p[1]); a != b {
			t.Errorf("stem(%q) = %q, stem(%q) = %q; want equal", p[0], a, p[1], b)
		}
	}
	differ := [][2]string{
		{"соль", "солод"}, {"сыр", "сырая"}, {"зелень", "зеленый"}, {"печень", "печенье"},
		{"варенье", "вареный"}, {"сало", "салями"}, {"горошек", "горошком"}, {"соль", "соленый"},
		{"мороженое", "мороженая"}, {"масло", "маслины"}, {"сливки", "слива"},
	}
	for _, p := range differ {
		if a, b := stem(p[0]), stem(p[1]); a == b {
			t.Errorf("stem(%q) = stem(%q) = %q; want different", p[0], p[1], a)
		}
	}
}

// TestCorpusMatchRate reports how many ingredient names of the caption
// corpus match a food, and keeps the rate from regressing.
func TestCorpusMatchRate(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "recipeimport", "testdata", "captions", "*.json"))
	if err != nil || len(files) == 0 {
		t.Skip("caption fixtures not found")
	}
	tb := Default()
	var (
		total, hit int
		unique     = map[string]bool{}
		missed     = map[string]bool{}
	)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var fx struct {
			Ingredients []struct {
				Name string `json:"name"`
			} `json:"ingredients"`
		}
		if err := json.Unmarshal(data, &fx); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, in := range fx.Ingredients {
			total++
			_, ok := tb.Match(in.Name)
			unique[in.Name] = ok
			if ok {
				hit++
			} else {
				missed[in.Name] = true
			}
		}
	}
	uniqueHit := 0
	for _, ok := range unique {
		if ok {
			uniqueHit++
		}
	}
	names := make([]string, 0, len(missed))
	for n := range missed {
		names = append(names, n)
	}
	sort.Strings(names)
	t.Logf("corpus: %d of %d ingredient names match (%.1f%%); %d of %d distinct names", hit, total,
		100*float64(hit)/float64(total), uniqueHit, len(unique))
	t.Logf("unmatched: %q", names)
	if rate := float64(hit) / float64(total); rate < 0.85 {
		t.Errorf("corpus match rate %.1f%% fell below 85%%", 100*rate)
	}
}
