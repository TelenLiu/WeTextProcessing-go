package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Punctuation struct {
	*tn.Processor
	Graph         *pynini.Fst
	Punct         *pynini.Fst
	Emphasis      *pynini.Fst
	deterministic bool
}

func NewPunctuation(args ...bool) *Punctuation {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	p := &Punctuation{
		Processor:   tn.NewProcessor("p", "en_tn"),
		deterministic: deterministic,
	}
	p.BuildTagger()
	p.BuildVerbalizer()
	return p
}

func (p *Punctuation) BuildTagger() {
	s := "!#%&'()*+,-./:;<=>?@^_`{|}~"
	var punctMarks []*pynini.Fst
	for _, c := range s {
		punctMarks = append(punctMarks, pynini.Accep(string(c)))
	}

	// Add unicode punctuation (simplified - common ones)
	unicodePunct := []string{
		"–", "—", "―", "‖", "‗", "‘", "’", "‛", "″", "‴", "‵", "‶", "‷", "‸",
		"‹", "›", "※", "‽", "⁇", "⁈", "⁉", "⁊", "⁋", "⁌", "⁍", "⁎", "⁏",
		"⁐", "⁑", "⁓", "⁔", "⁕", "⁖", "⁗", "⁘", "⁙", "⁚", "⁛", "⁜", "⁝", "⁞",
	}
	for _, c := range unicodePunct {
		punctMarks = append(punctMarks, pynini.Accep(c))
	}

	p.Punct = pynini.Union(punctMarks...)
	punct := p.Punct.Union(pynini.Cross("\\", "\\\\\\\\")).Union(pynini.Cross("\"", "\\\"")).Plus()

	// Emphasis: <...>, </...>, etc.
	p.Emphasis = pynini.Accep("<").Concat(
		pynini.Union(
			p.NOT_SPACE.Difference(pynini.Union(pynini.Accep("<"), pynini.Accep(">"))).Plus().Concat(
				pynini.Accep("/").Ques()),
			pynini.Accep("/").Concat(
				p.NOT_SPACE.Difference(pynini.Union(pynini.Accep("<"), pynini.Accep(">"))).Plus()),
		),
	).Concat(pynini.Accep(">"))

	punct = pynini.Union(p.Emphasis, punct)

	p.Graph = punct
	finalGraph := lib.Insert("v: \"").Concat(
		lib.AddWeight(pynini.Accep(" "), -1.0).Star()).Concat(
		punct).Concat(lib.AddWeight(pynini.Accep(" "), -1.0).Star()).Concat(
		lib.Insert("\""))
	p.Tagger = p.AddTokens(finalGraph)
}

func (p *Punctuation) BuildVerbalizer() {
	punct := p.Punct.Union(p.Emphasis).Union(pynini.Cross("\\\\\\\\", "\\")).Union(pynini.Cross("\\\"", "\"")).Plus()
	verbalizer := lib.DeleteString("v: \"").Concat(
		lib.AddWeight(pynini.Accep(" "), -1.0).Star()).Concat(
		punct).Concat(lib.AddWeight(pynini.Accep(" "), -1.0).Star()).Concat(
		lib.DeleteString("\""))
	p.Verbalizer = p.DeleteTokens(verbalizer)
}
