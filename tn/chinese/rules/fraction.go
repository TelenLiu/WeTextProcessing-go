package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Fraction struct {
	*tn.Processor
}

func NewFraction() *Fraction {
	f := &Fraction{
		Processor: tn.NewProcessor("fraction"),
	}
	f.BuildTagger()
	f.BuildVerbalizer()
	return f
}

func (f *Fraction) BuildTagger() {
	rmspace := lib.DeleteString(" ").Ques()
	number := NewCardinal().Number

	tagger := lib.Insert("numerator: \"").
		Concat(number).
		Concat(rmspace).
		Concat(lib.DeleteString("/")).
		Concat(rmspace).
		Concat(lib.Insert("\" denominator: \"")).
		Concat(number).
		Concat(lib.Insert("\"")).
		Optimize()
	f.Tagger = f.AddTokens(tagger)
}

func (f *Fraction) BuildVerbalizer() {
	denominator := lib.DeleteString("denominator: \"").Concat(f.SIGMA).Concat(lib.DeleteString("\" "))
	numerator := lib.DeleteString("numerator: \"").Concat(f.SIGMA).Concat(lib.DeleteString("\""))
	verbalizer := denominator.Concat(lib.Insert("分之")).Concat(numerator)
	f.Verbalizer = f.DeleteTokens(verbalizer)
}
