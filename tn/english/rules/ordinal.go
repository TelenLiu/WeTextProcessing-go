package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Ordinal struct {
	*tn.Processor
	Graph       *pynini.Fst
	GraphV      *pynini.Fst
	Suffix      *pynini.Fst
	deterministic bool
}

func NewOrdinal(args ...bool) *Ordinal {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	o := &Ordinal{
		Processor:   tn.NewProcessor("ordinal", "en_tn"),
		deterministic: deterministic,
	}
	o.BuildTagger()
	o.BuildVerbalizer()
	return o
}

func (o *Ordinal) BuildTagger() {
	cardinal := NewCardinal(o.deterministic)
	cardinalGraph := cardinal.Graph
	cardinalFormat := pynini.Union(o.DIGIT, lib.DeleteString(",")).Star()

	// st: ends with 1 (but not 11)
	stFormat := cardinalFormat.Concat(
		o.DIGIT.Difference(pynini.Accep("1")),
	).Ques().Concat(
		pynini.Accep("1"),
	).Concat(
		pynutilDeleteUnion("st", "ST", "ˢᵗ"),
	)

	// nd: ends with 2 (but not 12)
	ndFormat := cardinalFormat.Concat(
		o.DIGIT.Difference(pynini.Accep("1")),
	).Ques().Concat(
		pynini.Accep("2"),
	).Concat(
		pynutilDeleteUnion("nd", "ND", "ⁿᵈ"),
	)

	// rd: ends with 3 (but not 13)
	rdFormat := cardinalFormat.Concat(
		o.DIGIT.Difference(pynini.Accep("1")),
	).Ques().Concat(
		pynini.Accep("3"),
	).Concat(
		pynutilDeleteUnion("rd", "RD", "ʳᵈ"),
	)

	// th: everything else
	thFormat := pynini.Union(
		o.DIGIT.Difference(pynini.Accep("1")).Difference(pynini.Accep("2")).Difference(pynini.Accep("3")),
		cardinalFormat.Concat(pynini.Accep("1")).Concat(o.DIGIT),
		cardinalFormat.Concat(o.DIGIT.Difference(pynini.Accep("1"))).Concat(o.DIGIT.Difference(pynini.Accep("1")).Difference(pynini.Accep("2")).Difference(pynini.Accep("3"))),
	).Plus().Concat(
		pynutilDeleteUnion("th", "TH", "ᵗʰ"),
	)

	o.Graph = pynini.Union(stFormat, ndFormat, rdFormat, thFormat).Compose(cardinalGraph)
	finalGraph := lib.Insert("integer: \"").Concat(o.Graph).Concat(lib.Insert("\""))
	o.Tagger = o.AddTokens(finalGraph)
}

func (o *Ordinal) BuildVerbalizer() {
	graphDigit, _ := pynini.StringFile(tn.EnglishDataPath("data/ordinal/digit.tsv"))
	graphTeens, _ := pynini.StringFile(tn.EnglishDataPath("data/ordinal/teen.tsv"))
	graphDigit = graphDigit.Invert()
	graphTeens = graphTeens.Invert()

	graph := lib.DeleteString("integer:").Concat(
		o.DELETE_SPACE).Concat(
		lib.DeleteString("\"").Concat(
			o.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\"")),
	)

	convertRest := lib.Insert("th")

	suffix := pynini.Union(
		graphDigit,
		graphTeens,
		pynini.Cross("ty", "tieth"),
		convertRest,
	)
	suffix = o.BuildRule(suffix, "", "").Optimize()

	o.GraphV = graph.Compose(suffix)
	o.Suffix = suffix
	deleteTokens := o.DeleteTokens(o.GraphV)
	o.Verbalizer = deleteTokens.Optimize()
}

func pynutilDeleteUnion(args ...string) *pynini.Fst {
	var fsts []*pynini.Fst
	for _, s := range args {
		fsts = append(fsts, lib.DeleteString(s))
	}
	return pynini.Union(fsts...)
}
