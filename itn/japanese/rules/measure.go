package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Measure struct {
	*tn.Processor
	enable_0_to_9 bool
}

func NewMeasure(enable_0_to_9 bool) *Measure {
	m := &Measure{
		Processor:   tn.NewProcessor("measure"),
		enable_0_to_9: enable_0_to_9,
	}
	m.BuildTagger()
	m.BuildVerbalizer()
	return m
}

func (m *Measure) BuildTagger() {
	unit_en, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/measure/unit_en.tsv"))
	unit_ja, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/measure/unit_ja.tsv"))

	var number *pynini.Fst
	if m.enable_0_to_9 {
		number = NewCardinal(false, true, false).Number
	} else {
		number = NewCardinal(false, true, false).NumberExclude_0_to_9
	}
	decimal := NewCardinal(false, true, false).Decimal

	suffix := lib.Insert("/").Concat(
		pynini.Union(lib.DeleteString("每"), lib.DeleteString("毎"))).Concat(
		pynini.Union(unit_en, pynini.Cross("時", "h"), pynini.Cross("分", "min"), pynini.Cross("秒", "s")),
	)

	measure := pynini.Union(
		pynini.Union(number, decimal).Concat(unit_en).Concat(suffix.Ques()),
		pynini.Union(number, decimal).Concat(unit_ja),
	)

	tagger := lib.Insert("value: \"").Concat(measure).Concat(lib.Insert("\""))
	m.Tagger = m.AddTokens(tagger)
}

func (m *Measure) BuildVerbalizer() {
	m.Processor.BuildVerbalizer()
}
