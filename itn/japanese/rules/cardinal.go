package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Cardinal struct {
	*tn.Processor
	TenThousandMinus       *pynini.Fst
	Number                 *pynini.Fst
	NumberExclude_0_to_9   *pynini.Fst
	Decimal                *pynini.Fst
	BigInteger             *pynini.Fst
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
	zero, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/number/zero.tsv"))
	digit, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/number/digit.tsv"))
	hundred_digit, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/number/hundred_digit.tsv"))
	sign, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/number/sign.tsv"))
	dot, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/number/dot.tsv"))
	ties, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/number/ties.tsv"))
	graph_teen, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/number/teen.tsv"))

	addzero := lib.Insert("0")
	// 〇 三 九
	digits := pynini.Union(zero, digit) // 0 ~ 9
	// 十 十一 十九
	teen := graph_teen
	teen = pynini.Union(teen, pynini.Cross("十", "1").Concat(pynini.Union(digit, addzero)))
	// 三十 三十一 九十 九十一
	tens := pynini.Union(ties.Concat(addzero), ties.Concat(pynini.Union(digit, addzero)))

	// 三百二 三百
	hundred := pynini.Union(
		digit.Concat(lib.DeleteString("百")).Concat(pynini.Union(tens, teen, pynini.Union(addzero.Concat(digits), addzero.Repeat(2)))),
		// 百 百十 百二十三
		pynini.Cross("百", "1").Concat(pynini.Union(tens, teen, addzero.Concat(digits), addzero.Repeat(2))),
		// 百一 百二
		hundred_digit,
	)

	// 二千百 二千三百 二千百一 九千百二十三 九千二十三 九千二十 九千二
	thousand := pynini.Union(
		pynini.Union(hundred, teen, tens, digits).Concat(lib.DeleteString("千")).Concat(
			pynini.Union(hundred, addzero.Concat(tens), addzero.Concat(teen), addzero.Repeat(2).Concat(digits), addzero.Repeat(3)),
		),
		//  千百 千三百 千百一 千百二十三 千二十三 千二十 千二
		pynini.Cross("千", "1").Concat(pynini.Union(hundred, addzero.Concat(tens), addzero.Concat(teen), addzero.Repeat(2).Concat(digits), addzero.Repeat(3))),
	)

	// 一万 二万二 二万二千百 二万二千三百 一万二千百一 一万九千百二十三 一万九千二十三 九万九千二十 四万九千二
	var ten_thousand *pynini.Fst
	if c.enable_million {
		ten_thousand = pynini.Union(thousand, hundred, teen, tens, digits).Concat(lib.DeleteString("万")).Concat(
			pynini.Union(
				thousand,
				addzero.Concat(hundred),
				addzero.Repeat(2).Concat(tens),
				addzero.Repeat(2).Concat(teen),
				addzero.Repeat(3).Concat(digits),
				addzero.Repeat(4),
			),
		)
	} else {
		// 二万 十万 三十四万
		ten_thousand = pynini.Union(teen, tens, digits).Concat(lib.DeleteString("万")).Concat(
			pynini.Union(
				thousand,
				addzero.Concat(hundred),
				addzero.Repeat(2).Concat(tens),
				addzero.Repeat(2).Concat(teen),
				addzero.Repeat(3).Concat(digits),
				addzero.Repeat(4),
			),
		)
		// 三百四十万 三千四百万
		ten_thousand = pynini.Union(
			ten_thousand,
			pynini.Union(thousand, hundred).Concat(pynini.Accep("万")).Concat(
				pynini.Union(thousand, hundred, tens, teen, digits).Ques(),
			),
		)
	}

	// 0~9999
	ten_thousand_minus := pynini.Union(digits, teen, tens, hundred, thousand)
	c.TenThousandMinus = ten_thousand_minus

	// 0~99999999
	positive_integer := pynini.Union(ten_thousand_minus, ten_thousand)

	// ±0~99999999
	number := sign.Ques().Concat(positive_integer).Optimize()
	c.Number = number
	c.NumberExclude_0_to_9 = pynini.Union(teen, tens, hundred, thousand, ten_thousand)

	// ±0.0~99999999.99...
	decimal := sign.Ques().Concat(positive_integer).Concat(dot).Concat(digits.Plus())
	c.Decimal = decimal
	// % like -27.00%
	percent := pynini.Union(number, decimal).Concat(pynini.Cross("パーセント", "%"))

	// ±100,000,000~∞  e.g. 一兆三百二十万五千 => 1兆320万5000
	big_integer := sign.Ques().Concat(pynini.Union(
		// (ten_thousand_minus + "兆").ques + ten_thousand_minus + "億" + ten_thousand_minus + "万".ques + ten_thousand_minus.ques
		ten_thousand_minus.Concat(pynini.Accep("兆")).Ques().Concat(
			ten_thousand_minus).Concat(pynini.Accep("億")).Concat(
			ten_thousand_minus).Concat(pynini.Accep("万").Ques()).Concat(
			ten_thousand_minus.Ques(),
		),
		// ten_thousand_minus + "兆" + (ten_thousand_minus + "億").ques + ten_thousand_minus + "万".ques + ten_thousand_minus.ques
		ten_thousand_minus.Concat(pynini.Accep("兆")).Concat(
			ten_thousand_minus.Concat(pynini.Accep("億")).Ques()).Concat(
			ten_thousand_minus).Concat(pynini.Accep("万").Ques()).Concat(
			ten_thousand_minus.Ques(),
		),
	))
	c.BigInteger = pynini.Union(number, big_integer)

	// cardinal string like 127.0.0.1, used in ID, IP, etc.
	cardinal := digits.Plus().Concat(dot.Concat(digits.Plus()).Plus())
	// float number like 1.11
	cardinal = pynini.Union(cardinal, decimal)
	// cardinal string like 110 or 12306 or 13125617878, used in phone
	cardinal = pynini.Union(cardinal, digits.Repeat(2).Concat(digits.Plus()))
	// % like -27.00%
	cardinal = pynini.Union(cardinal, percent)

	// allow convert standalone number
	if c.enable_standalone_number {
		if c.enable_0_to_9 {
			// 一 => 1    四 => 4    一秒 => 1秒   一万二 => 12000 二十三 => 23
			cardinal = pynini.Union(cardinal, number, big_integer)
		} else {
			// 一 => 一   四 => 四   一秒 => 1秒   一万二 => 一万二 二三 => 23
			number_two_plus := sign.Ques().Concat(pynini.Union(
				digits.Concat(digits.Plus()),
				teen,
				tens,
				hundred,
				thousand,
				ten_thousand,
				big_integer,
			))
			cardinal = pynini.Union(cardinal, number_two_plus)
		}
	}

	tagger := lib.Insert("value: \"").Concat(cardinal).Concat(lib.Insert("\""))
	c.Tagger = c.AddTokens(tagger).Optimize()
}

func (c *Cardinal) BuildVerbalizer() {
	c.Processor.BuildVerbalizer()
}
