package utils

import "testing"

func TestMatches(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		keywords []string
		want     bool
	}{
		{name: "case insensitive", text: "New CloudCone offer", keywords: []string{"cloudcone"}, want: true},
		{name: "trims keyword", text: "Go 1.22 released", keywords: []string{"  GO "}, want: true},
		{name: "no match", text: "Unrelated article", keywords: []string{"rss"}, want: false},
		{name: "empty keyword does not match everything", text: "Article", keywords: []string{""}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Matches(test.text, test.keywords); got != test.want {
				t.Fatalf("Matches(%q, %#v) = %v, want %v", test.text, test.keywords, got, test.want)
			}
		})
	}
}

func TestCanonicalLinkKeepsIdentityQueryAndDropsFragment(t *testing.T) {
	got := canonicalLink(" https://example.com/article?id=42#comments ")
	want := "https://example.com/article?id=42"
	if got != want {
		t.Fatalf("canonicalLink() = %q, want %q", got, want)
	}
}
