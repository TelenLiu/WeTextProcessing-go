package lib

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
)

func Insert(s string) *pynini.Fst {
	return pynini.Cross("", s)
}

func Delete(s *pynini.Fst) *pynini.Fst {
	return pynini.Cross(s, "")
}

func DeleteString(s string) *pynini.Fst {
	return pynini.Cross(s, "")
}

func AddWeight(fst *pynini.Fst, weight float64) *pynini.Fst {
	return pynini.AddWeight(fst, weight)
}