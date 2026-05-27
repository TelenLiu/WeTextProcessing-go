package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Money struct {
	*tn.Processor
	enable_0_to_9 bool
}

func NewMoney(enable_0_to_9 bool) *Money {
	m := &Money{
		Processor:   tn.NewProcessor("money"),
		enable_0_to_9: enable_0_to_9,
	}
	m.BuildTagger()
	m.BuildVerbalizer()
	return m
}

func (m *Money) BuildTagger() {
	code, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/money/code.tsv"))
	symbol, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/money/symbol.tsv"))
	digit, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/digit.tsv"))

	cardinal := NewCardinal(false, m.enable_0_to_9, false)
	var number *pynini.Fst
	if m.enable_0_to_9 {
		number = cardinal.Number
	} else {
		number = cardinal.NumberExclude_0_to_9
	}
	// 七八美元 => $7~8
	number = pynini.Union(number, digit.Concat(lib.Insert("~")).Concat(digit))

	// 三千三百八十元五毛八分 => ¥3380.58
	tagger := lib.Insert("value: \"").
		Concat(number).
		Concat(lib.Insert("\"")).
		Concat(lib.Insert(" currency: \"")).
		Concat(pynini.Union(code, symbol)).
		Concat(lib.Insert("\"")).
		Concat(lib.Insert(" decimal: \"")).
		Concat(lib.Insert(".").Concat(digit).Concat(pynini.Union(lib.DeleteString("毛"), lib.DeleteString("角"))).
			Concat(digit.Concat(lib.DeleteString("分")).Ques()).Ques()).
		Concat(lib.Insert("\""))

	m.Tagger = m.AddTokens(tagger)
}

func (m *Money) BuildVerbalizer() {
	currency := lib.DeleteString("currency: \"").Concat(m.SIGMA).Concat(lib.DeleteString("\""))
	value := lib.DeleteString(" value: \"").Concat(m.SIGMA).Concat(lib.DeleteString("\""))
	decimal := lib.DeleteString(" decimal: \"").Concat(m.SIGMA).Concat(lib.DeleteString("\""))
	verbalizer := currency.Concat(value).Concat(decimal)
	m.Verbalizer = m.DeleteTokens(verbalizer)
}
