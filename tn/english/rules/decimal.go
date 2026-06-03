package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Decimal struct {
	*tn.Processor
	Graph                             *pynini.Fst
	GraphFractional                   *pynini.Fst
	GraphInteger                      *pynini.Fst
	FinalGraphWoNegativeWAbbr         *pynini.Fst
	FinalGraphWoNegative              *pynini.Fst
	Numbers                           *pynini.Fst
	OptionalSign                      *pynini.Fst
	Integer                           *pynini.Fst
	OptionalInteger                   *pynini.Fst
	Fractional                        *pynini.Fst
	FractionalDefault                 *pynini.Fst
	Quantity                          *pynini.Fst
	OptionalQuantity                  *pynini.Fst
	deterministic                     bool
}

func NewDecimal(args ...bool) *Decimal {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	d := &Decimal{
		Processor:   tn.NewProcessor("decimal", "en_tn"),
		deterministic: deterministic,
	}
	d.BuildTagger()
	d.BuildVerbalizer()
	return d
}

func (d *Decimal) BuildTagger() {
	cardinal := NewCardinal(d.deterministic)
	cardinalGraph := cardinal.GraphWithAnd
	cardinalGraphHundred := cardinal.GraphHundredComponentAtLeastOneNoneZeroDigit

	d.Graph = cardinal.SingleDigitsGraph.Optimize()

	if !d.deterministic {
		d.Graph = d.Graph.Union(lib.AddWeight(cardinalGraph, 0.1))
	}

	point := lib.DeleteString(".")
	optionalGraphNegative := lib.Insert("negative: ").Concat(
		pynini.Cross("-", "\"true\" "),
	).Ques()

	d.GraphFractional = lib.Insert("fractional_part: \"").Concat(d.Graph).Concat(lib.Insert("\""))
	d.GraphInteger = lib.Insert("integer_part: \"").Concat(cardinalGraph).Concat(lib.Insert("\""))

	finalGraphWoSign := d.GraphInteger.Concat(
		lib.Insert(" ")).Ques().Concat(
		point).Concat(
		lib.Insert(" ")).Concat(
		d.GraphFractional)

	quantityWAbbr := d.getQuantity(finalGraphWoSign, cardinalGraphHundred, true)
	quantityWoAbbr := d.getQuantity(finalGraphWoSign, cardinalGraphHundred, false)
	d.FinalGraphWoNegativeWAbbr = finalGraphWoSign.Union(quantityWAbbr)
	d.FinalGraphWoNegative = finalGraphWoSign.Union(quantityWoAbbr)

	if !d.deterministic {
		// Python uses Difference(VCHAR.Star(), ...) to prevent mixing "oh" and "zero"
		// in the same output. Our Difference only handles simple character unions,
		// so we skip this constraint. The core decimal functionality still works;
		// only the "oh"/"zero" consistency refinement is omitted.
		// Also skip the Cdrewrite for "zero" -> "oh" in integer_part since it
		// requires Compose with VCHAR.Star() which causes state explosion.
	}

	finalGraph := optionalGraphNegative.Concat(d.FinalGraphWoNegative)
	d.Tagger = d.AddTokens(finalGraph)
}

func (d *Decimal) getQuantity(decimal, cardinalUpToHundred *pynini.Fst, includeAbbr bool) *pynini.Fst {
	quantities, _ := pynini.StringFile(tn.EnglishDataPath("data/number/thousand.tsv"))
	quantitiesAbbr, _ := pynini.StringFile(tn.EnglishDataPath("data/number/quantity_abbr.tsv"))

	tmpProcessor := tn.NewProcessor("tmp")
	quantitiesAbbr = quantitiesAbbr.Union(tmpProcessor.TO_UPPER.Compose(quantitiesAbbr))

	quantityWoThousand := pynini.Project(quantities, "input").Difference(
		pynini.Union(pynini.Accep("k"), pynini.Accep("K"), pynini.Accep("thousand")),
	)
	if includeAbbr {
		quantityWoThousand = quantityWoThousand.Union(
			pynini.Project(quantitiesAbbr, "input").Difference(
				pynini.Union(pynini.Accep("k"), pynini.Accep("K"), pynini.Accep("thousand")),
			),
		)
	}

	res := lib.Insert("integer_part: \"").Concat(
		cardinalUpToHundred).Concat(lib.Insert("\"")).Concat(
		lib.DeleteString(" ").Ques()).Concat(
		lib.Insert(" quantity: \"")).Concat(
		quantityWoThousand.Compose(
			pynini.Union(quantities, quantitiesAbbr),
		)).Concat(lib.Insert("\""))

	var quantity *pynini.Fst
	if includeAbbr {
		quantity = quantities.Union(quantitiesAbbr)
	} else {
		quantity = quantities
	}
	res = res.Union(
		decimal.Concat(lib.DeleteString(" ").Ques()).Concat(
			lib.Insert(" quantity: \"")).Concat(quantity).Concat(lib.Insert("\"")),
	)
	return res
}

func (d *Decimal) BuildVerbalizer() {
	cardinal := NewCardinal(d.deterministic)
	d.OptionalSign = pynini.Cross("negative: \"true\"", "minus ")
	if !d.deterministic {
		d.OptionalSign = d.OptionalSign.Union(
			lib.AddWeight(pynini.Cross("negative: \"true\"", "negative "), 0.1),
		)
	}
	d.OptionalSign = d.OptionalSign.Concat(d.DELETE_SPACE).Ques()

	d.Integer = lib.DeleteString("integer_part:").Concat(
		cardinal.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(
		cardinal.NOT_QUOTE.Star()).Concat(lib.DeleteString("\""))
	d.OptionalInteger = d.Integer.Concat(d.DELETE_SPACE).Concat(d.INSERT_SPACE).Ques()

	d.FractionalDefault = lib.DeleteString("fractional_part:").Concat(
		d.DELETE_SPACE).Concat(lib.DeleteString("\"")).Concat(
		d.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))

	d.Fractional = lib.Insert("point ").Concat(d.FractionalDefault)

	d.Quantity = d.DELETE_SPACE.Concat(d.INSERT_SPACE).Concat(
		lib.DeleteString("quantity:")).Concat(d.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(d.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))
	d.OptionalQuantity = d.Quantity.Ques()

	graph := d.OptionalSign.Concat(
		d.Integer.Union(
			d.Integer.Concat(d.Quantity)).Union(
			d.OptionalInteger.Concat(d.Fractional).Concat(d.OptionalQuantity)),
	)

	d.Numbers = graph
	deleteTokens := d.DeleteTokens(graph)

	if !d.deterministic {
		// Python uses Compose with VCHAR.Star() to add "half"/"quarter" verbalizations.
		// This causes state explosion in our implementation, so we skip it.
		// The core decimal verbalization still works correctly.
	}
	d.Verbalizer = deleteTokens.Optimize()
}
