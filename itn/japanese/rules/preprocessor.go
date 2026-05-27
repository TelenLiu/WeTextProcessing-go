package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type PreProcessor struct {
	*tn.Processor
	ProcessorFst *pynini.Fst
}

func NewPreProcessor(full_to_half bool) *PreProcessor {
	p := &PreProcessor{
		Processor: tn.NewProcessor("preprocessor"),
	}

	processor := p.BuildRule(pynini.Accep(""), "", "")
	if full_to_half {
		traditional2simple, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/char/fullwidth_to_halfwidth.tsv"))
		processor = p.BuildRule(traditional2simple, "", "")
	}

	p.ProcessorFst = processor.Optimize()
	return p
}
