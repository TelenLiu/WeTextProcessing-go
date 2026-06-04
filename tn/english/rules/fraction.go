package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Fraction struct {
	*tn.Processor
	Graph         *pynini.Fst
	GraphV        *pynini.Fst
	deterministic bool
}

func NewFraction(args ...bool) *Fraction {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	f := &Fraction{
		Processor:   tn.NewProcessor("fraction", "en_tn"),
		deterministic: deterministic,
	}
	f.BuildTagger()
	f.BuildVerbalizer()
	return f
}

func (f *Fraction) BuildTagger() {
	cardinal := NewCardinal(f.deterministic)
	cardinalGraph := cardinal.Graph

	integer := lib.Insert("integer_part: \"").Concat(cardinalGraph).Concat(lib.Insert("\""))
	numerator := lib.Insert("numerator: \"").Concat(
		cardinalGraph).Concat(
		pynini.Union(pynini.Cross("/", "\" "), pynini.Cross(" / ", "\" ")))

	endings := []string{"rd", "th", "st", "nd", "RD", "TH", "ST", "ND"}
	var endFsts []*pynini.Fst
	for _, e := range endings {
		endFsts = append(endFsts, pynini.Cross(e, ""))
	}
	optionalEnd := pynini.Union(endFsts...).Ques()

	denominator := lib.Insert("denominator: \"").Concat(
		cardinalGraph).Concat(optionalEnd).Concat(lib.Insert("\""))

	fractionTsv, _ := pynini.StringFile(tn.EnglishDataPath("data/number/fraction.tsv"))

	graph := pynini.Union(
		integer.Concat(pynini.Accep(" ")).Ques().Concat(numerator.Concat(denominator)),
		integer.Concat(pynini.Union(pynini.Accep(" "), lib.Insert(" "))).Ques().Concat(
			fractionTsv.Compose(numerator.Concat(denominator)),
		),
	)

	f.Graph = graph
	finalGraph := f.AddTokens(f.Graph)
	f.Tagger = finalGraph
}

func (f *Fraction) BuildVerbalizer() {
	ordinal := NewOrdinal(f.deterministic)
	suffix := ordinal.Suffix

	integer := lib.DeleteString("integer_part: \"").Concat(
		f.NOT_QUOTE.Star()).Concat(lib.DeleteString("\" "))

	// Build denominator verbalization
	// For numerator "one": use singular (half, quarter, third, fifth, etc.)
	// For numerator != "one": use plural (halves, quarters, thirds, fifths, etc.)

	// Singular denominators - with priority weights matching Python's _priority_union
	denominatorOne := pynini.Cross("denominator: \"one\"", "over one")
	denominatorHalf := lib.AddWeight(pynini.Cross("denominator: \"two\"", "half"), -0.0001)
	denominatorQuarter := lib.AddWeight(pynini.Cross("denominator: \"four\"", "quarter"), -0.0001)
	// Other singular: apply ordinal suffix (e.g., "three" -> "third", "five" -> "fifth")
	denominatorRestSingular := lib.DeleteString("denominator: \"").Concat(
		f.NOT_QUOTE.Star().Compose(suffix)).Concat(lib.DeleteString("\""))

	denominatorsSingular := pynini.Union(
		denominatorOne,
		denominatorHalf,
		denominatorQuarter,
		denominatorRestSingular,
	).Optimize()

	// Plural denominators - with priority weights matching Python's _priority_union
	denominatorHalves := lib.AddWeight(pynini.Cross("denominator: \"two\"", "halves"), -0.0001)
	denominatorQuarters := lib.AddWeight(pynini.Cross("denominator: \"four\"", "quarters"), -0.0001)
	// Other plural: apply ordinal suffix + "s" (e.g., "three" -> "thirds", "five" -> "fifths")
	denominatorRestPlural := lib.DeleteString("denominator: \"").Concat(
		f.NOT_QUOTE.Star().Compose(suffix)).Concat(
		lib.Insert("s")).Concat(lib.DeleteString("\""))

	denominatorsPlural := pynini.Union(
		denominatorOne,
		denominatorHalves,
		denominatorQuarters,
		denominatorRestPlural,
	).Optimize()

	// Build two separate verbalizer paths:
	// Path 1: numerator is "one" -> keep "one" in output + singular denominators
	// Path 2: numerator is not "one" -> keep numerator in output + plural denominators
	// Python: numerator_one = delete('numerator: "') + accep("one") + delete('" ')

	// Path 1: numerator "one" with singular denominators
	// Keep "one" in output (matching Python: pynini.accep("one"))
	pathOne := lib.AddWeight(
		lib.DeleteString("numerator: \"").Concat(
			pynini.Accep("one")).Concat(lib.DeleteString("\" ")).Concat(
			f.INSERT_SPACE).Concat(denominatorsSingular),
		-1.0,
	)

	// Path 2: numerator != "one" with plural denominators
	pathRest := lib.DeleteString("numerator: \"").Concat(
		f.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\" ")).Concat(
		f.INSERT_SPACE).Concat(denominatorsPlural)

	graph := pathOne.Union(pathRest)
	conjunction := lib.Insert("and ")
	integer = integer.Concat(f.INSERT_SPACE).Concat(conjunction).Ques()
	graph = integer.Concat(graph)

	f.GraphV = graph
	deleteTokens := f.DeleteTokens(f.GraphV)
	f.Verbalizer = deleteTokens.Optimize()
}
