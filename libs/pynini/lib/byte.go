package lib

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
)

var (
	LOWER = buildCharUnion("abcdefghijklmnopqrstuvwxyz")
	UPPER = buildCharUnion("ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	ALPHA = buildCharUnion("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	DIGIT = buildCharUnion("0123456789")
	HEX = buildCharUnion("0123456789abcdefABCDEF")
	SPACE = buildCharUnion(" \t\n\r")
	PUNCT = buildCharUnion("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~")
	GRAPH = buildCharUnion("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~")

	// BYTE: union of all byte values 1-255.
	BYTE = buildByte()

	// NOT_SPACE and NOT_QUOTE use optimized Difference path
	NOT_SPACE = BYTE.Difference(SPACE)
	NOT_QUOTE = BYTE.Difference(pynini.Accep("\""))
)

// buildCharUnion creates an FST that accepts any single character from the given string.
// Pattern: start -> char -> final (one state per character)
// This allows isCharUnion() to detect it and use the optimized Difference path.
func buildCharUnion(chars string) *pynini.Fst {
	result := pynini.NewFst()
	for _, ch := range chars {
		stateID := result.AddState()
		result.SetFinal(stateID, 0)
		result.AddArcStr(0, stateID, string(ch), string(ch), 0)
	}
	return result
}

func buildByte() *pynini.Fst {
	result := pynini.NewFst()
	for i := 1; i < 256; i++ {
		stateID := result.AddState()
		result.SetFinal(stateID, 0)
		result.AddArcStr(0, stateID, string(rune(i)), string(rune(i)), 0)
	}
	return result
}