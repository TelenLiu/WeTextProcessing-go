package lib

import (
	"sync"

	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
)

var (
	// VALID_UTF8_CHAR is the base character set (sigma) for difference and
	// cdrewrite operations. It uses a compact char-union pattern so that
	// isCharUnion() can detect it for optimized Difference.
	//
	// NOTE: CJK Unified Ideographs are NOT included here to keep memory low.
	// The Python pynini uses byte-level UTF-8 matching (RFC3629) which is
	// inherently compact. Our rune-level approach must enumerate characters.
	// For CJK text, call SetCJKVCHAR() before creating Processors.
	VALID_UTF8_CHAR = buildValidUTF8Char()

	// cjkVCHAR holds the CJK-extended VCHAR, set via SetCJKVCHAR.
	// When set, NewProcessor will use this instead of VALID_UTF8_CHAR.
	cjkVCHAR     *pynini.Fst
	cjkVCHAROnce sync.Once
)

func buildValidUTF8Char() *pynini.Fst {
	// Build a direct char union FST without using Union() to avoid epsilon arcs.
	// This pattern: start state -> char -> final state (for each character)
	// allows isCharUnion() to detect it and use the optimized Difference path.
	result := pynini.NewFst()

	addChar := func(ch string) {
		stateID := result.AddState()
		result.SetFinal(stateID, 0)
		result.AddArcStr(0, stateID, ch, ch, 0)
	}

	// ASCII printable (0x20-0x7E): space through tilde
	for i := 0x20; i <= 0x7E; i++ {
		addChar(string(rune(i)))
	}
	// Common whitespace/control chars
	addChar("\t")
	addChar("\n")
	addChar("\r")

	// Latin-1 Supplement (U+00A0-U+00FF): currency symbols, accented chars
	for i := 0x00A0; i <= 0x00FF; i++ {
		addChar(string(rune(i)))
	}

	// Full-width digits (０-９)
	for i := 0xFF10; i <= 0xFF19; i++ {
		addChar(string(rune(i)))
	}
	// Full-width uppercase (Ａ-Ｚ)
	for i := 0xFF21; i <= 0xFF3A; i++ {
		addChar(string(rune(i)))
	}
	// Full-width lowercase (ａ-ｚ)
	for i := 0xFF41; i <= 0xFF5A; i++ {
		addChar(string(rune(i)))
	}

	// Full-width punctuation
	fullWidthPunct := []rune{
		0xFF01, 0xFF02, 0xFF03, 0xFF04, 0xFF05, 0xFF06, 0xFF07, 0xFF08, 0xFF09,
		0xFF0A, 0xFF0B, 0xFF0C, 0xFF0D, 0xFF0E, 0xFF0F,
		0xFF1A, 0xFF1B, 0xFF1C, 0xFF1D, 0xFF1E, 0xFF1F, 0xFF20,
		0xFF3B, 0xFF3C, 0xFF3D, 0xFF3E, 0xFF3F, 0xFF40,
		0xFF5B, 0xFF5C, 0xFF5D, 0xFF5E, 0xFF5F, 0xFF60, 0xFF61, 0xFF62, 0xFF63, 0xFF64, 0xFF65,
	}
	for _, r := range fullWidthPunct {
		addChar(string(r))
	}

	// CJK Symbols and Punctuation (U+3000-U+303F): ideographic space, brackets, etc.
	for i := 0x3000; i <= 0x303F; i++ {
		addChar(string(rune(i)))
	}
	// IDEOGRAPHIC NUMBER ZERO (U+3007) - already in 3000-303F range

	// Japanese Hiragana (U+3040 - U+309F)
	for i := 0x3041; i <= 0x3096; i++ {
		addChar(string(rune(i)))
	}
	for _, r := range []rune{0x309B, 0x309C, 0x309D, 0x309E, 0x309F} {
		addChar(string(r))
	}

	// Japanese Katakana (U+30A0 - U+30FF)
	for i := 0x30A1; i <= 0x30FA; i++ {
		addChar(string(rune(i)))
	}
	for _, r := range []rune{0x30A0, 0x30FB, 0x30FC, 0x30FD, 0x30FE, 0x30FF} {
		addChar(string(r))
	}

	return result
}

// CJKCharsFromFile builds a char-union FST from a file containing one CJK
// character per line (e.g. charset_national_standard_2013_8105.tsv).
// Use this to extend VALID_UTF8_CHAR with CJK coverage for Chinese/Japanese TN.
func CJKCharsFromFile(path string) *pynini.Fst {
	return pynini.StringFileMust(path)
}

// ExtendValidUTF8Char extends the global VCHAR with CJK characters
// from a charset file. This affects all subsequently created Processors
// that use CJK VCHAR (Chinese/Japanese). English Processors are unaffected.
// It is safe to call multiple times; only the first call has effect.
func ExtendValidUTF8Char(charsetPath string) {
	cjkVCHAROnce.Do(func() {
		cjkFst := CJKCharsFromFile(charsetPath)
		if cjkFst != nil && len(cjkFst.States) > 0 {
			cjkVCHAR = pynini.Union(VALID_UTF8_CHAR, cjkFst)
		}
	})
}

// CJKVCHAR returns the CJK-extended VCHAR if set, otherwise nil.
func CJKVCHAR() *pynini.Fst {
	return cjkVCHAR
}

// ResetCJKVCHAR resets the CJK VCHAR so that subsequently created Processors
// will use the default VALID_UTF8_CHAR. Used by English normalizer to avoid
// the performance overhead of CJK characters.
func ResetCJKVCHAR() {
	cjkVCHAR = nil
	cjkVCHAROnce = sync.Once{}
}