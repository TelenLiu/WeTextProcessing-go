package rules

import (
	"strings"

	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

const (
	INPUT_CASED       = "cased"
	INPUT_LOWER_CASED = "lower_cased"
)

type Whitelist struct {
	*tn.Processor
	Graph         *pynini.Fst
	deterministic bool
	inputCase     string
}

func NewWhitelist(args ...bool) *Whitelist {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	w := &Whitelist{
		Processor:   tn.NewProcessor("whitelist", "en_tn"),
		deterministic: deterministic,
		inputCase:   INPUT_CASED,
	}
	w.BuildTagger()
	w.BuildVerbalizer()
	return w
}

func (w *Whitelist) BuildTagger() {
	graph := w.getWhitelistGraph(w.inputCase, tn.EnglishDataPath("data/whitelist/tts.tsv"), true)

	graph = graph.Union(
		w.VCHAR.Star().Difference(pynini.Accep("/")).Optimize().Compose(
			w.getWhitelistGraph(w.inputCase, tn.EnglishDataPath("data/whitelist/symbol.tsv"), false),
		).Optimize(),
	)

	if w.deterministic {
		names := getNames()
		graph = graph.Union(
			pynini.Cross(pynini.Union(pynini.Accep("st"), pynini.Accep("St"), pynini.Accep("ST")), "Saint").Concat(
				lib.DeleteString(".").Star()).Concat(
				pynini.Accep(" ")).Concat(names),
		)
	} else {
		graph = graph.Union(
			w.getWhitelistGraph(w.inputCase, tn.EnglishDataPath("data/whitelist/alternatives.tsv"), true),
		)
	}

	// Matching Python: self.UPPER + pynini.closure(pynutil.delete(x) + self.UPPER, 2) + pynutil.delete(".").ques
	// This matches abbreviations like "U.S.A.", "Ph.D." (at least 3 uppercase letters)
	// Python: closure(F, 2) = F{2,} = at least 2 repetitions
	for _, x := range []string{".", ". "} {
		abbrevPart := lib.DeleteString(x).Concat(w.UPPER) // delete(x) + UPPER
		graph = graph.Union(
			w.UPPER.Concat(
				abbrevPart.Repeat(2).Concat(abbrevPart.Star()), // (delete(x) + UPPER){2,}
			).Concat(
				lib.DeleteString(".").Ques(),
			),
		)
	}

	stateGraph := w.getStatesGraph()
	graph = graph.Union(
		w.ALPHA.Plus().Concat(
			pynini.Union(pynini.Accep(", "), pynini.Accep(","))).Concat(
			stateGraph.Invert().Optimize()),
	)

	w.Graph = graph.Optimize()
	finalGraph := lib.Insert("name: \"").Concat(w.Graph).Concat(lib.Insert("\"")).Optimize()
	w.Tagger = w.AddTokens(finalGraph)
}

func (w *Whitelist) getWhitelistGraph(inputCase, file string, keepPunctAddEnd bool) *pynini.Fst {
	labels, _ := tn.LoadLabels(file)
	if inputCase == INPUT_LOWER_CASED {
		for i, label := range labels {
			if len(label) >= 2 {
				labels[i][0] = strings.ToLower(label[0])
			}
		}
	}
	if keepPunctAddEnd {
		labels = append(labels, tn.AugmentLabelsWithPunctAtEnd(labels)...)
	}
	return pynini.StringMap(labels)
}

func (w *Whitelist) getStatesGraph() *pynini.Fst {
	states, _ := tn.LoadLabels(tn.EnglishDataPath("data/address/state.tsv"))
	additionalOptions := make([][]string, 0)
	for _, xy := range states {
		if len(xy) >= 2 {
			x, y := xy[0], xy[1]
			if w.inputCase == INPUT_LOWER_CASED {
				x = strings.ToLower(x)
			}
			additionalOptions = append(additionalOptions, []string{x, string(y[0]) + "." + y[1:]})
			if !w.deterministic {
				additionalOptions = append(additionalOptions, []string{x, string(y[0]) + "." + y[1:] + "."})
			}
		}
	}
	states = append(states, additionalOptions...)
	return pynini.StringMap(states)
}

func (w *Whitelist) BuildVerbalizer() {
	graph := lib.DeleteString("name:").Concat(w.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(w.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))
	finalGraph := graph.Optimize()
	w.Verbalizer = w.DeleteTokens(finalGraph)
}

func GetNames() *pynini.Fst {
	maleLabels, _ := tn.LoadLabels(tn.EnglishDataPath("data/roman/male.tsv"))
	femaleLabels, _ := tn.LoadLabels(tn.EnglishDataPath("data/roman/female.tsv"))
	for _, x := range maleLabels {
		if len(x) >= 1 {
			upper := strings.ToUpper(x[0])
			maleLabels = append(maleLabels, []string{upper})
		}
	}
	for _, x := range femaleLabels {
		if len(x) >= 1 {
			upper := strings.ToUpper(x[0])
			femaleLabels = append(femaleLabels, []string{upper})
		}
	}
	names := pynini.StringMap(maleLabels).Optimize()
	names = names.Union(pynini.StringMap(femaleLabels).Optimize())
	return names
}

func getNames() *pynini.Fst {
	return GetNames()
}
