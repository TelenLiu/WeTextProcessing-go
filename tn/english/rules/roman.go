package rules

import (
	"strings"

	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Roman struct {
	*tn.Processor
	deterministic bool
}

func NewRoman(args ...bool) *Roman {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	r := &Roman{
		Processor:   tn.NewProcessor("roman", "en_tn"),
		deterministic: deterministic,
	}
	r.BuildTagger()
	r.BuildVerbalizer()
	return r
}

func (r *Roman) BuildTagger() {
	romanDict, _ := tn.LoadLabels(tn.EnglishDataPath("data/roman/roman_to_spoken.tsv"))
	defaultGraph := pynini.StringMap(romanDict).Optimize()
	defaultGraph = lib.Insert("integer: \"").Concat(defaultGraph).Concat(lib.Insert("\""))
	ordinalLimit := 19

	startIdx := 1
	if !r.deterministic {
		startIdx = 0
	}

	// Build teen roman keys
	var teenKeys []string
	for i := startIdx; i < ordinalLimit && i < len(romanDict); i++ {
		if len(romanDict[i]) >= 1 {
			teenKeys = append(teenKeys, romanDict[i][0])
		}
	}
	graphTeens := pynini.StringMap(stringsToPairs(teenKeys)).Optimize()

	// Name + teen roman -> ordinal form
	names := getNames()
	graph := lib.Insert("key_the_ordinal: \"").Concat(names).Concat(lib.Insert("\"")).Concat(
		pynini.Accep(" ")).Concat(graphTeens.Compose(defaultGraph)).Optimize()

	// Key words + roman -> cardinal form
	keyWords := make([][]string, 0)
	if kw, _ := tn.LoadLabels(tn.EnglishDataPath("data/roman/key_word.tsv")); kw != nil {
		for _, kwLabel := range kw {
			if len(kwLabel) >= 1 {
				kwStr := kwLabel[0]
				keyWords = append(keyWords, []string{kwStr})
				if len(kwStr) > 0 {
					keyWords = append(keyWords, []string{strings.ToUpper(kwStr[:1]) + kwStr[1:]})
					keyWords = append(keyWords, []string{strings.ToUpper(kwStr)})
				}
			}
		}
	}
	keyWordsGraph := pynini.StringMap(keyWords).Optimize()
	graph = graph.Union(
		lib.Insert("key_cardinal: \"").Concat(keyWordsGraph).Concat(lib.Insert("\"")).Concat(
			pynini.Accep(" ")).Concat(defaultGraph)).Optimize()

	if r.deterministic {
		// Two digit roman up to 49
		var romanKeys []string
		for i := 0; i < 50 && i < len(romanDict); i++ {
			if len(romanDict[i]) >= 1 {
				romanKeys = append(romanKeys, romanDict[i][0])
			}
		}
		romanToCardinal := r.ALPHA.Repeat(2).Compose(
			lib.Insert("default_cardinal: \"default\" ").Concat(
				pynini.StringMap(stringsToPairs(romanKeys)).Optimize().Compose(defaultGraph)),
		)
		graph = graph.Union(romanToCardinal)
	} else {
		// Two or more digit roman numerals
		romanToCardinal := r.VCHAR.Star().Difference(pynini.Accep("I")).Compose(
			lib.Insert("default_cardinal: \"default\" integer: \"").Concat(
				pynini.StringMap(romanDict).Optimize()).Concat(lib.Insert("\"")),
		).Optimize()
		graph = graph.Union(romanToCardinal)
	}

	// Three digit+ roman with suffix -> ordinal
	romanToOrdinal := r.ALPHA.Repeat(3).Compose(
		lib.Insert("default_ordinal: \"default\" ").Concat(
			graphTeens.Compose(defaultGraph)).Concat(lib.DeleteString("th")),
	)
	graph = graph.Union(romanToOrdinal)

	graph = r.AddTokens(graph.Optimize())
	r.Tagger = graph.Optimize()
}

func stringsToPairs(keys []string) [][]string {
	var pairs [][]string
	for _, k := range keys {
		pairs = append(pairs, []string{k, k})
	}
	return pairs
}

func (r *Roman) BuildVerbalizer() {
	ordinal := NewOrdinal(r.deterministic)
	suffix := ordinal.Suffix

	cardinal := r.NOT_QUOTE.Star()
	ordinalGraph := cardinal.Compose(suffix)

	graph := lib.DeleteString("key_cardinal: \"").Concat(
		r.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\"")).Concat(
		pynini.Accep(" ")).Concat(
		lib.DeleteString("integer: \"")).Concat(
		cardinal).Concat(lib.DeleteString("\"")).Optimize()

	graph = graph.Union(
		lib.DeleteString("default_cardinal: \"default\" integer: \"").Concat(
			cardinal).Concat(lib.DeleteString("\"")).Optimize())

	graph = graph.Union(
		lib.DeleteString("default_ordinal: \"default\" integer: \"").Concat(
			ordinalGraph).Concat(lib.DeleteString("\"")).Optimize())

	graph = graph.Union(
		lib.DeleteString("key_the_ordinal: \"").Concat(
			r.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\"")).Concat(
			pynini.Accep(" ")).Concat(
			lib.DeleteString("integer: \"")).Concat(
			lib.Insert("the ").Ques()).Concat(
			ordinalGraph).Concat(lib.DeleteString("\"")).Optimize())

	deleteTokens := r.DeleteTokens(graph)
	r.Verbalizer = deleteTokens.Optimize()
}
