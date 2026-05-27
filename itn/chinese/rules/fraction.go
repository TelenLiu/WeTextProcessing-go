package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
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
	cardinal := NewCardinal(false, true, false)
	number := cardinal.Number
	sign, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/sign.tsv"))

	// sign: "" + sign.ques + denominator: "" + number + delete("分之") + numerator: "" + number + ""
	tagger := lib.Insert("sign: \"").
		Concat(lib.AddWeight(sign, -1.0).Ques()).
		Concat(lib.Insert("\" denominator: \"")).
		Concat(number).
		Concat(lib.DeleteString("分之")).
		Concat(lib.Insert("\" numerator: \"")).
		Concat(number).
		Concat(lib.Insert("\""))

	f.Tagger = f.AddTokens(tagger)
}

func (f *Fraction) BuildVerbalizer() {
	sign := lib.DeleteString("sign: \"").Concat(f.SIGMA).Concat(lib.DeleteString("\""))
	numerator := lib.DeleteString(" numerator: \"").Concat(f.SIGMA).Concat(lib.DeleteString("\""))
	denominator := lib.DeleteString(" denominator: \"").Concat(f.SIGMA).Concat(lib.DeleteString("\""))
	verbalizer := sign.Concat(numerator).Concat(lib.Insert("/")).Concat(denominator)
	f.Verbalizer = f.DeleteTokens(verbalizer)
}
