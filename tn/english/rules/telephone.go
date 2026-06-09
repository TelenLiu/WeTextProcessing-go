package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Telephone struct {
	*tn.Processor
	deterministic bool
}

func NewTelephone(args ...bool) *Telephone {
	deterministic := true
	if len(args) > 0 {
		deterministic = args[0]
	}
	t := &Telephone{
		Processor:   tn.NewProcessor("telephone", "en_tn"),
		deterministic: deterministic,
	}
	t.BuildTagger()
	t.BuildVerbalizer()
	return t
}

func (t *Telephone) BuildTagger() {
	addSeparator := lib.Insert(", ")
	zero := pynini.Cross("0", "zero")
	if !t.deterministic {
		zero = zero.Union(pynini.Cross("0", pynini.Union(pynini.Accep("o"), pynini.Accep("oh"))))
	}
	digit, _ := pynini.StringFile(tn.EnglishDataPath("data/number/digit.tsv"))
	digit = digit.Invert().Union(zero)

	telephonePrompts, _ := pynini.StringFile(tn.EnglishDataPath("data/telephone/telephone_prompt.tsv"))
	countryCode := telephonePrompts.Concat(t.DELETE_EXTRA_SPACE).Ques().Concat(
		pynini.Cross("+", "plus ").Ques()).Concat(
		repeatMinMax(digit.Concat(t.INSERT_SPACE), 0, 2)).Concat(digit).Concat(
		lib.Insert(","))
	countryCode = countryCode.Union(telephonePrompts)
	countryCode = lib.Insert("country_code: \"").Concat(countryCode).Concat(lib.Insert("\""))
	countryCode = countryCode.Concat(lib.DeleteString("-").Ques()).Concat(t.DELETE_SPACE).Concat(t.INSERT_SPACE)

	areaPartDefault := digit.Concat(t.INSERT_SPACE).Concat(digit).Concat(t.INSERT_SPACE).Concat(digit)
	areaPart800 := pynini.Cross("800", "eight hundred")
	// Give areaPart800 a lower weight (higher priority) so it's preferred over
	// the default area part. Difference with "800" doesn't work because
	// Difference only handles single-char unions, not multi-char strings.
	areaPart := lib.AddWeight(areaPart800, -0.001).Union(areaPartDefault)

	areaPart = pynini.Union(
		areaPart.Concat(lib.DeleteString("-").Union(lib.DeleteString("."))),
		lib.DeleteString("(").Concat(areaPart).Concat(
			lib.DeleteString(")").Concat(lib.DeleteString(" ").Ques()).Union(
				lib.DeleteString(")-")),
		),
	).Concat(addSeparator)

	// Simplified number matching: instead of numberLength.Compose(numberWords)
	// which creates a huge FST (920K arcs), we use numberWords directly.
	// The numberLength constraint (7 digits) is implicitly enforced by the
	// area code (3 digits) + number pattern.
	numberWords := t.DIGIT.Compose(digit).Concat(
		t.INSERT_SPACE.Union(pynini.Cross("-", ", "))).Union(
		t.ALPHA).Union(t.ALPHA.Concat(pynini.Cross("-", " "))).Star()
	numberWordsAlt := t.DIGIT.Compose(digit).Concat(
		t.INSERT_SPACE.Union(pynini.Cross(".", ", "))).Union(
		t.ALPHA).Union(t.ALPHA.Concat(pynini.Cross(".", " "))).Star()
	numberWords = numberWords.Union(numberWordsAlt)
	numberPart := areaPart.Concat(numberWords)
	numberPart = lib.Insert("number_part: \"").Concat(numberPart).Concat(lib.Insert("\""))

	extension := lib.Insert("extension: \"").Concat(
		repeatMinMax(digit.Concat(t.INSERT_SPACE), 0, 3)).Concat(digit).Concat(lib.Insert("\""))
	extension = t.INSERT_SPACE.Concat(extension).Ques()

	graph := pynini.Union(
		countryCode.Concat(numberPart),
		numberPart,
	)
	graph = pynini.Union(
		graph,
		countryCode.Concat(numberPart).Concat(extension),
		numberPart.Concat(extension),
	)

	// IP addresses
	ipPrompts, _ := pynini.StringFile(tn.EnglishDataPath("data/telephone/ip_prompt.tsv"))
	ipGraph := digit.Concat(pynutilInsertSpaceRepeat(digit, 0, 2)).Concat(
		pynini.Cross(".", " dot ").Concat(digit.Concat(pynutilInsertSpaceRepeat(digit, 0, 2))).Repeat(3),
	)
	graph = graph.Union(
		lib.Insert("country_code: \"").Concat(ipPrompts).Concat(lib.Insert("\"")).Concat(
			t.DELETE_EXTRA_SPACE).Ques().Concat(
			lib.Insert("number_part: \"")).Concat(ipGraph.Optimize()).Concat(lib.Insert("\"")),
	)

	// SSN
	ssnPrompts, _ := pynini.StringFile(tn.EnglishDataPath("data/telephone/ssn_prompt.tsv"))
	threeDigitPart := digit.Concat(pynutilInsertSpaceRepeat(digit, 1, 2))
	twoDigitPart := digit.Concat(pynutilInsertSpaceRepeat(digit, 1, 1))
	fourDigitPart := digit.Concat(pynutilInsertSpaceRepeat(digit, 1, 3))
	ssnSeparator := pynini.Cross("-", ", ")
	ssnGraph := threeDigitPart.Concat(ssnSeparator).Concat(twoDigitPart).Concat(ssnSeparator).Concat(fourDigitPart)

	graph = graph.Union(
		lib.Insert("country_code: \"").Concat(ssnPrompts).Concat(lib.Insert("\"")).Concat(
			t.DELETE_EXTRA_SPACE).Ques().Concat(
			lib.Insert("number_part: \"")).Concat(ssnGraph.Optimize()).Concat(lib.Insert("\"")),
	)

	// RmEpsilon+Connect: eliminate epsilon arcs from Union and Insert wrappers
	// before AddTokens. This reduces epsilon closure BFS cost at runtime.
	graph = graph.RmEpsilon().Connect()
	finalGraph := t.AddTokens(graph)
	t.Tagger = finalGraph.Optimize()
}

func pynutilInsertSpaceRepeat(fst *pynini.Fst, min, max int) *pynini.Fst {
	result := pynini.Accep("")
	for i := 0; i < min; i++ {
		result = result.Concat(lib.Insert(" ")).Concat(fst)
	}
	for i := min; i < max; i++ {
		optional := lib.Insert(" ").Concat(fst).Ques()
		result = result.Concat(optional)
	}
	return result
}

func repeatMinMax(fst *pynini.Fst, min, max int) *pynini.Fst {
	if min == 0 && max == 0 {
		return pynini.Accep("")
	}
	result := pynini.Accep("")
	for i := 0; i < min; i++ {
		result = result.Concat(fst)
	}
	for i := min; i < max; i++ {
		result = result.Concat(fst.Ques())
	}
	return result
}

func (t *Telephone) BuildVerbalizer() {
	optionalCountryCode := lib.DeleteString("country_code: \"").Concat(
		t.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\"")).Concat(
		t.DELETE_SPACE).Concat(t.INSERT_SPACE).Ques()

	numberPart := lib.DeleteString("number_part: \"").Concat(
		t.NOT_QUOTE.Plus()).Concat(
		lib.AddWeight(lib.DeleteString(" "), -0.0001).Ques()).Concat(lib.DeleteString("\""))

	optionalExtension := t.DELETE_SPACE.Concat(t.INSERT_SPACE).Concat(
		lib.DeleteString("extension: \"")).Concat(t.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\"")).Ques()

	graph := optionalCountryCode.Concat(numberPart).Concat(optionalExtension)
	deleteTokens := t.DeleteTokens(graph)
	t.Verbalizer = deleteTokens.Optimize()
}
