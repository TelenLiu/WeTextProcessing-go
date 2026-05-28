package chinese

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
	"github.com/TelenLiu/WeTextProcessing-go/itn/chinese/rules"

	_ "github.com/TelenLiu/WeTextProcessing-go/itn"
)

type InverseNormalizer struct {
	*tn.Processor

	remove_interjections     bool
	enable_standalone_number bool
	enable_0_to_9           bool
	enable_million          bool
	postprocessor           *pynini.Fst
}

func NewInverseNormalizer(
	cache_dir string,
	overwrite_cache bool,
	remove_interjections bool,
	enable_standalone_number bool,
	enable_0_to_9 bool,
	enable_million bool,
	progress ...tn.BuildProgressFn,
) *InverseNormalizer {
	n := &InverseNormalizer{
		Processor:                tn.NewProcessor("zh_inverse_normalizer", "itn"),
		remove_interjections:     remove_interjections,
		enable_standalone_number: enable_standalone_number,
		enable_0_to_9:           enable_0_to_9,
		enable_million:          enable_million,
	}
	var pf tn.BuildProgressFn
	if len(progress) > 0 {
		pf = progress[0]
	}
	n.BuildFst("zh_itn", cache_dir, overwrite_cache, 0, pf)
	return n
}

func (n *InverseNormalizer) BuildFst(prefix, cacheDir string, overwriteCache bool, concurrency int, progress tn.BuildProgressFn) {
	n.Processor.BuildFstWithCache(prefix, cacheDir, overwriteCache, concurrency, progress, n.buildTaggerInternal, n.buildVerbalizerInternal)
}

func (n *InverseNormalizer) buildTaggerInternal() {
	date := lib.AddWeight(rules.NewDate().Tagger, 1.02)
	whitelist := lib.AddWeight(rules.NewWhitelist().Tagger, 1.01)
	fraction := lib.AddWeight(rules.NewFraction().Tagger, 1.05)
	measure := lib.AddWeight(rules.NewMeasure(true, n.enable_0_to_9).Tagger, 1.05)
	money := lib.AddWeight(rules.NewMoney(n.enable_0_to_9).Tagger, 1.04)
	time := lib.AddWeight(rules.NewTime().Tagger, 1.05)
	cardinal := lib.AddWeight(rules.NewCardinal(n.enable_standalone_number, n.enable_0_to_9, n.enable_million).Tagger, 1.06)
	math := lib.AddWeight(rules.NewMath().Tagger, 1.10)
	license_plate := lib.AddWeight(rules.NewLicensePlate().Tagger, 1.0)
	char := lib.AddWeight(rules.NewChar().Tagger, 100)

	tagger := pynini.Union(date, whitelist, fraction, measure, money, time, cardinal, math, license_plate, char).Optimize()
	n.Tagger = tagger.Star()
}

func (n *InverseNormalizer) buildVerbalizerInternal() {
	cardinal := rules.NewCardinal(n.enable_standalone_number, n.enable_0_to_9, n.enable_million).Verbalizer
	char := rules.NewChar().Verbalizer
	date := rules.NewDate().Verbalizer
	fraction := rules.NewFraction().Verbalizer
	math := rules.NewMath().Verbalizer
	measure := rules.NewMeasure(true, n.enable_0_to_9).Verbalizer
	money := rules.NewMoney(n.enable_0_to_9).Verbalizer
	time := rules.NewTime().Verbalizer
	license_plate := rules.NewLicensePlate().Verbalizer
	whitelist := rules.NewWhitelist().Verbalizer

	verbalizer := pynini.Union(cardinal, char, date, fraction, math, measure, money, time, license_plate, whitelist).Optimize()

	// Store postprocessor for later use in Normalize
	if n.remove_interjections {
		n.postprocessor = rules.NewPostProcessor(n.remove_interjections).ProcessorFst
	}
	n.Verbalizer = verbalizer.Star()
}

func (n *InverseNormalizer) Normalize(input string) string {
	result := n.Processor.Normalize(input)
	if n.remove_interjections && n.postprocessor != nil && result != "" {
		result = pynini.Accep(result).ComposeShortestPath(n.postprocessor)
	}
	return result
}
