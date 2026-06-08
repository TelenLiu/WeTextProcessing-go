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
	cardinal := getSharedCardinal(true).GraphWithAnd
	week, _ := pynini.StringFile(tn.EnglishDataPath("data/date/week.tsv"))
	deleteSpace := lib.DeleteString(" ").Ques()

	approx := pynini.Cross("~", "approximately")

	// WEEK
	weekGraph := week.Concat(deleteSpace).Concat(
		pynini.Union(pynini.Cross("-", " to "), approx)).Concat(deleteSpace).Concat(week)

	// Skip TIME and YEAR sub-graphs: they are handled by the time and date rules
	// respectively, and including them (via Tagger.Compose(Verbalizer)) causes
	// the range tagger FST to explode to millions of states.
	r.Graph = weekGraph

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
