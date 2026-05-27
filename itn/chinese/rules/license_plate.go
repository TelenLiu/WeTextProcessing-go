package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type LicensePlate struct {
	*tn.Processor
}

func NewLicensePlate() *LicensePlate {
	l := &LicensePlate{
		Processor: tn.NewProcessor("licenseplate"),
	}
	l.BuildTagger()
	l.BuildVerbalizer()
	return l
}

func (l *LicensePlate) BuildTagger() {
	digit, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/digit.tsv"))
	zero, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/zero.tsv"))
	digits := pynini.Union(zero, digit)
	province, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/license_plate/province.tsv"))

	// license_plate = province + ALPHA + (ALPHA | digits) ** 5
	// license_plate |= province + ALPHA + (ALPHA | digits) ** 6
	alnum := pynini.Union(l.ALPHA, digits)

	license_plate := province.Concat(l.ALPHA).Concat(alnum.Repeat(5))
	license_plate = pynini.Union(license_plate, province.Concat(l.ALPHA).Concat(alnum.Repeat(6)))

	tagger := lib.Insert("value: \"").Concat(license_plate).Concat(lib.Insert("\""))
	l.Tagger = l.AddTokens(tagger)
}

func (l *LicensePlate) BuildVerbalizer() {
	l.Processor.BuildVerbalizer()
}
