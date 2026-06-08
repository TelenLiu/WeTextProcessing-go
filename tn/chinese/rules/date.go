package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Date struct {
	*tn.Processor
}

func NewDate() *Date {
	d := &Date{
		Processor: tn.NewProcessor("date"),
	}
	d.BuildTagger()
	d.BuildVerbalizer()
	return d
}

func (d *Date) BuildTagger() {
	digit, _ := pynini.StringFile(tn.ChineseDataPath("data/number/digit.tsv"))
	zero, _ := pynini.StringFile(tn.ChineseDataPath("data/number/zero.tsv"))

	yyyy := digit.Concat(pynini.Union(digit, zero).Repeat(3))
	m, _ := pynini.StringFile(tn.ChineseDataPath("data/date/m.tsv"))
	mm, _ := pynini.StringFile(tn.ChineseDataPath("data/date/mm.tsv"))
	d_tsv, _ := pynini.StringFile(tn.ChineseDataPath("data/date/d.tsv"))
	dd, _ := pynini.StringFile(tn.ChineseDataPath("data/date/dd.tsv"))
	rmsign := pynini.Union(lib.DeleteString("/"), lib.DeleteString("-"), lib.DeleteString(".")).Concat(lib.Insert(" "))

	year := lib.Insert("year: \"").Concat(yyyy).Concat(lib.Insert("年\""))
	month := lib.Insert("month: \"").Concat(pynini.Union(m, mm)).Concat(lib.Insert("\""))
	day := lib.Insert("day: \"").Concat(pynini.Union(d_tsv, dd)).Concat(lib.Insert("\""))

	mm_full := lib.Insert("month: \"").Concat(mm).Concat(lib.Insert("\""))
	date := pynini.Union(
		year.Concat(rmsign).Concat(month).Concat(rmsign).Concat(day),
		day.Concat(rmsign).Concat(month).Concat(rmsign).Concat(year),
		year.Concat(rmsign).Concat(mm_full),
		mm_full.Concat(rmsign).Concat(year),
		mm_full.Concat(rmsign).Concat(day),
	)
	date = date.RmEpsilon().Connect()
	tagger := d.AddTokens(date)

	to := pynini.Union(lib.DeleteString("-"), lib.DeleteString("~")).Concat(
		lib.Insert(" char { value: \"到\" } "),
	)
	d.Tagger = tagger.Concat(to.Concat(tagger).Ques())
}

func (d *Date) BuildVerbalizer() {
	year := lib.DeleteString("year: \"").Concat(d.SIGMA).Concat(lib.DeleteString("\" "))
	month := lib.DeleteString("month: \"").Concat(d.SIGMA).Concat(lib.DeleteString("\""))
	day := lib.DeleteString(" day: \"").Concat(d.SIGMA).Concat(lib.DeleteString("\""))
	verbalizer := year.Ques().Concat(month).Concat(day.Ques())
	d.Verbalizer = d.DeleteTokens(verbalizer)
}
