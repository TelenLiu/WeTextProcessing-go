package pynini

import (
	"container/heap"
	"encoding/gob"
	"fmt"
	"os"
	"sort"
	"strings"
)

type State struct {
	ID     int
	Final  bool
	Weight float64
	Arcs   []*Arc
}

type Arc struct {
	ILabel string
	OLabel string
	Weight float64
	Next   int
}

type Fst struct {
	Start  int
	States map[int]*State
	Sigma  *Fst
}

func NewFst() *Fst {
	return &Fst{
		Start:  0,
		States: make(map[int]*State),
	}
}

func maxStateID(f *Fst) int {
	maxID := 0
	for id := range f.States {
		if id > maxID {
			maxID = id
		}
	}
	return maxID
}

func (f *Fst) AddState(id int) *State {
	if _, exists := f.States[id]; !exists {
		f.States[id] = &State{
			ID:     id,
			Final:  false,
			Weight: 0,
			Arcs:   []*Arc{},
		}
	}
	return f.States[id]
}

func (f *Fst) AddArc(from, to int, ilabel, olabel string, weight float64) {
	f.AddState(from)
	f.AddState(to)
	f.States[from].Arcs = append(f.States[from].Arcs, &Arc{
		ILabel: ilabel,
		OLabel: olabel,
		Weight: weight,
		Next:   to,
	})
}

func (f *Fst) SetFinal(state int, weight float64) {
	f.AddState(state)
	f.States[state].Final = true
	f.States[state].Weight = weight
}

func Accep(s string) *Fst {
	f := NewFst()
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		f.AddArc(i, i+1, string(runes[i]), string(runes[i]), 0)
	}
	f.SetFinal(len(runes), 0)
	return f
}

func Cross(a, b interface{}) *Fst {
	switch aVal := a.(type) {
	case string:
		if bVal, ok := b.(string); ok {
			f := NewFst()
			runesA := []rune(aVal)
			runesB := []rune(bVal)
			maxLen := len(runesA)
			if len(runesB) > maxLen {
				maxLen = len(runesB)
			}
			for i := 0; i < maxLen; i++ {
				var ilabel, olabel string
				if i < len(runesA) {
					ilabel = string(runesA[i])
				}
				if i < len(runesB) {
					olabel = string(runesB[i])
				}
				f.AddArc(i, i+1, ilabel, olabel, 0)
			}
			f.SetFinal(maxLen, 0)
			return f
		}
		if bVal, ok := b.(*Fst); ok {
			f := NewFst()
			runesA := []rune(aVal)
			if len(runesA) == 0 {
				return epsilonInsert(bVal)
			}
			for i := 0; i < len(runesA); i++ {
				f.AddArc(i, i+1, string(runesA[i]), "", 0)
			}
			stateOffset := len(runesA)
			for from, state := range bVal.States {
				for _, arc := range state.Arcs {
					f.AddArc(from+stateOffset, arc.Next+stateOffset, arc.ILabel, arc.OLabel, arc.Weight)
				}
				if state.Final {
					f.SetFinal(from+stateOffset, state.Weight)
				}
			}
			f.AddArc(len(runesA), bVal.Start+stateOffset, "", "", 0)
			return f
		}
	case *Fst:
		if bVal, ok := b.(*Fst); ok {
			return aVal.Concat(bVal)
		}
		if bStr, ok := b.(string); ok {
			f := NewFst()
			f.Start = aVal.Start
			for id, state := range aVal.States {
				f.AddState(id)
				if state.Final {
					f.SetFinal(id, state.Weight)
				}
				for _, arc := range state.Arcs {
					if bStr == "" {
						f.AddArc(id, arc.Next, arc.ILabel, "", arc.Weight)
					} else {
						f.AddArc(id, arc.Next, arc.ILabel, bStr, arc.Weight)
					}
				}
			}
			return f
		}
	}
	return NewFst()
}

func Union(fsts ...*Fst) *Fst {
	var validFsts []*Fst
	for _, fst := range fsts {
		if fst != nil {
			validFsts = append(validFsts, fst)
		}
	}
	if len(validFsts) == 0 {
		return NewFst()
	}
	if len(validFsts) == 1 {
		return validFsts[0]
	}
	result := NewFst()
	for _, fst := range validFsts {
		stateOffset := maxStateID(result) + 1
		for from, state := range fst.States {
			for _, arc := range state.Arcs {
				result.AddArc(from+stateOffset, arc.Next+stateOffset, arc.ILabel, arc.OLabel, arc.Weight)
			}
			if state.Final {
				result.SetFinal(from+stateOffset, state.Weight)
			}
		}
		result.AddArc(0, fst.Start+stateOffset, "", "", 0)
	}
	return result
}

func (f *Fst) Union(other *Fst) *Fst {
	return Union(f, other)
}

func (f *Fst) Concat(other *Fst) *Fst {
	if f == nil || other == nil {
		return NewFst()
	}
	result := NewFst()
	for from, state := range f.States {
		for _, arc := range state.Arcs {
			result.AddArc(from, arc.Next, arc.ILabel, arc.OLabel, arc.Weight)
		}
	}
	stateOffset := maxStateID(f) + 1
	for from, state := range f.States {
		if state.Final {
			result.AddArc(from, other.Start+stateOffset, "", "", state.Weight)
		}
	}
	for from, state := range other.States {
		for _, arc := range state.Arcs {
			result.AddArc(from+stateOffset, arc.Next+stateOffset, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if state.Final {
			result.SetFinal(from+stateOffset, state.Weight)
		}
	}
	return result
}

func (f *Fst) Plus(other ...*Fst) *Fst {
	if f == nil {
		return NewFst()
	}
	if len(other) > 0 {
		return f.Concat(other[0])
	}
	result := NewFst()
	result.Start = f.Start
	for from, state := range f.States {
		for _, arc := range state.Arcs {
			result.AddArc(from, arc.Next, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if state.Final {
			result.SetFinal(from, state.Weight)
			result.AddArc(from, f.Start, "", "", 0)
		}
	}
	return result
}

func (f *Fst) Star() *Fst {
	if f == nil {
		result := NewFst()
		result.SetFinal(0, 0)
		return result
	}
	result := NewFst()
	result.SetFinal(0, 0)
	stateOffset := maxStateID(result) + 1
	for from, state := range f.States {
		for _, arc := range state.Arcs {
			result.AddArc(from+stateOffset, arc.Next+stateOffset, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if state.Final {
			result.AddArc(from+stateOffset, 0, "", "", state.Weight)
		}
	}
	result.AddArc(0, f.Start+stateOffset, "", "", 0)
	return result
}

func (f *Fst) Ques() *Fst {
	return Union(f, Accep(""))
}

func (f *Fst) Repeat(n int) *Fst {
	if f == nil {
		return Accep("")
	}
	if n == 0 {
		return Accep("")
	}
	result := f
	for i := 1; i < n; i++ {
		result = result.Concat(f)
	}
	return result
}

func (f *Fst) Invert() *Fst {
	if f == nil {
		return NewFst()
	}
	result := NewFst()
	result.Start = f.Start
	for from, state := range f.States {
		for _, arc := range state.Arcs {
			result.AddArc(from, arc.Next, arc.OLabel, arc.ILabel, arc.Weight)
		}
		if state.Final {
			result.SetFinal(from, state.Weight)
		}
	}
	return result
}

// collectInputSymbols collects all unique input symbols from an FST
func collectInputSymbols(f *Fst) map[string]bool {
	symbols := make(map[string]bool)
	for _, state := range f.States {
		for _, arc := range state.Arcs {
			if arc.ILabel != "" {
				symbols[arc.ILabel] = true
			}
		}
	}
	return symbols
}

// determinize converts an FST to a deterministic FST using the subset construction.
// Returns a new FST where each state represents a set of original states.
func determinize(f *Fst) *Fst {
	if f == nil || len(f.States) == 0 {
		return NewFst()
	}

	result := NewFst()

	setKey := func(states []int) string {
		// Sort states to ensure consistent key generation
		sorted := make([]int, len(states))
		copy(sorted, states)
		sort.Ints(sorted)
		var parts []string
		for _, s := range sorted {
			parts = append(parts, fmt.Sprintf("%d", s))
		}
		return strings.Join(parts, ",")
	}

	// Start subset
	startSubset := []int{f.Start}
	startKey := setKey(startSubset)
	stateMap := make(map[string]int)
	stateMap[startKey] = 0
	result.AddState(0)

	queue := []string{startKey}

	// Map from key to the actual states
	keyToStates := make(map[string][]int)
	keyToStates[startKey] = startSubset

	for len(queue) > 0 {
		currentKey := queue[0]
		queue = queue[1:]
		currentID := stateMap[currentKey]
		currentStates := keyToStates[currentKey]

		// Collect all arcs from all states in this subset
		type arcInfo struct {
			label string
			nexts []int
		}
		labelMap := make(map[string][]int)

		for _, sid := range currentStates {
			state, ok := f.States[sid]
			if !ok {
				continue
			}
			for _, arc := range state.Arcs {
				if arc.ILabel != "" {
					labelMap[arc.ILabel] = append(labelMap[arc.ILabel], arc.Next)
				}
			}
			if state.Final {
				result.States[currentID].Final = true
				result.States[currentID].Weight = state.Weight
			}
		}

		// Create transitions for each unique label
		for label, nexts := range labelMap {
			// Deduplicate
			seen := make(map[int]bool)
			var uniqueNexts []int
			for _, n := range nexts {
				if !seen[n] {
					seen[n] = true
					uniqueNexts = append(uniqueNexts, n)
				}
			}

			newKey := setKey(uniqueNexts)
			newStateID, exists := stateMap[newKey]
			if !exists {
				newStateID = len(stateMap)
				stateMap[newKey] = newStateID
				result.AddState(newStateID)
				queue = append(queue, newKey)
				keyToStates[newKey] = uniqueNexts
			}
			result.AddArc(currentID, newStateID, label, label, 0)
		}
	}

	return result
}

// makeComplete adds a sink state and missing transitions to make the FST complete
func makeComplete(f *Fst, sigma map[string]bool) *Fst {
	result := NewFst()
	for from, state := range f.States {
		for _, arc := range state.Arcs {
			result.AddArc(from, arc.Next, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if state.Final {
			result.SetFinal(from, state.Weight)
		}
	}

	sinkState := maxStateID(result) + 1
	result.AddState(sinkState)

	// For each state, find missing transitions and add them to sink
	for stateID, state := range result.States {
		existingLabels := make(map[string]bool)
		for _, arc := range state.Arcs {
			if arc.ILabel != "" {
				existingLabels[arc.ILabel] = true
			}
		}
		for sym := range sigma {
			if !existingLabels[sym] {
				result.AddArc(stateID, sinkState, sym, sym, 1e10) // high weight for non-matching
			}
		}
	}

	// Sink state loops to itself for all symbols
	for sym := range sigma {
		result.AddArc(sinkState, sinkState, sym, sym, 1e10)
	}

	return result
}

// complement swaps final and non-final states (assumes complete DFA)
func complement(f *Fst) *Fst {
	result := NewFst()
	for from, state := range f.States {
		for _, arc := range state.Arcs {
			result.AddArc(from, arc.Next, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if !state.Final {
			result.SetFinal(from, 0)
		}
	}
	return result
}

// intersect computes the intersection of two FSTs with epsilon handling
func intersect(a, b *Fst) *Fst {
	if a == nil || b == nil {
		return NewFst()
	}

	result := NewFst()

	type statePair struct {
		s1, s2 int
	}

	stateMap := make(map[statePair]int)
	queue := []statePair{}

	startPair := statePair{a.Start, b.Start}
	stateMap[startPair] = 0
	queue = append(queue, startPair)
	result.AddState(0)

	// epsilonClosure computes states reachable via epsilon input arcs
	epsilonClosure := func(fst *Fst, stateID int) map[int]bool {
		closure := make(map[int]bool)
		closure[stateID] = true
		q := []int{stateID}
		for len(q) > 0 {
			sid := q[0]
			q = q[1:]
			st, ok := fst.States[sid]
			if !ok {
				continue
			}
			for _, arc := range st.Arcs {
				if arc.ILabel == "" && !closure[arc.Next] {
					closure[arc.Next] = true
					q = append(q, arc.Next)
				}
			}
		}
		return closure
	}

	// collectNonEpsilonArcs gets all non-epsilon arcs reachable via epsilon paths
	collectArcs := func(fst *Fst, stateID int) []*Arc {
		closure := epsilonClosure(fst, stateID)
		var arcs []*Arc
		for sid := range closure {
			st, ok := fst.States[sid]
			if !ok {
				continue
			}
			for _, arc := range st.Arcs {
				if arc.ILabel != "" {
					arcs = append(arcs, &Arc{
						ILabel: arc.ILabel,
						OLabel: arc.OLabel,
						Weight: arc.Weight,
						Next:   arc.Next,
					})
				}
			}
		}
		return arcs
	}

	for len(queue) > 0 {
		pair := queue[0]
		queue = queue[1:]

		currentStateID := stateMap[pair]
		s1, s2 := pair.s1, pair.s2

		// Collect all non-epsilon arcs reachable via epsilon paths
		arcs1 := collectArcs(a, s1)
		arcs2 := collectArcs(b, s2)

		// Also process epsilon arcs from both FSTs
		state1, ok1 := a.States[s1]
		state2, ok2 := b.States[s2]

		// Match arcs with same input label (from epsilon-expanded sets)
		labelToArcs1 := make(map[string][]*Arc)
		for _, arc := range arcs1 {
			labelToArcs1[arc.ILabel] = append(labelToArcs1[arc.ILabel], arc)
		}

		for _, arc2 := range arcs2 {
			arcs1Match, ok := labelToArcs1[arc2.ILabel]
			if !ok {
				continue
			}
			for _, arc1 := range arcs1Match {
				newPair := statePair{arc1.Next, arc2.Next}
				newStateID, exists := stateMap[newPair]
				if !exists {
					newStateID = len(stateMap)
					stateMap[newPair] = newStateID
					queue = append(queue, newPair)
					result.AddState(newStateID)
				}
				result.AddArc(currentStateID, newStateID, arc1.ILabel, arc2.OLabel, arc1.Weight+arc2.Weight)
			}
		}

		// Handle epsilon arcs: if fst1 has epsilon input, follow it (fst2 stays)
		if ok1 {
			for _, arc1 := range state1.Arcs {
				if arc1.ILabel == "" {
					newPair := statePair{arc1.Next, s2}
					newStateID, exists := stateMap[newPair]
					if !exists {
						newStateID = len(stateMap)
						stateMap[newPair] = newStateID
						queue = append(queue, newPair)
						result.AddState(newStateID)
					}
					result.AddArc(currentStateID, newStateID, "", arc1.OLabel, arc1.Weight)
				}
			}
		}

		// Handle epsilon arcs: if fst2 has epsilon input, follow it (fst1 stays)
		if ok2 {
			for _, arc2 := range state2.Arcs {
				if arc2.ILabel == "" {
					newPair := statePair{s1, arc2.Next}
					newStateID, exists := stateMap[newPair]
					if !exists {
						newStateID = len(stateMap)
						stateMap[newPair] = newStateID
						queue = append(queue, newPair)
						result.AddState(newStateID)
					}
					result.AddArc(currentStateID, newStateID, "", arc2.OLabel, arc2.Weight)
				}
			}
		}

		// Check final states - need to check epsilon closure for finals too
		if ok1 && ok2 {
			// Check if any state in epsilon closure of s1 is final
			closure1 := epsilonClosure(a, s1)
			closure2 := epsilonClosure(b, s2)
			for cs1 := range closure1 {
				st1, ok1f := a.States[cs1]
				if !ok1f || !st1.Final {
					continue
				}
				for cs2 := range closure2 {
					st2, ok2f := b.States[cs2]
					if !ok2f || !st2.Final {
						continue
					}
					result.SetFinal(currentStateID, st1.Weight+st2.Weight)
					break
				}
			}
		}
	}

	return result
}

// collectExcludeSymbols collects all input symbols from an FST (for difference exclude set)
func collectExcludeSymbols(f *Fst) map[string]bool {
	symbols := make(map[string]bool)
	for _, state := range f.States {
		for _, arc := range state.Arcs {
			if arc.ILabel != "" {
				symbols[arc.ILabel] = true
			}
		}
	}
	return symbols
}

// isCharUnion checks if an FST is a simple "character union" - i.e., all arcs go from start state
// to individual final states, each with a single character label.
// This pattern is common for VALID_UTF8_CHAR, ALPHA, DIGIT, etc.
func isCharUnion(f *Fst) bool {
	if f == nil || len(f.States) == 0 {
		return false
	}
	startState, ok := f.States[f.Start]
	if !ok || len(startState.Arcs) == 0 {
		return false
	}
	// Check all arcs from start state
	for _, arc := range startState.Arcs {
		// Each arc should have a non-empty input label
		if arc.ILabel == "" {
			return false
		}
		// The destination should be a final state with no outgoing arcs
		destState, ok := f.States[arc.Next]
		if !ok || !destState.Final || len(destState.Arcs) != 0 {
			return false
		}
	}
	// Check there are no other non-start states with arcs
	for id, state := range f.States {
		if id != f.Start && len(state.Arcs) > 0 {
			return false
		}
	}
	return true
}

// charUnionDifference computes difference when fst1 is a "character union" FST.
// This is a highly optimized path for the common WeTextProcessing pattern:
// CHAR = VCHAR.Difference(Union(Accep("\\"), Accep("\"")))
// NOT_QUOTE = VCHAR.Difference(Accep("\""))
// NOT_SPACE = VCHAR.Difference(SPACE)
func charUnionDifference(fst1, fst2 *Fst) *Fst {
	excludeSymbols := collectExcludeSymbols(fst2)
	result := NewFst()

	// Copy start state
	result.AddState(fst1.Start)
	result.Start = fst1.Start

	startState := fst1.States[fst1.Start]
	for _, arc := range startState.Arcs {
		// Skip arcs that match excluded symbols
		if excludeSymbols[arc.ILabel] {
			continue
		}
		// Create a new final state for this character
		newStateID := arc.Next
		result.AddState(newStateID)
		result.SetFinal(newStateID, 0)
		result.AddArc(fst1.Start, newStateID, arc.ILabel, arc.OLabel, arc.Weight)
	}

	return result
}

// Difference computes fst1 - fst2 = fst1 ∩ complement(fst2)
// This is used for OOV detection in WeTextProcessing
// Optimized for the common case where fst1 is a "character union" FST
func Difference(a, b *Fst) *Fst {
	if a == nil || b == nil || len(b.States) == 0 {
		return a
	}
	if len(a.States) == 0 {
		return NewFst()
	}

	// Fast path: if fst1 is a character union, use optimized difference
	if isCharUnion(a) {
		return charUnionDifference(a, b)
	}

	// General path: determinize + complement + intersect
	// Collect input symbols from both FSTs
	sigma := collectInputSymbols(a)
	for sym := range collectInputSymbols(b) {
		sigma[sym] = true
	}

	// Step 1: Determinize b
	detB := determinize(b)

	// Step 2: Make b complete (add sink state for missing transitions)
	completeB := makeComplete(detB, sigma)

	// Step 3: Complement b (swap final/non-final)
	compB := complement(completeB)

	// Step 4: Intersect a with complement(b)
	return intersect(a, compB)
}

func (f *Fst) Difference(other *Fst) *Fst {
	return Difference(f, other)
}

// Connect removes unreachable and uncoaccessible states
// Optimized for large FSTs by using iterative BFS instead of recursive
func (f *Fst) Connect() *Fst {
	if f == nil || len(f.States) == 0 {
		return NewFst()
	}

	// Find reachable states from start using BFS
	reachable := make(map[int]bool)
	queue := []int{f.Start}
	reachable[f.Start] = true

	for len(queue) > 0 {
		sid := queue[0]
		queue = queue[1:]
		state, ok := f.States[sid]
		if !ok {
			continue
		}
		for _, arc := range state.Arcs {
			if !reachable[arc.Next] {
				reachable[arc.Next] = true
				queue = append(queue, arc.Next)
			}
		}
	}

	// Find coaccessible states (states that can reach a final state)
	// Build reverse graph and do backward BFS from final states
	reverseArcs := make(map[int][]int)
	finalStates := make([]int, 0)
	for id, state := range f.States {
		if !reachable[id] {
			continue // Only consider reachable states
		}
		if state.Final {
			finalStates = append(finalStates, id)
		}
		for _, arc := range state.Arcs {
			if reachable[arc.Next] {
				reverseArcs[arc.Next] = append(reverseArcs[arc.Next], id)
			}
		}
	}

	coaccessible := make(map[int]bool)
	bfsQueue := make([]int, 0, len(finalStates))
	for _, id := range finalStates {
		coaccessible[id] = true
		bfsQueue = append(bfsQueue, id)
	}

	for len(bfsQueue) > 0 {
		sid := bfsQueue[0]
		bfsQueue = bfsQueue[1:]
		for _, prevID := range reverseArcs[sid] {
			if !coaccessible[prevID] {
				coaccessible[prevID] = true
				bfsQueue = append(bfsQueue, prevID)
			}
		}
	}

	// Build new FST with only reachable AND coaccessible states
	result := NewFst()
	validStates := make(map[int]bool)
	for id := range f.States {
		if reachable[id] && coaccessible[id] {
			validStates[id] = true
			if id == f.Start {
				result.Start = id
			}
			result.AddState(id)
			if f.States[id].Final {
				result.SetFinal(id, f.States[id].Weight)
			}
		}
	}

	// Copy arcs between valid states
	for id := range validStates {
		state := f.States[id]
		for _, arc := range state.Arcs {
			if validStates[arc.Next] {
				result.AddArc(id, arc.Next, arc.ILabel, arc.OLabel, arc.Weight)
			}
		}
	}

	return result
}

// Optimize applies basic optimization: connect (removes unreachable states)
// Note: We do NOT remove epsilon transitions because in WeTextProcessing,
// epsilon arcs carry important output labels (like Insert("cardinal { ")).
// Removing them breaks the token format.
func (f *Fst) Optimize() *Fst {
	if f == nil || len(f.States) == 0 {
		return NewFst()
	}
	result := f.Connect()
	return result
}

// removeEpsilons removes epsilon transitions by computing epsilon closures
func (f *Fst) removeEpsilons() *Fst {
	if f == nil || len(f.States) == 0 {
		return NewFst()
	}

	result := NewFst()
	for id, state := range f.States {
		result.AddState(id)
		if state.Final {
			result.SetFinal(id, state.Weight)
		}
	}

	// For each state, compute its epsilon closure
	epsilonClosure := func(startState int) (map[int]float64, []*Arc) {
		// Returns: map of reachable states via epsilon -> min weight, and non-epsilon arcs reachable via epsilon
		closure := make(map[int]float64)
		closure[startState] = 0
		var nonEpsilonArcs []*Arc

		queue := []int{startState}
		visited := make(map[int]bool)

		for len(queue) > 0 {
			sid := queue[0]
			queue = queue[1:]
			if visited[sid] {
				continue
			}
			visited[sid] = true

			state, ok := f.States[sid]
			if !ok {
				continue
			}

			currentWeight := closure[sid]

			// If this state is final, propagate final weight
			if state.Final && sid != startState {
				newFinalWeight := currentWeight + state.Weight
				if !result.States[startState].Final || newFinalWeight < result.States[startState].Weight {
					result.States[startState].Final = true
					result.States[startState].Weight = newFinalWeight
				}
			}

			for _, arc := range state.Arcs {
				if arc.ILabel == "" {
					// Epsilon input arc - follow for closure
					newWeight := currentWeight + arc.Weight
					if existing, ok := closure[arc.Next]; !ok || newWeight < existing {
						closure[arc.Next] = newWeight
						queue = append(queue, arc.Next)
					}
					// If it also produces output, collect as a non-epsilon output arc
					if arc.OLabel != "" {
						newArc := &Arc{
							ILabel: "",
							OLabel: arc.OLabel,
							Weight: newWeight,
							Next:   arc.Next,
						}
						nonEpsilonArcs = append(nonEpsilonArcs, newArc)
					}
				} else {
					// Non-epsilon arc (has input label) reachable via epsilon path
					newArc := &Arc{
						ILabel: arc.ILabel,
						OLabel: arc.OLabel,
						Weight: currentWeight + arc.Weight,
						Next:   arc.Next,
					}
					nonEpsilonArcs = append(nonEpsilonArcs, newArc)
				}
			}
		}

		return closure, nonEpsilonArcs
	}

	for id := range f.States {
		_, nonEpsilonArcs := epsilonClosure(id)

		// Deduplicate arcs: keep the one with minimum weight for each (label, next) pair
		type arcKey struct {
			ilabel string
			olabel string
			next   int
		}
		bestArcs := make(map[arcKey]*Arc)

		for _, arc := range nonEpsilonArcs {
			key := arcKey{arc.ILabel, arc.OLabel, arc.Next}
			if existing, ok := bestArcs[key]; !ok || arc.Weight < existing.Weight {
				bestArcs[key] = arc
			}
		}

		// Also keep original non-epsilon arcs
		state := f.States[id]
		for _, arc := range state.Arcs {
			if arc.ILabel != "" || arc.OLabel != "" {
				key := arcKey{arc.ILabel, arc.OLabel, arc.Next}
				if existing, ok := bestArcs[key]; !ok || arc.Weight < existing.Weight {
					bestArcs[key] = arc
				}
			}
		}

		// Clear original arcs and add optimized ones
		result.States[id].Arcs = []*Arc{}
		for _, arc := range bestArcs {
			result.States[id].Arcs = append(result.States[id].Arcs, arc)
		}
	}

	return result
}

func (f *Fst) Compose(other *Fst) *Fst {
	if f == nil || other == nil {
		return NewFst()
	}
	result := NewFst()
	result.Start = 0

	type statePair struct{ s1, s2 int }
	stateMap := make(map[statePair]int)
	queue := []statePair{}
	startPair := statePair{f.Start, other.Start}
	stateMap[startPair] = 0
	queue = append(queue, startPair)
	result.AddState(0)

	for len(queue) > 0 {
		if len(stateMap) > 2000000 {
			return NewFst()
		}
		pair := queue[0]
		queue = queue[1:]
		currentStateID := stateMap[pair]
		s1, s2 := pair.s1, pair.s2

		st1, ok1 := f.States[s1]
		st2, ok2 := other.States[s2]
		if !ok1 || !ok2 {
			continue
		}

		// Case 1: Match arcs (fst1 output == fst2 input, both non-epsilon)
		for _, arc1 := range st1.Arcs {
			if arc1.OLabel == "" {
				continue
			}
			for _, arc2 := range st2.Arcs {
				if arc2.ILabel == "" {
					continue
				}
				if arc1.OLabel == arc2.ILabel {
					newPair := statePair{arc1.Next, arc2.Next}
					newStateID, exists := stateMap[newPair]
					if !exists {
						newStateID = len(stateMap)
						stateMap[newPair] = newStateID
						queue = append(queue, newPair)
						result.AddState(newStateID)
					}
					result.AddArc(currentStateID, newStateID, arc1.ILabel, arc2.OLabel, arc1.Weight+arc2.Weight)
				}
			}
		}

		// Case 2: fst1 has epsilon input arc - follow it (fst2 stays in place)
		for _, arc1 := range st1.Arcs {
			if arc1.ILabel == "" {
				newPair := statePair{arc1.Next, s2}
				newStateID, exists := stateMap[newPair]
				if !exists {
					newStateID = len(stateMap)
					stateMap[newPair] = newStateID
					queue = append(queue, newPair)
					result.AddState(newStateID)
				}
				result.AddArc(currentStateID, newStateID, "", arc1.OLabel, arc1.Weight)
			}
		}

		// Case 3: fst2 has epsilon output arc - follow it (fst1 stays in place)
		for _, arc2 := range st2.Arcs {
			if arc2.OLabel == "" && arc2.ILabel != "" {
				newPair := statePair{s1, arc2.Next}
				newStateID, exists := stateMap[newPair]
				if !exists {
					newStateID = len(stateMap)
					stateMap[newPair] = newStateID
					queue = append(queue, newPair)
					result.AddState(newStateID)
				}
				result.AddArc(currentStateID, newStateID, arc2.ILabel, "", arc2.Weight)
			}
		}

		// Case 4: fst2 has epsilon input arc - follow it (fst1 stays in place)
		for _, arc2 := range st2.Arcs {
			if arc2.ILabel == "" && arc2.OLabel != "" {
				newPair := statePair{s1, arc2.Next}
				newStateID, exists := stateMap[newPair]
				if !exists {
					newStateID = len(stateMap)
					stateMap[newPair] = newStateID
					queue = append(queue, newPair)
					result.AddState(newStateID)
				}
				result.AddArc(currentStateID, newStateID, "", arc2.OLabel, arc2.Weight)
			}
		}

		// Case 5: pure epsilon arc in fst2 (ILabel == "" && OLabel == "") - follow it
		for _, arc2 := range st2.Arcs {
			if arc2.ILabel == "" && arc2.OLabel == "" {
				newPair := statePair{s1, arc2.Next}
				newStateID, exists := stateMap[newPair]
				if !exists {
					newStateID = len(stateMap)
					stateMap[newPair] = newStateID
					queue = append(queue, newPair)
					result.AddState(newStateID)
				}
				result.AddArc(currentStateID, newStateID, "", "", arc2.Weight)
			}
		}

		// Case 6: pure epsilon arc in fst1 (ILabel == "" && OLabel == "") - follow it
		for _, arc1 := range st1.Arcs {
			if arc1.ILabel == "" && arc1.OLabel == "" {
				newPair := statePair{arc1.Next, s2}
				newStateID, exists := stateMap[newPair]
				if !exists {
					newStateID = len(stateMap)
					stateMap[newPair] = newStateID
					queue = append(queue, newPair)
					result.AddState(newStateID)
				}
				result.AddArc(currentStateID, newStateID, "", "", arc1.Weight)
			}
		}

		// Case 7: fst1 consumes input, produces epsilon output (e.g., delete arcs)
		for _, arc1 := range st1.Arcs {
			if arc1.ILabel != "" && arc1.OLabel == "" {
				newPair := statePair{arc1.Next, s2}
				newStateID, exists := stateMap[newPair]
				if !exists {
					newStateID = len(stateMap)
					stateMap[newPair] = newStateID
					queue = append(queue, newPair)
					result.AddState(newStateID)
				}
				result.AddArc(currentStateID, newStateID, arc1.ILabel, "", arc1.Weight)
			}
		}

		// Check if both states are final
		if st1.Final && st2.Final {
			result.SetFinal(currentStateID, st1.Weight+st2.Weight)
		}
	}
	return result
}

func (f *Fst) ComposeShortestPath(other *Fst) string {
	// Delegates to the new Viterbi algorithm.
	return ComposeInputWithFst("", f, other)
}

// ComposeInputWithFst performs composition of an input string with an FST
// using Viterbi beam search. This aligns with OpenFST's approach of processing
// matching arcs first, then epsilon closure, for each input position.
//
// The algorithm:
//  1. Extract the input string from the linear-chain input FST (or use raw string)
//  2. For each character:
//     a. Epsilon closure at current beam (follow tagger epsilon transitions)
//     b. Match arcs: for each beam state, sorted-matcher finds arcs by character
//     c. Epsilon closure at matched states
//     d. Prune beam to keep search tractable
//  3. Among final tagger states, return the one with minimum accumulated weight.
//
// If inputStr is non-empty, it is used directly. Otherwise, the string is
// extracted from the linear-chain FST f.
func ComposeInputWithFst(inputStr string, f *Fst, other *Fst) string {
	if other == nil {
		return ""
	}

	// Extract input string from FST if not provided
	input := inputStr
	if input == "" {
		if f == nil {
			return ""
		}
		var sb strings.Builder
		state := f.Start
		for {
			st := f.States[state]
			if st == nil || len(st.Arcs) == 0 {
				break
			}
			for _, arc := range st.Arcs {
				sb.WriteString(arc.ILabel)
				state = arc.Next
			}
		}
		input = sb.String()
	}

	runes := []rune(input)
	if len(runes) == 0 {
		return ""
	}

	// beamEntry represents a tagger state with accumulated weight and output
	type beamEntry struct {
		weight float64
		output string
	}

	const maxBeam = 500000
	const epsilonPenalty = 2.0

	// Sorted matcher cache (keyed by tagger state ID)
	type sortedState struct {
		arcs     []*Arc
		minLabel string
		maxLabel string
	}
	matcherCache := make(map[int]*sortedState)
	getMatcher := func(stateID int) *sortedState {
		if sm, ok := matcherCache[stateID]; ok {
			return sm
		}
		st := other.States[stateID]
		if st == nil {
			matcherCache[stateID] = nil
			return nil
		}
		var nonEpsilon []*Arc
		for _, arc := range st.Arcs {
			if arc.ILabel != "" {
				nonEpsilon = append(nonEpsilon, arc)
			}
		}
		sort.Slice(nonEpsilon, func(i, j int) bool {
			return nonEpsilon[i].ILabel < nonEpsilon[j].ILabel
		})
		sm := &sortedState{arcs: nonEpsilon}
		if len(nonEpsilon) > 0 {
			sm.minLabel = nonEpsilon[0].ILabel
			sm.maxLabel = nonEpsilon[len(nonEpsilon)-1].ILabel
		}
		matcherCache[stateID] = sm
		return sm
	}

	// epsilonClosure follows all epsilon arcs from the current beam,
 	// updating each reachable state with the best (lowest) weight.
 	epsilonClosure := func(beam map[int]*beamEntry) map[int]*beamEntry {
 		type bfsState struct {
 			s2     int
 			weight float64
 			output string
 		}
 		visited := make(map[int]float64)
 		result := make(map[int]*beamEntry, len(beam))
 		q := make([]bfsState, 0, len(beam)*2)

 		for s2, entry := range beam {
 			visited[s2] = entry.weight
 			result[s2] = &beamEntry{weight: entry.weight, output: entry.output}
 			q = append(q, bfsState{s2, entry.weight, entry.output})
 		}
 		for len(q) > 0 {
 			item := q[0]
 			q = q[1:]
 			st := other.States[item.s2]
 			if st == nil {
 				continue
 			}
 			for _, arc := range st.Arcs {
 				if arc.ILabel == "" {
 					newWeight := item.weight + arc.Weight + epsilonPenalty
 					newOutput := item.output + arc.OLabel
 					if prev, ok := visited[arc.Next]; !ok || newWeight < prev {
 						visited[arc.Next] = newWeight
 						result[arc.Next] = &beamEntry{weight: newWeight, output: newOutput}
 						q = append(q, bfsState{arc.Next, newWeight, newOutput})
 					}
 				}
 			}
 		}
 		return result
 	}

	// Initialize beam with start state
	beam := make(map[int]*beamEntry)
	beam[other.Start] = &beamEntry{weight: 0, output: ""}
	beam = epsilonClosure(beam)

	for _, ch := range runes {
		charStr := string(ch)

		// Phase 1: Match arcs (for each beam state, find arcs with ILabel == ch)
		nextBeam := make(map[int]*beamEntry)
		for s2, entry := range beam {
			sm := getMatcher(s2)
			if sm == nil {
				continue
			}
			if charStr < sm.minLabel || charStr > sm.maxLabel {
				continue
			}
			idx := sort.Search(len(sm.arcs), func(i int) bool {
				return sm.arcs[i].ILabel >= charStr
			})
			for idx < len(sm.arcs) && sm.arcs[idx].ILabel == charStr {
				arc := sm.arcs[idx]
				newWeight := entry.weight + arc.Weight
				if prev, ok := nextBeam[arc.Next]; !ok || newWeight < prev.weight {
					nextBeam[arc.Next] = &beamEntry{weight: newWeight, output: entry.output + arc.OLabel}
				}
				idx++
			}
		}

		if len(nextBeam) == 0 {
			// Character not matched by any rule (e.g., some punctuation
			// or OOV). Output as-is, keep current beam for next char.
			// This is equivalent to Python's preprocessor pass-through.
			continue
		}

		// Phase 2: Epsilon closure from matched states
		nextBeam = epsilonClosure(nextBeam)

		// Phase 3: Prune beam if needed
		if len(nextBeam) > maxBeam {
			entries := make([]struct {
				s2     int
				weight float64
				output string
			}, 0, len(nextBeam))
			for s2, entry := range nextBeam {
				entries = append(entries, struct {
					s2     int
					weight float64
					output string
				}{s2, entry.weight, entry.output})
			}
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].weight < entries[j].weight
			})
			for i := maxBeam; i < len(entries); i++ {
				delete(nextBeam, entries[i].s2)
			}
		}

		beam = nextBeam
	}

	// Find best path among final states
	bestOutput := ""
	minWeight := 1e30
	for s2, entry := range beam {
		st := other.States[s2]
		if st != nil && st.Final {
			w := entry.weight + st.Weight
			if w < minWeight {
				minWeight = w
				bestOutput = entry.output
			}
		}
	}

	return bestOutput
}

func (f *Fst) At(other *Fst) *Fst {
	return f.Compose(other)
}

type pathState struct {
	stateID int
	weight  float64
	path    []*Arc
}

// ShortestPath finds the n shortest paths through an FST using Dijkstra's algorithm
func ShortestPath(fst *Fst, nshortest int, unique bool) *Fst {
	if fst == nil {
		return NewFst()
	}

	if nshortest <= 0 {
		nshortest = 1
	}

	dist := make(map[int]float64)
	dist[fst.Start] = 0

	pq := &priorityQueue{}
	heap.Init(pq)
	heap.Push(pq, &pqItem{stateID: fst.Start, weight: 0, output: ""})

	type finalPath struct {
		output string
		weight float64
	}
	var finalPaths []finalPath
	foundCount := 0

	visited := make(map[int]int) // stateID -> number of times extracted

	for pq.Len() > 0 {
		item := heap.Pop(pq).(*pqItem)
		stateID := item.stateID
		weight := item.weight
		output := item.output

		visited[stateID]++

		state, ok := fst.States[stateID]
		if !ok {
			continue
		}

		if state.Final {
			finalPaths = append(finalPaths, finalPath{output, weight + state.Weight})
			foundCount++
			if foundCount >= nshortest {
				break
			}
			continue
		}

		for _, arc := range state.Arcs {
			newWeight := weight + arc.Weight
			newOutput := output + arc.OLabel

			// Allow revisiting states for n-shortest paths
			if visited[arc.Next] < nshortest {
				if existingWeight, ok := dist[arc.Next]; !ok || newWeight < existingWeight {
					dist[arc.Next] = newWeight
					heap.Push(pq, &pqItem{stateID: arc.Next, weight: newWeight, output: newOutput})
				}
			}
		}
	}

	if len(finalPaths) == 0 {
		return NewFst()
	}

	// Return the best path
	bestPath := finalPaths[0]

	result := NewFst()
	runes := []rune(bestPath.output)
	for i, r := range runes {
		result.AddArc(i, i+1, string(r), string(r), 0)
	}
	result.SetFinal(len(runes), bestPath.weight)

	return result
}

type pqItem struct {
	stateID int
	weight  float64
	output  string
	index   int
}

type priorityQueue []*pqItem

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	return pq[i].weight < pq[j].weight
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*pqItem)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

func (f *Fst) PathString() string {
	if f == nil {
		return ""
	}

	dist := make(map[int]float64)
	dist[f.Start] = 0

	pq := &priorityQueue{}
	heap.Init(pq)
	heap.Push(pq, &pqItem{stateID: f.Start, weight: 0, output: ""})

	var bestOutput string
	minWeight := 1e30 // Use a larger value for safety

	for pq.Len() > 0 {
		item := heap.Pop(pq).(*pqItem)
		stateID := item.stateID
		weight := item.weight
		output := item.output

		if weight > minWeight {
			continue
		}

		if existingDist, ok := dist[stateID]; ok && existingDist < weight {
			continue
		}

		state, ok := f.States[stateID]
		if !ok {
			continue
		}

		if state.Final {
			finalWeight := weight + state.Weight
			if finalWeight < minWeight {
				minWeight = finalWeight
				bestOutput = output
			}
			continue
		}

		for _, arc := range state.Arcs {
			newWeight := weight + arc.Weight
			newOutput := output + arc.OLabel

			if existingWeight, ok := dist[arc.Next]; !ok || newWeight < existingWeight {
				dist[arc.Next] = newWeight
				heap.Push(pq, &pqItem{stateID: arc.Next, weight: newWeight, output: newOutput})
			}
		}
	}

	return bestOutput
}

func Escape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

func Cdrewrite(fst *Fst, l, r string, sigma *Fst) *Fst {
	if fst == nil {
		return NewFst()
	}

	if sigma == nil {
		return fst
	}

	if l == "" && r == "" {
		if len(fst.States) == 1 && len(fst.States[0].Arcs) == 0 && fst.Start == 0 && fst.States[0].Final {
			return sigma.Star()
		}
		prefix := sigma.Star().Concat(fst)
		repeated := prefix.Star()
		result := repeated.Concat(sigma.Star())
		return result
	}

	sigmaStar := sigma.Star()
	left := Accep(l)

	if r == "[EOS]" {
		rule := left.At(fst)
		return sigmaStar.At(rule).Union(sigmaStar)
	}

	if l == "[BOS]" {
		rule := fst.At(Accep(r))
		return sigmaStar.At(sigmaStar.At(rule).Star())
	}

	// Normal case: rule can apply anywhere
	right := Accep(r)
	rule := left.At(fst).At(right)
	return sigmaStar.At(rule.At(sigmaStar).Star())
}

func Project(f *Fst, projType string) *Fst {
	if f == nil || len(f.States) == 0 {
		return NewFst()
	}
	result := NewFst()
	result.Start = f.Start
	for from, state := range f.States {
		for _, arc := range state.Arcs {
			if projType == "input" {
				// project to input: keep ilabel, set olabel = ilabel
				result.AddArc(from, arc.Next, arc.ILabel, arc.ILabel, arc.Weight)
			} else {
				// project to output: keep olabel, set ilabel = olabel
				result.AddArc(from, arc.Next, arc.OLabel, arc.OLabel, arc.Weight)
			}
		}
		if state.Final {
			result.SetFinal(from, state.Weight)
		}
	}
	return result
}

func Invert(f *Fst) *Fst {
	return f.Invert()
}

func Compose(a, b *Fst) *Fst {
	return a.Compose(b)
}

func (f *Fst) String() string {
	return f.PathString()
}

// FstSerializable is used for gob serialization
type FstSerializable struct {
	Start  int
	States []StateSerializable
}

type StateSerializable struct {
	ID     int
	Final  bool
	Weight float64
	Arcs   []ArcSerializable
}

type ArcSerializable struct {
	ILabel string
	OLabel string
	Weight float64
	Next   int
}

func (f *Fst) Write(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := gob.NewEncoder(file)
	s := fstToSerializable(f)
	return enc.Encode(s)
}

func FstRead(path string) (*Fst, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	dec := gob.NewDecoder(file)
	var s FstSerializable
	if err := dec.Decode(&s); err != nil {
		return nil, err
	}
	return fstFromSerializable(s), nil
}

func fstToSerializable(f *Fst) *FstSerializable {
	if f == nil {
		return &FstSerializable{}
	}

	// Collect and sort state IDs for deterministic ordering
	stateIDs := make([]int, 0, len(f.States))
	for id := range f.States {
		stateIDs = append(stateIDs, id)
	}
	sort.Ints(stateIDs)

	s := &FstSerializable{
		Start:  f.Start,
		States: make([]StateSerializable, 0, len(f.States)),
	}

	for _, id := range stateIDs {
		state := f.States[id]
		ss := StateSerializable{
			ID:     state.ID,
			Final:  state.Final,
			Weight: state.Weight,
			Arcs:   make([]ArcSerializable, len(state.Arcs)),
		}
		for i, arc := range state.Arcs {
			ss.Arcs[i] = ArcSerializable{
				ILabel: arc.ILabel,
				OLabel: arc.OLabel,
				Weight: arc.Weight,
				Next:   arc.Next,
			}
		}
		s.States = append(s.States, ss)
	}

	return s
}

func fstFromSerializable(s FstSerializable) *Fst {
	f := NewFst()
	f.Start = s.Start

	for _, ss := range s.States {
		state := &State{
			ID:     ss.ID,
			Final:  ss.Final,
			Weight: ss.Weight,
			Arcs:   make([]*Arc, len(ss.Arcs)),
		}
		for i, as := range ss.Arcs {
			state.Arcs[i] = &Arc{
				ILabel: as.ILabel,
				OLabel: as.OLabel,
				Weight: as.Weight,
				Next:   as.Next,
			}
		}
		f.States[ss.ID] = state
	}

	return f
}
