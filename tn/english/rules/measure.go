package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Measure struct {
	*tn.Processor
	deterministic bool
	decimal       *Decimal
}

func NewMeasure(args ...bool) *Measure {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	m := &Measure{
		Processor:   tn.NewProcessor("measure", "en_tn"),
		deterministic: deterministic,
	}
	m.BuildTagger()
	m.BuildVerbalizer()
	return m
}

func (m *Measure) BuildTagger() {
	cardinal := getSharedCardinal(m.deterministic)
	cardinalGraph := cardinal.GraphWithAnd

	// Load unit mappings directly from TSV files
	graphUnit, _ := pynini.StringFile(tn.EnglishDataPath("data/measure/unit.tsv"))
	if !m.deterministic {
		graphUnitAlt, _ := pynini.StringFile(tn.EnglishDataPath("data/measure/unit_alternatives.tsv"))
		graphUnit = graphUnit.Union(graphUnitAlt)
	}
	graphUnit = graphUnit.Optimize()

	graphUnitPlural := graphUnit.Compose(SingularToPlural())

	// Simplified: only support cardinal + unit (most common pattern like "10 km")
	unitPlural := lib.Insert(" units: \"").Concat(graphUnitPlural).Concat(lib.Insert("\""))
	unitSingular := lib.Insert(" units: \"").Concat(graphUnit).Concat(lib.Insert("\""))

	subgraphCardinal := lib.Insert("integer: \"").Concat(cardinalGraph).Concat(
		lib.Insert("\"")).Concat(pynini.Accep(" ").Ques()).Concat(unitPlural)

	subgraphCardinal = subgraphCardinal.Union(
		lib.Insert("integer: \"").Concat(
			pynini.Cross("1", "one")).Concat(lib.Insert("\"")).Concat(
			pynini.Accep(" ").Ques()).Concat(unitSingular),
	)

	// Also support decimal + unit with simplified decimal (no quantity)
	m.decimal = getSharedDecimal(m.deterministic)
	graphDecimalInteger := lib.Insert("integer_part: \"").Concat(cardinalGraph).Concat(lib.Insert("\""))
	graphDecimalFractional := lib.Insert("fractional_part: \"").Concat(m.decimal.Graph).Concat(lib.Insert("\""))
	graphDecimalWoSign := graphDecimalInteger.Concat(
		lib.Insert(" ")).Ques().Concat(
		lib.DeleteString(".")).Concat(
		lib.Insert(" ")).Concat(
		graphDecimalFractional)

	subgraphDecimal := graphDecimalWoSign.Concat(pynini.Accep(" ").Ques()).Concat(unitPlural)

	finalGraph := pynini.Union(subgraphCardinal, subgraphDecimal)
	finalGraph = m.AddTokens(finalGraph)
	m.Tagger = finalGraph.Optimize()
}

func (m *Measure) BuildVerbalizer() {
	unit := lib.DeleteString("units: \"").Concat(
		m.NOT_QUOTE.Plus()).Concat(
		lib.DeleteString("\"")).Concat(m.DELETE_SPACE)

	if !m.deterministic {
		unit = unit.Union(
			unit.Compose(pynini.Cross(pynini.Union(pynini.Accep("inch"), pynini.Accep("inches")), "\"")),
		)
	}

	graphDecimal := m.decimal.Numbers

	// For integer field: just extract the text between quotes
	graphInteger := lib.DeleteString("integer:").Concat(m.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(m.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))

	graph := pynini.Union(graphInteger, graphDecimal).Concat(
		pynini.Accep(" ")).Concat(unit)

	deleteTokens := m.DeleteTokens(graph)
	m.Verbalizer = deleteTokens.Optimize()
}
