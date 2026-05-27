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
	digit, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/digit.tsv"))
	zero, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/zero.tsv"))
	mm, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/date/mm.tsv"))
	dd, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/date/dd.tsv"))

	// yyyy = digit + (digit | zero) ** 3
	yyyy := digit.Concat(pynini.Union(digit, zero).Repeat(3))
	// yyy = digit + (digit | zero) ** 2
	yyy := digit.Concat(pynini.Union(digit, zero).Repeat(2))
	// yy = (digit | zero) ** 2
	yy := pynini.Union(digit, zero).Repeat(2)

	// year = insert('year: "') + (yyyy | yyy | yy) + delete("年") + insert('" ')
	year := lib.Insert("year: \"").Concat(pynini.Union(yyyy, yyy, yy)).Concat(lib.DeleteString("年")).Concat(lib.Insert("\" "))
	// year_only = insert('year: "') + (yyyy | yyy | yy) + accep("年") + insert('"')
	year_only := lib.Insert("year: \"").Concat(pynini.Union(yyyy, yyy, yy)).Concat(pynini.Accep("年")).Concat(lib.Insert("\""))
	// month = insert('month: "') + mm + insert('"')
	month := lib.Insert("month: \"").Concat(mm).Concat(lib.Insert("\""))
	// day = insert(' day: "') + dd + insert('"')
	day := lib.Insert(" day: \"").Concat(dd).Concat(lib.Insert("\""))

	// (year + month + day) | (year + month) | (month + day) | year_only
	date := pynini.Union(
		year.Concat(month).Concat(day),
		year.Concat(month),
		month.Concat(day),
		year_only,
	)
	d.Tagger = d.AddTokens(date)
}

func (d *Date) BuildVerbalizer() {
	addsign := lib.Insert("/")
	year := lib.DeleteString("year: \"").Concat(d.SIGMA).Concat(lib.DeleteString("\" "))
	year_only := lib.DeleteString("year: \"").Concat(d.SIGMA).Concat(lib.DeleteString("\""))
	month := lib.DeleteString("month: \"").Concat(d.SIGMA).Concat(lib.DeleteString("\""))
	day := lib.DeleteString(" day: \"").Concat(d.SIGMA).Concat(lib.DeleteString("\""))

	verbalizer := pynini.Union(
		pynini.Accep(""),
		year.Concat(addsign),
	).Ques().Concat(month).Concat(
		pynini.Union(
			pynini.Accep(""),
			addsign.Concat(day),
		).Ques(),
	)
	verbalizer = pynini.Union(verbalizer, year_only)
	d.Verbalizer = d.DeleteTokens(verbalizer)
}
