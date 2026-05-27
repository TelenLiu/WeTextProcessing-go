package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Whitelist struct {
	*tn.Processor
}

func NewWhitelist() *Whitelist {
	w := &Whitelist{
		Processor: tn.NewProcessor("whitelist"),
	}
	w.BuildTagger()
	w.BuildVerbalizer()
	return w
}

func (w *Whitelist) BuildTagger() {
	whitelist, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/default/whitelist.tsv"))

	tagger := lib.Insert("value: \"").Concat(whitelist).Concat(lib.Insert("\""))
	w.Tagger = w.AddTokens(tagger)
}

func (w *Whitelist) BuildVerbalizer() {
	w.Processor.BuildVerbalizer()
}
