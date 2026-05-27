package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Word struct {
	*tn.Processor
	Char          *pynini.Fst
	deterministic bool
}

func NewWord(args ...bool) *Word {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	w := &Word{
		Processor:   tn.NewProcessor("w", "en_tn"),
		deterministic: deterministic,
	}
	w.BuildTagger()
	w.BuildVerbalizer()
	return w
}

func (w *Word) BuildTagger() {
	punct := NewPunctuation(w.deterministic).Graph
	defaultGraph := w.NOT_SPACE.Difference(pynini.Project(punct, "input"))
	symbolsToExclude := pynini.Union(
		pynini.Accep("$"), pynini.Accep("€"), pynini.Accep("₩"),
		pynini.Accep("£"), pynini.Accep("¥"), pynini.Accep("#"),
		pynini.Accep("%"),
	).Union(w.DIGIT)
	w.Char = defaultGraph.Difference(symbolsToExclude)

	graph := lib.Insert("v: \"").Concat(w.Char.Plus()).Concat(lib.Insert("\"")).Optimize()
	finalGraph := w.AddTokens(graph)
	w.Tagger = finalGraph.Optimize()
}

func (w *Word) BuildVerbalizer() {
	graph := lib.DeleteString("v: ").Concat(lib.DeleteString("\"")).Concat(
		w.Char.Plus()).Concat(lib.DeleteString("\""))
	finalGraph := w.DeleteTokens(graph)
	w.Verbalizer = finalGraph.Optimize()
}
