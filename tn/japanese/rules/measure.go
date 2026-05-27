package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Measure struct {
	*tn.Processor
}

func NewMeasure() *Measure {
	m := &Measure{
		Processor: tn.NewProcessor("measure"),
	}
	m.BuildTagger()
	m.BuildVerbalizer()
	return m
}

func (m *Measure) BuildTagger() {
	units_en, _ := pynini.StringFile(tn.JapaneseDataPath("data/measure/units_en.tsv"))
	units_ja, _ := pynini.StringFile(tn.JapaneseDataPath("data/measure/units_ja.tsv"))

	// taking '-' '~' as 'から' if the following word in units
	units := pynini.Union(units_en, units_ja)
	rmspace := lib.DeleteString(" ").Ques()
	to := pynini.Union(pynini.Cross("-", "から"), pynini.Cross("~", "から"), pynini.Accep("から"))

	number := NewCardinal().Number

	// 1-11月, 1月-11月
	prefix := number.Concat(rmspace.Concat(units).Ques()).Concat(to)
	measure := prefix.Ques().Concat(number).Concat(rmspace).Concat(units)
	measure = pynini.Union(measure, measure.Concat(rmspace).Concat(lib.DeleteString("/")).Concat(lib.Insert("毎")).Concat(rmspace).Concat(units))

	tagger := lib.Insert("value: \"").Concat(measure).Concat(lib.Insert("\""))
	m.Tagger = m.AddTokens(tagger)
}

func (m *Measure) BuildVerbalizer() {
	m.Processor.BuildVerbalizer()
}
