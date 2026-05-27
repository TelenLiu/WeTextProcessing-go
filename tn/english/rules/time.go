package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Time struct {
	*tn.Processor
	deterministic bool
}

func NewTime(args ...bool) *Time {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	t := &Time{
		Processor:   tn.NewProcessor("time", "en_tn"),
		deterministic: deterministic,
	}
	t.BuildTagger()
	t.BuildVerbalizer()
	return t
}

func (t *Time) BuildTagger() {
	suffixGraph := t.suffixGraph()
	timeZoneGraph, _ := pynini.StringFile(tn.EnglishDataPath("data/time/zone.tsv"))
	cardinal := NewCardinal(t.deterministic).Graph

	labelsHour := make([]string, 24)
	for i := 0; i < 24; i++ {
		labelsHour[i] = string(rune('0' + i/10)) + string(rune('0'+i%10))
		if i < 10 {
			labelsHour[i] = string(rune('0' + i))
		}
	}

	labelsMinuteSingle := make([]string, 9)
	for i := 1; i < 10; i++ {
		labelsMinuteSingle[i-1] = string(rune('0' + i))
	}

	labelsMinuteDouble := make([]string, 50)
	for i := 10; i < 60; i++ {
		labelsMinuteDouble[i-10] = string(rune('0'+i/10)) + string(rune('0'+i%10))
	}

	var hourFsts []*pynini.Fst
	for _, h := range labelsHour {
		hourFsts = append(hourFsts, pynini.Accep(h))
	}
	hourUnion := pynini.Union(hourFsts...)

	var minuteSingleFsts []*pynini.Fst
	for _, m := range labelsMinuteSingle {
		minuteSingleFsts = append(minuteSingleFsts, pynini.Accep(m))
	}
	minuteSingleUnion := pynini.Union(minuteSingleFsts...)

	var minuteDoubleFsts []*pynini.Fst
	for _, m := range labelsMinuteDouble {
		minuteDoubleFsts = append(minuteDoubleFsts, pynini.Accep(m))
	}
	minuteDoubleUnion := pynini.Union(minuteDoubleFsts...)

	deleteLeadingZero := lib.DeleteString("0").Ques()
	graphHour := t.DIGIT.Concat(t.DIGIT).Union(deleteLeadingZero.Concat(t.DIGIT)).Compose(hourUnion).Compose(cardinal)

	graphMinuteSingle := minuteSingleUnion.Compose(cardinal)
	graphMinuteDouble := minuteDoubleUnion.Compose(cardinal)

	finalGraphHour := lib.Insert("hours: \"").Concat(graphHour).Concat(lib.Insert("\""))
	finalGraphMinute := lib.Insert("minutes: \"").Concat(
		pynini.Cross("0", "o").Concat(t.INSERT_SPACE).Concat(graphMinuteSingle).Union(graphMinuteDouble),
	).Concat(lib.Insert("\""))

	finalGraphSecond := lib.Insert("seconds: \"").Concat(
		pynini.Cross("0", "o").Concat(t.INSERT_SPACE).Concat(graphMinuteSingle).Union(graphMinuteDouble),
	).Concat(lib.Insert("\""))

	finalSuffix := lib.Insert("suffix: \"").Concat(suffixGraph).Concat(lib.Insert("\""))
	finalSuffixOptional := t.DELETE_SPACE.Concat(t.INSERT_SPACE).Concat(finalSuffix).Ques()
	finalTimeZoneOptional := t.DELETE_SPACE.Concat(t.INSERT_SPACE).Concat(
		lib.Insert("zone: \"")).Concat(timeZoneGraph).Concat(lib.Insert("\"")).Ques()

	// H:M patterns
	graphHM := finalGraphHour.Concat(
		lib.DeleteString(":")).Concat(
		lib.DeleteString("00").Union(t.INSERT_SPACE.Concat(finalGraphMinute))).Concat(
		finalSuffixOptional).Concat(finalTimeZoneOptional)

	// H:M:S patterns
	graphHMS := finalGraphHour.Concat(
		lib.DeleteString(":")).Concat(
		pynini.Cross("00", " minutes: \"zero\"").Union(t.INSERT_SPACE.Concat(finalGraphMinute))).Concat(
		lib.DeleteString(":")).Concat(
		pynini.Cross("00", " seconds: \"zero\"").Union(t.INSERT_SPACE.Concat(finalGraphSecond))).Concat(
		finalSuffixOptional).Concat(finalTimeZoneOptional)

	// H.MM patterns
	graphHM2 := finalGraphHour.Concat(
		lib.DeleteString(".")).Concat(
		lib.DeleteString("00").Union(t.INSERT_SPACE.Concat(finalGraphMinute))).Concat(
		t.DELETE_SPACE).Concat(t.INSERT_SPACE).Concat(finalSuffix).Concat(finalTimeZoneOptional)

	// H patterns
	graphH := finalGraphHour.Concat(t.DELETE_SPACE).Concat(t.INSERT_SPACE).Concat(finalSuffix).Concat(finalTimeZoneOptional)

	finalGraph := pynini.Union(graphHM, graphH, graphHM2, graphHMS).Optimize()
	finalGraph = t.AddTokens(finalGraph)
	t.Tagger = finalGraph.Optimize()
}

func (t *Time) suffixGraph() *pynini.Fst {
	fst, _ := pynini.StringFile(tn.EnglishDataPath("data/time/suffix.tsv"))
	return fst
}

func (t *Time) BuildVerbalizer() {
	hour := lib.DeleteString("hours:").Concat(t.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(t.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))
	minute := lib.DeleteString("minutes:").Concat(t.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(t.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))
	suffix := lib.DeleteString("suffix:").Concat(t.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(t.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))
	optionalSuffix := t.DELETE_SPACE.Concat(t.INSERT_SPACE).Concat(suffix).Ques()
	zone := lib.DeleteString("zone:").Concat(t.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(t.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))
	optionalZone := t.DELETE_SPACE.Concat(t.INSERT_SPACE).Concat(zone).Ques()
	second := lib.DeleteString("seconds:").Concat(t.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(t.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))

	graphHMS := hour.Concat(lib.Insert(" hours ")).Concat(t.DELETE_SPACE).Concat(minute).Concat(
		lib.Insert(" minutes and ")).Concat(t.DELETE_SPACE).Concat(second).Concat(
		lib.Insert(" seconds")).Concat(optionalSuffix).Concat(optionalZone)
	graphHMS = graphHMS.Compose(
		pynini.Union(
			lib.DeleteString("o "),
			pynini.Cross("one minutes", "one minute"),
			pynini.Cross("one seconds", "one second"),
			pynini.Cross("one hours", "one hour"),
		),
	)

	graph := hour.Concat(t.DELETE_SPACE).Concat(t.INSERT_SPACE).Concat(minute).Concat(optionalSuffix).Concat(optionalZone)
	graph = graph.Union(hour.Concat(t.INSERT_SPACE).Concat(lib.Insert("o'clock")).Concat(optionalZone))
	graph = graph.Union(hour.Concat(t.DELETE_SPACE).Concat(t.INSERT_SPACE).Concat(suffix).Concat(optionalZone))
	graph = graph.Union(graphHMS)

	deleteTokens := t.DeleteTokens(graph)
	t.Verbalizer = deleteTokens.Optimize()
}
