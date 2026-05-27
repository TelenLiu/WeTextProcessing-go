package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Math struct {
	*tn.Processor
}

func NewMath() *Math {
	m := &Math{
		Processor: tn.NewProcessor("math"),
	}
	m.BuildTagger()
	m.BuildVerbalizer()
	return m
}

func (m *Math) BuildTagger() {
	operator, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/math/operator.tsv"))

	cardinal := NewCardinal(false, false, false)
	number := cardinal.BigInteger
	decimal := cardinal.Decimal
	number = pynini.Union(number, decimal)
	
	tagger := number.Concat(operator.Concat(number).Plus())
	tagger = lib.Insert("value: \"").Concat(tagger).Concat(lib.Insert("\""))
	m.Tagger = m.AddTokens(tagger)
}

func (m *Math) BuildVerbalizer() {
	m.Processor.BuildVerbalizer()
}
