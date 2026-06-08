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
	h, _ := pynini.StringFile(tn.ChineseDataPath("data/time/hour.tsv"))
	m, _ := pynini.StringFile(tn.ChineseDataPath("data/time/minute.tsv"))
	s, _ := pynini.StringFile(tn.ChineseDataPath("data/time/second.tsv"))
	noon, _ := pynini.StringFile(tn.ChineseDataPath("data/time/noon.tsv"))
	colon := lib.DeleteString(":").Union(lib.DeleteString("\uff1a"))

	tagger := lib.Insert("hour: \"").
		Concat(h).
		Concat(lib.Insert("\" ")).
		Concat(colon).
		Concat(lib.Insert("minute: \"")).
		Concat(m).
		Concat(lib.Insert("\"")).
		Concat(
			colon.
				Concat(lib.Insert(" second: \"")).
				Concat(s).
				Concat(lib.Insert("\"")).
				Ques(),
		).
		Concat(lib.DeleteString(" ").Ques()).
		Concat(
			lib.Insert(" noon: \"").
				Concat(noon).
				Concat(lib.Insert("\"")).
				Ques(),
		)
	tagger = tagger.RmEpsilon().Connect()
	tagger = t.AddTokens(tagger)

	to := lib.DeleteString("-").Union(lib.DeleteString("~")).
		Concat(lib.Insert(" char { value: \"到\" } "))
	t.Tagger = tagger.Concat(to.Concat(tagger).Ques())
}

func (t *Time) BuildVerbalizer() {
	noon := lib.DeleteString("noon: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\" "))
	hour := lib.DeleteString("hour: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\" "))
	minute := lib.DeleteString("minute: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\""))
	second := lib.DeleteString(" second: \"").Concat(t.SIGMA).Concat(lib.DeleteString("\""))
	verbalizer := noon.Ques().Concat(hour).Concat(minute).Concat(second.Ques())
	t.Verbalizer = t.DeleteTokens(verbalizer)
}
