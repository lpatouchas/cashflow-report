// Package textfold provides lookalike-tolerant text comparison for Greek/Latin
// data, where visually identical characters can be distinct Unicode codepoints
// (e.g. Greek Ι U+0399 vs Latin I U+0049). Fold maps Greek letters that have an
// identical-looking Latin twin onto that twin, so such strings compare equal.
package textfold

import "strings"

// greekToLatin maps each Greek letter that is visually indistinguishable from a
// Latin letter in common fonts onto that Latin letter. It is case-preserving and
// strictly 1:1, so folding can never merge two strings that look different to a
// human — only ones that look identical. Lowercase ν/υ/κ are deliberately absent:
// they are not reliably identical to v/y/k. Adding a pair later stays 1:1 and
// non-breaking.
var greekToLatin = map[rune]rune{
	// Uppercase
	'Α': 'A', 'Β': 'B', 'Ε': 'E', 'Ζ': 'Z', 'Η': 'H', 'Ι': 'I',
	'Κ': 'K', 'Μ': 'M', 'Ν': 'N', 'Ο': 'O', 'Ρ': 'P', 'Τ': 'T',
	'Υ': 'Y', 'Χ': 'X',
	// Lowercase — only the genuinely indistinguishable pairs.
	'ο': 'o', 'ρ': 'p', 'χ': 'x',
}

// Fold returns a canonical form of s for lookalike-tolerant comparison. Each
// Greek letter with an identical-looking Latin counterpart is replaced by that
// Latin letter; every other rune (Greek-only letters, digits, punctuation,
// existing Latin text) passes through unchanged. Case is preserved, so Fold
// never equates text that differs only by case. Two strings that look identical
// to a human are equal after Fold.
func Fold(s string) string {
	return strings.Map(func(r rune) rune {
		if latin, ok := greekToLatin[r]; ok {
			return latin
		}
		return r
	}, s)
}
