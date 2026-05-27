package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Measure struct {
	*tn.Processor
}

func NewMeasure() *Measure {
	m := &Measure{
		Processor: tn.NewProcessor("measure"),
	}
	m.BuildTagger()
	m.BuildVerbalizer()
	return m
}

func (m *Measure) BuildTagger() {
	units_en, _ := pynini.StringFile(tn.ChineseDataPath("data/measure/units_en.tsv"))
	units_zh, _ := pynini.StringFile(tn.ChineseDataPath("data/measure/units_zh.tsv"))
	units := lib.AddWeight(
		pynini.Union(pynini.Cross("k", "千"), pynini.Cross("w", "万")), 0.1,
	).Ques().Concat(pynini.Union(units_en, units_zh))
	rmspace := lib.DeleteString(" ").Ques()
	to := pynini.Union(
		pynini.Cross("-", "到"),
		pynini.Cross("~", "到"),
		pynini.Accep("到"),
	)

	// Build measure-specific number with 二→两 rewrite for standalone numbers
	// Python: number = Cardinal().number; number @= self.build_rule(cross("二", "两"), "[BOS]", "[EOS]")
	// Since we can't use cdrewrite, create number variants for different contexts
	baseNumber := NewCardinal().Number
	
	// For the second number in measure (before units), use "两" for standalone "2"
	// But we need to avoid "两两" when unit is "两", so we'll handle this via Union patterns
	numberWithTwo := lib.AddWeight(pynini.Cross("2", "两"), -0.1)
	number := pynini.Union(baseNumber, numberWithTwo)

	// Create a version of number without the "两" rewrite (for special units like "两", "月", "号")
	numberNoTwo := baseNumber

	// 1-11个, 1个-11个
	prefix := number.Concat(rmspace.Concat(units).Ques()).Concat(to)
	prefixNoTwo := numberNoTwo.Concat(rmspace.Concat(units).Ques()).Concat(to)
	
	// Pattern for units other than "两", "月", "号" - use "两" for standalone "2"
	measure := prefix.Ques().Concat(number).Concat(rmspace).Concat(units)
	
	// Special pattern for "两", "月", "号" units - use "二" for "2" to avoid "两两", "两月", "两号"
	// Give these patterns lower weight to prefer them over the general pattern
	for _, unit := range []string{"两", "月", "号"} {
		unitFst := pynini.Accep(unit)
		specialMeasure := prefixNoTwo.Ques().Concat(numberNoTwo).Concat(rmspace).Concat(unitFst)
		specialMeasure = lib.AddWeight(specialMeasure, -0.3)
		measure = pynini.Union(measure, specialMeasure)
		
		// Also handle "到两"+unit → "到二"+unit pattern
		toTwoUnit := lib.AddWeight(pynini.Cross("到两"+unit, "到二"+unit), -0.3)
		measure = pynini.Union(measure, toTwoUnit)
	}

	// -xxxx年, -xx年
	digits := NewCardinal().Digits()
	cardinal := digits.Repeat(2).Union(digits.Repeat(4))
	unit_fst := pynini.Union(pynini.Accep("年"), pynini.Accep("年度"), pynini.Accep("赛季"))
	prefix = cardinal.Concat(rmspace.Concat(unit_fst).Ques()).Concat(to)
	annual := prefix.Ques().Concat(cardinal).Concat(unit_fst)

	tagger := lib.Insert("value: \"").Concat(pynini.Union(measure, annual)).Concat(lib.Insert("\""))

	// 10km/h
	rmsign := rmspace.Concat(lib.DeleteString("/")).Concat(rmspace)
	tagger = tagger.Union(
		lib.Insert("numerator: \"").Concat(measure).Concat(rmsign).Concat(lib.Insert("\" denominator: \"")).Concat(units).Concat(lib.Insert("\"")),
	)
	m.Tagger = m.AddTokens(tagger)
}

func (m *Measure) BuildVerbalizer() {
	m.Processor.BuildVerbalizer()
	denominator := lib.DeleteString("denominator: \"").Concat(m.SIGMA).Concat(lib.DeleteString("\" "))
	numerator := lib.DeleteString("numerator: \"").Concat(m.SIGMA).Concat(lib.DeleteString("\""))
	verbalizer := lib.Insert("每").Concat(denominator).Concat(numerator)
	m.Verbalizer = m.Verbalizer.Union(m.DeleteTokens(verbalizer))
}
