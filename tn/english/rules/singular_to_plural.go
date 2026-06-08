package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

func SingularToPlural() *pynini.Fst {
	suppletive, _ := pynini.StringFile(tn.EnglishDataPath("data/suppletive.tsv"))
	c := pynini.Union(
		pynini.Accep("b"), pynini.Accep("c"), pynini.Accep("d"), pynini.Accep("f"),
		pynini.Accep("g"), pynini.Accep("h"), pynini.Accep("j"), pynini.Accep("k"),
		pynini.Accep("l"), pynini.Accep("m"), pynini.Accep("n"), pynini.Accep("p"),
		pynini.Accep("q"), pynini.Accep("r"), pynini.Accep("s"), pynini.Accep("t"),
		pynini.Accep("v"), pynini.Accep("w"), pynini.Accep("x"), pynini.Accep("y"),
		pynini.Accep("z"),
	)

	tmp := tn.NewProcessor("tmp")
	ies := tmp.VCHAR.Star().Concat(c).Concat(pynini.Cross("y", "ies"))
	es := tmp.VCHAR.Star().Concat(pynini.Union(
		pynini.Accep("s"), pynini.Accep("sh"), pynini.Accep("ch"),
		pynini.Accep("x"), pynini.Accep("z"),
	)).Concat(lib.Insert("es"))
	s := tmp.VCHAR.Star().Concat(lib.Insert("s"))

	graphPlural := pynini.Union(suppletive, ies, es, s).Optimize()
	return graphPlural
}
