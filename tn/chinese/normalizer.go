package chinese

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
	"github.com/TelenLiu/WeTextProcessing-go/tn/chinese/rules"
)

type Normalizer struct {
	*tn.Processor

	remove_interjections   bool
	remove_erhua          bool
	traditional_to_simple bool
	remove_puncts        bool
	full_to_half         bool
	tag_oov              bool

	preProcessor  *rules.PreProcessor
	postProcessor *rules.PostProcessor

	// Rule instances shared between tagger and verbalizer
	cardinalRule  *rules.Cardinal
	dateRule      *rules.Date
	whitelistRule *rules.Whitelist
	sportRule     *rules.Sport
	fractionRule  *rules.Fraction
	measureRule   *rules.Measure
	moneyRule     *rules.Money
	timeRule      *rules.Time
	mathRule      *rules.Math
	charRule      *rules.Char
}

func NewNormalizer(
	cache_dir string,
	overwrite_cache bool,
	remove_interjections bool,
	remove_erhua bool,
	traditional_to_simple bool,
	remove_puncts bool,
	full_to_half bool,
	tag_oov bool,
) *Normalizer {
	n := &Normalizer{
		Processor:            tn.NewProcessor("zh_normalizer"),
		remove_interjections: remove_interjections,
		remove_erhua:         remove_erhua,
		traditional_to_simple: traditional_to_simple,
		remove_puncts:        remove_puncts,
		full_to_half:         full_to_half,
		tag_oov:              tag_oov,
	}
	n.BuildFst("zh_tn", cache_dir, overwrite_cache)
	return n
}

func (n *Normalizer) BuildFst(prefix, cacheDir string, overwriteCache bool) {
	n.Processor.BuildFstWithCache(prefix, cacheDir, overwriteCache, n.buildTaggerInternal, n.buildVerbalizerInternal)
}

// BuildTagger builds the tagger FST (kept for backward compatibility)
func (n *Normalizer) BuildTagger() {
	n.buildTaggerInternal()
}

func (n *Normalizer) buildTaggerInternal() {
	// Create all rules once and share between tagger and verbalizer
	n.cardinalRule = rules.NewCardinal()
	n.dateRule = rules.NewDate()
	n.whitelistRule = rules.NewWhitelist(n.remove_erhua)
	n.sportRule = rules.NewSport()
	n.fractionRule = rules.NewFraction()
	n.measureRule = rules.NewMeasure()
	n.moneyRule = rules.NewMoney()
	n.timeRule = rules.NewTime()
	n.mathRule = rules.NewMathWithCardinal(n.cardinalRule)
	n.charRule = rules.NewChar()

	// Do NOT compose PreProcessor into the Tagger FST - apply at runtime instead
	n.preProcessor = rules.NewPreProcessor(n.traditional_to_simple)

	date := lib.AddWeight(n.dateRule.Tagger, 1.02)
	whitelist := lib.AddWeight(n.whitelistRule.Tagger, 1.03)
	sport := lib.AddWeight(n.sportRule.Tagger, 1.04)
	fraction := lib.AddWeight(n.fractionRule.Tagger, 1.05)
	measure := lib.AddWeight(n.measureRule.Tagger, 1.05)
	money := lib.AddWeight(n.moneyRule.Tagger, 1.05)
	time := lib.AddWeight(n.timeRule.Tagger, 1.05)
	cardinal := lib.AddWeight(n.cardinalRule.Tagger, 1.06)
	math := lib.AddWeight(n.mathRule.Tagger, 90)
	char := lib.AddWeight(n.charRule.Tagger, 100)

	tagger := pynini.Union(date, whitelist, sport, fraction, measure, money, time, cardinal, math, char)
	n.Tagger = tagger.Star()
}

// BuildVerbalizer builds the verbalizer FST (kept for backward compatibility)
func (n *Normalizer) BuildVerbalizer() {
	n.buildVerbalizerInternal()
}

func (n *Normalizer) buildVerbalizerInternal() {
	cardinal := n.cardinalRule.Verbalizer
	ch := n.charRule.Verbalizer
	date := n.dateRule.Verbalizer
	fraction := n.fractionRule.Verbalizer
	math := n.mathRule.Verbalizer
	measure := n.measureRule.Verbalizer
	money := n.moneyRule.Verbalizer
	sport := n.sportRule.Verbalizer
	time := n.timeRule.Verbalizer
	whitelist := n.whitelistRule.Verbalizer

	verbalizer := pynini.Union(cardinal, ch, date, fraction, math, measure, money, sport, time, whitelist)

	// Do NOT compose PostProcessor into the Verbalizer FST - apply at runtime instead
	n.postProcessor = rules.NewPostProcessor(
		n.remove_interjections,
		n.remove_puncts,
		n.full_to_half,
		n.tag_oov,
	)
	n.Verbalizer = verbalizer.Star()
}

// Normalize applies full text normalization pipeline
func (n *Normalizer) Normalize(input string) string {
	if len(input) == 0 {
		return ""
	}
	// Apply preprocessor first
	if n.preProcessor != nil {
		input = n.preProcessor.Apply(input)
	}
	// Try to tag and verbalize
	tagged := n.Processor.Tag(input)
	if len(tagged) > 0 {
		// Tagging succeeded, use verbalizer output even if empty
		output := n.Processor.Verbalize(tagged)
		if n.postProcessor != nil {
			output = n.postProcessor.Apply(output)
		}
		return output
	}
	// If tagging failed, apply postprocessor directly to input
	if n.postProcessor != nil {
		return n.postProcessor.Apply(input)
	}
	return input
}
