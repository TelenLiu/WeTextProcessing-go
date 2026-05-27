package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Cardinal struct {
	*tn.Processor
	Thousand         *pynini.Fst
	PositiveInteger  *pynini.Fst
	Number           *pynini.Fst
	Digits           *pynini.Fst
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
	zero, _ := pynini.StringFile(tn.JapaneseDataPath("data/number/zero.tsv"))
	en_zero := pynini.Cross("0", "ゼロ")
	digit, _ := pynini.StringFile(tn.JapaneseDataPath("data/number/digit.tsv"))
	teen, _ := pynini.StringFile(tn.JapaneseDataPath("data/number/teen.tsv"))
	sign, _ := pynini.StringFile(tn.JapaneseDataPath("data/number/sign.tsv"))
	dot, _ := pynini.StringFile(tn.JapaneseDataPath("data/number/dot.tsv"))

	rmzero := pynini.Union(lib.DeleteString("0"), lib.DeleteString("０"))
	rmpunct := lib.DeleteString(",").Ques()
	rmspace := lib.DeleteString(" ").Ques()

	// 0-9
	digits := pynini.Union(zero, digit)
	c.Digits = digits

	// 10-99
	ten := teen.Concat(lib.Insert("十")).Concat(pynini.Union(digit, rmzero))

	// 100-999
	hundred := teen.Concat(lib.Insert("百")).Concat(
		pynini.Union(
			ten,
			rmzero.Concat(digit),
			rmzero.Repeat(2),
		),
	)
	// hundred prefix containing "," like "1,11" in "1,115,000"
	hundred_prefix := teen.Concat(lib.Insert("百")).Concat(rmpunct).Concat(
		pynini.Union(
			ten,
			rmzero.Concat(digit),
			rmzero.Repeat(2),
		),
	)

	// 1000-9999
	thousand := teen.Concat(lib.Insert("千")).Concat(rmpunct).Concat(
		pynini.Union(
			hundred,
			rmzero.Concat(rmpunct).Concat(ten),
			rmzero.Concat(rmpunct).Concat(rmzero).Concat(digit),
			rmzero.Repeat(3),
		),
	)
	// thousand prefix containing "," like "10,00" in "10,000,000"
	thousand_prefix := digit.Concat(lib.Insert("千")).Concat(
		pynini.Union(
			hundred_prefix,
			rmzero.Concat(rmpunct).Concat(ten),
			rmzero.Concat(rmpunct).Concat(rmzero).Concat(digit),
			rmzero.Concat(rmpunct).Concat(rmzero.Repeat(2)),
		),
	)
	c.Thousand = thousand

	// 10000-99999999
	ten_thousand := pynini.Union(thousand_prefix, hundred_prefix, ten, digit).
		Concat(lib.Insert("万")).
		Concat(
			pynini.Union(
				thousand,
				rmzero.Concat(rmpunct).Concat(hundred),
				rmzero.Concat(rmpunct).Concat(rmzero).Concat(ten),
				rmzero.Concat(rmpunct).Concat(rmzero).Concat(rmzero).Concat(digit),
				rmzero.Concat(rmpunct).Concat(rmzero.Repeat(3)),
			),
		)

	// 100,000,000+
	hundred_millon := pynini.Union(thousand_prefix, hundred_prefix, ten, digit).
		Concat(lib.Insert("億")).
		Concat(rmzero.Repeat(2)).
		Concat(rmpunct).
		Concat(rmzero.Repeat(2)).
		Concat(
			pynini.Union(
				thousand,
				rmzero.Concat(rmpunct).Concat(hundred),
				rmzero.Concat(rmpunct).Concat(rmzero).Concat(ten),
				rmzero.Concat(rmpunct).Concat(rmzero).Concat(rmzero).Concat(digit),
				rmzero.Concat(rmpunct).Concat(rmzero.Repeat(3)),
			),
		)

	// 0-99999999
	number := pynini.Union(digits, ten, hundred, thousand, ten_thousand, hundred_millon)
	c.PositiveInteger = number

	// ±0.0 - ±99999999.99999999
	number = sign.Ques().Concat(number).Concat(dot.Concat(digits.Plus()).Ques())
	c.Number = number

	// % like -27.00%
	percent := number.Concat(lib.DeleteString("%")).Concat(lib.Insert("パーセント"))

	// ip like 127.0.0.1
	ip := digits.Plus().Concat(dot.Concat(digits.Plus()).Repeat(3))

	// phone like 0xx-xxxx-xxxx
	country_code := pynini.Cross("+81", "プラス八一").Concat(pynini.Cross("-", "の").Ques()).Concat(rmspace)
	en_digits := pynini.Union(en_zero, digit)
	phone := en_zero.Concat(digit).
		Concat(en_zero.Ques()).
		Concat(pynini.Cross("-", "の")).
		Concat(en_digits.Repeat(3)).
		Concat(en_digits.Ques()).
		Concat(pynini.Cross("-", "の").Ques()).
		Concat(en_digits.Repeat(4))
	phone = country_code.Ques().Concat(
		pynini.Union(phone, en_zero.Concat(digit).Concat(en_zero.Ques()).Concat(en_digits.Repeat(8))),
	)

	// No. 番号
	ordinal := pynini.Union(pynini.Accep("No."), pynini.Accep("番号"), pynini.Accep("番号は")).Concat(digits.Plus())
	room := digits.Plus().Concat(pynini.Accep("号室"))

	// others like 342388491 (8+ digits, read digit by digit)
	// Use digits (not en_digits) to get 〇 for zero instead of ゼロ
	// Add small weight to make it less preferred than number rule
	others := sign.Ques().Concat(digits.Repeat(8)).Concat(digits.Plus())
	others = pynini.AddWeight(others, 0.1)

	number = pynini.Union(number, percent, ip, phone, ordinal, room, others)

	tagger := lib.Insert("value: \"").Concat(number).Concat(lib.Insert("\"")).Optimize()
	c.Tagger = c.AddTokens(tagger)
}

func (c *Cardinal) BuildVerbalizer() {
	c.Processor.BuildVerbalizer()
}
