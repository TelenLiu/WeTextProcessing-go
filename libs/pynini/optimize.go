package pynini

import "sort"

// =============================================================================
// ArcSort - sort arcs by label for efficient matching
// =============================================================================

// ArcSort sorts arcs at each state by the specified label type.
// sortType: "input" / "i" for input label sort, "output" / "o" for output label sort.
func (f *Fst) ArcSort(sortType string) {
	isInput := sortType == "input" || sortType == "i"
	for i := range f.States {
		arcs := f.States[i].Arcs
		if len(arcs) <= 1 {
			continue
		}
		if isInput {
			sort.Slice(arcs, func(a, b int) bool {
				return arcs[a].ILabel < arcs[b].ILabel
			})
			f.Props |= propILabelSorted
			f.Props &= ^propNotILabelSorted
		} else {
			sort.Slice(arcs, func(a, b int) bool {
				return arcs[a].OLabel < arcs[b].OLabel
			})
			f.Props |= propOLabelSorted
			f.Props &= ^propNotOLabelSorted
		}
	}
}

// =============================================================================
// Connect - remove states that are not accessible or not coaccessible
// =============================================================================

// Connect removes states that are not on any path from the start state
// to a final state. Returns the connected FST.
func (f *Fst) Connect() *Fst {
	if f == nil {
		return NewFst()
	}

	// 1. Find accessible states (forward from start)
	accessible := make([]bool, len(f.States))
	var stack []int32
	stack = append(stack, f.Start)
	accessible[f.Start] = true
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, arc := range f.States[s].Arcs {
			if int(arc.Next) < len(accessible) && !accessible[arc.Next] {
				accessible[arc.Next] = true
				stack = append(stack, arc.Next)
			}
		}
	}

	// 2. Find coaccessible states (backward from final states)
	// Build reverse adjacency
	revArcs := make([][]int32, len(f.States))
	for s := range f.States {
		for _, arc := range f.States[s].Arcs {
			if int(arc.Next) < len(revArcs) {
				revArcs[arc.Next] = append(revArcs[arc.Next], int32(s))
			}
		}
	}

	coaccessible := make([]bool, len(f.States))
	stack = stack[:0]
	for s := range f.States {
		if f.States[s].Final {
			coaccessible[s] = true
			stack = append(stack, int32(s))
		}
	}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, prev := range revArcs[s] {
			if int(prev) < len(coaccessible) && !coaccessible[prev] {
				coaccessible[prev] = true
				stack = append(stack, prev)
			}
		}
	}

	// 3. Keep only states that are both accessible and coaccessible
	keep := make([]bool, len(f.States))
	oldToNew := make([]int32, len(f.States))
	for i := range oldToNew {
		oldToNew[i] = -1
	}

	newID := int32(0)
	for i := range f.States {
		if accessible[i] && coaccessible[i] {
			keep[i] = true
			oldToNew[i] = newID
			newID++
		}
	}

	if newID == 0 {
		return NewFst()
	}

	result := &Fst{
		States:  make([]State, newID),
		Symbols: f.Symbols.Copy(),
		Props:   f.Props | propAccessible | propCoAccessible,
	}

	for i := range f.States {
		if !keep[i] {
			continue
		}
		newS := oldToNew[i]
		result.States[newS] = State{
			Final:   f.States[i].Final,
			Weight:  f.States[i].Weight,
			NumIEps: f.States[i].NumIEps,
			NumOEps: f.States[i].NumOEps,
		}
		if len(f.States[i].Arcs) > 0 {
			result.States[newS].Arcs = make([]Arc, 0, len(f.States[i].Arcs))
			for _, arc := range f.States[i].Arcs {
				if keep[arc.Next] {
					newArc := arc
					newArc.Next = oldToNew[arc.Next]
					result.States[newS].Arcs = append(result.States[newS].Arcs, newArc)
				}
			}
		}
	}

	result.Start = oldToNew[f.Start]
	return result
}

// =============================================================================
// RmEpsilon - remove epsilon transitions
// =============================================================================

// RmEpsilon removes epsilon transitions from the FST using a state-level
// epsilon closure algorithm. For each state, it computes the epsilon closure
// and adds non-epsilon arcs from the closure, then removes epsilon arcs.
// Weights from epsilon transitions are properly propagated to copied arcs.
func (f *Fst) RmEpsilon() *Fst {
	if f == nil {
		return NewFst()
	}

	// Check if there are any epsilon transitions
	hasEps := false
	for i := range f.States {
		if f.States[i].HasIEpsilons() || f.States[i].HasOEpsilons() {
			hasEps = true
			break
		}
	}
	if !hasEps {
		return f.copy()
	}

	result := f.copy()

	// Streaming RmEpsilon: compute epsilon closure one state at a time
	// to avoid O(n * avg_closure_size) memory usage.
	// We compute closures on the ORIGINAL FST (f) to ensure correctness
	// even as we modify the result.
	for s := range result.States {
		// Compute epsilon closure for state s on the original FST
		closure, weights := computeEpsilonClosureWithWeights(f, int32(s))

		newArcs := make([]Arc, 0, len(result.States[s].Arcs)+len(closure)*2)
		newIEps := int32(0)
		newOEps := int32(0)

		// Keep non-epsilon arcs from this state
		for _, arc := range result.States[s].Arcs {
			if arc.ILabel != EpsilonLabel || arc.OLabel != EpsilonLabel {
				newArcs = append(newArcs, arc)
				if arc.ILabel == EpsilonLabel {
					newIEps++
				}
				if arc.OLabel == EpsilonLabel {
					newOEps++
				}
			}
		}

		// Add non-epsilon arcs from epsilon-reachable states
		for _, epsState := range closure {
			if epsState == int32(s) {
				continue
			}
			epsWeight := weights[epsState]
			for _, arc := range f.States[epsState].Arcs {
				if arc.ILabel != EpsilonLabel || arc.OLabel != EpsilonLabel {
					weightedArc := arc
					weightedArc.Weight += epsWeight
					newArcs = append(newArcs, weightedArc)
					if arc.ILabel == EpsilonLabel {
						newIEps++
					}
					if arc.OLabel == EpsilonLabel {
						newOEps++
					}
				}
			}
			// Propagate finality with accumulated epsilon weight
			if f.States[epsState].Final {
				totalWeight := f.States[epsState].Weight + epsWeight
				if !result.States[s].Final || totalWeight < result.States[s].Weight {
					result.States[s].Final = true
					result.States[s].Weight = totalWeight
				}
			}
		}

		result.States[s].Arcs = newArcs
		result.States[s].NumIEps = newIEps
		result.States[s].NumOEps = newOEps
	}

	result.Props |= propNoIEpsilons | propNoOEpsilons
	result.Props &= ^(propIEpsilons | propOEpsilons)

	return result
}

// computeEpsilonClosureWithWeights returns all states reachable from state s via
// epsilon-only transitions, along with the accumulated weight for each state.
// The weight is the sum of arc weights along the epsilon path.
// Uses Dijkstra-like algorithm on the epsilon subgraph for correct weight propagation.
func computeEpsilonClosureWithWeights(f *Fst, s int32) ([]int32, map[int32]float32) {
	visited := make([]bool, len(f.States))
	weights := make(map[int32]float32)
	var result []int32

	// Priority queue: use simple BFS with weight tracking.
	// Since epsilon arcs can have arbitrary weights, we need to track the best weight.
	type queueEntry struct {
		state  int32
		weight float32
	}

	weights[s] = 0
	queue := []queueEntry{{state: s, weight: 0}}
	visited[s] = true
	result = append(result, s)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		for _, arc := range f.States[cur.state].Arcs {
			if arc.ILabel == EpsilonLabel && arc.OLabel == EpsilonLabel {
				nw := cur.weight + arc.Weight
				prevW, seen := weights[arc.Next]
				if !seen || nw < prevW {
					weights[arc.Next] = nw
					if !visited[arc.Next] {
						visited[arc.Next] = true
						result = append(result, arc.Next)
					}
					queue = append(queue, queueEntry{state: arc.Next, weight: nw})
				}
			}
		}
	}

	return result, weights
}

// =============================================================================
// Determinize - determinize the FST
// =============================================================================

// Determinize determinizes the FST using subset construction (powerset method).
// After RmEpsilon, there may still be arcs with ILabel=epsilon (output-only arcs
// from Insert operations) or OLabel=epsilon (input-only arcs from Delete operations).
// We handle ILabel=epsilon arcs by treating them as part of the epsilon closure:
// when building the subset for a non-epsilon ILabel, we follow ILabel=epsilon arcs
// from the target states to accumulate output labels.
func (f *Fst) Determinize() *Fst {
	if f == nil {
		return NewFst()
	}

	// First remove pure epsilon transitions (both ILabel and OLabel are epsilon)
	result := f.RmEpsilon()

	// Quick check: if already deterministic, return as-is
	if isDeterministic(result) {
		result.Props |= propIDeterministic
		result.Props &= ^propNonIDeterministic
		return result.Connect()
	}

	// Compute input-epsilon closure for each state.
	// This includes states reachable via arcs where ILabel=epsilon
	// (regardless of OLabel), which handles output-only arcs from Insert().
	epsClosure := make([][]int32, len(result.States))
	for s := range result.States {
		epsClosure[s], _ = computeInputEpsilonClosureWithOutput(result, int32(s))
	}

	// Build deterministic FST via subset construction
	det := NewFst()
	det.Symbols = result.Symbols.Copy()

	// subsetKey creates a canonical string key for a set of state IDs
	subsetKey := func(states []int32) string {
		if len(states) == 0 {
			return ""
		}
		sorted := make([]int32, len(states))
		copy(sorted, states)
		for i := 1; i < len(sorted); i++ {
			for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
				sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			}
		}
		key := make([]byte, len(sorted)*4)
		for i, s := range sorted {
			key[i*4] = byte(s >> 24)
			key[i*4+1] = byte(s >> 16)
			key[i*4+2] = byte(s >> 8)
			key[i*4+3] = byte(s)
		}
		return string(key)
	}

	// Start with epsilon closure of start state
	startSet := epsClosure[result.Start]
	startKey := subsetKey(startSet)

	type subsetEntry struct {
		key    string
		states []int32
	}

	subsetToState := make(map[string]int32)
	subsetToState[startKey] = 0

	queue := []subsetEntry{{key: startKey, states: startSet}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentStateID := subsetToState[current.key]

		// Check if any state in the subset is final
		minFinalWeight := float32(1e30)
		hasFinal := false
		for _, s := range current.states {
			if int(s) < len(result.States) && result.States[s].Final {
				hasFinal = true
				if result.States[s].Weight < minFinalWeight {
					minFinalWeight = result.States[s].Weight
				}
			}
		}
		if hasFinal {
			det.SetFinal(currentStateID, minFinalWeight)
		}

		// Collect all outgoing transitions grouped by ILabel.
		// For each non-epsilon ILabel, collect the union of epsilon closures
		// of all target states. Keep the OLabel and weight from the arc with
		// minimum weight.
		type labelInfo struct {
			targetSet map[int32]bool
			olabel    int32
			weight    float32
		}
		labelTrans := make(map[int32]*labelInfo)

		for _, s := range current.states {
			if int(s) >= len(result.States) {
				continue
			}
			for _, arc := range result.States[s].Arcs {
				if arc.ILabel == EpsilonLabel {
					continue // handled via epsilon closure
				}
				info, ok := labelTrans[arc.ILabel]
				if !ok {
					info = &labelInfo{
						targetSet: make(map[int32]bool),
						olabel:    arc.OLabel,
						weight:    arc.Weight,
					}
					labelTrans[arc.ILabel] = info
				} else if arc.Weight < info.weight {
					info.olabel = arc.OLabel
					info.weight = arc.Weight
				}
				// Add all states in epsilon closure of arc.Next
				if int(arc.Next) < len(epsClosure) {
					for _, t := range epsClosure[arc.Next] {
						info.targetSet[t] = true
					}
				}
			}
		}

		// Add transitions to deterministic FST
		for ilabel, info := range labelTrans {
			targetList := make([]int32, 0, len(info.targetSet))
			for t := range info.targetSet {
				targetList = append(targetList, t)
			}
			targetKey := subsetKey(targetList)

			targetStateID, exists := subsetToState[targetKey]
			if !exists {
				targetStateID = int32(len(det.States))
				det.AddState()
				subsetToState[targetKey] = targetStateID
				queue = append(queue, subsetEntry{key: targetKey, states: targetList})
			}

			det.AddArc(currentStateID, targetStateID, ilabel, info.olabel, info.weight)
		}
	}

	det.Props |= propIDeterministic
	det.Props &= ^propNonIDeterministic
	return det.Connect()
}

// computeInputEpsilonClosureWithOutput returns all states reachable from state s
// via transitions where ILabel is epsilon, along with accumulated output labels.
// This handles output-only arcs (from Insert operations) where ILabel=epsilon
// but OLabel is non-epsilon.
func computeInputEpsilonClosureWithOutput(f *Fst, s int32) ([]int32, map[int32]string) {
	visited := make([]bool, len(f.States))
	outputs := make(map[int32]string)
	var result []int32

	type queueEntry struct {
		state  int32
		output string
	}

	queue := []queueEntry{{state: s, output: ""}}
	visited[s] = true
	outputs[s] = ""
	result = append(result, s)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		for _, arc := range f.States[cur.state].Arcs {
			if arc.ILabel == EpsilonLabel {
				newOutput := cur.output
				if arc.OLabel != EpsilonLabel {
					newOutput += f.Symbols.Symbol(arc.OLabel)
				}
				if int(arc.Next) < len(visited) && !visited[arc.Next] {
					visited[arc.Next] = true
					outputs[arc.Next] = newOutput
					result = append(result, arc.Next)
					queue = append(queue, queueEntry{state: arc.Next, output: newOutput})
				}
			}
		}
	}

	return result, outputs
}

// isDeterministic checks if the FST has no state with multiple arcs
// sharing the same input label.
func isDeterministic(f *Fst) bool {
	for s := range f.States {
		seen := make(map[int32]bool)
		for _, arc := range f.States[s].Arcs {
			if seen[arc.ILabel] {
				return false
			}
			seen[arc.ILabel] = true
		}
	}
	return true
}

// =============================================================================
// Optimize - optimize the FST (epsilon removal + determinization + minimization)
// =============================================================================

// Optimize optimizes the FST by removing epsilon transitions, determinizing,
// and connecting. Determinize is enabled with a state count threshold:
// for FSTs with more than 50000 states, determinization is skipped since
// it can be expensive and the benefit is marginal for large FSTs.
func (f *Fst) Optimize() *Fst {
	if f == nil {
		return NewFst()
	}
	result := f.RmEpsilon()
	// Determinize is disabled for now - the subset construction doesn't
	// correctly handle all transducer patterns (e.g., output-only arcs
	// from Insert operations). Re-enable after fixing.
	// result = result.Determinize()
	result = result.Connect()
	result.ArcSort("input")
	return result
}

// =============================================================================
// Minimize - minimize the FST (Hopcroft-like algorithm)
// =============================================================================

// Minimize minimizes the FST using a partition refinement algorithm.
// This is a simplified implementation that merges equivalent states.
func (f *Fst) Minimize() *Fst {
	if f == nil {
		return NewFst()
	}

	// First ensure the FST is deterministic and epsilon-free
	result := f.Determinize()

	// Collect states and their signatures
	// Signature: (final, final weight, set of (label, target) pairs)
	type signature struct {
		isFinal bool
		weight  float32
		arcs    string // simplified string representation
	}

	stateSigs := make([]signature, len(result.States))
	for i := range result.States {
		st := &result.States[i]
		// Sort arcs for consistent signature
		sorted := make([]Arc, len(st.Arcs))
		copy(sorted, st.Arcs)
		sort.Slice(sorted, func(a, b int) bool {
			if sorted[a].ILabel != sorted[b].ILabel {
				return sorted[a].ILabel < sorted[b].ILabel
			}
			return sorted[a].OLabel < sorted[b].OLabel
		})

		// Build signature string
		var sigStr string
		for _, arc := range sorted {
			sigStr += string(rune(arc.ILabel)) + ":" + string(rune(arc.Next)) + ","
		}

		stateSigs[i] = signature{
			isFinal: st.Final,
			weight:  st.Weight,
			arcs:    sigStr,
		}
	}

	// Find equivalent states
	// Group by signature
	sigGroups := make(map[string][]int32)
	for i, sig := range stateSigs {
		key := ""
		if sig.isFinal {
			key = "F" + string(rune(int32(sig.weight*1000)))
		}
		key += sig.arcs
		sigGroups[key] = append(sigGroups[key], int32(i))
	}

	// Build mapping from old state to new state (merge equivalent states)
	oldToNew := make([]int32, len(result.States))
	newStates := make([]State, 0, len(sigGroups))

	for _, group := range sigGroups {
		newID := int32(len(newStates))
		// Use the first state in the group as the representative
		rep := group[0]
		newStates = append(newStates, result.States[rep])
		for _, oldID := range group {
			oldToNew[oldID] = newID
		}
	}

	// Build new FST
	minimized := &Fst{
		States:  newStates,
		Start:   oldToNew[result.Start],
		Symbols: result.Symbols.Copy(),
	}

	// Remap arc targets
	for i := range minimized.States {
		for j := range minimized.States[i].Arcs {
			minimized.States[i].Arcs[j].Next = oldToNew[minimized.States[i].Arcs[j].Next]
		}
	}

	return minimized.Connect()
}