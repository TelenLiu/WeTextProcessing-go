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
	cardinal := NewCardinal(false, false, false).TenThousandMinus
	day, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/date/day.tsv"))
	month, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/date/month.tsv"))
	to := pynini.Cross("から", "〜")

	// 一月 一日 一年
	year := lib.Insert("year: \"").Concat(cardinal).Concat(to.Concat(cardinal).Ques()).Concat(lib.DeleteString("年")).Concat(lib.Insert("\""))
	monthTag := lib.Insert("month: \"").Concat(month).Concat(to.Concat(month).Ques()).Concat(lib.DeleteString("月")).Concat(lib.Insert("\""))
	dayTag := lib.Insert("day: \"").Concat(day).Concat(to.Concat(day).Ques()).Concat(lib.DeleteString("日")).Concat(lib.Insert("\""))

	// 二千二十四年十月一日 二千二十四年十月 十月一日
	graph_date := pynini.Union(
		year.Concat(lib.Insert(" ")).Concat(monthTag),
		monthTag.Concat(lib.Insert(" ")).Concat(dayTag),
		year.Concat(lib.Insert(" ")).Concat(monthTag).Concat(lib.Insert(" ")).Concat(dayTag),
	)

	// specific context for era year, e.g., L6 -> "令和6年"
	context := pynini.Union(
		pynini.Accep("今年は"), pynini.Accep("来年は"), pynini.Accep("再来年は"),
		pynini.Accep("去年は"), pynini.Accep("一昨年は"), pynini.Accep("おととしは"),
	)
	era := pynini.Union(
		pynini.Cross("R", "令和"), pynini.Cross("H", "平成"), pynini.Cross("S", "昭和"),
		pynini.Cross("T", "大正"), pynini.Cross("M", "明治"),
	)
	era_year := lib.Insert("year: \"").Concat(context).Concat(era).Concat(cardinal).Concat(lib.Insert("\""))

	date := pynini.Union(graph_date, era_year)
	d.Tagger = d.AddTokens(date).Optimize()
}

func (d *Date) BuildVerbalizer() {
	year := lib.DeleteString("year: \"").Concat(d.SIGMA).Concat(lib.Insert("年")).Concat(lib.DeleteString("\""))
	era_year := lib.DeleteString("year: \"").Concat(d.SIGMA).Concat(lib.DeleteString("\""))
	month := lib.DeleteString("month: \"").Concat(d.SIGMA).Concat(lib.Insert("月")).Concat(lib.DeleteString("\""))
	day := lib.DeleteString("day: \"").Concat(d.SIGMA).Concat(lib.Insert("日")).Concat(lib.DeleteString("\""))

	graph_regular := pynini.Union(
		year.Concat(lib.DeleteString(" ")).Concat(month),
		month.Concat(lib.DeleteString(" ")).Concat(day),
		year.Concat(lib.DeleteString(" ")).Concat(month).Concat(lib.DeleteString(" ")).Concat(day),
	)

	graph := pynini.Union(graph_regular, era_year)
	d.Verbalizer = d.DeleteTokens(graph).Optimize()
}
