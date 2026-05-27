package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Ordinal struct {
	*tn.Processor
}

func NewOrdinal() *Ordinal {
	o := &Ordinal{
		Processor: tn.NewProcessor("ordinal"),
	}
	o.BuildTagger()
	o.BuildVerbalizer()
	return o
}

func (o *Ordinal) BuildTagger() {
	cardinal := NewCardinal(false, false, false).Number
	ordinal := pynini.Union(
		cardinal.Concat(pynini.Accep("番目")),
		pynini.Accep("第").Concat(cardinal),
	)
	tagger := lib.Insert("value: \"").Concat(ordinal).Concat(lib.Insert("\""))
	o.Tagger = o.AddTokens(tagger)
}

func (o *Ordinal) BuildVerbalizer() {
	o.Processor.BuildVerbalizer()
}
