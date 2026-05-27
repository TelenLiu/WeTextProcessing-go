package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Cardinal struct {
	*tn.Processor
	Number *pynini.Fst
	digits *pynini.Fst
}

func NewCardinal() *Cardinal {
	c := &Cardinal{
		Processor: tn.NewProcessor("cardinal"),
	}
	c.BuildTagger()
	c.BuildVerbalizer()
	return c
}

func (c *Cardinal) BuildTagger() {
	zero, _ := pynini.StringFile(tn.ChineseDataPath("data/number/zero.tsv"))
	digit, _ := pynini.StringFile(tn.ChineseDataPath("data/number/digit.tsv"))
	teen, _ := pynini.StringFile(tn.ChineseDataPath("data/number/teen.tsv"))
	sign, _ := pynini.StringFile(tn.ChineseDataPath("data/number/sign.tsv"))
	dot, _ := pynini.StringFile(tn.ChineseDataPath("data/number/dot.tsv"))

	rmzero := pynini.Union(lib.DeleteString("0"), lib.DeleteString("０"))
	rmpunct := lib.DeleteString(",").Ques()
	digits := pynini.Union(zero, digit)
	c.digits = digits

	// digit for "2" -> "两" rewrite (used in hundred, thousand, ten_thousand)
	two_liang := pynini.Cross("2", "两")

	ten := teen.Concat(lib.Insert("十")).Concat(pynini.Union(digit, rmzero))
	tens := digit.Concat(lib.Insert("十")).Concat(pynini.Union(digit, rmzero))
	hundred := digit.Concat(lib.Insert("百")).Concat(
		pynini.Union(
			tens,
			zero.Concat(digit),
			rmzero.Repeat(2),
		),
	)
	hundred2 := two_liang.Concat(lib.Insert("百")).Concat(
		pynini.Union(
			tens,
			zero.Concat(digit),
			rmzero.Repeat(2),
		),
	)
	thousand := digit.Concat(lib.Insert("千")).Concat(rmpunct).Concat(
		pynini.Union(
			hundred,
			zero.Concat(rmpunct).Concat(tens),
			rmzero.Concat(rmpunct).Concat(zero).Concat(digit),
			rmzero.Repeat(3),
		),
	)
	thousand2 := two_liang.Concat(lib.Insert("千")).Concat(rmpunct).Concat(
		pynini.Union(
			hundred,
			zero.Concat(rmpunct).Concat(tens),
			rmzero.Concat(rmpunct).Concat(zero).Concat(digit),
			rmzero.Repeat(3),
		),
	)
	ten_thousand := pynini.Union(thousand, hundred, ten, digit).
		Concat(lib.Insert("万")).
		Concat(
			pynini.Union(
				thousand,
				zero.Concat(rmpunct).Concat(hundred),
				rmzero.Concat(rmpunct).Concat(zero).Concat(tens),
				rmzero.Concat(rmpunct).Concat(rmzero).Concat(zero).Concat(digit),
				rmzero.Repeat(4),
			),
		)
	ten_thousand2 := two_liang.Concat(lib.Insert("万")).
		Concat(
			pynini.Union(
				thousand,
				zero.Concat(rmpunct).Concat(hundred),
				rmzero.Concat(rmpunct).Concat(zero).Concat(tens),
				rmzero.Concat(rmpunct).Concat(rmzero).Concat(zero).Concat(digit),
				rmzero.Repeat(4),
			),
		)

	number := pynini.Union(digits, ten, hundred, hundred2, thousand, thousand2, ten_thousand, ten_thousand2)
	number = sign.Ques().Concat(number).Concat(dot.Concat(digits.Plus()).Ques())
	percent := lib.Insert("百分之").Concat(number).Concat(lib.DeleteString("%"))
	c.Number = pynini.Accep("约").Ques().Concat(pynini.Accep("人均").Ques()).Concat(pynini.Union(number, percent))

	cardinal := digits.Plus().Concat(dot.Concat(digits.Plus()).Repeat(3))
	cardinal = cardinal.Union(percent)
	cardinal = cardinal.Union(digits.Plus().Concat(lib.DeleteString("-").Concat(digits.Plus()).Repeat(2)))
	cardinal = cardinal.Union(digits.Repeat(3).Concat(lib.DeleteString("-")).Concat(digits.Repeat(8)))

	// Phone number: digits with 一→幺 rewrite via composition (matching Python)
	// Python: phone_digits = digits @ self.build_rule(cross("一", "幺"))
	// This composes digits with the rule that maps "一" to "幺"
	yaoRule := pynini.Union(
		pynini.Cross("一", "幺"),
		pynini.Accep("零"),
		pynini.Accep("二"),
		pynini.Accep("三"),
		pynini.Accep("四"),
		pynini.Accep("五"),
		pynini.Accep("六"),
		pynini.Accep("七"),
		pynini.Accep("八"),
		pynini.Accep("九"),
	)
	phone_digits := digits.Compose(yaoRule)
	phone := pynini.Union(phone_digits.Repeat(3), phone_digits.Repeat(5), phone_digits.Repeat(11))
	phone = phone.Union(
		pynini.Accep("尾号").Concat(
			pynini.Union(pynini.Accep("是"), pynini.Accep("为")).Ques(),
		).Concat(phone_digits.Repeat(4)),
	)
	cardinal = cardinal.Union(lib.AddWeight(phone, -1.0))

	tagger := lib.Insert("value: \"").Concat(cardinal).Concat(lib.Insert("\""))
	c.Tagger = c.AddTokens(tagger)
}

func (c *Cardinal) Digits() *pynini.Fst {
	return c.digits
}

func (c *Cardinal) BuildVerbalizer() {
	c.Processor.BuildVerbalizer()
}
