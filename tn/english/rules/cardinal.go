package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Cardinal struct {
	*tn.Processor
	Graph                                                          *pynini.Fst
	GraphWithAnd                                                   *pynini.Fst
	GraphHundredComponentAtLeastOneNoneZeroDigit                   *pynini.Fst
	SingleDigitsGraph                                              *pynini.Fst
	LongNumbers                                                    *pynini.Fst
	deterministic                                                  bool
}

func NewCardinal(args ...bool) *Cardinal {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	c := &Cardinal{
		Processor:   tn.NewProcessor("cardinal", "en_tn"),
		deterministic: deterministic,
	}
	c.BuildTagger()
	c.BuildVerbalizer()
	return c
}

func (c *Cardinal) BuildTagger() {
	graph_teen, _ := pynini.StringFile(tn.EnglishDataPath("data/number/teen.tsv"))
	graph_zero, _ := pynini.StringFile(tn.EnglishDataPath("data/number/zero.tsv"))
	graph_digit, _ := pynini.StringFile(tn.EnglishDataPath("data/number/digit.tsv"))
	graph_ty, _ := pynini.StringFile(tn.EnglishDataPath("data/number/ty.tsv"))
	graph_thousand, _ := pynini.StringFile(tn.EnglishDataPath("data/number/thousand.tsv"))
	_ = graph_thousand

	// single digit component: 0-9 (text -> digit)
	single_digits := pynini.Union(graph_zero, graph_digit)
	// tens component: 20, 30, ..., 90 (text -> digit)
	ties_graph := graph_ty
	// tens with optional ones: 20-99 (text -> digit)
	ties_and_ones := pynini.Union(
		ties_graph,
		ties_graph.Concat(lib.Insert(" ")).Concat(graph_digit),
	)
	// full 0-99 (text -> digit)
	two_digits := pynini.Union(single_digits, graph_teen, ties_and_ones)
	// digit -> text (inverted for composition with digitPattern)
	two_digits_inv := two_digits.Invert()

	// hundred component (digit -> text)
	hundred_component_inv := two_digits_inv.Concat(
		pynini.Union(
			lib.Insert(" hundred"),
			lib.Insert(" hundred ").Concat(two_digits_inv),
		),
	)
	// ensure at least one non-zero digit: 2-3 digits OR single non-zero digit
	atLeastOneNonzero := pynini.Union(
		c.DIGIT.Concat(c.DIGIT),
		c.DIGIT.Concat(c.DIGIT).Concat(c.DIGIT),
		c.DIGIT.Difference(pynini.Accep("0")),
	)
	c.GraphHundredComponentAtLeastOneNoneZeroDigit = atLeastOneNonzero.Compose(hundred_component_inv)

	// Build main graph for numbers like 1,234,567 or 1234567
	// Pattern: (1-3 digits)((comma + 3 digits)* | (3 digits)*)
	digit1to3 := pynini.Union(
		c.DIGIT,
		c.DIGIT.Concat(c.DIGIT),
		c.DIGIT.Concat(c.DIGIT).Concat(c.DIGIT),
	)
	comma3digits := lib.DeleteString(",").Concat(c.DIGIT).Concat(c.DIGIT).Concat(c.DIGIT)
	digitPattern := digit1to3.Concat(
		pynini.Union(
			comma3digits.Star(),
			c.DIGIT.Concat(c.DIGIT).Concat(c.DIGIT).Star(),
		),
	)

	c.Graph = digitPattern.Compose(hundred_component_inv).Optimize()

	graph_au := c.AddOptionalAnd(c.Graph)

	// single digits graph: "123" -> "one two three" (for long numbers)
	singleDigitsGraph := graph_digit.Union(graph_zero).Invert()
	c.SingleDigitsGraph = singleDigitsGraph.Concat(
		lib.Insert(" ").Concat(singleDigitsGraph).Star(),
	)

	var finalGraph *pynini.Fst
	if c.deterministic {
		longNumbers := c.DIGIT.Repeat(5).Compose(
			c.SingleDigitsGraph,
		).Optimize()
		c.GraphWithAnd = c.AddOptionalAnd(c.Graph)
		longNumbers = pynini.Union(longNumbers, c.GraphWithAnd).Optimize()
		cardinalWithLeadingZeros := pynini.Accep("0").Concat(c.DIGIT.Star()).Compose(
			c.SingleDigitsGraph,
		)
		finalGraph = longNumbers.Union(cardinalWithLeadingZeros)
		finalGraph = finalGraph.Union(graph_au)
	} else {
		leadingZeros := pynini.Accep("0").Plus().Compose(
			c.SingleDigitsGraph,
		)
		c.GraphWithAnd = c.AddOptionalAnd(c.Graph)
		cardinalWithLeadingZeros := leadingZeros.Concat(
			lib.Insert(" ")).Concat(
			c.DIGIT.Star().Compose(c.GraphWithAnd),
		)
		longNumbers := c.GraphWithAnd.Union(
			lib.AddWeight(c.SingleDigitsGraph, 0.0001),
		)
		finalGraph = pynini.Union(
			longNumbers,
			cardinalWithLeadingZeros,
		).Optimize()

		// one to a replacement
		oneToA := pynini.Union(
			pynini.Cross("one hundred", "a hundred"),
			pynini.Cross("one thousand", "thousand"),
			pynini.Cross("one million", "a million"),
		)
		finalGraph = finalGraph.Union(
			finalGraph.Compose(
				oneToA.Optimize().Concat(c.VCHAR.Star()),
			).Optimize(),
		)
		// remove commas for 4 digit numbers
		fourDigitComma := c.DIGIT.Difference(pynini.Accep("0")).Concat(
			lib.DeleteString(",")).Concat(
			c.DIGIT.Concat(c.DIGIT).Concat(c.DIGIT),
		)
		finalGraph = finalGraph.Union(
			fourDigitComma.Optimize().Compose(finalGraph).Optimize(),
		)
	}

	optionalMinus := lib.Insert("negative: \"").Concat(
		pynini.Cross("-", "true\" "),
	).Ques()
	finalGraph = optionalMinus.Concat(
		lib.Insert("integer: \"").Concat(finalGraph).Concat(lib.Insert("\"")),
	)
	c.Tagger = c.AddTokens(finalGraph)
}

func (c *Cardinal) AddOptionalAnd(graph *pynini.Fst) *pynini.Fst {
	graphWithAnd := lib.AddWeight(graph, 0.00001)
	notQuote := c.NOT_QUOTE.Star()

	// no thousand/million context
	noThousandMillion := notQuote.Difference(
		notQuote.Concat(
			pynini.Union(pynini.Accep("thousand"), pynini.Accep("million")).Concat(notQuote),
		),
	).Optimize()

	integer := notQuote.Concat(
		lib.AddWeight(
			pynini.Cross("hundred ", "hundred and ").Concat(noThousandMillion),
			-0.0001,
		),
	).Optimize()

	// no hundred context
	noHundred := c.VCHAR.Star().Difference(
		notQuote.Concat(pynini.Accep("hundred").Concat(notQuote)),
	).Optimize()
	integer = integer.Union(
		notQuote.Concat(
			lib.AddWeight(
				pynini.Cross("thousand ", "thousand and ").Concat(noHundred),
				-0.0001,
			),
		),
	).Optimize()

	// optional hundred: 3 digits without leading zero
	optionalHundred := c.DIGIT.Difference(pynini.Accep("0")).Repeat(3)
	optionalHundred = optionalHundred.Compose(graph).Optimize()
	optionalHundred = optionalHundred.Compose(
		c.VCHAR.Star().Concat(
			pynini.Cross(" hundred", "")).Concat(c.VCHAR.Star()),
	)

	graphWithAnd = graph.Compose(integer).Optimize().Union(graphWithAnd)
	graphWithAnd = graphWithAnd.Union(optionalHundred)
	return graphWithAnd
}

func (c *Cardinal) BuildVerbalizer() {
	optionalSign := pynini.Cross("negative: \"true\"", "minus ")
	if !c.deterministic {
		optionalSign = optionalSign.Union(
			pynini.Cross("negative: \"true\"", "negative "),
		).Union(
			pynini.Cross("negative: \"true\"", "dash "),
		)
	}

	optionalSign = (optionalSign.Concat(c.DELETE_SPACE)).Ques()

	integer := c.DELETE_SPACE.Concat(
		lib.DeleteString("\"").Concat(
			c.NOT_QUOTE.Star()).Concat(lib.DeleteString("\"")),
	)
	integer = lib.DeleteString("integer:").Concat(integer)

	numbers := optionalSign.Concat(integer)
	deleteTokens := c.DeleteTokens(numbers)
	c.Verbalizer = deleteTokens.Optimize()
}
