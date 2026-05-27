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
	h, _ := pynini.StringFile(tn.JapaneseDataPath("data/time/hour.tsv"))
	m, _ := pynini.StringFile(tn.JapaneseDataPath("data/time/minute.tsv"))
	s, _ := pynini.StringFile(tn.JapaneseDataPath("data/time/second.tsv"))
	noon, _ := pynini.StringFile(tn.JapaneseDataPath("data/time/noon.tsv"))

	colon := lib.DeleteString(":").Union(lib.DeleteString("："))

	h_noon := lib.Insert("hour: \"").Concat(h).Concat(lib.Insert("\" noon: \"")).Concat(noon).Concat(lib.Insert("\""))
	h_tag := lib.Insert("hour: \"").Concat(h).Concat(lib.Insert("\" "))
	m_tag := lib.Insert("minute: \"").Concat(m).Concat(lib.Insert("\""))
	s_tag := lib.Insert(" second: \"").Concat(s).Concat(lib.Insert("\""))
	noon_tag := lib.Insert(" noon: \"").Concat(noon).Concat(lib.Insert("\""))

	tagger := pynini.Union(
		h_tag.Concat(colon).Concat(m_tag).Concat(
			colon.Concat(s_tag).Ques()).Concat(
			lib.DeleteString(" ").Ques()).Concat(
			noon_tag.Ques()),
		h_noon,
	)
	tagger = t.AddTokens(tagger)

	to := pynini.Union(lib.DeleteString("-"), lib.DeleteString("~")).
		Concat(lib.Insert(" char { value: \"から\" } "))
	t.Tagger = tagger.Concat(to.Concat(tagger).Ques())
}

func (t *Time) BuildVerbalizer() {
	noon := lib.DeleteString("noon: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\" "))
	hour := lib.DeleteString("hour: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\""))
	minute := lib.DeleteString(" minute: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\""))
	second := lib.DeleteString(" second: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\""))
	verbalizer := noon.Ques().Concat(hour).Concat(minute).Concat(second.Ques()).Union(noon.Concat(hour))
	t.Verbalizer = t.DeleteTokens(verbalizer)
}
