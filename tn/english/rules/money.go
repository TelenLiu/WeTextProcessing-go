package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Money struct {
	*tn.Processor
	deterministic bool
	decimal       *Decimal
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
	cardinal := getSharedCardinal(m.deterministic)
	cardinalGraph := cardinal.GraphWithAnd
	m.decimal = getSharedDecimal(m.deterministic)

	majSingular, _ := pynini.StringFile(tn.EnglishDataPath("data/money/currency_major.tsv"))
	majUnitPlural := majSingular.Compose(SingularToPlural())
	majUnitSingular := majSingular

	graphMajSingular := lib.Insert("currency_maj: \"").Concat(majUnitSingular).Concat(lib.Insert("\""))
	graphMajPlural := lib.Insert("currency_maj: \"").Concat(majUnitPlural).Concat(lib.Insert("\""))

	optionalDeleteFractionalZeros := lib.DeleteString(".").Concat(
		lib.AddWeight(lib.DeleteString("0"), -0.2).Plus()).Ques()

	graphIntegerOne := lib.Insert("integer_part: \"").Concat(pynini.Cross("1", "one")).Concat(lib.Insert("\""))

	// Integer-only money: $12, $1
	graphInteger := lib.Insert("integer_part: \"").Concat(
		m.VCHAR.Star().Difference(pynini.Accep("1")).Compose(cardinalGraph)).Concat(lib.Insert("\""))

	graphIntegerOnly := pynini.Union(
		graphMajSingular.Concat(m.INSERT_SPACE).Concat(graphIntegerOne),
		graphMajPlural.Concat(m.INSERT_SPACE).Concat(graphInteger),
	)

	// Integer-only with quantity: $1 million, $2 billion
	// Python: decimal_with_quantity @ graph_decimal_final handles this
	quantity, _ := pynini.StringFile(tn.EnglishDataPath("data/number/thousand.tsv"))
	graphQuantity := lib.Insert(" quantity: \"").Concat(quantity).Concat(lib.Insert("\""))
	graphIntegerWithQuantity := graphMajPlural.Concat(m.INSERT_SPACE).Concat(
		graphInteger.Concat(lib.DeleteString(" ").Ques()).Concat(graphQuantity))

	// Decimal money: $12.345
	graphDecimalInteger := lib.Insert("integer_part: \"").Concat(cardinalGraph).Concat(lib.Insert("\""))
	graphDecimalFractional := lib.Insert("fractional_part: \"").Concat(m.decimal.Graph).Concat(lib.Insert("\""))

	graphDecimalWoSign := graphDecimalInteger.Concat(
		lib.Insert(" ")).Ques().Concat(
		lib.DeleteString(".")).Concat(
		lib.Insert(" ")).Concat(
		graphDecimalFractional)

	graphDecimal := graphMajPlural.Concat(m.INSERT_SPACE).Concat(graphDecimalWoSign)

	// Decimal with quantity: $1.2 million
	graphDecimalWithQuantity := graphMajPlural.Concat(m.INSERT_SPACE).Concat(
		graphDecimalWoSign.Concat(lib.DeleteString(" ").Ques()).Concat(graphQuantity))

	graphDecimal = graphDecimal.Union(graphDecimalWithQuantity)

	finalGraph := graphIntegerOnly.Concat(optionalDeleteFractionalZeros).Union(graphDecimal).Union(graphIntegerWithQuantity)
	m.Tagger = m.AddTokens(finalGraph.Optimize())
}

func (m *Money) BuildVerbalizer() {
	keepSpace := pynini.Accep(" ")
	maj := lib.DeleteString("currency_maj: \"").Concat(m.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))

	// Use decimal.Integer which includes delete("integer_part:") prefix
	integerPart := m.decimal.Integer

	// graph_integer: integer_part + space + currency_maj
	graphInteger := integerPart.Concat(keepSpace).Concat(maj)

	// graph_decimal: decimal.numbers (optional_sign + integer + fractional_part) + space + currency_maj
	// Matching Python: decimal.numbers + keep_space + maj
	graphDecimal := m.decimal.Numbers.Concat(keepSpace).Concat(maj)

	graph := graphInteger.Union(graphDecimal)
	deleteTokens := m.DeleteTokens(graph)
	m.Verbalizer = deleteTokens.Optimize()
}
