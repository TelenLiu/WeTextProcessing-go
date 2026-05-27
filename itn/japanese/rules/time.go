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
	h, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/time/hour.tsv"))
	m, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/time/minute.tsv"))
	s, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/time/second.tsv"))

	// 一時三十分三秒 一時三十分 三十分三秒 一時 三十分 三秒
	tagger := pynini.Union(
		lib.Insert("hour: \"").Concat(h).Concat(lib.Insert("\" ")).Ques().Concat(
			lib.Insert("minute: \"").Concat(m).Concat(lib.Insert("\""))).Concat(
			lib.Insert(" second: \"").Concat(s).Concat(lib.Insert("\"")).Ques(),
		),
		lib.Insert("hour: \"").Concat(h).Concat(lib.Insert("\" ")),
		lib.Insert(" second: \"").Concat(s).Concat(lib.Insert("\"")),
	)
	tagger = t.AddTokens(tagger)
	t.Tagger = tagger
}

func (t *Time) BuildVerbalizer() {
	hour := lib.DeleteString("hour: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\"")).Concat(lib.DeleteString(" ").Ques()).Concat(lib.Insert("時"))
	minute := lib.DeleteString("minute: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\"")).Concat(lib.Insert("分"))
	second := lib.DeleteString(" ").Ques().Concat(lib.DeleteString("second: \"")).Concat(t.SIGMA).Concat(lib.DeleteString("\"")).Concat(lib.Insert("秒"))

	verbalizer := pynini.Union(
		hour.Ques().Concat(minute).Concat(second.Ques()),
		second,
		hour,
	)
	t.Verbalizer = t.DeleteTokens(verbalizer)
}
