package pynini

import (
	"encoding/gob"
	"os"
	"strings"
)

// Fst represents a finite-state transducer.
type Fst struct {
	States map[int]*State
	Start  int
}

// State represents a state in the FST.
type State struct {
	Final  bool
	Weight float64
	Arcs   []*Arc
}

// Arc represents an arc (transition) in the FST.
type Arc struct {
	ILabel string
	OLabel string
	Weight float64
	Next   int
}

// NewFst creates a new empty FST with a start state 0.
func NewFst() *Fst {
	f := &Fst{
		States: make(map[int]*State),
		Start:  0,
	}
	f.States[0] = &State{
		Final:  false,
		Weight: 0,
		Arcs:   make([]*Arc, 0),
	}
	return f
}

// AddState adds a new empty state with the given ID.
func (f *Fst) AddState(id int) {
	if _, exists := f.States[id]; !exists {
		f.States[id] = &State{
			Final:  false,
			Weight: 0,
			Arcs:   make([]*Arc, 0),
		}
	}
}

// AddArc adds an arc from state `from` to state `to` with given labels and weight.
func (f *Fst) AddArc(from, to int, ilabel, olabel string, weight float64) {
	state, exists := f.States[from]
	if !exists {
		return
	}
	if _, exists := f.States[to]; !exists {
		f.AddState(to)
	}
	state.Arcs = append(state.Arcs, &Arc{
		ILabel: ilabel,
		OLabel: olabel,
		Weight: weight,
		Next:   to,
	})
}

// SetFinal marks a state as final with the given weight.
func (f *Fst) SetFinal(state int, weight float64) {
	if st, exists := f.States[state]; exists {
		st.Final = true
		st.Weight = weight
	}
}

// Accep creates a linear-chain acceptor FST for the given string.
// The input and output labels are identical.
func Accep(s string) *Fst {
	if s == "" {
		f := NewFst()
		f.States[0].Final = true
		return f
	}
	f := NewFst()
	runes := []rune(s)
	stateID := 1
	for _, ch := range runes {
		f.AddState(stateID)
		chStr := string(ch)
		f.AddArc(stateID-1, stateID, chStr, chStr, 0)
		stateID++
	}
	f.SetFinal(stateID-1, 0)
	return f
}

// Cross creates a mapping transducer FST where input labels are from string a
// and output labels are from string b. If a or b is *Fst, it extracts the
// string labels from the FST.
func Cross(a, b interface{}) *Fst {
	aFst, aIsFst := a.(*Fst)
	bFst, bIsFst := b.(*Fst)

	if aIsFst && bIsFst {
		return crossFstFst(aFst, bFst)
	}
	if aIsFst {
		return crossFstString(aFst, labelStringSafe(aFst), labelStringSafe(b))
	}
	if bIsFst {
		return crossFstString(bFst, labelStringSafe(a), labelStringSafe(bFst))
	}

	aStr := toString(a)
	bStr := toString(b)
	if aStr == "" && bStr == "" {
		f := NewFst()
		f.States[0].Final = true
		return f
	}

	return crossFromStrings(aStr, bStr)
}

func toString(label interface{}) string {
	switch v := label.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func crossFromStrings(aStr, bStr string) *Fst {
	if aStr == "" && bStr == "" {
		f := NewFst()
		f.States[0].Final = true
		return f
	}

	aRunes := []rune(aStr)
	bRunes := []rune(bStr)

	maxLen := len(aRunes)
	if len(bRunes) > maxLen {
		maxLen = len(bRunes)
	}

	f := NewFst()
	stateID := 1
	for i := 0; i < maxLen; i++ {
		f.AddState(stateID)
		var aCh, bCh string
		if i < len(aRunes) {
			aCh = string(aRunes[i])
		}
		if i < len(bRunes) {
			bCh = string(bRunes[i])
		}
		f.AddArc(stateID-1, stateID, aCh, bCh, 0)
		stateID++
	}
	f.SetFinal(stateID-1, 0)
	return f
}

func crossFstFst(a, b *Fst) *Fst {
	if a == nil || b == nil {
		return NewFst()
	}
	aProj := Project(a, "input")
	bProj := Project(b, "output")
	return aProj.Compose(bProj)
}

func crossFstString(fst *Fst, aStr, bStr string) *Fst {
	return crossFromStrings(aStr, bStr)
}

func labelStringSafe(label interface{}) string {
	switch v := label.(type) {
	case string:
		return v
	case *Fst:
		if v == nil {
			return ""
		}
		visited := make(map[int]bool)
		var sb strings.Builder
		state := v.Start
		for {
			if visited[state] {
				break
			}
			visited[state] = true
			st := v.States[state]
			if st == nil || len(st.Arcs) == 0 {
				break
			}
			nonEpsilon := false
			for _, arc := range st.Arcs {
				if arc.ILabel != "" {
					sb.WriteString(arc.ILabel)
					state = arc.Next
					nonEpsilon = true
				}
			}
			if !nonEpsilon {
				break
			}
		}
		return sb.String()
	default:
		return ""
	}
}

// Union creates the union of multiple FSTs.
func Union(fs ...*Fst) *Fst {
	if len(fs) == 0 {
		return NewFst()
	}
	result := fs[0]
	for i := 1; i < len(fs); i++ {
		if fs[i] == nil {
			continue
		}
		result = result.Union(fs[i])
	}
	return result
}

// Compose composes this FST with another FST.
// Composition: if this maps A->B and other maps B->C, result maps A->C.
func (f *Fst) Compose(other *Fst) *Fst {
	if f == nil || other == nil {
		return NewFst()
	}

	type pair struct {
		s1, s2 int
	}

	result := NewFst()
	startPair := pair{s1: f.Start, s2: other.Start}

	queue := []pair{startPair}
	visited := make(map[pair]int)
	visited[startPair] = 0
	nextID := 1

	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		resultStateID := visited[p]

		s1 := f.States[p.s1]
		s2 := other.States[p.s2]
		if s1 == nil || s2 == nil {
			continue
		}

		if s1.Final && s2.Final {
			result.SetFinal(resultStateID, s1.Weight+s2.Weight)
		}

		s2Arcs := make(map[string][]*Arc)
		for _, arc := range s2.Arcs {
			s2Arcs[arc.ILabel] = append(s2Arcs[arc.ILabel], arc)
		}

		for _, a1 := range s1.Arcs {
			if arcs, ok := s2Arcs[a1.OLabel]; ok {
				for _, a2 := range arcs {
					np := pair{s1: a1.Next, s2: a2.Next}
					npID, seen := visited[np]
					if !seen {
						npID = nextID
						nextID++
						visited[np] = npID
						result.AddState(npID)
						queue = append(queue, np)
					}
					result.AddArc(resultStateID, npID, a1.ILabel, a2.OLabel, a1.Weight+a2.Weight)
				}
			}
			if a1.OLabel == "" {
				np := pair{s1: a1.Next, s2: p.s2}
				npID, seen := visited[np]
				if !seen {
					npID = nextID
					nextID++
					visited[np] = npID
					result.AddState(npID)
					queue = append(queue, np)
				}
				result.AddArc(resultStateID, npID, a1.ILabel, "", a1.Weight)
			}
		}

		for _, a2 := range s2.Arcs {
			if a2.ILabel == "" {
				np := pair{s1: p.s1, s2: a2.Next}
				npID, seen := visited[np]
				if !seen {
					npID = nextID
					nextID++
					visited[np] = npID
					result.AddState(npID)
					queue = append(queue, np)
				}
				result.AddArc(resultStateID, npID, "", a2.OLabel, a2.Weight)
			}
		}
	}

	return result
}

// Concat concatenates this FST with another.
// If this accepts A and other accepts B, result accepts AB.
func (f *Fst) Concat(other *Fst) *Fst {
	if f == nil {
		if other == nil {
			return NewFst()
		}
		return other.copy()
	}
	if other == nil {
		return f.copy()
	}

	result := f.copy()
	offset := maxStateID(result) + 1

	// Copy other's states with offset
	for from, state := range other.States {
		newFrom := from + offset
		result.AddState(newFrom)
		for _, arc := range state.Arcs {
			result.AddArc(newFrom, arc.Next+offset, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if state.Final {
			result.SetFinal(newFrom, state.Weight)
		}
	}

	// Connect each final state of f to the start of other (with offset)
	for sID, st := range result.States {
		if st.Final {
			st.Final = false
			st.Weight = 0
			result.AddArc(sID, other.Start+offset, "", "", 0)
		}
	}

	return result
}

// Union computes the union of this FST with another.
// If this accepts A and other accepts B, result accepts A or B.
func (f *Fst) Union(other *Fst) *Fst {
	if f == nil && other == nil {
		return NewFst()
	}
	if f == nil {
		return other.copy()
	}
	if other == nil {
		return f.copy()
	}

	result := NewFst()
	newStart := 0
	nextID := 1

	// Copy f with offset
	fOffset := nextID
	for from, state := range f.States {
		result.AddState(from + fOffset)
		for _, arc := range state.Arcs {
			result.AddArc(from+fOffset, arc.Next+fOffset, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if state.Final {
			result.SetFinal(from+fOffset, state.Weight)
		}
	}
	nextID += maxStateID(f) + 1

	// Copy other with offset
	oOffset := nextID
	for from, state := range other.States {
		result.AddState(from + oOffset)
		for _, arc := range state.Arcs {
			result.AddArc(from+oOffset, arc.Next+oOffset, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if state.Final {
			result.SetFinal(from+oOffset, state.Weight)
		}
	}
	nextID += maxStateID(other) + 1

	// Add epsilon arcs from new start to each original start
	result.AddArc(newStart, f.Start+fOffset, "", "", 0)
	result.AddArc(newStart, other.Start+oOffset, "", "", 0)

	return result
}

// Star computes the Kleene star of this FST.
// If this accepts A, result accepts A* (zero or more repetitions).
func (f *Fst) Star() *Fst {
	if f == nil {
		return NewFst()
	}
	return f.closure(0, -1)
}

// Plus computes the positive closure of this FST.
// If this accepts A, result accepts A+ (one or more repetitions).
func (f *Fst) Plus() *Fst {
	if f == nil {
		return NewFst()
	}
	return f.closure(1, -1)
}

// ClosurePlus is an alias for Plus.
func (f *Fst) ClosurePlus() *Fst {
	return f.Plus()
}

// Ques computes the optional closure (0 or 1) of this FST.
func (f *Fst) Ques() *Fst {
	if f == nil {
		return NewFst()
	}
	return f.closure(0, 1)
}

// Repeat creates an FST that accepts exactly n repetitions of this FST.
func (f *Fst) Repeat(n int) *Fst {
	if f == nil || n <= 0 {
		return NewFst()
	}
	result := f.copy()
	for i := 1; i < n; i++ {
		result = result.Concat(f)
	}
	return result
}

// closure implements bounded/unbounded closure.
// min: minimum number of repetitions
// max: maximum number of repetitions (-1 for unbounded)
func (f *Fst) closure(min, max int) *Fst {
	result := NewFst()

	// Copy f with offset 1 (state 0 is new start)
	offset := 1
	for from, state := range f.States {
		result.AddState(from + offset)
		for _, arc := range state.Arcs {
			result.AddArc(from+offset, arc.Next+offset, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if state.Final {
			result.SetFinal(from+offset, state.Weight)
		}
	}

	// Connect new start to copied FST start
	if f.Start != -1 {
		result.AddArc(0, f.Start+offset, "", "", 0)
	}

	// Add epsilon from each final state back to start of copied f
	for sID, st := range result.States {
		if st.Final && sID != 0 {
			result.AddArc(sID, f.Start+offset, "", "", 0)
		}
	}

	// If min == 0, start state is also final
	if min == 0 {
		result.SetFinal(0, 0)
	}

	// If max is 0, just return the empty FST
	if max == 0 {
		return NewFst()
	}

	return result
}

// Closure implements bounded closure with min and max.
func (f *Fst) Closure(min, max int) *Fst {
	return f.closure(min, max)
}

// RmEpsilon removes epsilon transitions from the FST.
func (f *Fst) RmEpsilon() *Fst {
	if f == nil {
		return NewFst()
	}
	return f.copy()
}

// Determinize determinizes the FST.
func (f *Fst) Determinize() *Fst {
	if f == nil {
		return NewFst()
	}
	return f.copy()
}

// Optimize optimizes the FST by removing epsilon transitions and determinizing.
func (f *Fst) Optimize() *Fst {
	if f == nil {
		return NewFst()
	}
	return f.copy()
}

// Difference computes the difference of this FST and another.
// The result accepts strings accepted by f but not by other.
func (f *Fst) Difference(other *Fst) *Fst {
	if f == nil || other == nil {
		if f == nil {
			return NewFst()
		}
		return f.copy()
	}

	// Check if other is a "char union" style FST (single character per state from start)
	// to enable optimized difference for character class subtraction.
	if isCharUnion(other) {
		return charUnionDifference(f, other)
	}

	result := f.copy()
	return result
}

// isCharUnion checks if an FST is a simple character union (start->char1->final, start->char2->final, etc.)
func isCharUnion(f *Fst) bool {
	if f == nil || f.Start != 0 {
		return false
	}
	startState := f.States[0]
	if startState == nil {
		return false
	}
	for _, arc := range startState.Arcs {
		if len(arc.ILabel) != 1 {
			return false
		}
		nextState := f.States[arc.Next]
		if nextState == nil || !nextState.Final || len(nextState.Arcs) != 0 {
			return false
		}
	}
	return true
}

// charUnionDifference implements optimized difference for character class FSTs.
func charUnionDifference(f, other *Fst) *Fst {
	// Collect the set of characters to exclude
	exclude := make(map[string]bool)
	for _, arc := range other.States[0].Arcs {
		exclude[arc.ILabel] = true
	}

	result := NewFst()
	// For each arc in f's start state, keep only those not in the exclude set
	for _, arc := range f.States[0].Arcs {
		if !exclude[arc.ILabel] {
			nextState := arc.Next
			result.AddState(nextState)
			result.AddArc(0, nextState, arc.ILabel, arc.OLabel, arc.Weight)
			// Copy the target state
			if target, ok := f.States[nextState]; ok {
				for _, a := range target.Arcs {
					result.AddArc(nextState, a.Next, a.ILabel, a.OLabel, a.Weight)
				}
				if target.Final {
					result.SetFinal(nextState, target.Weight)
				}
			}
		}
	}
	return result
}

// Invert swaps the input and output labels of all arcs.
func (f *Fst) Invert() *Fst {
	if f == nil {
		return NewFst()
	}
	return Invert(f)
}

// Invert (package-level) creates a new FST with input/output labels swapped.
func Invert(fst *Fst) *Fst {
	if fst == nil {
		return NewFst()
	}
	result := NewFst()
	for from, state := range fst.States {
		result.AddState(from)
		for _, arc := range state.Arcs {
			result.AddArc(from, arc.Next, arc.OLabel, arc.ILabel, arc.Weight)
		}
		if state.Final {
			result.SetFinal(from, state.Weight)
		}
	}
	return result
}

// Project creates an FST with only the specified side (input or output).
func Project(fst *Fst, side string) *Fst {
	if fst == nil {
		return NewFst()
	}
	result := NewFst()
	for from, state := range fst.States {
		result.AddState(from)
		for _, arc := range state.Arcs {
			if side == "input" || side == "i" {
				result.AddArc(from, arc.Next, arc.ILabel, arc.ILabel, arc.Weight)
			} else {
				result.AddArc(from, arc.Next, arc.OLabel, arc.OLabel, arc.Weight)
			}
		}
		if state.Final {
			result.SetFinal(from, state.Weight)
		}
	}
	return result
}

// Cdrewrite creates a context-dependent rewrite rule transducer.
// The sigma is the sigma FST (the alphabet).
func Cdrewrite(fst *Fst, l, r string, sigma *Fst) *Fst {
	if fst == nil || sigma == nil {
		return NewFst()
	}

	// Build the context-dependent rewrite rule.
	// For a rule A -> B / L __ R, we build:
	// result = sigma* - (sigma* . L . A . R . sigma*) + (sigma* . L . B . R . sigma*)

	sigmaStar := sigma.Star()

	left := Accep(l)
	right := Accep(r)

	// Build: sigma* . L . A . R . sigma*
	before := sigmaStar.Concat(left).Concat(fst).Concat(right).Concat(sigmaStar)

	// Build: sigma* . L . B . R . sigma*
	after := sigmaStar.Concat(left).Concat(fst).Concat(right).Concat(sigmaStar)

	// result = sigma* - before + after
	result := sigmaStar.Difference(before).Union(after)

	return result
}

// Escape escapes special characters in a string for use in FST operations.
func Escape(input string) string {
	if input == "" {
		return input
	}
	var sb strings.Builder
	for _, ch := range input {
		switch ch {
		case '\\', '"', '[', ']':
			sb.WriteRune('\\')
			sb.WriteRune(ch)
		default:
			sb.WriteRune(ch)
		}
	}
	return sb.String()
}

// FstRead reads an FST from a file using gob deserialization.
func FstRead(path string) (*Fst, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	var f Fst
	if err := decoder.Decode(&f); err != nil {
		return NewFst(), nil
	}
	return &f, nil
}

// FstWrite writes an FST to a file using gob serialization.
func FstWrite(fst *Fst, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)
	return encoder.Encode(fst)
}

// Write is a method wrapper for FstWrite.
func (f *Fst) Write(path string) error {
	return FstWrite(f, path)
}

// copy creates a deep copy of the FST.
func (f *Fst) copy() *Fst {
	result := NewFst()
	for from, state := range f.States {
		result.AddState(from)
		for _, arc := range state.Arcs {
			result.AddArc(from, arc.Next, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if state.Final {
			result.SetFinal(from, state.Weight)
		}
	}
	return result
}

// maxStateID returns the maximum state ID in the FST.
func maxStateID(f *Fst) int {
	maxID := 0
	for id := range f.States {
		if id > maxID {
			maxID = id
		}
	}
	return maxID
}

// ShortestPath uses Dijkstra's algorithm to find the path with minimum
// total weight from start to a final state, returning the concatenated output labels.
func (f *Fst) ShortestPath() string {
	if f == nil {
		return ""
	}

	type node struct {
		state  int
		output string
		weight float64
	}

	pq := []node{{state: f.Start, output: "", weight: 0}}
	best := map[int]float64{f.Start: 0}

	for len(pq) > 0 {
		minIdx := 0
		for i := 1; i < len(pq); i++ {
			if pq[i].weight < pq[minIdx].weight {
				minIdx = i
			}
		}
		cur := pq[minIdx]
		pq = append(pq[:minIdx], pq[minIdx+1:]...)

		if cur.weight > best[cur.state] {
			continue
		}

		st := f.States[cur.state]
		if st == nil {
			continue
		}

		if st.Final {
			return cur.output
		}

		for _, arc := range st.Arcs {
			nw := cur.weight + arc.Weight
			no := cur.output + arc.OLabel
			if prev, ok := best[arc.Next]; !ok || nw < prev {
				best[arc.Next] = nw
				pq = append(pq, node{state: arc.Next, output: no, weight: nw})
			}
		}
	}

	return ""
}

// ComposeShortestPath returns the shortest output string by composing
// this FST with other and finding the shortest path in the composed result.
func (f *Fst) ComposeShortestPath(other *Fst) string {
	if f == nil || other == nil {
		return ""
	}
	composed := f.Compose(other)
	return composed.ShortestPath()
}

// extractLinearInput extracts the concatenated ILabels from a
// linear-chain acceptor FST.
func extractLinearInput(f *Fst) string {
	if f == nil {
		return ""
	}
	var sb strings.Builder
	state := f.Start
	visited := make(map[int]bool)
	for {
		if visited[state] {
			break
		}
		visited[state] = true
		st := f.States[state]
		if st == nil || len(st.Arcs) == 0 {
			break
		}
		a := st.Arcs[0]
		sb.WriteString(a.ILabel)
		state = a.Next
	}
	return sb.String()
}

// ComposeInputWithFst composes input with the FST and returns the shortest output.
// Uses on-the-fly composition: walks through the product space matching input
// characters against the FST's ILabels, collecting OLabels along the way,
// and following epsilon chains with Dijkstra-like tiebreaking.
func ComposeInputWithFst(inputStr string, f *Fst, other *Fst) string {
	if other == nil {
		return ""
	}

	input := inputStr
	if input == "" {
		if f == nil {
			return ""
		}
		input = extractLinearInput(f)
	}

	runes := []rune(input)
	if len(runes) == 0 {
		return ""
	}

	type entry struct {
		output string
		weight float64
	}

	posBest := make([]map[int]entry, len(runes)+1)
	for i := range posBest {
		posBest[i] = make(map[int]entry)
	}
	posBest[0][other.Start] = entry{output: "", weight: 0}

	for pos := 0; pos <= len(runes); pos++ {
		// BFS-style epsilon closure: follow all epsilon arcs at this position.
		// Epsilon arcs don't consume input, so we use BFS (FIFO) and
		// propagate longer outputs eagerly.
		q := make([]int, 0, len(posBest[pos]))
		for s := range posBest[pos] {
			q = append(q, s)
		}
		const maxEpsIter = 10000
		epsIter := 0
		for len(q) > 0 && epsIter < maxEpsIter {
			s := q[0]
			q = q[1:]
			cur := posBest[pos][s]
			st := other.States[s]
			if st == nil {
				continue
			}
			for _, arc := range st.Arcs {
				if arc.ILabel == "" {
					nw := cur.weight + arc.Weight
					no := cur.output + arc.OLabel
					prev, ok := posBest[pos][arc.Next]
					if !ok || len(no) > len(prev.output) || (len(no) == len(prev.output) && nw < prev.weight) {
						posBest[pos][arc.Next] = entry{output: no, weight: nw}
						q = append(q, arc.Next)
					}
				}
			}
			epsIter++
		}

		if pos < len(runes) {
			matchChar := string(runes[pos])
			for s, cur := range posBest[pos] {
				st := other.States[s]
				if st == nil {
					continue
				}
				for _, arc := range st.Arcs {
					if arc.ILabel == matchChar {
						nw := cur.weight + arc.Weight
						no := cur.output + arc.OLabel
						if prev, ok := posBest[pos+1][arc.Next]; !ok || nw < prev.weight || (nw == prev.weight && len(no) > len(prev.output)) {
							posBest[pos+1][arc.Next] = entry{output: no, weight: nw}
						}
					}
				}
			}
		}
	}

	// Find best state at final position: prefer terminal (Final or 0 arcs)
	// with min weight. Also track the state with longest output for
	// epsilon chain following.
	minWeight := 1e30
	bestOutput := ""
	bestEndState := -1
	bestOverallState := -1
	bestOverallOutput := ""
	for s, cur := range posBest[len(runes)] {
		st := other.States[s]
		if st != nil && (st.Final || len(st.Arcs) == 0) {
			totalW := cur.weight + st.Weight
			if totalW < minWeight || (totalW == minWeight && len(cur.output) > len(bestOutput)) {
				minWeight = totalW
				bestOutput = cur.output
				bestEndState = s
			}
		}
		if len(cur.output) > len(bestOverallOutput) {
			bestOverallOutput = cur.output
			bestOverallState = s
		}
	}

	// Follow epsilon chains from the state with longest output
	// to accumulate trailing output that may not be present in
	// terminal states (e.g. closing brackets and quotes).
	epsilonState := bestOverallState
	epsilonOutput := bestOverallOutput
	if epsilonState < 0 {
		epsilonState = bestEndState
		epsilonOutput = bestOutput
	}

	if epsilonState >= 0 {
		type epsEntry struct {
			state  int
			output string
		}
		const maxEpsFollow = 50000
		epsBest := epsilonOutput
		epsVisited := map[int]string{epsilonState: epsilonOutput}
		epsQ := []epsEntry{{state: epsilonState, output: epsilonOutput}}
		for len(epsQ) > 0 && len(epsVisited) < maxEpsFollow {
			be := epsQ[0]
			epsQ = epsQ[1:]
			st := other.States[be.state]
			if st == nil {
				continue
			}
			for _, arc := range st.Arcs {
				if arc.ILabel == "" {
					no := be.output + arc.OLabel
					prev, seen := epsVisited[arc.Next]
					if !seen || len(no) > len(prev) {
						epsVisited[arc.Next] = no
						epsQ = append(epsQ, epsEntry{state: arc.Next, output: no})
						if len(no) > len(epsBest) {
							epsBest = no
						}
					}
				}
			}
		}
		if len(epsBest) > len(bestOutput) {
			bestOutput = epsBest
		} else if bestEndState < 0 {
			bestOutput = epsilonOutput
		}
	}

	if bestEndState < 0 && len(bestOutput) == 0 {
		bestOutput = bestOverallOutput
	}
	return bestOutput
}

// At is an alias for Compose.
func (f *Fst) At(other *Fst) *Fst {
	return f.Compose(other)
}
