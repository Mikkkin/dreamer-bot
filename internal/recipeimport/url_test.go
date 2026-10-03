package recipeimport

import "testing"

func TestParseURL(t *testing.T) {
	tests := []struct {
		raw  string
		want Ref
		ok   bool
	}{
		{"https://www.instagram.com/reel/DItfAhKCJ3h/", Ref{"reel", "DItfAhKCJ3h"}, true},
		{"https://www.instagram.com/p/DVs8ssPihOG/?igsh=MWR4ZnRqNnJ6bGtxbQ==", Ref{"p", "DVs8ssPihOG"}, true},
		{"https://www.instagram.com/reel/DCjGOmwo84T/?utm_source=ig_web_copy_link&igsh=abc", Ref{"reel", "DCjGOmwo84T"}, true},
		{"https://www.instagram.com/reels/DCjGOmwo84T/", Ref{"reel", "DCjGOmwo84T"}, true},
		{"https://instagram.com/p/DVs8ssPihOG", Ref{"p", "DVs8ssPihOG"}, true},
		{"https://m.instagram.com/p/DVs8ssPihOG/#comments", Ref{"p", "DVs8ssPihOG"}, true},
		{"instagram.com/tv/B_tv-code_1/", Ref{"tv", "B_tv-code_1"}, true},
		{"www.instagram.com/p/DVs8ssPihOG/", Ref{"p", "DVs8ssPihOG"}, true},
		{"HTTPS://WWW.INSTAGRAM.COM/P/DVs8ssPihOG/", Ref{"p", "DVs8ssPihOG"}, true},
		{"http://www.instagram.com/reel/DItfAhKCJ3h/", Ref{"reel", "DItfAhKCJ3h"}, true},
		{"https://www.instagram.com/v_ogorod/p/DVs8ssPihOG/", Ref{"p", "DVs8ssPihOG"}, true},
		{"https://www.instagram.com/p/DVs8ssPihOG/embed/captioned/", Ref{"p", "DVs8ssPihOG"}, true},
		// trailing junk and text around the link
		{"Смотри рецепт: https://www.instagram.com/reel/DItfAhKCJ3h/?igsh=x).", Ref{"reel", "DItfAhKCJ3h"}, true},
		{"«https://www.instagram.com/p/DVs8ssPihOG/»", Ref{"p", "DVs8ssPihOG"}, true},
		{"  https://www.instagram.com/p/DVs8ssPihOG/\n", Ref{"p", "DVs8ssPihOG"}, true},
		{"https://www.instagram.com./p/DVs8ssPihOG/", Ref{"p", "DVs8ssPihOG"}, true},
		// rejected
		{"", Ref{}, false},
		{"просто текст", Ref{}, false},
		{"https://www.instagram.com/", Ref{}, false},
		{"https://www.instagram.com/v_ogorod/", Ref{}, false},
		{"https://www.instagram.com/stories/v_ogorod/3512345678901234567/", Ref{}, false},
		{"https://www.instagram.com/explore/tags/рецепт/", Ref{}, false},
		{"https://www.instagram.com/p/abc/", Ref{}, false},
		{"https://www.instagram.com/p/a$b%c/", Ref{}, false},
		{"https://instagram.com.evil.example/p/DVs8ssPihOG/", Ref{}, false},
		{"https://evil.example/www.instagram.com/p/DVs8ssPihOG/", Ref{}, false},
		{"https://notinstagram.com/p/DVs8ssPihOG/", Ref{}, false},
		{"https://www.instagram.com@evil.example/p/DVs8ssPihOG/", Ref{}, false},
		{"https://user:pass@www.instagram.com/p/DVs8ssPihOG/", Ref{}, false},
		{"https://www.instagram.com:8443/p/DVs8ssPihOG/", Ref{}, false},
		{"ftp://www.instagram.com/p/DVs8ssPihOG/", Ref{}, false},
		{"javascript:alert(1)//www.instagram.com/p/DVs8ssPihOG/", Ref{}, false},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", Ref{}, false},
		// Instagram's own first segments are never a username
		{"https://www.instagram.com/share/reel/BAbcdEFgh12/", Ref{}, false},
		{"https://www.instagram.com/share/p/BAbcdEFgh12/", Ref{}, false},
		{"https://www.instagram.com/SHARE/reel/BAbcdEFgh12/", Ref{}, false},
		{"https://www.instagram.com/explore/p/DVs8ssPihOG/", Ref{}, false},
		{"https://www.instagram.com/stories/reel/DVs8ssPihOG/", Ref{}, false},
		{"https://www.instagram.com/accounts/p/DVs8ssPihOG/", Ref{}, false},
		{"https://www.instagram.com/direct/reel/DVs8ssPihOG/", Ref{}, false},
		// a username that only starts like a reserved word is a username
		{"https://www.instagram.com/share_recipes/reel/DItfAhKCJ3h/", Ref{"reel", "DItfAhKCJ3h"}, true},
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
		"https://m.instagram.com/reels/DCjGOmwo84T/?igsh=1": "https://www.instagram.com/reel/DCjGOmwo84T/",
		"instagram.com/p/DVs8ssPihOG":                       "https://www.instagram.com/p/DVs8ssPihOG/",
		"https://www.instagram.com/tv/B_tv-code_1/x/y":      "https://www.instagram.com/tv/B_tv-code_1/",
	} {
		ref, ok := ParseURL(raw)
		if !ok || ref.URL() != want {
			t.Errorf("ParseURL(%q).URL() = %q, %v; want %q", raw, ref.URL(), ok, want)
		}
	}
}
