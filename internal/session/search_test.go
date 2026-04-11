package session

import "testing"

func TestSanitizeFTS5Query_EnglishPreserved(t *testing.T) {
	got := sanitizeFTS5Query(`deploy-script v1.2`)
	want := `"deploy-script" "v1.2"`
	if got != want {
		t.Fatalf("sanitizeFTS5Query english mismatch: got %q, want %q", got, want)
	}
}

func TestSanitizeFTS5Query_CJKExpanded(t *testing.T) {
	got := sanitizeFTS5Query("西红柿")
	want := `"西红" OR "红柿"`
	if got != want {
		t.Fatalf("sanitizeFTS5Query cjk mismatch: got %q, want %q", got, want)
	}
}

func TestSanitizeFTS5Query_CJKWithNonCJK(t *testing.T) {
	got := sanitizeFTS5Query("西红柿 deploy")
	want := `"西红" OR "红柿" OR "deploy"`
	if got != want {
		t.Fatalf("sanitizeFTS5Query mixed mismatch: got %q, want %q", got, want)
	}
}

