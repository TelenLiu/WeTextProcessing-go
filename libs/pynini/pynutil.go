package pynini

func Insert(s interface{}, weight ...float64) *Fst {
	var w float64
	hasWeight := len(weight) > 0
	if hasWeight {
		w = weight[0]
	}

	var result *Fst
	switch val := s.(type) {
	case string:
		result = Cross("", val)
	case *Fst:
		// Cross("", expr) means epsilon x expr: input=epsilon, output=expr
		result = epsilonInsert(val)
	default:
		result = NewFst()
	}

	if hasWeight {
		return AddWeight(result, w)
	}
	return result
}

func Delete(s interface{}, weight ...float64) *Fst {
	var w float64
	hasWeight := len(weight) > 0
	if hasWeight {
		w = weight[0]
	}

	var result *Fst
	switch val := s.(type) {
	case string:
		result = Cross(val, "")
	case *Fst:
		// Cross(expr, "") means expr x epsilon: input=expr, output=epsilon
		result = epsilonDelete(val)
	default:
		result = NewFst()
	}

	if hasWeight {
		return AddWeight(result, w)
	}
	return result
}

func DeleteString(s string, weight ...float64) *Fst {
	return Delete(s, weight...)
}

func AddWeight(fst *Fst, weight float64) *Fst {
	if fst == nil {
		return nil
	}
	// Python does: pynini.accep("", weight=weight).concat(expr)
	// This creates a single-state FST with the weight, then concatenates.
	// The concat adds epsilon arcs from the weighted state to expr's start.
	// So we create a weighted start state and epsilon-bridge to expr.
	result := NewFst()
	result.AddState(0)

	// Bridge from new start to original start with the weight
	result.AddArc(0, fst.Start+1, "", "", weight)

	// Copy original FST with state offset of 1
	for from, state := range fst.States {
		result.AddState(from + 1)
		for _, arc := range state.Arcs {
			result.AddArc(from+1, arc.Next+1, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if state.Final {
			result.SetFinal(from+1, state.Weight)
		}
	}

	return result
}

func Join(expr *Fst, sep *Fst) *Fst {
	if expr == nil || sep == nil {
		return NewFst()
	}
	// Python: cdr = (sep + expr).closure(); return expr + cdr
	cdr := sep.Concat(expr).Star()
	return expr.Concat(cdr)
}

// epsilonInsert creates epsilon x expr: input=epsilon, output=expr's output
func epsilonInsert(f *Fst) *Fst {
	result := NewFst()
	for from, state := range f.States {
		result.AddState(from)
		for _, arc := range state.Arcs {
			result.AddArc(from, arc.Next, "", arc.OLabel, arc.Weight)
		}
		if state.Final {
			result.SetFinal(from, state.Weight)
		}
	}
	return result
}

// epsilonDelete creates expr x epsilon: input=expr's input, output=epsilon
func epsilonDelete(f *Fst) *Fst {
	result := NewFst()
	for from, state := range f.States {
		result.AddState(from)
		for _, arc := range state.Arcs {
			result.AddArc(from, arc.Next, arc.ILabel, "", arc.Weight)
		}
		if state.Final {
			result.SetFinal(from, state.Weight)
		}
	}
	return result
}
