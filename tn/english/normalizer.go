package english

import (
	"strings"

	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
	"github.com/TelenLiu/WeTextProcessing-go/tn/english/rules"
)

type Normalizer struct {
	*tn.Processor

	// Rule instances shared between tagger and verbalizer
	cardinalRule   *rules.Cardinal
	ordinalRule    *rules.Ordinal
	decimalRule    *rules.Decimal
	fractionRule   *rules.Fraction
	dateRule       *rules.Date
	timeRule       *rules.Time
	measureRule    *rules.Measure
	moneyRule      *rules.Money
	telephoneRule  *rules.Telephone
	electronicRule *rules.Electronic
	wordRule       *rules.Word
	whitelistRule  *rules.Whitelist
	punctRule      *rules.Punctuation
	rangeRule      *rules.Range
}

func NewNormalizer(
	cacheDir string,
	overwriteCache bool,
) *Normalizer {
	n := &Normalizer{
		Processor: tn.NewProcessor("en_normalizer", "en_tn"),
	}
	n.BuildFst("en_tn", cacheDir, overwriteCache)
	return n
}

func (n *Normalizer) BuildFst(prefix, cacheDir string, overwriteCache bool) {
	n.Processor.BuildFstWithCache(prefix, cacheDir, overwriteCache, n.buildTaggerInternal, n.buildVerbalizerInternal)
}

func (n *Normalizer) BuildTagger() {
	n.buildTaggerInternal()
}

func (n *Normalizer) buildTaggerInternal() {
	n.cardinalRule = rules.NewCardinal()
	n.ordinalRule = rules.NewOrdinal()
	n.decimalRule = rules.NewDecimal()
	n.fractionRule = rules.NewFraction()
	n.dateRule = rules.NewDate()
	n.timeRule = rules.NewTime()
	n.measureRule = rules.NewMeasure()
	n.moneyRule = rules.NewMoney()
	n.telephoneRule = rules.NewTelephone()
	n.electronicRule = rules.NewElectronic()
	n.wordRule = rules.NewWord()
	n.whitelistRule = rules.NewWhitelist()
	n.punctRule = rules.NewPunctuation()
	n.rangeRule = rules.NewRange()

	cardinal := lib.AddWeight(n.cardinalRule.Tagger, 1.0)
	ordinal := lib.AddWeight(n.ordinalRule.Tagger, 1.0)
	decimal := lib.AddWeight(n.decimalRule.Tagger, 1.0)
	fraction := lib.AddWeight(n.fractionRule.Tagger, 1.0)
	date := lib.AddWeight(n.dateRule.Tagger, 0.99)
	time := lib.AddWeight(n.timeRule.Tagger, 1.00)
	measure := lib.AddWeight(n.measureRule.Tagger, 1.00)
	money := lib.AddWeight(n.moneyRule.Tagger, 1.00)
	telephone := lib.AddWeight(n.telephoneRule.Tagger, 1.00)
	electronic := lib.AddWeight(n.electronicRule.Tagger, 1.00)
	word := lib.AddWeight(n.wordRule.Tagger, 100)
	whitelist := lib.AddWeight(n.whitelistRule.Tagger, 1.00)
	punct := lib.AddWeight(n.punctRule.Tagger, 2.00)
	rang := lib.AddWeight(n.rangeRule.Tagger, 1.01)

	// Python: (union).optimize() + (punct.plus | self.DELETE_SPACE)
	tagger := pynini.Union(cardinal, ordinal, word, date, decimal, fraction, time, measure, money, telephone, electronic, whitelist, rang, punct).Optimize()
	punctPlusOrDeleteSpace := pynini.Union(n.punctRule.Tagger.Plus(), n.DELETE_SPACE)
	tagger = tagger.Concat(punctPlusOrDeleteSpace)

	// Python: (delete(" ").star + tagger.star) @ self.build_rule(delete(" "), r="[EOS]")
	// Apply leading space prefix and tagger star, but skip EOS composition to avoid state explosion
	deleteSingleSpaceStar := lib.DeleteString(" ").Star()
	n.Tagger = deleteSingleSpaceStar.Concat(tagger.Star())
}

func (n *Normalizer) BuildVerbalizer() {
	n.buildVerbalizerInternal()
}

func (n *Normalizer) buildVerbalizerInternal() {
	cardinal := n.cardinalRule.Verbalizer
	ordinal := n.ordinalRule.Verbalizer
	decimal := n.decimalRule.Verbalizer
	fraction := n.fractionRule.Verbalizer
	word := n.wordRule.Verbalizer
	date := n.dateRule.Verbalizer
	time := n.timeRule.Verbalizer
	measure := n.measureRule.Verbalizer
	money := n.moneyRule.Verbalizer
	telephone := n.telephoneRule.Verbalizer
	electronic := n.electronicRule.Verbalizer
	whitelist := n.whitelistRule.Verbalizer
	punct := n.punctRule.Verbalizer
	rang := n.rangeRule.Verbalizer

	// Python: (union).optimize() + (punct.plus | self.INSERT_SPACE)
	verbalizer := pynini.Union(cardinal, ordinal, word, date, decimal, fraction, time, measure, money, telephone, electronic, whitelist, punct, rang).Optimize()
	punctPlusOrInsertSpace := pynini.Union(n.punctRule.Verbalizer.Plus(), n.INSERT_SPACE)
	verbalizer = verbalizer.Concat(punctPlusOrInsertSpace)

	// Python: verbalizer.star @ self.build_rule(delete(" "), r="[EOS]")
	// Skip EOS composition to avoid state explosion, handle trailing space cleanup at runtime
	n.Verbalizer = verbalizer.Star()
}

// Normalize applies full text normalization pipeline
func (n *Normalizer) Normalize(input string) string {
	if len(input) == 0 {
		return ""
	}
	tagged := n.Processor.Tag(input)
	if len(tagged) > 0 {
		output := n.Processor.Verbalize(tagged)
		// Remove trailing space (matches Python EOS behavior)
		output = strings.TrimRight(output, " ")
		return output
	}
	return input
}