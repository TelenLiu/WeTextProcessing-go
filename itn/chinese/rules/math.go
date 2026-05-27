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
	operator, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/math/operator.tsv"))

	cardinal := NewCardinal(false, true, false)
	number := cardinal.Number

	// number + (operator + number).plus
	tagger := number.Concat(operator.Concat(number).Plus())
	tagger = lib.Insert("value: \"").Concat(tagger).Concat(lib.Insert("\""))

	m.Tagger = m.AddTokens(tagger)
}

func (m *Math) BuildVerbalizer() {
	m.Processor.BuildVerbalizer()
}
