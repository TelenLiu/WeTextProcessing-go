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
	symbol, _ := pynini.StringFile(tn.ITNJapaneseDataPath("data/money/symbol.tsv"))

	cardinal := NewCardinal(false, m.enable_0_to_9, false)
	number := cardinal.Number
	decimal := cardinal.Decimal

	tagger := lib.Insert("value: \"").Concat(pynini.Union(number, decimal)).Concat(lib.Insert("\"")).Concat(
		lib.Insert(" currency: \"")).Concat(symbol).Concat(lib.Insert("\""))
	m.Tagger = m.AddTokens(tagger)
}

func (m *Money) BuildVerbalizer() {
	currency := lib.DeleteString("currency: \"").Concat(m.SIGMA).Concat(lib.DeleteString("\""))
	value := lib.DeleteString(" value: \"").Concat(m.SIGMA).Concat(lib.DeleteString("\""))
	verbalizer := currency.Concat(value)
	m.Verbalizer = m.DeleteTokens(verbalizer)
}
