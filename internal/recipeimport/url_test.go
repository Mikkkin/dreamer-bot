package recipeimport

import "testing"

func TestParseURL(t *testing.T) {
	tests := []struct {
		raw  string
		want Ref
		ok   bool
	}{
		{"https://www.instagram.com/reel/DEMOreel001/", Ref{"reel", "DEMOreel001"}, true},
		{"https://www.instagram.com/p/DEMOpost002/?igsh=MWR4ZnRqNnJ6bGtxbQ==", Ref{"p", "DEMOpost002"}, true},
		{"https://www.instagram.com/reel/DEMOreel003/?utm_source=ig_web_copy_link&igsh=abc", Ref{"reel", "DEMOreel003"}, true},
		{"https://www.instagram.com/reels/DEMOreel003/", Ref{"reel", "DEMOreel003"}, true},
		{"https://instagram.com/p/DEMOpost002", Ref{"p", "DEMOpost002"}, true},
		{"https://m.instagram.com/p/DEMOpost002/#comments", Ref{"p", "DEMOpost002"}, true},
		{"instagram.com/tv/B_tv-code_1/", Ref{"tv", "B_tv-code_1"}, true},
		{"www.instagram.com/p/DEMOpost002/", Ref{"p", "DEMOpost002"}, true},
		{"HTTPS://WWW.INSTAGRAM.COM/P/DEMOpost002/", Ref{"p", "DEMOpost002"}, true},
		{"http://www.instagram.com/reel/DEMOreel001/", Ref{"reel", "DEMOreel001"}, true},
		{"https://www.instagram.com/demo_kitchen/p/DEMOpost002/", Ref{"p", "DEMOpost002"}, true},
		{"https://www.instagram.com/p/DEMOpost002/embed/captioned/", Ref{"p", "DEMOpost002"}, true},
		// trailing junk and text around the link
		{"Смотри рецепт: https://www.instagram.com/reel/DEMOreel001/?igsh=x).", Ref{"reel", "DEMOreel001"}, true},
		{"«https://www.instagram.com/p/DEMOpost002/»", Ref{"p", "DEMOpost002"}, true},
		{"  https://www.instagram.com/p/DEMOpost002/\n", Ref{"p", "DEMOpost002"}, true},
		{"https://www.instagram.com./p/DEMOpost002/", Ref{"p", "DEMOpost002"}, true},
		// rejected
		{"", Ref{}, false},
		{"просто текст", Ref{}, false},
		{"https://www.instagram.com/", Ref{}, false},
		{"https://www.instagram.com/demo_kitchen/", Ref{}, false},
		{"https://www.instagram.com/stories/demo_kitchen/3512345678901234567/", Ref{}, false},
		{"https://www.instagram.com/explore/tags/рецепт/", Ref{}, false},
		{"https://www.instagram.com/p/abc/", Ref{}, false},
		{"https://www.instagram.com/p/a$b%c/", Ref{}, false},
		{"https://instagram.com.evil.example/p/DEMOpost002/", Ref{}, false},
		{"https://evil.example/www.instagram.com/p/DEMOpost002/", Ref{}, false},
		{"https://notinstagram.com/p/DEMOpost002/", Ref{}, false},
		{"https://www.instagram.com@evil.example/p/DEMOpost002/", Ref{}, false},
		{"https://user:pass@www.instagram.com/p/DEMOpost002/", Ref{}, false},
		{"https://www.instagram.com:8443/p/DEMOpost002/", Ref{}, false},
		{"ftp://www.instagram.com/p/DEMOpost002/", Ref{}, false},
		{"javascript:alert(1)//www.instagram.com/p/DEMOpost002/", Ref{}, false},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", Ref{}, false},
		// Instagram's own first segments are never a username
		{"https://www.instagram.com/share/reel/BAbcdEFgh12/", Ref{}, false},
		{"https://www.instagram.com/share/p/BAbcdEFgh12/", Ref{}, false},
		{"https://www.instagram.com/SHARE/reel/BAbcdEFgh12/", Ref{}, false},
		{"https://www.instagram.com/explore/p/DEMOpost002/", Ref{}, false},
		{"https://www.instagram.com/stories/reel/DEMOpost002/", Ref{}, false},
		{"https://www.instagram.com/accounts/p/DEMOpost002/", Ref{}, false},
		{"https://www.instagram.com/direct/reel/DEMOpost002/", Ref{}, false},
		// a username that only starts like a reserved word is a username
		{"https://www.instagram.com/share_recipes/reel/DEMOreel001/", Ref{"reel", "DEMOreel001"}, true},
	}
	for _, tt := range tests {
		got, ok := ParseURL(tt.raw)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseURL(%q) = %+v, %v; want %+v, %v", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRefURLIsCanonical(t *testing.T) {
	for raw, want := range map[string]string{
		"https://m.instagram.com/reels/DEMOreel003/?igsh=1": "https://www.instagram.com/reel/DEMOreel003/",
		"instagram.com/p/DEMOpost002":                       "https://www.instagram.com/p/DEMOpost002/",
		"https://www.instagram.com/tv/B_tv-code_1/x/y":      "https://www.instagram.com/tv/B_tv-code_1/",
	} {
		ref, ok := ParseURL(raw)
		if !ok || ref.URL() != want {
			t.Errorf("ParseURL(%q).URL() = %q, %v; want %q", raw, ref.URL(), ok, want)
		}
	}
}
