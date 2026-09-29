package stores

import (
	"net/url"
	"strings"
	"testing"
)

func TestTemplatesAreSafeHTTPSSearchLinks(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("no stores")
	}
	seen := map[string]bool{}
	for _, s := range all {
		t.Run(s.ID, func(t *testing.T) {
			if s.ID == "" || s.Name == "" || s.Emoji == "" {
				t.Fatalf("incomplete store %+v", s)
			}
			if s.ID != strings.ToLower(s.ID) || strings.ContainsAny(s.ID, " /?#") {
				t.Errorf("id %q must be a lower-case slug", s.ID)
			}
			if seen[s.ID] {
				t.Errorf("duplicate id %q", s.ID)
			}
			seen[s.ID] = true

			if n := strings.Count(s.SearchTemplate, Placeholder); n != 1 {
				t.Fatalf("template must contain exactly one %s, got %d: %s", Placeholder, n, s.SearchTemplate)
			}
			if !strings.HasPrefix(s.SearchTemplate, "https://") {
				t.Errorf("template must be https: %s", s.SearchTemplate)
			}
			// The placeholder must live in the query string, so a search can
			// never change where the link points.
			q := strings.Index(s.SearchTemplate, "?")
			if q < 0 || strings.Index(s.SearchTemplate, Placeholder) < q || strings.Contains(s.SearchTemplate, "#") {
				t.Errorf("placeholder must be inside the query string: %s", s.SearchTemplate)
			}

			hostile := url.QueryEscape("молоко 3,2% & хлеб/#?@evil.example")
			u, err := url.Parse(s.SearchURL(hostile))
			if err != nil {
				t.Fatalf("filled template does not parse: %v", err)
			}
			tmpl, err := url.Parse(strings.Replace(s.SearchTemplate, Placeholder, "x", 1))
			if err != nil {
				t.Fatalf("template does not parse: %v", err)
			}
			if u.Scheme != "https" || u.Host == "" || u.User != nil || u.Host != tmpl.Host || u.Path != tmpl.Path || u.Fragment != "" {
				t.Errorf("query escaped the query string: %+v", u)
			}
			if !strings.Contains(u.RawQuery, hostile) {
				t.Errorf("query not substituted: %s", u.String())
			}
		})
	}
}

func TestAllReturnsACopy(t *testing.T) {
	a := All()
	a[0].SearchTemplate = "https://evil.example/?q={q}"
	if All()[0].SearchTemplate == a[0].SearchTemplate {
		t.Fatal("All must not expose the catalog")
	}
}
