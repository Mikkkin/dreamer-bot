package recipeimport

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// noiseRe matches what a caption carries besides the recipe: hashtags,
// mentions and calls to action. None of it may reach a parsed recipe.
var noiseRe = regexp.MustCompile(`(?i)(^|\s)[#@][\p{L}\d_]|сохран(и|яй)|подпис(ыв|ывайтесь|ывайся)|ссылк[аи] в (шапке|био|профиле)|пиш(и|ите) в коммент|ставь(те)? лайк`)

func TestCorpusNeverLeaksHashtagsOrCTA(t *testing.T) {
	for _, noise := range []string{"#шашлык", "Сохрани рецепт", "Подписывайтесь на канал", "ссылка в шапке профиля", "@v_ogorod"} {
		if !noiseRe.MatchString(noise) {
			t.Fatalf("the noise pattern misses %q", noise)
		}
	}
	paths, err := filepath.Glob(filepath.Join("testdata", "captions", "*.txt"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no captions: %v", err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		p := Parse(string(raw))
		name := filepath.Base(path)
		if noiseRe.MatchString(p.Title) {
			t.Errorf("%s: noise in the title %q", name, p.Title)
		}
		for _, ing := range p.Ingredients {
			if noiseRe.MatchString(ing.Name) {
				t.Errorf("%s: noise in an ingredient %q", name, ing.Name)
			}
		}
		for _, step := range p.Steps {
			if noiseRe.MatchString(step) || strings.Contains(step, "#") {
				t.Errorf("%s: noise in a step %q", name, step)
			}
		}
	}
}
