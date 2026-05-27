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
	cardinal := NewCardinal(true, true, true).Number
	decimal := NewCardinal(true, true, true).Decimal
	sign, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/number/sign.tsv"))
	signTag := lib.Insert("sign: \"").Concat(sign).Concat(lib.Insert("\""))

	fraction_word := pynini.Union(lib.DeleteString("分の"), lib.DeleteString(" 分 の "), lib.DeleteString("分 の "), lib.DeleteString("分 の"))
	root_word := pynini.Union(pynini.Accep("√"), pynini.Cross("ルート", "√"))

	// denominator
	denominator := pynini.Union(
		decimal,
		cardinal.Concat(root_word).Concat(cardinal),
		root_word.Concat(cardinal),
		cardinal,
	).Concat(lib.DeleteString(" ").Ques())
	denominator = lib.Insert("denominator: \"").Concat(denominator).Concat(lib.Insert("\""))

	// numerator
	numerator := lib.DeleteString(" ").Ques().Concat(pynini.Union(
		decimal,
		cardinal.Concat(root_word).Concat(cardinal),
		root_word.Concat(cardinal),
		cardinal,
	))
	numerator = lib.Insert("numerator: \"").Concat(numerator).Concat(lib.Insert("\""))

	// fraction
	fraction_sign := signTag.Concat(lib.Insert(" ")).Concat(denominator).Concat(lib.Insert(" ")).Concat(fraction_word).Concat(numerator)
	fraction_no_sign := denominator.Concat(lib.Insert(" ")).Concat(fraction_word).Concat(numerator)
	regular_fractions := pynini.Union(fraction_sign, fraction_no_sign)

	integer_fraction_sign := signTag.Concat(lib.Insert(" ")).Ques().Concat(denominator).Concat(lib.Insert(" ")).Concat(fraction_word).Concat(numerator)
	fraction := pynini.Union(regular_fractions, integer_fraction_sign)
	f.Tagger = f.AddTokens(fraction).Optimize()
}

func (f *Fraction) BuildVerbalizer() {
	sign := lib.DeleteString("sign: \"").Concat(f.SIGMA).Concat(lib.DeleteString("\""))
	denominator := lib.DeleteString("denominator: \"").Concat(f.SIGMA).Concat(lib.DeleteString("\""))
	numerator := lib.DeleteString("numerator: \"").Concat(f.SIGMA).Concat(lib.DeleteString("\""))
	fraction := pynini.Union(
		sign.Concat(lib.DeleteString(" ")).Ques(),
		lib.Insert(""),
	).Concat(numerator).Concat(lib.DeleteString(" ")).Concat(lib.Insert("/")).Concat(denominator)
	f.Verbalizer = f.DeleteTokens(fraction).Optimize()
}
