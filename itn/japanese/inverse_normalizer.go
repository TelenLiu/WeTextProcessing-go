package japanese

import (
	_ "github.com/TelenLiu/WeTextProcessing-go/itn"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/itn/japanese/rules"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type InverseNormalizer struct {
	*tn.Processor

	full_to_half             bool
	enable_standalone_number bool
	enable_0_to_9            bool
	enable_million           bool

	// Rule instances shared between tagger and verbalizer
	cardinalRule *rules.Cardinal
	charRule     *rules.Char
	dateRule     *rules.Date
	fractionRule *rules.Fraction
	mathRule     *rules.Math
	measureRule  *rules.Measure
	moneyRule    *rules.Money
	ordinalRule  *rules.Ordinal
	timeRule     *rules.Time
	whitelistRule *rules.Whitelist
}

func NewInverseNormalizer(
	cache_dir string,
	overwrite_cache bool,
	full_to_half bool,
	enable_standalone_number bool,
	enable_0_to_9 bool,
	enable_million bool,
) *InverseNormalizer {
	n := &InverseNormalizer{
		Processor:                tn.NewProcessor("ja_inverse_normalizer", "itn"),
		full_to_half:             full_to_half,
		enable_standalone_number: enable_standalone_number,
		enable_0_to_9:            enable_0_to_9,
		enable_million:           enable_million,
	}
	n.BuildFst("ja_itn", cache_dir, overwrite_cache)
	return n
}

func (n *InverseNormalizer) BuildFst(prefix, cacheDir string, overwriteCache bool) {
	n.Processor.BuildFstWithCache(prefix, cacheDir, overwriteCache, n.buildTaggerInternal, n.buildVerbalizerInternal)
}

// BuildTagger builds the tagger FST (kept for backward compatibility)
func (n *InverseNormalizer) BuildTagger() {
	n.buildTaggerInternal()
}

func (n *InverseNormalizer) buildTaggerInternal() {
	// Create all rules once and share between tagger and verbalizer
	n.cardinalRule = rules.NewCardinal(n.enable_standalone_number, n.enable_0_to_9, n.enable_million)
	n.charRule = rules.NewChar()
	n.dateRule = rules.NewDate()
	n.fractionRule = rules.NewFraction()
	n.mathRule = rules.NewMath()
	n.measureRule = rules.NewMeasure(n.enable_0_to_9)
	n.moneyRule = rules.NewMoney(n.enable_0_to_9)
	n.ordinalRule = rules.NewOrdinal()
	n.timeRule = rules.NewTime()
	n.whitelistRule = rules.NewWhitelist()

	cardinal := lib.AddWeight(n.cardinalRule.Tagger, 1.06)
	char := lib.AddWeight(n.charRule.Tagger, 100)
	date := lib.AddWeight(n.dateRule.Tagger, 1.02)
	fraction := lib.AddWeight(n.fractionRule.Tagger, 1.05)
	math := lib.AddWeight(n.mathRule.Tagger, 90)
	measure := lib.AddWeight(n.measureRule.Tagger, 1.05)
	money := lib.AddWeight(n.moneyRule.Tagger, 1.04)
	ordinal := lib.AddWeight(n.ordinalRule.Tagger, 1.04)
	time := lib.AddWeight(n.timeRule.Tagger, 1.04)
	whitelist := lib.AddWeight(n.whitelistRule.Tagger, 1.01)

	tagger := pynini.Union(cardinal, char, date, fraction, math, measure, money, ordinal, time, whitelist).Optimize()
	tagger = tagger.Star()
	
	n.Tagger = tagger
}

// BuildVerbalizer builds the verbalizer FST (kept for backward compatibility)
func (n *InverseNormalizer) BuildVerbalizer() {
	n.buildVerbalizerInternal()
}

func (n *InverseNormalizer) buildVerbalizerInternal() {
	cardinal := n.cardinalRule.Verbalizer
	char := n.charRule.Verbalizer
	date := n.dateRule.Verbalizer
	fraction := n.fractionRule.Verbalizer
	math := n.mathRule.Verbalizer
	measure := n.measureRule.Verbalizer
	money := n.moneyRule.Verbalizer
	ordinal := n.ordinalRule.Verbalizer
	time := n.timeRule.Verbalizer
	whitelist := n.whitelistRule.Verbalizer

	verbalizer := pynini.Union(cardinal, char, date, fraction, math, measure, money, ordinal, time, whitelist).Optimize()

	// Only apply postprocessor if needed (currently no postprocessing is configured)
	// postprocessor := rules.NewPostProcessor(false, false, false).ProcessorFst
	// n.Verbalizer = verbalizer.At(postprocessor).Star()
	n.Verbalizer = verbalizer.Star()
}
