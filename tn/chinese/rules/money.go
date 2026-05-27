package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Money struct {
	*tn.Processor
}

func NewMoney() *Money {
	m := &Money{
		Processor: tn.NewProcessor("money"),
	}
	m.BuildTagger()
	m.BuildVerbalizer()
	return m
}

func (m *Money) BuildTagger() {
	code, _ := pynini.StringFile(tn.ChineseDataPath("data/money/code.tsv"))
	symbol, _ := pynini.StringFile(tn.ChineseDataPath("data/money/symbol.tsv"))

	number := NewCardinal().Number
	tagger := lib.Insert("currency: \"").
		Concat(code.Union(symbol)).
		Concat(lib.DeleteString(" ").Ques()).
		Concat(lib.Insert("\" ")).
		Concat(lib.Insert("value: \"")).
		Concat(number).
		Concat(lib.Insert("\""))
	m.Tagger = m.AddTokens(tagger)
}

func (m *Money) BuildVerbalizer() {
	value := lib.DeleteString("value: \"").Concat(m.SIGMA).Concat(lib.DeleteString("\" "))
	currency := lib.DeleteString("currency: \"").Concat(m.SIGMA).Concat(lib.DeleteString("\""))
	verbalizer := value.Concat(currency)
	m.Verbalizer = m.DeleteTokens(verbalizer)
}
