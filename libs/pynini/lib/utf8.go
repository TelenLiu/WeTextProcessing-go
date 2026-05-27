package lib

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
)

var (
	// VALID_UTF8_CHAR is a union of common ASCII characters and frequently-used
	// Chinese CJK Unified Ideographs. It serves as the sigma (catch-all) for
	// difference operations and cdrewrite's context.
	VALID_UTF8_CHAR = buildValidUTF8Char()
)

func buildValidUTF8Char() *pynini.Fst {
	// Build a direct char union FST without using Union() to avoid epsilon arcs.
	// This pattern: start state -> char -> final state (for each character)
	// allows isCharUnion() to detect it and use the optimized Difference path.
	result := pynini.NewFst()
	nextState := 1

	addChar := func(ch string) {
		result.AddState(nextState)
		result.SetFinal(nextState, 0)
		result.AddArc(0, nextState, ch, ch, 0)
		nextState++
	}

	// ASCII printable (0x20-0x7E): space through tilde
	for i := 0x20; i <= 0x7E; i++ {
		addChar(string(rune(i)))
	}
	// Common whitespace/control chars
	addChar("\t")
	addChar("\n")
	addChar("\r")

	// Latin-1 Supplement (U+00A0-U+00FF): common currency symbols and accented chars
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

	// Full-width punctuation marks (！-／ U+FF01-FF0F, ：-＠ U+FF1A-FF20, ［-｀ U+FF3B-FF40, ｛-～ U+FF5B-FF65)
	// This includes full-width comma ，, period 。, exclamation ！, etc.
	for _, r := range []rune{
		0xFF01, 0xFF02, 0xFF03, 0xFF04, 0xFF05, 0xFF06, 0xFF07, 0xFF08, 0xFF09,
		0xFF0A, 0xFF0B, 0xFF0C, 0xFF0D, 0xFF0E, 0xFF0F,
		0xFF1A, 0xFF1B, 0xFF1C, 0xFF1D, 0xFF1E, 0xFF1F, 0xFF20,
		0xFF3B, 0xFF3C, 0xFF3D, 0xFF3E, 0xFF3F, 0xFF40,
		0xFF5B, 0xFF5C, 0xFF5D, 0xFF5E, 0xFF5F, 0xFF60, 0xFF61, 0xFF62, 0xFF63, 0xFF64, 0xFF65,
	} {
		addChar(string(r))
	}

	// CJK Unified Ideographs common range (U+4E00 - U+9FFF)
	// This is a pragmatic subset; the full range is U+4E00-U+9FA5.
	for i := 0x4E00; i <= 0x9FA5; i++ {
		addChar(string(rune(i)))
	}

	// IDEOGRAPHIC NUMBER ZERO (U+3007) - used in Japanese/Chinese numerals
	addChar(string(rune(0x3007)))

	// Japanese Hiragana (U+3040 - U+309F)
	for i := 0x3041; i <= 0x3096; i++ {
		addChar(string(rune(i)))
	}
	// Add remaining hiragana chars
	for _, r := range []rune{
		0x309B, 0x309C, 0x309D, 0x309E, 0x309F,
	} {
		addChar(string(r))
	}

	// Japanese Katakana (U+30A0 - U+30FF)
	for i := 0x30A1; i <= 0x30FA; i++ {
		addChar(string(rune(i)))
	}
	// Add remaining katakana chars
	for _, r := range []rune{
		0x30A0, 0x30FB, 0x30FC, 0x30FD, 0x30FE, 0x30FF,
	} {
		addChar(string(r))
	}

	return result
}
