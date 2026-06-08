package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Ordinal struct {
	*tn.Processor
	Graph       *pynini.Fst
	GraphV      *pynini.Fst
	Suffix      *pynini.Fst
	deterministic bool
}

// newOrdinalInternal creates an Ordinal instance without using the shared
// singleton. Used by getSharedOrdinal to avoid recursive Once calls.
func newOrdinalInternal(deterministic bool) *Ordinal {
	o := &Ordinal{
		Processor:     tn.NewProcessor("ordinal", "en_tn"),
		deterministic: deterministic,
	}
	o.BuildTagger()
	o.BuildVerbalizer()
	return o
}

func NewOrdinal(args ...bool) *Ordinal {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	return getSharedOrdinal(deterministic)
}

func (o *Ordinal) BuildTagger() {
	cardinal := getSharedCardinal(o.deterministic)
	cardinalGraph := cardinal.Graph
	cardinalFormat := pynini.Union(o.DIGIT, lib.DeleteString(",")).Star()

	// st: ends with 1 (but not 11)
	stFormat := cardinalFormat.Concat(
		o.DIGIT.Difference(pynini.Accep("1")),
	).Ques().Concat(
		pynini.Accep("1"),
	).Concat(
		pynutilDeleteUnion("st", "ST", "ˢᵗ"),
	)

	// nd: ends with 2 (but not 12)
	ndFormat := cardinalFormat.Concat(
		o.DIGIT.Difference(pynini.Accep("1")),
	).Ques().Concat(
		pynini.Accep("2"),
	).Concat(
		pynutilDeleteUnion("nd", "ND", "ⁿᵈ"),
	)

	// rd: ends with 3 (but not 13)
	rdFormat := cardinalFormat.Concat(
		o.DIGIT.Difference(pynini.Accep("1")),
	).Ques().Concat(
		pynini.Accep("3"),
	).Concat(
		pynutilDeleteUnion("rd", "RD", "ʳᵈ"),
	)

	// th: everything else
	thFormat := pynini.Union(
		o.DIGIT.Difference(pynini.Accep("1")).Difference(pynini.Accep("2")).Difference(pynini.Accep("3")),
		cardinalFormat.Concat(pynini.Accep("1")).Concat(o.DIGIT),
		cardinalFormat.Concat(o.DIGIT.Difference(pynini.Accep("1"))).Concat(o.DIGIT.Difference(pynini.Accep("1")).Difference(pynini.Accep("2")).Difference(pynini.Accep("3"))),
	).Plus().Concat(
		pynutilDeleteUnion("th", "TH", "ᵗʰ"),
	)

	o.Graph = pynini.Union(stFormat, ndFormat, rdFormat, thFormat).Compose(cardinalGraph)
	finalGraph := lib.Insert("integer: \"").Concat(o.Graph).Concat(lib.Insert("\""))
	o.Tagger = o.AddTokens(finalGraph)
}

func (o *Ordinal) BuildVerbalizer() {
	graphDigit, _ := pynini.StringFile(tn.EnglishDataPath("data/ordinal/digit.tsv"))
	graphTeens, _ := pynini.StringFile(tn.EnglishDataPath("data/ordinal/teen.tsv"))
	graphDigit = graphDigit.Invert()
	graphTeens = graphTeens.Invert()

	graph := lib.DeleteString("integer:").Concat(
		o.DELETE_SPACE).Concat(
		lib.DeleteString("\"").Concat(
			o.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\"")),
	)

	// Build suffix replacement that applies at the end of the integer string.
	// Python: cdrewrite(suffix, "", "[EOS]", VCHAR.star)
	// We decompose the integer into: prefix + ending, where ending is matched by suffix.
	// The suffix FST maps:
	//   "one" -> "first", "two" -> "second", ..., "eleven" -> "eleventh", ...
	//   "ty" -> "tieth", and default: append "th"
	//
	// Key insight: suffix must replace the END of the string, not insert at any position.
	// We split NOT_QUOTE.Plus() into: NOT_QUOTE.Star() + (suffix_match)
	// where suffix_match is the last part that gets transformed by the suffix FST.
	//
	// For "three" -> "third": graphDigit maps "three" -> "third" (entire word)
	// For "twenty" -> "twentieth": Cross("ty", "tieth") replaces "ty" at end
	// For "five" -> "fifth": graphDigit maps "five" -> "fifth" (entire word)
	// For "hundred" -> "hundredth": Insert("th") appends "th"
	//
	// We use NOT_QUOTE.Star() for the prefix (unchanged part) and then
	// apply the suffix transformation to the remaining part.

	suffix := pynini.Union(
		lib.AddWeight(graphDigit, -0.001),      // "one"->"first", "two"->"second", "three"->"third", etc.
		lib.AddWeight(graphTeens, -0.001),      // "eleven"->"eleventh", "twelve"->"twelfth", etc.
		lib.AddWeight(pynini.Cross("ty", "tieth"), -0.001), // "twenty"->"twentieth", "thirty"->"thirtieth"
	).Union(
		// Default: append "th" to the end (e.g., "hundred" -> "hundredth")
		// Must match at least one character to avoid inserting "th" at position 0.
		// Use Plus() to match multi-char endings like "thousand", "million"
		// Higher weight than graphDigit/graphTeens so they take priority
		lib.AddWeight(o.NOT_QUOTE.Plus().Concat(lib.Insert("th")), 0.001),
	)

	// The full verbalizer: prefix (unchanged) + suffix (transformed ending)
	o.GraphV = graph.Compose(
		o.NOT_QUOTE.Star().Concat(suffix),
	)
	o.Suffix = suffix
	deleteTokens := o.DeleteTokens(o.GraphV)
	o.Verbalizer = deleteTokens.Optimize()
}

func pynutilDeleteUnion(args ...string) *pynini.Fst {
	var fsts []*pynini.Fst
	for _, s := range args {
		fsts = append(fsts, lib.DeleteString(s))
	}
	return pynini.Union(fsts...)
}
