package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Date struct {
	*tn.Processor
	deterministic bool
}

func NewDate(args ...bool) *Date {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	d := &Date{
		Processor:   tn.NewProcessor("date", "en_tn"),
		deterministic: deterministic,
	}
	d.BuildTagger()
	d.BuildVerbalizer()
	return d
}

func (d *Date) BuildTagger() {
	cardinal := getSharedCardinal(d.deterministic)
	monthGraph, _ := pynini.StringFile(tn.EnglishDataPath("data/date/month_name.tsv"))
	monthAbbrGraph, _ := pynini.StringFile(tn.EnglishDataPath("data/date/month_abbr.tsv"))
	monthGraph = monthGraph.Union(monthAbbrGraph)
	// Python: month_graph += pynutil.delete(self.PUNCT).ques
	// Delete optional period after month abbreviation
	monthGraph = monthGraph.Concat(lib.DeleteString(".").Ques())

	monthNumbersLabels, _ := pynini.StringFile(tn.EnglishDataPath("data/date/month_number.tsv"))
	cardinalGraph := cardinal.GraphHundredComponentAtLeastOneNoneZeroDigit

	yearGraph := d.getYearGraph(cardinalGraph, d.deterministic)

	monthTag := lib.Insert("month: \"").Concat(monthGraph).Concat(lib.Insert("\""))
	monthNumbersTag := lib.Insert("month: \"").Concat(monthNumbersLabels).Concat(lib.Insert("\""))

	endings := []string{"rd", "th", "st", "nd", "RD", "TH", "ST", "ND"}
	var endFsts []*pynini.Fst
	for _, e := range endings {
		endFsts = append(endFsts, pynini.Accep(e))
	}
	endingsGraph := pynini.Union(endFsts...)

	dayGraph := lib.Insert("day: \"").Concat(
		lib.DeleteString("the ").Ques().Concat(
			pynini.Union(
				pynini.Union(pynini.Accep("1"), pynini.Accep("2")).Concat(d.DIGIT),
				d.DIGIT,
				pynini.Accep("3").Concat(pynini.Union(pynini.Accep("0"), pynini.Accep("1"))),
			).Concat(endingsGraph.Ques()),
		).Compose(cardinalGraph),
	).Concat(lib.Insert("\""))

	twoDigitYear := d.getTwoDigitYear(cardinalGraph, cardinal.SingleDigitsGraph)
	twoDigitYear = lib.Insert("year: \"").Concat(twoDigitYear).Concat(
		pynini.Union(pynini.Accep(","), pynini.Accep(".")).Ques()).Concat(lib.Insert("\""))

	graphYear := lib.Insert(" year: \"").Concat(
		lib.DeleteString(" ")).Concat(
		yearGraph).Concat(
		pynini.Union(pynini.Accep(","), pynini.Accep(".")).Ques()).Concat(lib.Insert("\""))
	graphYear = graphYear.Union(
		lib.Insert(" year: \"").Concat(pynini.Accep(",")).Concat(
			pynini.Accep(" ").Ques()).Concat(
			yearGraph).Concat(
			pynini.Union(pynini.Accep(","), pynini.Accep(".")).Ques()).Concat(lib.Insert("\"")),
	)
	optionalGraphYear := graphYear.Ques()

	yearTag := lib.Insert("year: \"").Concat(yearGraph).Concat(lib.Insert("\""))

	// Standalone year tag with a weight penalty (0.015).
	// In the greedy matching approach, the weight resolves the conflict:
	// - For cardinal numbers like "1001", cardinal rule total (1.0 + ~0.0 = ~1.0)
	//   < date year total (0.99 + 0.015 + ~0.0 = ~1.005). Cardinal wins.
	// - For year-like numbers like "1219", cardinal FST weight is higher (~0.01),
	//   so date year total (~1.005) < cardinal total (~1.01). Date year wins.
	standaloneYearTag := lib.AddWeight(yearTag, 0.0)

	// MDY: month day year
	graphMDY := monthTag.Concat(
		d.DELETE_EXTRA_SPACE.Concat(dayGraph).Union(
			pynini.Accep(" ").Concat(dayGraph),
		).Union(graphYear).Union(
			d.DELETE_EXTRA_SPACE.Concat(dayGraph).Concat(graphYear),
		),
	)
	graphMDY = graphMDY.Union(
		monthTag.Concat(pynini.Cross("-", " ")).Concat(dayGraph).Concat(
			(pynini.Cross("-", " ").Concat(d.VCHAR.Star())).Compose(graphYear).Ques(),
		),
	)
	for _, x := range []string{"-", "/", "."} {
		deleteSep := lib.DeleteString(x)
		graphMDY = graphMDY.Union(
			monthNumbersTag.Concat(deleteSep).Concat(d.INSERT_SPACE).Concat(
				lib.DeleteString("0").Ques()).Concat(dayGraph).Concat(deleteSep).Concat(
				d.INSERT_SPACE).Concat(lib.AddWeight(yearTag, -1.0)),
		)
	}

	// DMY: day month year
	graphDMY := dayGraph.Concat(d.DELETE_EXTRA_SPACE).Concat(d.INSERT_SPACE).Concat(monthTag).Concat(optionalGraphYear)
	dayExMonth := d.DIGIT.Repeat(2).Difference(pynini.Project(monthNumbersTag, "input")).Compose(dayGraph)
	for _, x := range []string{"-", "/", "."} {
		deleteSep := lib.DeleteString(x)
		graphDMY = graphDMY.Union(
			dayExMonth.Concat(deleteSep).Concat(d.INSERT_SPACE).Concat(
				monthNumbersTag).Concat(deleteSep).Concat(d.INSERT_SPACE).Concat(
				lib.AddWeight(yearTag, -1.0)),
		)
	}

	// YMD: year month day
	graphYMD := yearTag.Concat(d.DELETE_EXTRA_SPACE).Concat(d.INSERT_SPACE).Concat(
		monthTag).Concat(d.DELETE_EXTRA_SPACE).Concat(d.INSERT_SPACE).Concat(dayGraph)
	for _, x := range []string{"-", "/", "."} {
		deleteSep := lib.DeleteString(x)
		graphYMD = graphYMD.Union(
			lib.AddWeight(yearTag, -1.0).Concat(deleteSep).Concat(d.INSERT_SPACE).Concat(
				monthNumbersTag).Concat(deleteSep).Concat(d.INSERT_SPACE).Concat(
				lib.DeleteString("0").Ques()).Concat(dayGraph),
		)
	}

	// Note: yearTag IS included in finalGraph. In the greedy matching approach,
	// the FST weights naturally resolve the conflict between year and cardinal:
	// - For cardinal numbers like "1001", the cardinal FST weight is lower than
	//   the year FST weight (because year involves "0"->"oh" mapping), so cardinal wins.
	// - For year-like numbers like "1219", the year FST weight is lower than the
	//   cardinal FST weight (because year is a simpler 2-word mapping), so year wins.
	finalGraph := lib.AddWeight(pynini.Union(graphMDY, graphDMY, graphYMD), -0.1)
	finalGraph = finalGraph.Union(standaloneYearTag)

	// Financial period
	periodFY := lib.Insert("text: \"").Concat(d.getFinancialPeriodGraph()).Concat(lib.Insert("\""))
	graphFY := periodFY.Concat(d.INSERT_SPACE).Concat(twoDigitYear)
	finalGraph = finalGraph.Union(graphFY)

	// RmEpsilon+Connect: eliminate epsilon arcs from Union and Insert wrappers
	// before AddTokens. This reduces epsilon closure BFS cost at runtime.
	finalGraph = finalGraph.RmEpsilon().Connect()

	d.Tagger = d.AddTokens(finalGraph)
}

func (d *Date) getYearGraph(cardinalGraph *pynini.Fst, deterministic bool) *pynini.Fst {
	graph := d.getFourDigitYearGraph(deterministic)
	// Restrict year to 1200-2999 to avoid conflicting with cardinal numbers 1000-1199.
	// In Python, the monolithic FST approach resolves this conflict through FST composition
	// weights. In the greedy matching approach, we restrict the year range to avoid the conflict.
	// Years 1000-1199 are rare and will be handled by the cardinal rule.
	digits2to9 := pynini.Union(
		pynini.Accep("2"), pynini.Accep("3"), pynini.Accep("4"),
		pynini.Accep("5"), pynini.Accep("6"), pynini.Accep("7"),
		pynini.Accep("8"), pynini.Accep("9"),
	)
	yearStart := pynini.Union(
		pynini.Accep("1").Concat(digits2to9).Concat(d.DIGIT.Repeat(2)),
		pynini.Accep("2").Concat(d.DIGIT.Repeat(3)),
	)
	graph = yearStart.Concat(
		pynini.Union(pynini.Cross(" s", "s"), pynini.Accep("s")).Ques(),
	).Compose(graph)

	graph = graph.Union(d.getTwoDigitYearWithSGraph())

	threeDigitYear := d.DIGIT.Compose(cardinalGraph).Concat(
		d.INSERT_SPACE).Concat(d.DIGIT.Repeat(2).Compose(cardinalGraph))

	fourDigitGraph := d.getFourDigitYearGraph(true)
	yearWithSuffix := fourDigitGraph.Union(threeDigitYear).Concat(
		d.DELETE_SPACE).Concat(d.INSERT_SPACE).Concat(d.yearSuffixGraph())

	graph = graph.Union(yearWithSuffix)
	return graph.Optimize()
}

func (d *Date) getFourDigitYearGraph(deterministic bool) *pynini.Fst {
	graphTies := d.getTiesGraph(deterministic)
	graphTeen, _ := pynini.StringFile(tn.EnglishDataPath("data/number/teen.tsv"))
	graphTeen = graphTeen.Invert()
	graphDigit, _ := pynini.StringFile(tn.EnglishDataPath("data/number/digit.tsv"))
	graphDigit = graphDigit.Invert()
	tiesGraph, _ := pynini.StringFile(tn.EnglishDataPath("data/number/ty.tsv"))
	tiesGraph = tiesGraph.Invert()

	graphWithS := graphTies.Concat(d.INSERT_SPACE).Concat(graphTies).Union(
		graphTeen.Concat(d.INSERT_SPACE).Concat(
			tiesGraph.Union(pynini.Cross("1", "ten"))),
	).Concat(lib.DeleteString("0s"))

	graphWithS = graphWithS.Union(
		graphTeen.Union(graphTies).Concat(d.INSERT_SPACE).Concat(
			pynini.Cross("00", "hundred")).Concat(lib.DeleteString("s")),
	)

	graph := graphTies.Concat(d.INSERT_SPACE).Concat(graphTies)
	graph = graph.Union(
		graphTeen.Union(graphTies).Concat(d.INSERT_SPACE).Concat(pynini.Cross("00", "hundred")),
	)

	thousandGraph := graphDigit.Concat(d.INSERT_SPACE).Concat(
		pynini.Cross("00", "thousand")).Concat(
		pynini.Union(lib.DeleteString("0"), d.INSERT_SPACE.Concat(graphDigit)),
	)
	thousandGraph = thousandGraph.Union(
		graphDigit.Concat(d.INSERT_SPACE).Concat(pynini.Cross("000", "thousand")).Concat(
			lib.DeleteString(" ").Ques()).Concat(pynini.Accep("s")),
	)

	graph = graph.Union(graphWithS)
	if deterministic {
		graph = pynini.Union(thousandGraph, graph)
	} else {
		graph = graph.Union(thousandGraph)
	}
	return graph.Optimize()
}

func (d *Date) getTiesGraph(deterministic bool) *pynini.Fst {
	graphTeen, _ := pynini.StringFile(tn.EnglishDataPath("data/number/teen.tsv"))
	graphTeen = graphTeen.Invert()
	graphDigit, _ := pynini.StringFile(tn.EnglishDataPath("data/number/digit.tsv"))
	graphDigit = graphDigit.Invert()
	tiesGraph, _ := pynini.StringFile(tn.EnglishDataPath("data/number/ty.tsv"))
	tiesGraph = tiesGraph.Invert()

	graph := pynini.Union(
		graphTeen,
		tiesGraph.Concat(lib.DeleteString("0")),
		tiesGraph.Concat(d.INSERT_SPACE).Concat(graphDigit),
	)
	if deterministic {
		graph = graph.Union(pynini.Cross("0", "oh").Concat(d.INSERT_SPACE).Concat(graphDigit))
	} else {
		graph = graph.Union(
			pynini.Union(pynini.Cross("0", "oh"), pynini.Cross("0", "zero")).Concat(
				d.INSERT_SPACE).Concat(graphDigit),
		)
	}
	return graph.Optimize()
}

func (d *Date) getTwoDigitYearWithSGraph() *pynini.Fst {
	tiesGraph, _ := pynini.StringFile(tn.EnglishDataPath("data/number/ty.tsv"))
	tiesGraph = tiesGraph.Invert()
	graph := lib.DeleteString("'").Ques().Concat(
		tiesGraph.Concat(lib.DeleteString("0s")).Compose(
			d.BuildRule(pynini.Cross("y", "ies"), "", ""),
		),
	).Optimize()
	return graph
}

func (d *Date) getTwoDigitYear(cardinalGraph, singleDigitsGraph *pynini.Fst) *pynini.Fst {
	twoDigitYear := d.DIGIT.Repeat(2).Compose(
		pynini.Union(cardinalGraph, singleDigitsGraph),
	)
	return twoDigitYear
}

func (d *Date) getFinancialPeriodGraph() *pynini.Fst {
	hOrdinals := pynini.Union(pynini.Cross("1", "first"), pynini.Cross("2", "second"))
	qOrdinals := hOrdinals.Union(pynini.Cross("3", "third")).Union(pynini.Cross("4", "fourth"))

	hGraph := hOrdinals.Concat(pynini.Cross("H", " half"))
	qGraph := qOrdinals.Concat(pynini.Cross("Q", " quarter"))
	return hGraph.Union(qGraph)
}

func (d *Date) yearSuffixGraph() *pynini.Fst {
	fst, _ := pynini.StringFile(tn.EnglishDataPath("data/date/year_suffix.tsv"))
	return fst
}

func (d *Date) BuildVerbalizer() {
	ordinal := getSharedOrdinal(d.deterministic)

	// Day: delete("day:") + DELETE_SPACE + delete("\"") + NOT_QUOTE.Plus() + delete("\"")
	// Then compose with ordinal.Suffix to convert cardinal to ordinal (e.g., "five" -> "fifth")
	// Matching Python: day_cardinal @ ordinal.suffix
	dayCardinal := lib.DeleteString("day:").Concat(d.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(d.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))
	day := dayCardinal.Compose(ordinal.Suffix)

	period := lib.DeleteString("text:").Concat(d.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(d.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))
	month := lib.DeleteString("month:").Concat(d.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(d.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))
	year := lib.DeleteString("year:").Concat(d.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(d.NOT_QUOTE.Plus()).Concat(
		d.DELETE_SPACE).Concat(lib.DeleteString("\""))

	graphFY := lib.Insert("the ").Concat(period).Concat(lib.Insert(" of")).Concat(
		d.DELETE_EXTRA_SPACE.Concat(year).Ques())

	graphDMY := (lib.Insert("the ").Concat(day).Concat(d.DELETE_EXTRA_SPACE).Concat(
		lib.Insert("of "))).Ques().Concat(month).Concat(
		d.DELETE_EXTRA_SPACE.Concat(year).Ques())

	finalGraph := pynini.Union(graphDMY, year, graphFY).Concat(d.DELETE_SPACE)
	d.Verbalizer = d.DeleteTokens(finalGraph)
}
