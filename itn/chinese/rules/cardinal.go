package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Cardinal struct {
	*tn.Processor
	Number               *pynini.Fst
	NumberExclude_0_to_9 *pynini.Fst
	enable_standalone_number bool
	enable_0_to_9           bool
	enable_million          bool
}

func NewCardinal(enable_standalone_number, enable_0_to_9, enable_million bool) *Cardinal {
	c := &Cardinal{
		Processor:                tn.NewProcessor("cardinal"),
		enable_standalone_number: enable_standalone_number,
		enable_0_to_9:           enable_0_to_9,
		enable_million:          enable_million,
	}
	c.BuildTagger()
	c.BuildVerbalizer()
	return c
}

func (c *Cardinal) BuildTagger() {
	zero, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/zero.tsv"))
	digit, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/digit.tsv"))
	special_tilde, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/special_tilde.tsv"))
	special_dash, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/special_dash.tsv"))
	sign, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/sign.tsv"))
	dot, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/dot.tsv"))

	addzero := lib.Insert("0")
	digits := pynini.Union(zero, digit)

	// 十一 => 11, 十二 => 12
	teen := pynini.Cross("十", "1").Concat(pynini.Union(digit, lib.AddWeight(addzero, 0.1)))
	// 一十一 => 11, 二十一 => 21, 三十 => 30
	tens := digit.Concat(lib.DeleteString("十")).Concat(pynini.Union(digit, lib.AddWeight(addzero, 0.1)))
	// 一百一十 => 110, 一百零一 => 101, 一百一 => 110, 一百 => 100
	hundred := digit.Concat(lib.DeleteString("百")).Concat(
		pynini.Union(
			tens,
			teen,
			lib.AddWeight(zero.Concat(digit), 0.1),
			lib.AddWeight(digit.Concat(addzero), 0.5),
			lib.AddWeight(addzero.Repeat(2), 1.0),
		),
	)
	// 一千一百一十一 => 1111, 一千零一十一 => 1011, 一千零一 => 1001
	// 一千一 => 1100, 一千 => 1000
	thousand := digit.Concat(lib.DeleteString("千")).Concat(
		pynini.Union(
			hundred,
			lib.AddWeight(zero.Concat(pynini.Union(tens, teen)), 0.1),
			lib.AddWeight(addzero.Concat(zero).Concat(digit), 0.5),
			lib.AddWeight(digit.Concat(addzero.Repeat(2)), 0.8),
			lib.AddWeight(addzero.Repeat(3), 1.0),
		),
	)

	// ten_thousand
	var ten_thousand *pynini.Fst
	if c.enable_million {
		ten_thousand = pynini.Union(thousand, hundred, teen, tens, digit).Concat(lib.DeleteString("万")).Concat(
			pynini.Union(
				thousand,
				lib.AddWeight(zero.Concat(hundred), 0.1),
				lib.AddWeight(addzero.Concat(zero).Concat(pynini.Union(tens, teen)), 0.5),
				lib.AddWeight(addzero.Concat(addzero).Concat(zero).Concat(digit), 0.5),
				lib.AddWeight(digit.Concat(addzero.Repeat(3)), 0.8),
				lib.AddWeight(addzero.Repeat(4), 1.0),
			),
		)
	} else {
		ten_thousand = pynini.Union(teen, tens, digit).Concat(lib.DeleteString("万")).Concat(
			pynini.Union(
				thousand,
				lib.AddWeight(zero.Concat(hundred), 0.1),
				lib.AddWeight(addzero.Concat(zero).Concat(pynini.Union(tens, teen)), 0.5),
				lib.AddWeight(addzero.Concat(addzero).Concat(zero).Concat(digit), 0.5),
				lib.AddWeight(digit.Concat(addzero.Repeat(3)), 0.8),
				lib.AddWeight(addzero.Repeat(4), 1.0),
			),
		)
		ten_thousand = pynini.Union(
			ten_thousand,
			pynini.Union(thousand, hundred).Concat(pynini.Accep("万")).Concat(lib.DeleteString("零").Ques()).Concat(
				pynini.Union(thousand, hundred, tens, teen, digits).Ques(),
			),
		)
	}

	// number = digits | teen | tens | hundred | thousand | ten_thousand
	number := pynini.Union(digits, teen, tens, hundred, thousand, ten_thousand)
	// 兆/亿 - make prefix optional but always require number at the end
	zhao_prefix := pynini.Union(number, pynini.Union(thousand, hundred, teen, tens, digit)).Concat(
		pynini.Accep("兆"),
	).Concat(lib.DeleteString("零").Ques())
	number = pynini.Union(zhao_prefix, pynini.Accep("")).Concat(number)
	// Add 亿 support
	// number = ((number + "兆" + "零"?).ques + (number + "亿" + "零"?).ques + number
	// Simplified: number + (dot + digits)+
	number = sign.Ques().Concat(number).Concat(dot.Concat(digits.Plus()).Ques())

	// special_tilde with optional 万/亿
	special_tilde = special_tilde.Concat(lib.AddWeight(pynini.Accep("万").Union(pynini.Accep("亿")), -0.1).Ques())
	special_dash = special_dash.Concat(lib.AddWeight(pynini.Accep("万").Union(pynini.Accep("亿")), -0.1).Ques())

	number = pynini.Union(number, special_tilde)

	// special dash patterns
	_special_dash := pynini.Cross("十", "1").Concat(special_dash)
	_special_dash = pynini.Union(
		_special_dash,
		digit.Concat(lib.DeleteString("十")).Concat(special_dash),
		digit.Concat(lib.DeleteString("百")).Concat(special_dash),
		digit.Concat(lib.DeleteString("万")).Concat(digit).Concat(lib.Insert("000-")).Concat(digit).Concat(lib.Insert("000")),
	)
	number = pynini.Union(number, _special_dash)

	c.Number = number.Optimize()

	// number_exclude_0_to_9
	number_exclude_0_to_9 := pynini.Union(teen, tens, hundred, thousand, ten_thousand)
	// 兆/亿
	number_exclude_0_to_9 = pynini.Union(
		pynini.Union(number_exclude_0_to_9, digits).Concat(pynini.Accep("兆")).Concat(lib.DeleteString("零").Ques()).Ques().Concat(
			pynini.Union(number_exclude_0_to_9, digits).Concat(pynini.Accep("亿")).Concat(lib.DeleteString("零").Ques()).Ques().Concat(
				number_exclude_0_to_9,
			),
		),
		number_exclude_0_to_9,
	)
	// (number_exclude_0_to_9 | digits) + (dot + digits).plus
	number_exclude_0_to_9 = pynini.Union(
		number_exclude_0_to_9,
		pynini.Union(number_exclude_0_to_9, digits).Concat(dot.Concat(digits.Plus()).Plus()),
	)
	number_exclude_0_to_9 = pynini.Union(number_exclude_0_to_9, special_tilde)
	number_exclude_0_to_9 = pynini.Union(number_exclude_0_to_9, lib.AddWeight(_special_dash, -0.1))

	c.NumberExclude_0_to_9 = sign.Ques().Concat(number_exclude_0_to_9).Optimize()

	// cardinal for IP, phone, ID
	cardinal := digits.Plus().Concat(dot.Concat(digits.Plus()).Plus())
	// float number like 1.11
	cardinal = pynini.Union(cardinal, number.Concat(dot).Concat(digits.Plus()))
	// phone/ID patterns
	idcard_last_char := pynini.Union(digits, pynini.Accep("X"), pynini.Accep("x"))
	cardinal = pynini.Union(
		cardinal,
		digits.Repeat(3),
		digits.Repeat(4),
		digits.Repeat(5),
		digits.Repeat(11),
		digits.Repeat(17).Concat(idcard_last_char),
		digits.Repeat(18),
	)

	// Combine with number
	if c.enable_standalone_number {
		if c.enable_0_to_9 {
			cardinal = pynini.Union(cardinal, lib.AddWeight(number, 0.1))
		} else {
			cardinal = pynini.Union(cardinal, lib.AddWeight(number_exclude_0_to_9, 0.1))
		}
	}

	tagger := lib.Insert("value: \"").Concat(cardinal).Concat(lib.Insert(" ").Concat(cardinal).Star()).Concat(lib.Insert("\""))
	c.Tagger = c.AddTokens(tagger)
}

func (c *Cardinal) BuildVerbalizer() {
	c.Processor.BuildVerbalizer()
}
