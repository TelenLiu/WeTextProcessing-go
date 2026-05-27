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

func NewMathWithCardinal(cardinal *Cardinal) *Math {
	m := &Math{
		Processor: tn.NewProcessor("math"),
	}
	m.BuildTaggerWithCardinal(cardinal)
	m.BuildVerbalizer()
	return m
}

func (m *Math) BuildTagger() {
	cardinal := NewCardinal()
	m.BuildTaggerWithCardinal(cardinal)
}

func (m *Math) BuildTaggerWithCardinal(cardinal *Cardinal) {
	operator, _ := pynini.StringFile(tn.JapaneseDataPath("data/math/operator.tsv"))

	number := cardinal.Number
	operator = number.Concat(
		lib.DeleteString(" ").Ques().
			Concat(operator).
			Concat(lib.DeleteString(" ").Ques()).
			Concat(number).
			Star(),
	)
	tagger := lib.Insert("value: \"").Concat(operator).Concat(lib.Insert("\""))
	m.Tagger = m.AddTokens(tagger)
}

func (m *Math) BuildVerbalizer() {
	m.Processor.BuildVerbalizer()
}
