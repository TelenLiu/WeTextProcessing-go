package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type PostProcessor struct {
	*tn.Processor
	ProcessorFst *pynini.Fst
}

func NewPostProcessor(remove_interjections bool) *PostProcessor {
	p := &PostProcessor{
		Processor: tn.NewProcessor("postprocessor"),
	}

	blacklist, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/default/blacklist.tsv"))
	processor := p.VSIGMA
	if remove_interjections {
		processor = p.BuildRule(lib.Delete(blacklist), "", "")
	}
	p.ProcessorFst = processor.Optimize()
	return p
}
