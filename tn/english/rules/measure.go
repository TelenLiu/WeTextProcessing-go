package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Measure struct {
	*tn.Processor
	deterministic bool
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
	cardinal := NewCardinal(m.deterministic)
	cardinalGraph := cardinal.GraphWithAnd.Union(m.getRange(cardinal.GraphWithAnd))

	graphUnit, _ := pynini.StringFile(tn.EnglishDataPath("data/measure/unit.tsv"))
	if !m.deterministic {
		graphUnitAlt, _ := pynini.StringFile(tn.EnglishDataPath("data/measure/unit_alternatives.tsv"))
		graphUnit = graphUnit.Union(graphUnitAlt)
	}

	graphUnit = graphUnit.Union(
		m.TO_LOWER.Plus().Concat(m.ALPHA.Union(m.TO_LOWER)).Concat(
			m.ALPHA.Union(m.TO_LOWER).Star()).Compose(graphUnit),
	).Optimize()

	graphUnitPlural := graphUnit.Compose(SingularToPlural())

	optionalGraphNegative := lib.Insert("negative: ").Concat(
		pynini.Cross("-", "\"true\" ")).Ques()

	graphUnit2 := pynini.Cross("/", "per").Concat(m.DELETE_ZERO_OR_ONE_SPACE).Concat(
		lib.Insert(" ")).Concat(graphUnit)
	optionalGraphUnit2 := m.DELETE_ZERO_OR_ONE_SPACE.Concat(lib.Insert(" ")).Concat(graphUnit2).Ques()

	unitPlural := lib.Insert(" units: \"").Concat(
		graphUnitPlural.Concat(optionalGraphUnit2).Union(graphUnit2)).Concat(lib.Insert("\""))
	unitSingular := lib.Insert(" units: \"").Concat(
		graphUnit.Concat(optionalGraphUnit2).Union(graphUnit2)).Concat(lib.Insert("\""))

	decimal := NewDecimal(m.deterministic)
	subgraphDecimal := optionalGraphNegative.Concat(
		decimal.FinalGraphWoNegative).Concat(pynini.Accep(" ").Ques()).Concat(unitPlural)

	subgraphDecimal = subgraphDecimal.Union(
		decimal.FinalGraphWoNegative.Concat(pynini.Accep(" ").Ques()).Concat(
			lib.Insert(" units: \"")).Concat(
			pynini.Union(pynini.Accep("AM"), pynini.Accep("FM"))).Concat(lib.Insert("\"")),
	)

	// Optimized: avoid Difference which is slow on large FSTs
	// subgraphCardinal matches all numbers except "1" (which has its own singular pattern below)
	subgraphCardinal := optionalGraphNegative.Concat(
		lib.Insert("integer: \"")).Concat(cardinalGraph).Concat(
		lib.Insert("\"")).Concat(pynini.Accep(" ").Ques()).Concat(unitPlural)

	subgraphCardinal = subgraphCardinal.Union(
		optionalGraphNegative.Concat(lib.Insert("integer: \"")).Concat(
			pynini.Cross("1", "one")).Concat(lib.Insert("\"")).Concat(
			pynini.Accep(" ").Ques()).Concat(unitSingular),
	)

	unitGraph := lib.Insert("integer: \"-\" units: \"").Concat(
		pynini.Union(
			pynini.Cross("/", "per").Concat(m.DELETE_ZERO_OR_ONE_SPACE),
			pynini.Accep("per").Concat(lib.DeleteString(" ")),
		).Concat(lib.Insert(" ")).Concat(graphUnit)).Concat(lib.Insert("\""))

	decimalDashAlpha := decimal.FinalGraphWoNegative.Concat(pynini.Cross("-", "")).Concat(
		lib.Insert(" units: \"")).Concat(m.ALPHA.Plus()).Concat(lib.Insert("\""))

	decimalTimes := decimal.FinalGraphWoNegative.Concat(lib.Insert(" units: \"")).Concat(
		pynini.Union(
			pynini.Cross("x", "x"),
			pynini.Cross("X", "x"),
			pynini.Cross("x", " times"),
			pynini.Cross("X", " times"),
		)).Concat(lib.Insert("\""))

	alphaDashDecimal := lib.Insert("units: \"").Concat(m.ALPHA.Plus()).Concat(
		pynini.Accep("-")).Concat(lib.Insert("\"")).Concat(decimal.FinalGraphWoNegative)

	fraction := NewFraction(m.deterministic)
	subgraphFraction := fraction.Graph.Concat(pynini.Accep(" ").Ques()).Concat(unitPlural)

	address := m.getAddressGraph(cardinal)
	address = lib.Insert("units: \"address\" integer: \"").Concat(address).Concat(lib.Insert("\""))

	mathOp, _ := pynini.StringFile(tn.EnglishDataPath("data/measure/math_operation.tsv"))
	delimiter := pynini.Accep(" ").Union(lib.Insert(" "))

	math := cardinalGraph.Union(m.ALPHA).Concat(delimiter).Concat(mathOp).Concat(
		delimiter.Union(m.ALPHA)).Concat(cardinalGraph).Concat(delimiter).Concat(
		pynini.Cross("=", "equals")).Concat(delimiter).Concat(
		cardinalGraph.Union(m.ALPHA))
	math = math.Union(
		cardinalGraph.Union(m.ALPHA).Concat(delimiter).Concat(
			pynini.Cross("=", "equals")).Concat(delimiter).Concat(
			cardinalGraph.Union(m.ALPHA)).Concat(delimiter).Concat(
			mathOp).Concat(delimiter).Concat(cardinalGraph),
	)
	math = lib.Insert("units: \"math\" integer: \"").Concat(math).Concat(lib.Insert("\""))

	finalGraph := pynini.Union(
		subgraphDecimal, subgraphCardinal, unitGraph, decimalDashAlpha,
		decimalTimes, alphaDashDecimal, subgraphFraction, address, math,
	)
	finalGraph = m.AddTokens(finalGraph)
	m.Tagger = finalGraph.Optimize()
}

func (m *Measure) getRange(cardinal *pynini.Fst) *pynini.Fst {
	rangeGraph := cardinal.Concat(pynini.Cross(pynini.Union(pynini.Accep("-"), pynini.Accep(" - ")), " to ")).Concat(cardinal)
	for _, x := range []string{" x ", "x"} {
		rangeGraph = rangeGraph.Union(cardinal.Concat(pynini.Cross(x, " by ")).Concat(cardinal))
		if !m.deterministic {
			rangeGraph = rangeGraph.Union(cardinal.Concat(pynini.Cross(x, " times ")).Concat(cardinal.Ques()))
		}
	}
	for _, x := range []string{"*", " * "} {
		rangeGraph = rangeGraph.Union(cardinal.Concat(pynini.Cross(x, " times ")).Concat(cardinal))
	}
	return rangeGraph.Optimize()
}

func (m *Measure) getAddressGraph(cardinal *Cardinal) *pynini.Fst {

	addressNum := m.DIGIT.Repeat(2).Compose(cardinal.GraphHundredComponentAtLeastOneNoneZeroDigit)
	addressNum = addressNum.Concat(m.INSERT_SPACE).Concat(m.DIGIT.Repeat(2).Compose(
		pynini.Cross("0", "zero ").Ques().Concat(cardinal.GraphHundredComponentAtLeastOneNoneZeroDigit),
	))
	addressNum = m.DIGIT.Repeat(4).Compose(addressNum)
	addressNum = pynini.Union(addressNum, cardinal.Graph)

	direction := pynini.Union(
		pynini.Cross("E", "East"), pynini.Cross("S", "South"),
		pynini.Cross("W", "West"), pynini.Cross("N", "North"),
	).Concat(lib.DeleteString(".").Ques())
	direction = pynini.Accep(" ").Concat(direction).Ques()

	addressWords, _ := pynini.StringFile(tn.EnglishDataPath("data/address/address_word.tsv"))
	addressPattern := pynini.Accep(" ").Concat(
		pynini.Accep(" ").Concat(addressWords),
	)

	stateGraph, _ := pynini.StringFile(tn.EnglishDataPath("data/address/state.tsv"))
	state := pynini.Invert(stateGraph)
	state = pynini.Accep(",").Concat(pynini.Accep(" ")).Concat(state).Ques()

	zipCode := m.DIGIT.Repeat(5).Compose(cardinal.SingleDigitsGraph)
	zipCode = pynini.Accep(",").Ques().Concat(pynini.Accep(" ")).Concat(zipCode)

	address := addressNum.Concat(direction).Concat(addressPattern).Concat(
		pynini.Accep(", ").Concat(m.ALPHA.Union(pynini.Accep(" ")).Plus()).Concat(
			state).Concat(zipCode).Ques(),
	)
	address = address.Union(
		addressNum.Concat(direction).Concat(addressPattern).Concat(lib.DeleteString(".").Ques()),
	)
	return address
}

func (m *Measure) BuildVerbalizer() {
	cardinal := NewCardinal(m.deterministic)
	// Optimized: avoid Difference in verbalizer
	unit := lib.DeleteString("units: \"").Concat(
		m.NOT_QUOTE.Plus()).Concat(
		lib.DeleteString("\"")).Concat(m.DELETE_SPACE)

	if !m.deterministic {
		unit = unit.Union(
			unit.Compose(pynini.Cross(pynini.Union(pynini.Accep("inch"), pynini.Accep("inches")), "\"")),
		)
	}

	decimal := NewDecimal(m.deterministic)
	graphDecimal := decimal.Numbers
	graphCardinal := cardinal.DELETE_SPACE.Concat(
		lib.DeleteString("\"")).Concat(cardinal.NOT_QUOTE.Star()).Concat(lib.DeleteString("\""))

	fraction := NewFraction(m.deterministic)
	graphFraction := fraction.GraphV

	graph := pynini.Union(graphCardinal, graphDecimal, graphFraction).Concat(
		pynini.Accep(" ")).Concat(unit)
	graph = graph.Union(
		unit.Concat(m.INSERT_SPACE).Concat(pynini.Union(graphCardinal, graphDecimal)).Concat(m.DELETE_SPACE),
	)
	graph = graph.Union(
		lib.DeleteString("integer: \"-\"").Concat(m.DELETE_SPACE).Concat(unit),
	)

	address := lib.DeleteString("units: \"address\" ").Concat(m.DELETE_SPACE).Concat(graphCardinal).Concat(m.DELETE_SPACE)
	math := lib.DeleteString("units: \"math\" ").Concat(m.DELETE_SPACE).Concat(graphCardinal).Concat(m.DELETE_SPACE)
	graph = graph.Union(address).Union(math)

	deleteTokens := m.DeleteTokens(graph)
	m.Verbalizer = deleteTokens.Optimize()
}

func SingularToPlural() *pynini.Fst {
	suppletive, _ := pynini.StringFile(tn.EnglishDataPath("data/suppletive.tsv"))
	c := pynini.Union(
		pynini.Accep("b"), pynini.Accep("c"), pynini.Accep("d"), pynini.Accep("f"),
		pynini.Accep("g"), pynini.Accep("h"), pynini.Accep("j"), pynini.Accep("k"),
		pynini.Accep("l"), pynini.Accep("m"), pynini.Accep("n"), pynini.Accep("p"),
		pynini.Accep("q"), pynini.Accep("r"), pynini.Accep("s"), pynini.Accep("t"),
		pynini.Accep("v"), pynini.Accep("w"), pynini.Accep("x"), pynini.Accep("y"),
		pynini.Accep("z"),
	)

	tmp := tn.NewProcessor("tmp")
	ies := tmp.VCHAR.Star().Concat(c).Concat(pynini.Cross("y", "ies"))
	es := tmp.VCHAR.Star().Concat(pynini.Union(
		pynini.Accep("s"), pynini.Accep("sh"), pynini.Accep("ch"),
		pynini.Accep("x"), pynini.Accep("z"),
	)).Concat(lib.Insert("es"))
	s := tmp.VCHAR.Star().Concat(lib.Insert("s"))

	graphPlural := pynini.Union(suppletive, ies, es, s).Optimize()
	return graphPlural
}
