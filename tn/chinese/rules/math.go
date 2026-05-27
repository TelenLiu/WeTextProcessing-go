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

// NewMathWithCardinal creates a Math rule using an existing Cardinal rule for number matching
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
	operator, _ := pynini.StringFile(tn.ChineseDataPath("data/math/operator.tsv"))
	// When it appears alone, it is treated as punctuation
	symbols := pynini.Union(
		pynini.Cross("~", "到"),
		pynini.Cross(":", "比"),
		pynini.Cross("<", "小于"),
		pynini.Cross(">", "大于"),
	)

	number := cardinal.Number
	tagger := number.Concat(
		lib.DeleteString(" ").Ques().
			Concat(operator.Union(symbols)).
			Concat(lib.DeleteString(" ").Ques()).
			Concat(number).
			Star(),
	)
	tagger = tagger.Union(operator)
	tagger = lib.Insert("value: \"").Concat(tagger).Concat(lib.Insert("\""))
	m.Tagger = m.AddTokens(tagger)
}

func (m *Math) BuildVerbalizer() {
	m.Processor.BuildVerbalizer()
}
