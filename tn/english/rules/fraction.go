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

	denominatorOne := pynini.Cross("denominator: \"one\"", "over one")
	denominatorHalf := pynini.Cross("denominator: \"two\"", "half")
	denominatorQuarter := pynini.Cross("denominator: \"four\"", "quarter")

	denominatorRest := lib.DeleteString("denominator: \"").Concat(
		f.NOT_QUOTE.Star().Compose(suffix)).Concat(lib.DeleteString("\""))

	denominators := pynini.Union(
		denominatorOne,
		denominatorHalf,
		denominatorQuarter,
		denominatorRest,
	).Optimize()

	if !f.deterministic {
		graphFour, _ := pynini.StringFile(tn.EnglishDataPath("data/ordinal/digit.tsv"))
		graphFour = graphFour.Invert()
		denominators = denominators.Union(
			lib.DeleteString("denominator: \"").Concat(
				pynini.Accep("four").Compose(graphFour)).Concat(lib.DeleteString("\"")),
		)
	}

	// Optimized: avoid Difference which is slow on large FSTs
	// Use separate patterns for "one" and "not one"
	numeratorOne := lib.DeleteString("numerator: \"one\"").Concat(
		f.INSERT_SPACE).Concat(denominators)

	// For numerator rest, just match any content (skip the half->halves cdrewrite for now)
	numeratorRest := lib.DeleteString("numerator: \"").Concat(
		f.NOT_QUOTE.Star()).Concat(lib.DeleteString("\" ")).Concat(
		f.INSERT_SPACE).Concat(denominators)

	graph := numeratorOne.Union(numeratorRest)
	conjunction := lib.Insert("and ")
	integer = integer.Concat(f.INSERT_SPACE).Concat(conjunction).Ques()
	graph = integer.Concat(graph)

	f.GraphV = graph
	deleteTokens := f.DeleteTokens(f.GraphV)
	f.Verbalizer = deleteTokens.Optimize()
}
