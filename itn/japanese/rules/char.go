package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Char struct {
	*tn.Processor
}

func NewChar() *Char {
	c := &Char{
		Processor: tn.NewProcessor("char"),
	}
	c.BuildTagger()
	c.BuildVerbalizer()
	return c
}

func (c *Char) BuildTagger() {
	tagger := lib.Insert("value: \"").Concat(c.CHAR).Concat(lib.Insert("\""))
	c.Tagger = c.AddTokens(tagger)
}

func (c *Char) BuildVerbalizer() {
	c.Processor.BuildVerbalizer()
}
