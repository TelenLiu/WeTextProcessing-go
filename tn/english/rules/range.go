package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Range struct {
	*tn.Processor
	Graph         *pynini.Fst
	deterministic bool
}

func NewRange(args ...bool) *Range {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	r := &Range{
		Processor:   tn.NewProcessor("range", "en_tn"),
		deterministic: deterministic,
	}
	r.BuildTagger()
	r.BuildVerbalizer()
	return r
}

func (r *Range) BuildTagger() {
	cardinal := NewCardinal(true).GraphWithAnd
	timeRule := NewTime(r.deterministic)
	timeGraph := timeRule.Tagger.Compose(timeRule.Verbalizer)
	dateRule := NewDate(r.deterministic)
	dateGraph := dateRule.Tagger.Compose(dateRule.Verbalizer)
	week, _ := pynini.StringFile(tn.EnglishDataPath("data/date/week.tsv"))
	deleteSpace := lib.DeleteString(" ").Ques()

	approx := pynini.Cross("~", "approximately")

	// WEEK
	weekGraph := week.Concat(deleteSpace).Concat(
		pynini.Union(pynini.Cross("-", " to "), approx)).Concat(deleteSpace).Concat(week)

	// TIME
	timeToTime := timeGraph.Concat(deleteSpace).Concat(pynini.Cross("-", " to ")).Concat(deleteSpace).Concat(timeGraph)
	r.Graph = timeToTime.Union(approx.Concat(timeGraph)).Union(weekGraph)

	// YEAR
	dateYearFourDigit := r.DIGIT.Repeat(4).Concat(pynini.Accep("s").Ques()).Compose(dateGraph)
	dateYearTwoDigit := r.DIGIT.Repeat(2).Concat(pynini.Accep("s").Ques()).Compose(dateGraph)
	yearToYear := dateYearFourDigit.Concat(deleteSpace).Concat(pynini.Cross("-", " to ")).Concat(
		deleteSpace).Concat(
		dateYearFourDigit.Union(dateYearTwoDigit).Union(r.DIGIT.Repeat(2).Compose(cardinal)))
	midYear := pynini.Accep("mid").Concat(pynini.Cross("-", " ")).Concat(
		dateYearFourDigit.Union(dateYearTwoDigit))

	r.Graph = r.Graph.Union(yearToYear).Union(midYear)

	// ADDITION
	rangeGraph := cardinal.Concat(pynini.Cross("+", " plus ").Concat(cardinal).Plus())
	rangeGraph = rangeGraph.Union(
		cardinal.Concat(pynini.Cross(" + ", " plus ").Concat(cardinal).Plus()))
	rangeGraph = rangeGraph.Union(approx.Concat(cardinal))
	rangeGraph = rangeGraph.Union(
		cardinal.Concat(pynini.Union(pynini.Cross("...", " ... "), pynini.Accep(" ... ")).Concat(cardinal)))

	if !r.deterministic {
		cardinalToCardinal := cardinal.Concat(deleteSpace).Concat(
			pynini.Cross("-", pynini.Union(pynini.Accep(" to "), pynini.Accep(" minus ")))).Concat(deleteSpace).Concat(cardinal)
		rangeGraph = rangeGraph.Union(cardinalToCardinal)
		rangeGraph = rangeGraph.Union(
			cardinal.Concat(deleteSpace).Concat(pynini.Cross(":", " to ")).Concat(deleteSpace).Concat(cardinal))

		for _, x := range []string{" x ", "x"} {
			rangeGraph = rangeGraph.Union(
				cardinal.Concat(pynini.Cross(x, pynini.Union(pynini.Accep(" by "), pynini.Accep(" times ")))).Concat(cardinal))
		}
		for _, x := range []string{" x", "x"} {
			rangeGraph = rangeGraph.Union(cardinal.Concat(pynini.Cross(x, " times")))
		}
		for _, x := range []string{"*", " * "} {
			rangeGraph = rangeGraph.Union(
				cardinal.Concat(pynini.Cross(x, " times ").Concat(cardinal).Plus()))
		}

		rangeGraph = rangeGraph.Union(
			pynini.Union(
				pynini.Cross("NO", "Number"),
				pynini.Cross("No", "Number"),
			).Concat(pynini.Union(pynini.Accep(". "), pynini.Accep(" ")).Ques()).Concat(cardinal),
		)
		rangeGraph = rangeGraph.Union(
			pynini.Cross("no", "number").Concat(pynini.Union(pynini.Accep(". "), pynini.Accep(" ")).Ques()).Concat(cardinal),
		)

		for _, x := range []string{"/", " / "} {
			rangeGraph = rangeGraph.Union(
				cardinal.Concat(pynini.Cross(x, " divided by ").Concat(cardinal).Plus()))
		}

		percentRange := cardinal.Concat(
			pynini.Union(pynini.Cross("%", " percent"), lib.DeleteString("%")).Ques()).Concat(
			pynini.Union(pynini.Accep(" to "), pynini.Accep("-"), pynini.Accep(" - "))).Concat(
			cardinal).Concat(pynini.Cross("%", " percent"))
		rangeGraph = rangeGraph.Union(percentRange)
	}

	r.Graph = r.Graph.Union(rangeGraph)
	finalGraph := lib.Insert("value: \"").Concat(r.Graph).Concat(lib.Insert("\""))
	r.Tagger = r.AddTokens(finalGraph)
}

func (r *Range) BuildVerbalizer() {
	// Range uses value: field for verbalization
	graph := lib.DeleteString("value: \"").Concat(r.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))
	r.Verbalizer = r.DeleteTokens(graph)
}
