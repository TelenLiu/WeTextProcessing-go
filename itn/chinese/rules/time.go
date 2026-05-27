package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Time struct {
	*tn.Processor
}

func NewTime() *Time {
	t := &Time{
		Processor: tn.NewProcessor("time"),
	}
	t.BuildTagger()
	t.BuildVerbalizer()
	return t
}

func (t *Time) BuildTagger() {
	h, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/time/hour.tsv"))
	m, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/time/minute.tsv"))
	s, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/time/second.tsv"))
	noon, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/time/noon.tsv"))

	tagger := lib.Insert("noon: \"").Concat(noon).Concat(lib.Insert("\" ")).Ques().
		Concat(lib.Insert("hour: \"")).
		Concat(h).
		Concat(lib.Insert("\"")).
		Concat(lib.Insert(" minute: \"")).
		Concat(m).
		Concat(lib.DeleteString("分").Ques()).
		Concat(lib.Insert("\"")).
		Concat(lib.Insert(" second: \"").Concat(s).Concat(lib.Insert("\"")).Ques())

	t.Tagger = t.AddTokens(tagger)
}

func (t *Time) BuildVerbalizer() {
	addcolon := lib.Insert(":")
	hour := lib.DeleteString("hour: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\""))
	minute := lib.DeleteString(" minute: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\""))
	second := lib.DeleteString(" second: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\""))
	noon := lib.DeleteString(" noon: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\""))

	verbalizer := hour.Concat(addcolon).Concat(minute).
		Concat(addcolon.Concat(second).Ques()).
		Concat(noon.Ques())

	t.Verbalizer = t.DeleteTokens(verbalizer)
}
