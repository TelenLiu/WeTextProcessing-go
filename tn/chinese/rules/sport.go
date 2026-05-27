package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Sport struct {
	*tn.Processor
}

func NewSport() *Sport {
	s := &Sport{
		Processor: tn.NewProcessor("sport"),
	}
	s.BuildTagger()
	s.BuildVerbalizer()
	return s
}

func (s *Sport) BuildTagger() {
	country, _ := pynini.StringFile(tn.ChineseDataPath("data/sport/country.tsv"))
	club, _ := pynini.StringFile(tn.ChineseDataPath("data/sport/club.tsv"))
	rmsign := lib.DeleteString("/").Union(lib.DeleteString("-")).Union(lib.DeleteString(":"))
	rmspace := lib.DeleteString(" ").Ques()

	number := NewCardinal().Number
	score := rmspace.Concat(number).Concat(rmsign).Concat(lib.Insert("比")).Concat(number).Concat(rmspace)
	tagger := lib.Insert("team: \"").
		Concat(country.Union(club)).
		Concat(lib.Insert("\" score: \"")).
		Concat(score).
		Concat(lib.Insert("\""))
	s.Tagger = s.AddTokens(tagger)
}

func (s *Sport) BuildVerbalizer() {
	s.Processor.BuildVerbalizer()
	team := lib.DeleteString("team: \"").Concat(s.SIGMA).Concat(lib.DeleteString("\" "))
	score := lib.DeleteString("score: \"").Concat(s.SIGMA).Concat(lib.DeleteString("\""))
	verbalizer := team.Concat(score)
	s.Verbalizer = s.Verbalizer.Union(s.DeleteTokens(verbalizer))
}
