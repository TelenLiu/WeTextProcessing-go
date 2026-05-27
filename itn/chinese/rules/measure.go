package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Measure struct {
	*tn.Processor
	exclude_one  bool
	enable_0_to_9 bool
}

func NewMeasure(exclude_one, enable_0_to_9 bool) *Measure {
	m := &Measure{
		Processor:   tn.NewProcessor("measure"),
		exclude_one:  exclude_one,
		enable_0_to_9: enable_0_to_9,
	}
	m.BuildTagger()
	m.BuildVerbalizer()
	return m
}

func (m *Measure) BuildTagger() {
	units_en, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/measure/units_en.tsv"))
	units_zh, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/measure/units_zh.tsv"))
	sign, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/sign.tsv"))
	digit, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/digit.tsv"))
	digit_zh, _ := pynini.StringFile(tn.ITNChineseDataPath("../../itn/chinese/data/number/digit_zh.tsv"))
	addzero := lib.Insert("0")
	to := pynini.Cross("到", "~").Union(pynini.Cross("到百分之", "~"))

	// units = add_weight((accep("亿") | accep("兆") | accep("万")), -0.5).ques + units_zh
	units := lib.AddWeight(pynini.Union(pynini.Accep("亿"), pynini.Accep("兆"), pynini.Accep("万")), -0.5).Ques().Concat(units_zh)
	// units |= add_weight((cross("亿", "00M") | cross("兆", "T") | cross("万", "W")), -0.5).ques + add_weight(units_en, -1.0)
	units = pynini.Union(
		units,
		lib.AddWeight(pynini.Union(pynini.Cross("亿", "00M"), pynini.Cross("兆", "T"), pynini.Cross("万", "W")), -0.5).Ques().
			Concat(lib.AddWeight(units_en, -1.0)),
	)

	cardinal := NewCardinal(false, m.enable_0_to_9, false)
	var number *pynini.Fst
	if m.enable_0_to_9 {
		number = cardinal.Number
	} else {
		number = cardinal.NumberExclude_0_to_9
	}

	// percent = (sign + delete("的").ques).ques + delete("百分") + delete("之").ques + ...
	percent := sign.Concat(lib.DeleteString("的").Ques()).Ques().
		Concat(lib.DeleteString("百分")).
		Concat(lib.DeleteString("之").Ques()).
		Concat(pynini.Union(
			cardinal.Number.Concat(
				to.Concat(cardinal.Number).Ques(),
			),
			pynini.Union(
				pynini.Accep(""),
				cardinal.Number.Concat(to),
			).Ques().Concat(pynini.Cross("百", "100")),
		)).
		Concat(lib.Insert("%"))

	// measure = number + (to + number).ques + units
	measure := number.Concat(to.Concat(number).Ques()).Concat(units)

	// Special case: digit + 百/千/万 + digit + unit
	unit_sp_case1 := pynini.Union(
		pynini.Accep("年"), pynini.Accep("月"), pynini.Accep("个月"), pynini.Accep("周"),
		pynini.Accep("天"), pynini.Accep("位"), pynini.Accep("次"), pynini.Accep("个"),
		pynini.Accep("顿"),
	)

	var measure_sp *pynini.Fst
	hundred_digit := digit.Concat(lib.DeleteString("百")).Concat(lib.AddWeight(addzero.Repeat(2), 1.0))
	thousand_digit := digit.Concat(lib.DeleteString("千")).Concat(lib.AddWeight(addzero.Repeat(3), 1.0))
	wan_digit := digit.Concat(lib.DeleteString("万")).Concat(lib.AddWeight(addzero.Repeat(4), 1.0))
	hundred_thousand_wan := pynini.Union(hundred_digit, thousand_digit, wan_digit)

	if m.enable_0_to_9 {
		measure_sp = lib.AddWeight(hundred_thousand_wan.Concat(lib.Insert(" ")).Concat(digit).Concat(unit_sp_case1), -0.5)
	} else {
		measure_sp = lib.AddWeight(hundred_thousand_wan.Concat(digit_zh).Concat(unit_sp_case1), -0.5)
	}

	tagger := lib.Insert("value: \"").Concat(pynini.Union(measure, measure_sp, percent)).Concat(lib.Insert("\""))

	// denominator: "每" + units + numerator: measure
	tagger = pynini.Union(
		tagger,
		lib.Insert("denominator: \"").Concat(lib.DeleteString("每")).Concat(units).Concat(lib.Insert("\" numerator: \"")).Concat(measure).Concat(lib.Insert("\"")),
	)

	m.Tagger = m.AddTokens(tagger)
}

func (m *Measure) BuildVerbalizer() {
	m.Processor.BuildVerbalizer()
	numerator := lib.DeleteString("numerator: \"").Concat(m.SIGMA).Concat(lib.DeleteString("\""))
	denominator := lib.DeleteString(" denominator: \"").Concat(m.SIGMA).Concat(lib.DeleteString("\""))
	verbalizer := numerator.Concat(lib.Insert("/")).Concat(denominator)
	m.Verbalizer = pynini.Union(m.Verbalizer, m.DeleteTokens(verbalizer))
}
