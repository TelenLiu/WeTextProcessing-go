package pynini

// Insert creates an epsilon x expr FST (input=epsilon, output=expr).
func Insert(s interface{}, weight ...float64) *Fst {
	var w float32
	if len(weight) > 0 {
		w = float32(weight[0])
	}

	var result *Fst
	switch val := s.(type) {
	case string:
		result = Cross("", val)
	case *Fst:
		result = epsilonInsert(val)
	default:
		result = NewFst()
	}

	if len(weight) > 0 {
		return AddWeight(result, float64(w))
	}
	return result
}

// Delete creates an expr x epsilon FST (input=expr, output=epsilon).
func Delete(s interface{}, weight ...float64) *Fst {
	var w float32
	if len(weight) > 0 {
		w = float32(weight[0])
	}

	var result *Fst
	switch val := s.(type) {
	case string:
		result = Cross(val, "")
	case *Fst:
		result = epsilonDelete(val)
	default:
		result = NewFst()
	}

	if len(weight) > 0 {
		return AddWeight(result, float64(w))
	}
	return result
}

// DeleteString is a convenience wrapper for Delete with a string.
func DeleteString(s string, weight ...float64) *Fst {
	return Delete(s, weight...)
}

// AddWeight adds a weight to an FST by creating a weighted start state and
// epsilon-bridging to the original start.
func AddWeight(fst *Fst, weight float64) *Fst {
	if fst == nil {
		return nil
	}
	w := float32(weight)
	result := NewFst()
	// Merge fst's symbols
	result.Symbols.Merge(fst.Symbols)
	result.AddState() // state 0 is the weighted start
	// Bridge from new start to original start with the weight
	result.AddArc(0, fst.Start+1, EpsilonLabel, EpsilonLabel, w)

	// Copy original FST with state offset of 1.
	// Use ensureState to avoid duplicating states already created by AddArc.
	for i := range fst.States {
		from := int32(i) + 1
		ensureState(result, from)
		for _, arc := range fst.States[i].Arcs {
			result.AddArc(from, arc.Next+1, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if fst.States[i].Final {
			result.SetFinal(from, fst.States[i].Weight)
		}
	}

	return result
}

// Join creates (expr . (sep . expr)*) which is expr separated by sep.
func Join(expr *Fst, sep *Fst) *Fst {
	if expr == nil || sep == nil {
		return NewFst()
	}
	cdr := sep.Concat(expr).Star()
	return expr.Concat(cdr)
}

// epsilonInsert creates epsilon x expr: input=epsilon, output=expr's output.
func epsilonInsert(f *Fst) *Fst {
	result := f.copy()
	for i := range result.States {
		for j := range result.States[i].Arcs {
			// Track epsilon changes
			if result.States[i].Arcs[j].ILabel != EpsilonLabel {
				result.States[i].NumIEps++
			}
			result.States[i].Arcs[j].ILabel = EpsilonLabel
		}
	}
	return result
}

// epsilonDelete creates expr x epsilon: input=expr's input, output=epsilon.
func epsilonDelete(f *Fst) *Fst {
	result := f.copy()
	for i := range result.States {
		for j := range result.States[i].Arcs {
			// Track epsilon changes
			if result.States[i].Arcs[j].OLabel != EpsilonLabel {
				result.States[i].NumOEps++
			}
			result.States[i].Arcs[j].OLabel = EpsilonLabel
		}
	}
	return result
}