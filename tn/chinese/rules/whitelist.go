package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Whitelist struct {
	*tn.Processor
	removeErhua bool
}

func NewWhitelist(remove_erhua bool) *Whitelist {
	w := &Whitelist{
		Processor:   tn.NewProcessor("whitelist"),
		removeErhua: remove_erhua,
	}
	w.BuildTagger()
	w.BuildVerbalizer()
	return w
}

// NewWhitelistEmpty creates a Whitelist with a Processor but without building FSTs.
// Used when loading pre-built FSTs from cache.
func NewWhitelistEmpty(remove_erhua bool) *Whitelist {
	return &Whitelist{
		Processor:   tn.NewProcessor("whitelist"),
		removeErhua: remove_erhua,
	}
}

func (w *Whitelist) BuildTagger() {
	whitelist, _ := pynini.StringFile(tn.ChineseDataPath("data/default/whitelist.tsv"))
	erhua_whitelist, _ := pynini.StringFile(tn.ChineseDataPath("data/erhua/whitelist.tsv"))
	whitelist = whitelist.Union(erhua_whitelist)

	erhua := lib.AddWeight(lib.Insert("erhua: \"").Concat(pynini.Accep("儿")), 0.1)
	tagger := erhua.Union(lib.Insert("value: \"").Concat(whitelist)).Concat(lib.Insert("\""))
	w.Tagger = w.AddTokens(tagger)
}

func (w *Whitelist) BuildVerbalizer() {
	w.Processor.BuildVerbalizer()
	var verbalizer *pynini.Fst
	if w.removeErhua {
		verbalizer = w.DeleteTokens(lib.DeleteString("erhua: \"儿\""))
	} else {
		verbalizer = w.DeleteTokens(lib.DeleteString("erhua: \"").Concat(pynini.Accep("儿")).Concat(lib.DeleteString("\"")))
	}
	w.Verbalizer = w.Verbalizer.Union(verbalizer)
}
