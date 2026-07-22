package textfold

import "testing"

func TestFold(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Greek uppercase lookalikes fold to Latin.
		{"greek visa", "VΙSΑ", "VISA"}, // Greek Ι U+0399, Α U+0391
		{"pure latin unchanged", "VISA", "VISA"},
		{"greek header word", "Ημ/νία", "Hμ/νία"},  // only Η has a Latin twin here
		{"lowercase lookalikes", "χρονο", "xpoνo"}, // χ→x, ρ→p, ο→o; ν stays
		// No false merges.
		{"digits not folded", "CO0P", "CO0P"},
		{"case preserved", "coop", "coop"},
		// Greek-only letters pass through.
		{"greek only", "λδψω", "λδψω"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Fold(tt.in); got != tt.want {
				t.Errorf("Fold(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFoldMakesLookalikesEqual(t *testing.T) {
	if Fold("VΙSΑ") != Fold("VISA") {
		t.Errorf("Fold should equate VΙSΑ and VISA")
	}
}

func TestFoldDoesNotMergeDifferentText(t *testing.T) {
	// Genuinely different text must stay different after folding.
	if Fold("CO0P") == Fold("COOP") {
		t.Errorf("Fold must not merge digit 0 with letter O")
	}
	if Fold("COOP") == Fold("coop") {
		t.Errorf("Fold must not merge different case")
	}
}

func TestFoldIdempotent(t *testing.T) {
	for _, s := range []string{"VΙSΑ", "Ημ/νία συναλλαγής", "χρονο", "λδψω"} {
		if Fold(Fold(s)) != Fold(s) {
			t.Errorf("Fold not idempotent for %q", s)
		}
	}
}
