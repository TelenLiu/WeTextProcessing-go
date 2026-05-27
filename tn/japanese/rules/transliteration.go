package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Transliteration struct {
	*tn.Processor
}

func NewTransliteration() *Transliteration {
	t := &Transliteration{
		Processor: tn.NewProcessor("transliteration"),
	}
	t.BuildTagger()
	t.BuildVerbalizer()
	return t
}

func (t *Transliteration) BuildTagger() {
	transliteration, _ := pynini.StringFile(tn.JapaneseDataPath("data/pyopenjtalk/transliteration.tsv"))
	tagger := lib.Insert("value: \"").Concat(transliteration).Concat(lib.Insert("\""))
	t.Tagger = t.AddTokens(tagger)
}

func (t *Transliteration) BuildVerbalizer() {
	t.Processor.BuildVerbalizer()
}
