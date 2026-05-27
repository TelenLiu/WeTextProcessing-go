package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Money struct {
	*tn.Processor
	deterministic bool
}

func NewMoney(args ...bool) *Money {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	m := &Money{
		Processor:   tn.NewProcessor("money", "en_tn"),
		deterministic: deterministic,
	}
	m.BuildTagger()
	m.BuildVerbalizer()
	return m
}

func (m *Money) BuildTagger() {
	cardinal := NewCardinal(m.deterministic)
	cardinalGraph := cardinal.GraphWithAnd
	decimal := NewDecimal(m.deterministic)
	graphDecimalFinal := decimal.FinalGraphWoNegativeWAbbr

	majSingular, _ := pynini.StringFile(tn.EnglishDataPath("data/money/currency_major.tsv"))
	majUnitPlural := majSingular.Compose(SingularToPlural())
	majUnitSingular := majSingular

	graphMajSingular := lib.Insert("currency_maj: \"").Concat(majUnitSingular).Concat(lib.Insert("\""))
	graphMajPlural := lib.Insert("currency_maj: \"").Concat(majUnitPlural).Concat(lib.Insert("\""))

	optionalDeleteFractionalZeros := lib.DeleteString(".").Concat(
		lib.AddWeight(lib.DeleteString("0"), -0.2).Plus()).Ques()

	graphIntegerOne := lib.Insert("integer_part: \"").Concat(pynini.Cross("1", "one")).Concat(lib.Insert("\""))

	decimalDeleteLastZeros := m.DIGIT.Union(lib.DeleteString(",")).Star().Concat(
		pynini.Accep(".")).Concat(m.DIGIT.Plus()).Concat(
		lib.AddWeight(lib.DeleteString("0"), -0.01).Star())

	decimalWithQuantity := m.VCHAR.Star().Concat(m.ALPHA)

	graphDecimal := graphMajPlural.Concat(m.INSERT_SPACE).Concat(
		decimalDeleteLastZeros.Union(decimalWithQuantity)).Compose(graphDecimalFinal)

	graphInteger := lib.Insert("integer_part: \"").Concat(
		m.VCHAR.Star().Difference(pynini.Accep("1")).Compose(cardinalGraph)).Concat(lib.Insert("\""))

	graphIntegerOnly := pynini.Union(
		graphMajSingular.Concat(m.INSERT_SPACE).Concat(graphIntegerOne),
		graphMajPlural.Concat(m.INSERT_SPACE).Concat(graphInteger),
	)

	finalGraph := graphIntegerOnly.Concat(optionalDeleteFractionalZeros).Union(graphDecimal)
	m.Tagger = m.AddTokens(finalGraph.Optimize())
}

func (m *Money) BuildVerbalizer() {
	decimal := NewDecimal(m.deterministic)
	keepSpace := pynini.Accep(" ")
	maj := lib.DeleteString("currency_maj: \"").Concat(m.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))

	_ = lib.DeleteString("fractional_part: \"").Concat(m.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\"")) // fractionalPart reference

	integerPart := decimal.DELETE_SPACE.Concat(
		lib.DeleteString("\"")).Concat(decimal.NOT_QUOTE.Star()).Concat(lib.DeleteString("\""))

	graphInteger := integerPart.Concat(keepSpace).Concat(maj)
	graphDecimal := decimal.OptionalSign.Concat(decimal.Integer).Concat(keepSpace).Concat(maj)

	graph := graphInteger.Union(graphDecimal)
	deleteTokens := m.DeleteTokens(graph)
	m.Verbalizer = deleteTokens.Optimize()
}
