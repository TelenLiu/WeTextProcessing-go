package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type PostProcessor struct {
	*tn.Processor
	ProcessorFst *pynini.Fst
}

func NewPostProcessor(remove_interjections, remove_puncts, tag_oov bool) *PostProcessor {
	p := &PostProcessor{
		Processor: tn.NewProcessor("postprocessor"),
	}

	processor := p.BuildRule(pynini.Accep(""), "", "")
	_ = remove_interjections
	_ = remove_puncts
	_ = tag_oov

	p.ProcessorFst = processor.Optimize()
	return p
}
