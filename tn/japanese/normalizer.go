package japanese

import (
	"sync"

	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
	"github.com/TelenLiu/WeTextProcessing-go/tn/japanese/rules"
)

type Normalizer struct {
	*tn.Processor

	transliterate         bool
	remove_interjections  bool
	remove_puncts         bool
	full_to_half          bool
	tag_oov               bool

	preProcessor  *rules.PreProcessor
	postProcessor *rules.PostProcessor

	// Rule instances shared between tagger and verbalizer
	cardinalRule     *rules.Cardinal
	dateRule         *rules.Date
	whitelistRule    *rules.Whitelist
	sportRule        *rules.Sport
	fractionRule     *rules.Fraction
	measureRule      *rules.Measure
	moneyRule        *rules.Money
	timeRule         *rules.Time
	mathRule         *rules.Math
	charRule         *rules.Char
	translitRule     *rules.Transliteration
}

func NewNormalizer(
	cache_dir string,
	overwrite_cache bool,
	transliterate bool,
	remove_interjections bool,
	remove_puncts bool,
	full_to_half bool,
	tag_oov bool,
	progress ...tn.BuildProgressFn,
) *Normalizer {
	n := &Normalizer{
		Processor:            tn.NewProcessor("ja_normalizer"),
		transliterate:        transliterate,
		remove_interjections: remove_interjections,
		remove_puncts:        remove_puncts,
		full_to_half:         full_to_half,
		tag_oov:              tag_oov,
	}
	var pf tn.BuildProgressFn
	if len(progress) > 0 {
		pf = progress[0]
	}
	n.BuildFst("ja_tn", cache_dir, overwrite_cache, 0, pf)
	return n
}

func (n *Normalizer) BuildFst(prefix, cacheDir string, overwriteCache bool, concurrency int, progress tn.BuildProgressFn) {
	n.Processor.BuildFstWithCache(prefix, cacheDir, overwriteCache, concurrency, progress, n.buildTaggerInternal, n.buildVerbalizerInternal)
}

// BuildTagger builds the tagger FST (kept for backward compatibility)
func (n *Normalizer) BuildTagger() {
	n.buildTaggerInternal()
}

func (n *Normalizer) buildTaggerInternal() {
	concurrency, progress := n.Processor.GetBuildConfig()
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	type task struct{ name string; fn func() }
	tasks := []task{
		{"cardinal", func() { n.cardinalRule = rules.NewCardinal() }},
		{"date", func() { n.dateRule = rules.NewDate() }},
		{"whitelist", func() { n.whitelistRule = rules.NewWhitelist() }},
		{"sport", func() { n.sportRule = rules.NewSport() }},
		{"fraction", func() { n.fractionRule = rules.NewFraction() }},
		{"measure", func() { n.measureRule = rules.NewMeasure() }},
		{"money", func() { n.moneyRule = rules.NewMoney() }},
		{"time", func() { n.timeRule = rules.NewTime() }},
		{"char", func() { n.charRule = rules.NewChar() }},
		{"translit", func() { n.translitRule = rules.NewTransliteration() }},
	}
	for i, t := range tasks {
		wg.Add(1)
		go func(tt task, idx int) {
			defer wg.Done()
			sem <- struct{}{}
			tt.fn()
			<-sem
			if progress != nil {
				progress("构建Tagger-"+tt.name, idx+1, len(tasks)+1)
			}
		}(t, i)
	}
	wg.Wait()

	n.mathRule = rules.NewMathWithCardinal(n.cardinalRule)
	if progress != nil {
		progress("构建Tagger-math", len(tasks)+1, len(tasks)+1)
	}

	n.preProcessor = rules.NewPreProcessor(n.full_to_half)

	cardinal := lib.AddWeight(n.cardinalRule.Tagger, 1.06)
	char := lib.AddWeight(n.charRule.Tagger, 100)
	date := lib.AddWeight(n.dateRule.Tagger, 1.02)
	fraction := lib.AddWeight(n.fractionRule.Tagger, 1.05)
	math := lib.AddWeight(n.mathRule.Tagger, 90)
	measure := lib.AddWeight(n.measureRule.Tagger, 1.05)
	money := lib.AddWeight(n.moneyRule.Tagger, 1.05)
	sport := lib.AddWeight(n.sportRule.Tagger, 1.06)
	time := lib.AddWeight(n.timeRule.Tagger, 1.05)
	whitelist := lib.AddWeight(n.whitelistRule.Tagger, 1.03)

	tagger := pynini.Union(cardinal, char, date, fraction, math, measure, money, sport, time, whitelist)
	if n.transliterate {
		transliteration := lib.AddWeight(n.translitRule.Tagger, 1.04)
		tagger = pynini.Union(tagger, transliteration)
	}
	n.Tagger = tagger.Star()
}

// BuildVerbalizer builds the verbalizer FST (kept for backward compatibility)
func (n *Normalizer) BuildVerbalizer() {
	n.buildVerbalizerInternal()
}

func (n *Normalizer) buildVerbalizerInternal() {
	cardinal := n.cardinalRule.Verbalizer
	char := n.charRule.Verbalizer
	date := n.dateRule.Verbalizer
	fraction := n.fractionRule.Verbalizer
	math := n.mathRule.Verbalizer
	measure := n.measureRule.Verbalizer
	money := n.moneyRule.Verbalizer
	sport := n.sportRule.Verbalizer
	time := n.timeRule.Verbalizer
	whitelist := n.whitelistRule.Verbalizer
	transliteration := n.translitRule.Verbalizer

	verbalizer := pynini.Union(cardinal, char, date, fraction, math, measure, money, sport, time, whitelist)
	if n.transliterate {
		verbalizer = pynini.Union(verbalizer, transliteration)
	}

	// Do NOT compose PostProcessor into the Verbalizer FST - apply at runtime instead
	n.postProcessor = rules.NewPostProcessor(
		n.remove_interjections,
		n.remove_puncts,
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

// Close releases resources held by the Normalizer, including stopping
// background cache eviction goroutines. After calling Close, the Normalizer
// should not be used for further Normalize calls. It is safe to call
// Close multiple times.
func (n *Normalizer) Close() {
	n.Processor.Close()
}
