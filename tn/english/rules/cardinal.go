package rules

import (
	"bufio"
	"os"

	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Cardinal struct {
	*tn.Processor
	Graph                                        *pynini.Fst
	GraphWithAnd                                 *pynini.Fst
	GraphHundredComponentAtLeastOneNoneZeroDigit *pynini.Fst
	SingleDigitsGraph                            *pynini.Fst
	LongNumbers                                  *pynini.Fst
	deterministic                                bool
}

// newCardinalInternal creates a Cardinal instance without using the shared
// singleton. Used by getSharedCardinal to avoid recursive Once calls.
func newCardinalInternal(deterministic bool) *Cardinal {
	c := &Cardinal{
		Processor:     tn.NewProcessor("cardinal", "en_tn"),
		deterministic: deterministic,
	}
	c.BuildTagger()
	c.BuildVerbalizer()
	return c
}

func NewCardinal(args ...bool) *Cardinal {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	return getSharedCardinal(deterministic)
}

func (c *Cardinal) BuildTagger() {
	graph_digit, _ := pynini.StringFile(tn.EnglishDataPath("data/number/digit.tsv"))
	graph_zero, _ := pynini.StringFile(tn.EnglishDataPath("data/number/zero.tsv"))
	graph_teen, _ := pynini.StringFile(tn.EnglishDataPath("data/number/teen.tsv"))
	graph_ty, _ := pynini.StringFile(tn.EnglishDataPath("data/number/ty.tsv"))

	digit_t := pynini.Union(graph_digit, graph_zero).Invert()
	teen_t := graph_teen.Invert()
	ty_t := graph_ty.Invert()

	ones := digit_t
	teens := teen_t
	twenties := ty_t
	tens := pynini.Union(
		twenties.Concat(lib.DeleteString("0")),
		lib.AddWeight(twenties.Concat(lib.Insert(" ")).Concat(ones), 0.01),
	)
	two_digits := pynini.Union(ones, teens, tens).Optimize()
	two_digits_with_zero := pynini.Union(lib.DeleteString("0").Concat(ones), two_digits)

	hundred_wa := pynini.Union(
		digit_t.Concat(lib.Insert(" hundred")).Concat(lib.DeleteString("0").Repeat(2)),
		lib.AddWeight(digit_t.Concat(lib.Insert(" hundred and ")).Concat(two_digits_with_zero), 0.01),
		lib.AddWeight(digit_t.Concat(lib.Insert(" hundred ")).Concat(two_digits_with_zero), 0.02),
	)
	zero3 := lib.DeleteString("0").Repeat(3)
	last_grp := pynini.Union(two_digits, hundred_wa, lib.AddWeight(zero3, -0.01)).Optimize()
	hundred_wo := pynini.Union(
		digit_t.Concat(lib.Insert(" hundred")).Concat(lib.DeleteString("0").Repeat(2)),
		lib.AddWeight(digit_t.Concat(lib.Insert(" hundred ")).Concat(two_digits_with_zero), 0.02),
	)
	higher := pynini.Union(two_digits, hundred_wo, lib.AddWeight(zero3, -0.01)).Optimize()

	// c.Graph is used by ordinal and time rules for composition. It needs the full
	// cardinal graph (including thousand-level handling), not just last_grp.
	c.GraphWithAnd = last_grp
	c.Graph = last_grp // will be updated to the full graph after all levels are built

	// Build 3-digit input patterns that handle leading zeros in intermediate groups.
	// The original 3-digit composition doesn't handle "024" -> "twenty four",
	// because "024" goes through hundred_wa -> "zero hundred and twenty four".
	// We add leading-zero deletion variants to fix this.
	dig3 := c.DIGIT.Concat(c.DIGIT).Concat(c.DIGIT)
	// "0XX" -> delete leading zero, match 2 digits
	dig3_del1 := lib.DeleteString("0").Concat(c.DIGIT).Concat(c.DIGIT)
	// "00X" -> delete 2 leading zeros, match 1 digit
	dig3_del2 := lib.DeleteString("0").Repeat(2).Concat(c.DIGIT)
	dig3_all := pynini.Union(dig3, dig3_del1, dig3_del2, lib.DeleteString("0").Repeat(3))
	d3 := dig3_all.Compose(higher)
	d2 := c.DIGIT.Concat(c.DIGIT).Compose(higher)
	d1_nz := pynini.Union(
		pynini.Accep("1"), pynini.Accep("2"), pynini.Accep("3"),
		pynini.Accep("4"), pynini.Accep("5"), pynini.Accep("6"),
		pynini.Accep("7"), pynini.Accep("8"), pynini.Accep("9"),
	).Compose(higher)
	d3_last := dig3_all.Compose(last_grp)
	// For "and" tail: remainder must be non-zero and less than 100
	// We exclude all-zeros patterns to avoid "one thousand and zero"
	nonZeroDigit := c.DIGIT.Difference(pynini.Accep("0"))
	// "0XX" with at least one non-zero in XX
	dig3_del1_nz := lib.DeleteString("0").Concat(
		pynini.Union(
			nonZeroDigit.Concat(c.DIGIT),
			pynini.Accep("0").Concat(nonZeroDigit),
		),
	)
	// "00X" with non-zero X
	dig3_del2_nz := lib.DeleteString("0").Repeat(2).Concat(nonZeroDigit)
	dig3_all_nz := pynini.Union(dig3, dig3_del1_nz, dig3_del2_nz)
	d3_last_no_hundred := dig3_all_nz.Compose(two_digits)

	allLevels := []*pynini.Fst{last_grp}
	intermediate := d3_last

	names := readThousandNames()
	// Limit to 3 levels for performance (thousand -> billion).
	// 5 levels (up to quadrillion) creates 136K+ states which is too slow.
	// Most real-world text only needs up to billions.
	if len(names) > 3 {
		names = names[:3]
	}
	for i, name := range names {
		// Without "and"
		l1 := lib.AddWeight(d1_nz.Concat(lib.Insert(" "+name+" ")).Concat(intermediate), 0.01)
		l2 := lib.AddWeight(d2.Concat(lib.Insert(" "+name+" ")).Concat(intermediate), 0.01)
		l3 := lib.AddWeight(d3.Concat(lib.Insert(" "+name+" ")).Concat(intermediate), 0.01)

		// With "and" - only for thousand level, remainder must be < 100 and non-zero
		var level *pynini.Fst
		if i == 0 {
			l1_and := d1_nz.Concat(lib.Insert(" " + name + " and ")).Concat(d3_last_no_hundred)
			l2_and := d2.Concat(lib.Insert(" " + name + " and ")).Concat(d3_last_no_hundred)
			l3_and := d3.Concat(lib.Insert(" " + name + " and ")).Concat(d3_last_no_hundred)
			level = pynini.Union(l1, l2, l3, l1_and, l2_and, l3_and).Optimize()
		} else {
			level = pynini.Union(l1, l2, l3).Optimize()
		}

		intermediate = d3.Concat(lib.Insert(" " + name + " ")).Concat(intermediate).Optimize()
		allLevels = append(allLevels, pynini.Union(allLevels[i], level).Optimize())
	}

	all := allLevels[len(allLevels)-1]

	// add_optional_and: Python's add_optional_and adds variants:
	// 1. "hundred " -> "hundred and " (already handled in hundred_wa)
	// 2. optional_hundred: for 3-digit numbers, allow omitting "hundred"
	//    e.g., "256" -> "two fifty six" (in addition to "two hundred and fifty six")
	// Python: optional_hundred = compose((DIGIT - "0")**3, graph) then
	// compose(optional_hundred, VCHAR* + cross(" hundred", "") + VCHAR*)
	// We implement this directly: digit_t + " " + (DIGIT{2} @ two_digits_with_zero)
	// Weight is set higher than hundred_wa (0.02) so "two hundred and fifty six"
	// is preferred by default, but "two fifty six" is available as alternative.
	dig2 := c.DIGIT.Concat(c.DIGIT)
	optionalHundred := lib.AddWeight(
		digit_t.Concat(lib.Insert(" ")).Concat(dig2.Compose(two_digits_with_zero)),
		0.03,
	)
	all = all.Union(optionalHundred).Optimize()

	// Build SingleDigitsGraph with both "zero" and "oh" variants.
	// Python: single_digits_graph_zero uses "zero", single_digits_graph_oh uses "oh".
	// The same variant must be used consistently within a single token.
	// "zero" variant is preferred (lower weight) matching Python's default behavior.
	graphOh := pynini.Union(graph_digit).Invert().Union(pynini.Cross("0", "oh"))
	singleDigitsOh := lib.AddWeight(
		graphOh.Concat(lib.Insert(" ").Concat(graphOh).Star()),
		0.0001,
	)
	singleDigitsZero := digit_t.Concat(lib.Insert(" ").Concat(digit_t).Star())

	c.SingleDigitsGraph = lib.AddWeight(
		singleDigitsZero.Union(singleDigitsOh), 0.1,
	)

	// Tagger: positive: "integer: ...", negative: "negative: true integer: ..."
	// Matching Python's field name "integer:" (not "value:")
	posTag := lib.Insert("integer: \"").Concat(all).Concat(lib.Insert("\""))
	negTag := pynini.Cross("-", "negative: \"true\" ").Concat(
		lib.Insert("integer: \"").Concat(all).Concat(lib.Insert("\"")),
	)
	tagger := pynini.Union(posTag, negTag)
	// RmEpsilon+Connect: eliminate epsilon arcs from Union and Insert wrappers
	// before AddTokens. This reduces epsilon closure BFS cost at runtime.
	tagger = tagger.RmEpsilon().Connect()
	c.Tagger = c.AddTokens(tagger)
	// RmEpsilon+Connect after AddTokens too, to eliminate epsilon arcs
	// from the Insert("cardinal { ") and Insert(" } ") wrappers.
	c.Tagger = c.Tagger.RmEpsilon().Connect()

	atLeastOneNonzero := pynini.Union(
		c.DIGIT.Concat(c.DIGIT),
		c.DIGIT.Concat(c.DIGIT).Concat(c.DIGIT),
		c.DIGIT.Difference(pynini.Accep("0")),
	)
	c.GraphHundredComponentAtLeastOneNoneZeroDigit = atLeastOneNonzero.Compose(last_grp).Optimize()
	c.LongNumbers = c.SingleDigitsGraph
}

func (c *Cardinal) BuildVerbalizer() {
	// Matching Python: optional_sign maps "negative: true" to "negative" or "minus"
	// Python test data expects "negative" as the default output
	negativeSign := pynini.Cross("negative: \"true\"", "negative ")
	minusSign := lib.AddWeight(pynini.Cross("negative: \"true\"", "minus "), 0.0001)
	optionalSign := negativeSign.Union(minusSign)
	optionalSign = (optionalSign.Concat(c.DELETE_SPACE)).Ques()
	integer := c.DELETE_SPACE.Concat(
		lib.DeleteString("\"").Concat(c.NOT_QUOTE.Star()).Concat(lib.DeleteString("\"")),
	)
	integer = lib.DeleteString("integer:").Concat(integer)
	c.Verbalizer = c.DeleteTokens(optionalSign.Concat(integer)).Optimize()
}

func (c *Cardinal) SetCachedGraph(graph *pynini.Fst) { c.Graph = graph }
func (c *Cardinal) SetCachedGraphHundredComponent(f *pynini.Fst) {
	c.GraphHundredComponentAtLeastOneNoneZeroDigit = f
}
func (c *Cardinal) SetCachedSingleDigits(f *pynini.Fst) { c.SingleDigitsGraph = f }
func (c *Cardinal) SetCachedLongNumbers(f *pynini.Fst)  { c.LongNumbers = f }

func readThousandNames() []string {
	f, err := os.Open(tn.EnglishDataPath("data/number/thousand.tsv"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var names []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 0 && line != "hundred" {
			names = append(names, line)
		}
	}
	return names
}
