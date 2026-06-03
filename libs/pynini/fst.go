package pynini

import (
	"encoding/gob"
	"os"
	"strings"
)

// =============================================================================
// FST Property bits (OpenFST-compatible)
// =============================================================================

// Binary properties
const (
	propExpanded uint64 = 0x0000000000000001
	propMutable  uint64 = 0x0000000000000002
	propError    uint64 = 0x0000000000000004
)

// Trinary properties (positive bit at odd/lower position, negative at even/higher)
const (
	propAcceptor          uint64 = 0x0000000000010000
	propNotAcceptor       uint64 = 0x0000000000020000
	propIDeterministic    uint64 = 0x0000000000040000
	propNonIDeterministic uint64 = 0x0000000000080000
	propODeterministic    uint64 = 0x0000000000100000
	propNonODeterministic uint64 = 0x0000000000200000
	propIEpsilons         uint64 = 0x0000000001000000
	propNoIEpsilons       uint64 = 0x0000000002000000
	propOEpsilons         uint64 = 0x0000000004000000
	propNoOEpsilons       uint64 = 0x0000000008000000
	propILabelSorted      uint64 = 0x0000000010000000
	propNotILabelSorted   uint64 = 0x0000000020000000
	propOLabelSorted      uint64 = 0x0000000040000000
	propNotOLabelSorted   uint64 = 0x0000000080000000
	propAccessible        uint64 = 0x0000000100000000
	propNotAccessible     uint64 = 0x0000000200000000
	propCoAccessible      uint64 = 0x0000000400000000
	propNotCoAccessible   uint64 = 0x0000000800000000
)

// =============================================================================
// Core Data Structures
// =============================================================================

// Arc represents a transition in the FST. Labels are int32 IDs from the
// SymbolTable (0 = epsilon). Stored by value for cache efficiency and
// to avoid per-arc heap allocations.
type Arc struct {
	ILabel int32
	OLabel int32
	Weight float32
	Next   int32
}

// State represents a state in the FST. Arcs are stored by value in a slice.
// Epsilon counts are tracked for O(1) epsilon presence checks.
type State struct {
	Final   bool
	Weight  float32
	Arcs    []Arc
	NumIEps int32
	NumOEps int32
}

// Fst represents a finite-state transducer.
// States are stored in a dense slice (index = state ID) for O(1) access
// and contiguous memory layout. Labels are int32 IDs from the SymbolTable.
type Fst struct {
	States  []State
	Start   int32
	Symbols *SymbolTable
	Props   uint64
}

// =============================================================================
// State/Arc helpers
// =============================================================================

// HasIEpsilons returns true if the state has any input epsilon arcs.
func (s *State) HasIEpsilons() bool { return s.NumIEps > 0 }

// HasOEpsilons returns true if the state has any output epsilon arcs.
func (s *State) HasOEpsilons() bool { return s.NumOEps > 0 }

// AddArc adds an arc to the state, tracking epsilon counts.
func (s *State) AddArc(arc Arc) {
	if arc.ILabel == EpsilonLabel {
		s.NumIEps++
	}
	if arc.OLabel == EpsilonLabel {
		s.NumOEps++
	}
	s.Arcs = append(s.Arcs, arc)
}

// =============================================================================
// FST Construction
// =============================================================================

// NewFst creates a new empty FST with a start state (state 0).
func NewFst() *Fst {
	f := &Fst{
		States:  make([]State, 1),
		Start:   0,
		Symbols: NewSymbolTable(),
		Props:   propExpanded | propMutable | propAcceptor | propIDeterministic | propODeterministic | propNoIEpsilons | propNoOEpsilons | propILabelSorted | propOLabelSorted | propAccessible | propCoAccessible,
	}
	return f
}

// AddState adds a new empty state and returns its ID.
func (f *Fst) AddState() int32 {
	id := int32(len(f.States))
	f.States = append(f.States, State{})
	f.Props &= ^(propCoAccessible | propNotCoAccessible) // invalidate coaccessibility
	return id
}

// AddArc adds an arc from state `from` to state `to` with given labels and weight.
// The labels are int32 IDs from the symbol table.
func (f *Fst) AddArc(from, to int32, ilabel, olabel int32, weight float32) {
	// Ensure target state exists
	for int(to) >= len(f.States) {
		f.AddState()
	}
	f.States[from].AddArc(Arc{
		ILabel: ilabel,
		OLabel: olabel,
		Weight: weight,
		Next:   to,
	})
	// Update properties
	if ilabel != olabel {
		f.Props &= ^(propAcceptor | propNotAcceptor)
		f.Props |= propNotAcceptor
	}
	if ilabel == EpsilonLabel {
		f.Props &= ^(propNoIEpsilons | propIEpsilons)
		f.Props |= propIEpsilons
	}
	if olabel == EpsilonLabel {
		f.Props &= ^(propNoOEpsilons | propOEpsilons)
		f.Props |= propOEpsilons
	}
	f.Props &= ^(propILabelSorted | propNotILabelSorted | propOLabelSorted | propNotOLabelSorted | propIDeterministic | propNonIDeterministic | propODeterministic | propNonODeterministic)
}

// SetFinal marks a state as final with the given weight.
func (f *Fst) SetFinal(state int32, weight float32) {
	for int(state) >= len(f.States) {
		f.AddState()
	}
	f.States[state].Final = true
	f.States[state].Weight = weight
}

// AddArcStr is a convenience method that adds an arc with string labels,
// converting them to int32 IDs via the symbol table.
func (f *Fst) AddArcStr(from, to int32, ilabel, olabel string, weight float32) {
	il := f.Symbols.FindOrAdd(ilabel)
	ol := f.Symbols.FindOrAdd(olabel)
	f.AddArc(from, to, il, ol, weight)
}

// NumStates returns the number of states.
func (f *Fst) NumStates() int { return len(f.States) }

// =============================================================================
// String-based API (backward compatible with Python pynini)
// =============================================================================

// Accep creates a linear-chain acceptor FST for the given string.
// The input and output labels are identical.
func Accep(s string) *Fst {
	f := NewFst()
	if s == "" {
		f.States[0].Final = true
		return f
	}
	runes := []rune(s)
	stateID := int32(1)
	for _, ch := range runes {
		chStr := string(ch)
		label := f.Symbols.FindOrAdd(chStr)
		f.AddState()
		f.AddArc(stateID-1, stateID, label, label, 0)
		stateID++
	}
	f.SetFinal(stateID-1, 0)
	return f
}

// Cross creates a mapping transducer where input labels are from a and output from b.
func Cross(a, b interface{}) *Fst {
	// Handle FST operands directly (for Delete/Insert on non-linear FSTs)
	aFst, aIsFst := a.(*Fst)
	bFst, bIsFst := b.(*Fst)

	if aIsFst && bIsFst {
		// Cross(fst1, fst2): project and compose
		aProj := Project(aFst, "input")
		bProj := Project(bFst, "output")
		return aProj.Compose(bProj)
	}

	if aIsFst {
		// Cross(fst, str): map input labels of fst to output string
		// If bStr is "", this is Delete(fst) - map input to epsilon
		bStr := toString(b)
		if bStr == "" {
			// Delete: copy the FST and set all OLabels to epsilon
			return withOutputEpsilon(aFst)
		}
		// Cross(fst, non-empty str): use string representation
		aStr := strFromFst(aFst)
		return crossFromStrings(aStr, bStr)
	}

	if bIsFst {
		// Cross(str, fst): map input string to output labels of fst
		aStr := toString(a)
		if aStr == "" {
			// Insert: copy the FST and set all ILabels to epsilon
			return withInputEpsilon(bFst)
		}
		bStr := strFromFst(bFst)
		return crossFromStrings(aStr, bStr)
	}

	// Both are strings (or neither is FST)
	aStr := toString(a)
	bStr := toString(b)
	if aStr == "" && bStr == "" {
		f := NewFst()
		f.States[0].Final = true
		return f
	}
	return crossFromStrings(aStr, bStr)
}

// withOutputEpsilon returns a copy of fst with all OLabels set to epsilon.
// Used by Delete(fst) to map input labels to epsilon output.
func withOutputEpsilon(fst *Fst) *Fst {
	if fst == nil {
		return NewFst()
	}
	result := fst.copy()
	for i := range result.States {
		for j := range result.States[i].Arcs {
			result.States[i].Arcs[j].OLabel = EpsilonLabel
		}
		result.States[i].NumOEps = int32(len(result.States[i].Arcs))
	}
	return result
}

// withInputEpsilon returns a copy of fst with all ILabels set to epsilon.
// Used by Insert(str) when str is an FST.
func withInputEpsilon(fst *Fst) *Fst {
	if fst == nil {
		return NewFst()
	}
	result := fst.copy()
	for i := range result.States {
		for j := range result.States[i].Arcs {
			result.States[i].Arcs[j].ILabel = EpsilonLabel
		}
		result.States[i].NumIEps = int32(len(result.States[i].Arcs))
	}
	return result
}

func toString(label interface{}) string {
	switch v := label.(type) {
	case string:
		return v
	case *Fst:
		return strFromFst(v)
	default:
		return ""
	}
}

func strFromFst(f *Fst) string {
	if f == nil {
		return ""
	}
	visited := make(map[int32]bool)
	var sb strings.Builder
	state := int32(f.Start)
	for {
		if visited[state] {
			break
		}
		visited[state] = true
		if int(state) >= len(f.States) {
			break
		}
		st := &f.States[state]
		if len(st.Arcs) == 0 {
			break
		}
		found := false
		for _, arc := range st.Arcs {
			if arc.ILabel != EpsilonLabel {
				sb.WriteString(f.Symbols.Symbol(arc.ILabel))
				state = arc.Next
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	return sb.String()
}

func crossFromStrings(aStr, bStr string) *Fst {
	f := NewFst()
	aRunes := []rune(aStr)
	bRunes := []rune(bStr)
	maxLen := len(aRunes)
	if len(bRunes) > maxLen {
		maxLen = len(bRunes)
	}
	stateID := int32(1)
	for i := 0; i < maxLen; i++ {
		f.AddState()
		var aLabel, bLabel int32 = EpsilonLabel, EpsilonLabel
		if i < len(aRunes) {
			aLabel = f.Symbols.FindOrAdd(string(aRunes[i]))
		}
		if i < len(bRunes) {
			bLabel = f.Symbols.FindOrAdd(string(bRunes[i]))
		}
		f.AddArc(stateID-1, stateID, aLabel, bLabel, 0)
		stateID++
	}
	f.SetFinal(stateID-1, 0)
	return f
}

// CrossFst is a variant of Cross for FST operands.
func CrossFst(a, b *Fst) *Fst {
	if a == nil || b == nil {
		return NewFst()
	}
	aProj := Project(a, "input")
	bProj := Project(b, "output")
	return aProj.Compose(bProj)
}

// FstAccep creates an acceptor from an FST (projects to input side).
func FstAccep(f *Fst) *Fst {
	if f == nil {
		return NewFst()
	}
	return Project(f, "input")
}

// =============================================================================
// FST Operations
// =============================================================================

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
// Uses int label matching for O(1) comparison instead of string matching.
func (f *Fst) Compose(other *Fst) *Fst {
	if f == nil || other == nil {
		return NewFst()
	}

	// Merge symbol tables so both FSTs share the same label IDs
	result := NewFst()
	// Copy f's symbols in order (using idToSym slice for deterministic ordering)
	for _, sym := range f.Symbols.idToSym {
		result.Symbols.FindOrAdd(sym)
	}
	// Merge other's symbols, getting the mapping from old IDs to new IDs
	otherMapping := result.Symbols.Merge(other.Symbols)

	type pair struct{ s1, s2 int32 }
	startPair := pair{s1: f.Start, s2: other.Start}

	queue := []pair{startPair}
	visited := make(map[pair]int32)
	visited[startPair] = 0
	nextID := int32(1)

	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		resultStateID := visited[p]

		if int(p.s1) >= len(f.States) || int(p.s2) >= len(other.States) {
			continue
		}
		s1 := &f.States[p.s1]
		s2 := &other.States[p.s2]

		if s1.Final && s2.Final {
			result.SetFinal(resultStateID, s1.Weight+s2.Weight)
		}

		// Build a map of s2 arcs by input label for O(1) lookup
		s2ByILabel := make(map[int32][]Arc)
		for _, arc := range s2.Arcs {
			mappedLabel := otherMapping[arc.ILabel]
			s2ByILabel[mappedLabel] = append(s2ByILabel[mappedLabel], arc)
		}

		// Process s1 arcs
		for _, a1 := range s1.Arcs {
			if arcs, ok := s2ByILabel[a1.OLabel]; ok {
				for _, a2 := range arcs {
					np := pair{s1: a1.Next, s2: a2.Next}
					npID, seen := visited[np]
					if !seen {
						npID = nextID
						nextID++
						visited[np] = npID
						result.AddState()
						queue = append(queue, np)
					}
					mappedOLabel := otherMapping[a2.OLabel]
					result.AddArc(resultStateID, npID, a1.ILabel, mappedOLabel, a1.Weight+a2.Weight)
				}
			}
			// Epsilon output on a1: stay in same s2 state
			if a1.OLabel == EpsilonLabel {
				np := pair{s1: a1.Next, s2: p.s2}
				npID, seen := visited[np]
				if !seen {
					npID = nextID
					nextID++
					visited[np] = npID
					result.AddState()
					queue = append(queue, np)
				}
				result.AddArc(resultStateID, npID, a1.ILabel, EpsilonLabel, a1.Weight)
			}
		}

		// Process s2 epsilon input arcs
		for _, a2 := range s2.Arcs {
			if a2.ILabel == EpsilonLabel {
				np := pair{s1: p.s1, s2: a2.Next}
				npID, seen := visited[np]
				if !seen {
					npID = nextID
					nextID++
					visited[np] = npID
					result.AddState()
					queue = append(queue, np)
				}
				mappedOLabel := otherMapping[a2.OLabel]
				result.AddArc(resultStateID, npID, EpsilonLabel, mappedOLabel, a2.Weight)
			}
		}
	}

	return result
}

// Concat concatenates this FST with another.
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
	// Merge other's symbols
	otherMapping := result.Symbols.Merge(other.Symbols)
	offset := int32(result.NumStates())

	// Copy other's states with offset.
	// Use ensureState to avoid duplicating states already created by AddArc.
	for i := range other.States {
		newFrom := int32(i) + offset
		ensureState(result, newFrom)
		for _, arc := range other.States[i].Arcs {
			mappedILabel := otherMapping[arc.ILabel]
			mappedOLabel := otherMapping[arc.OLabel]
			result.AddArc(newFrom, arc.Next+offset, mappedILabel, mappedOLabel, arc.Weight)
		}
		if other.States[i].Final {
			result.SetFinal(newFrom, other.States[i].Weight)
		}
	}

	// Connect f's final states to other's start (with offset)
	for sID := int32(0); sID < offset; sID++ {
		st := &result.States[sID]
		if st.Final {
			st.Final = false
			st.Weight = 0
			result.AddArc(sID, other.Start+offset, EpsilonLabel, EpsilonLabel, 0)
		}
	}

	return result
}

// Union computes the union of this FST with another.
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
	newStart := int32(0)

	// Merge f's symbols
	result.Symbols.Merge(f.Symbols)
	// Copy f with offset.
	// Use ensureState to avoid duplicating states already created by AddArc.
	fOffset := int32(1)
	for i := range f.States {
		from := int32(i) + fOffset
		ensureState(result, from)
		for _, arc := range f.States[i].Arcs {
			result.AddArc(from, arc.Next+fOffset, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if f.States[i].Final {
			result.SetFinal(from, f.States[i].Weight)
		}
	}
	nextID := int32(result.NumStates())

	// Merge other's symbols and copy
	otherMapping := result.Symbols.Merge(other.Symbols)
	oOffset := nextID
	for i := range other.States {
		from := int32(i) + oOffset
		ensureState(result, from)
		for _, arc := range other.States[i].Arcs {
			mappedILabel := otherMapping[arc.ILabel]
			mappedOLabel := otherMapping[arc.OLabel]
			result.AddArc(from, arc.Next+oOffset, mappedILabel, mappedOLabel, arc.Weight)
		}
		if other.States[i].Final {
			result.SetFinal(from, other.States[i].Weight)
		}
	}

	// Add epsilon arcs from new start to each original start
	result.AddArc(newStart, f.Start+fOffset, EpsilonLabel, EpsilonLabel, 0)
	result.AddArc(newStart, other.Start+oOffset, EpsilonLabel, EpsilonLabel, 0)

	return result
}

// Star computes the Kleene star of this FST (zero or more repetitions).
func (f *Fst) Star() *Fst {
	if f == nil {
		return NewFst()
	}
	return f.closure(0, -1)
}

// Plus computes the positive closure (one or more repetitions).
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

// Ques computes the optional closure (0 or 1).
func (f *Fst) Ques() *Fst {
	if f == nil {
		return NewFst()
	}
	return f.closure(0, 1)
}

// Repeat creates an FST that accepts exactly n repetitions.
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
func (f *Fst) closure(min, max int) *Fst {
	result := f.copy()
	// Copy f with offset 1 (state 0 is new start)
	// Shift all states by 1
	oldStates := result.States
	result.States = make([]State, 1)
	result.States[0] = State{} // new start state
	for _, st := range oldStates {
		result.States = append(result.States, st)
	}
	// Adjust all arc Next pointers
	for i := 1; i < len(result.States); i++ {
		for j := range result.States[i].Arcs {
			result.States[i].Arcs[j].Next++
		}
	}
	// Adjust Start
	oldStart := f.Start
	if oldStart != -1 {
		result.AddArc(0, oldStart+1, EpsilonLabel, EpsilonLabel, 0)
	}

	// Add epsilon from each final state back to the start of copied f
	// Only add back-loop when unlimited repetition is allowed (max < 0 or max > 1).
	// For Ques() where max=1, no back-loop is needed (at most one repetition).
	if max < 0 || max > 1 {
		for sID := int32(1); sID < int32(len(result.States)); sID++ {
			if result.States[sID].Final {
				result.AddArc(sID, oldStart+1, EpsilonLabel, EpsilonLabel, 0)
			}
		}
	}

	// If min == 0, start state is also final
	if min == 0 {
		result.SetFinal(0, 0)
	}

	// If max is 0, return empty FST
	if max == 0 {
		return NewFst()
	}

	return result
}

// Closure implements bounded closure with min and max.
func (f *Fst) Closure(min, max int) *Fst {
	return f.closure(min, max)
}

// At is an alias for Compose.
func (f *Fst) At(other *Fst) *Fst {
	return f.Compose(other)
}

// =============================================================================
// Project, Invert, Difference
// =============================================================================

// Invert swaps the input and output labels of all arcs.
func (f *Fst) Invert() *Fst {
	if f == nil {
		return NewFst()
	}
	return Invert(f)
}

// Invert (package-level) creates a new FST with swapped labels.
func Invert(fst *Fst) *Fst {
	if fst == nil {
		return NewFst()
	}
	result := fst.copy()
	for i := range result.States {
		for j := range result.States[i].Arcs {
			result.States[i].Arcs[j].ILabel, result.States[i].Arcs[j].OLabel = result.States[i].Arcs[j].OLabel, result.States[i].Arcs[j].ILabel
		}
		// Swap epsilon counts since ILabel/OLabel are swapped
		result.States[i].NumIEps, result.States[i].NumOEps = result.States[i].NumOEps, result.States[i].NumIEps
	}
	return result
}

// Project creates an FST with only the specified side (input or output).
func Project(fst *Fst, side string) *Fst {
	if fst == nil {
		return NewFst()
	}
	result := fst.copy()
	isInput := side == "input" || side == "i"
	for i := range result.States {
		for j := range result.States[i].Arcs {
			if isInput {
				result.States[i].Arcs[j].OLabel = result.States[i].Arcs[j].ILabel
			} else {
				result.States[i].Arcs[j].ILabel = result.States[i].Arcs[j].OLabel
			}
		}
	}
	result.Props |= propAcceptor
	result.Props &= ^propNotAcceptor
	return result
}

// Difference computes the difference of this FST and another.
func (f *Fst) Difference(other *Fst) *Fst {
	if f == nil || other == nil {
		if f == nil {
			return NewFst()
		}
		return f.copy()
	}

	// Check if other is a char union for optimized difference
	if isCharUnion(other) {
		return charUnionDifference(f, other)
	}

	return f.copy()
}

// isCharUnion checks if an FST is a simple character union.
func isCharUnion(f *Fst) bool {
	if f == nil || f.Start != 0 || len(f.States) == 0 {
		return false
	}
	startState := &f.States[0]
	for _, arc := range startState.Arcs {
		sym := f.Symbols.Symbol(arc.ILabel)
		if len([]rune(sym)) != 1 {
			return false
		}
		if int(arc.Next) >= len(f.States) {
			return false
		}
		nextState := &f.States[arc.Next]
		if !nextState.Final || len(nextState.Arcs) != 0 {
			return false
		}
	}
	return len(startState.Arcs) > 0
}

// charUnionDifference implements optimized difference for character class FSTs.
func charUnionDifference(f, other *Fst) *Fst {
	// Collect the set of character strings to exclude (using strings, not label IDs,
	// since the two FSTs may have different symbol tables).
	exclude := make(map[string]bool)
	for _, arc := range other.States[0].Arcs {
		exclude[other.Symbols.Symbol(arc.ILabel)] = true
	}

	result := f.copy()
	// Filter arcs from start state by comparing symbol strings
	newArcs := make([]Arc, 0, len(result.States[0].Arcs))
	removedIEps := int32(0)
	removedOEps := int32(0)
	for _, arc := range result.States[0].Arcs {
		sym := result.Symbols.Symbol(arc.ILabel)
		if !exclude[sym] {
			newArcs = append(newArcs, arc)
		} else {
			if arc.ILabel == EpsilonLabel {
				removedIEps++
			}
			if arc.OLabel == EpsilonLabel {
				removedOEps++
			}
		}
	}
	result.States[0].Arcs = newArcs
	result.States[0].NumIEps -= removedIEps
	result.States[0].NumOEps -= removedOEps
	return result
}

// =============================================================================
// Cdrewrite
// =============================================================================

// Cdrewrite creates a context-dependent rewrite rule transducer.
// sigma should already be a starred alphabet (e.g. VSIGMA = VCHAR.Star()).
//
// Builds: sigma* concat L concat phi concat R concat sigma*
// in a single pass to avoid the explosion from chaining Concat which
// copies the large sigma* FST 5+ times.
//
// The sigma* copies have a small weight penalty on character-consuming arcs
// to ensure the rule (phi) is preferred over consuming input via sigma*.
// This matches the C++ Cdrewrite semantics where the rule application path
// should win when it can match.
func Cdrewrite(fst *Fst, l, r string, sigma *Fst) *Fst {
	if fst == nil || sigma == nil {
		return NewFst()
	}

	// Build the core: Accep(l) concat fst concat Accep(r)
	// These are small FSTs so chaining Concat is cheap.
	left := Accep(l)
	right := Accep(r)
	core := left.Concat(fst).Concat(right)

	// Build result: sigma concat core concat sigma — in one pass.
	// We use a small weight penalty on sigma's character arcs so that
	// the rule application path (epsilon-to-core) has lower total weight
	// than the path where sigma* consumes characters and the rule
	// falls through to a default (e.g., Insert("th")).
	const sigmaArcPenalty = 0.0001

	// Step 1: copy sigma with weighted character arcs
	sigma1 := sigma.copy()
	for i := range sigma1.States {
		for j := range sigma1.States[i].Arcs {
			if sigma1.States[i].Arcs[j].ILabel != EpsilonLabel {
				sigma1.States[i].Arcs[j].Weight += sigmaArcPenalty
			}
		}
	}
	result := sigma1
	sigmaOffset := int32(result.NumStates())

	// Step 2: add core states with offset
	coreMapping := result.Symbols.Merge(core.Symbols)
	for i := range core.States {
		newFrom := int32(i) + sigmaOffset
		ensureState(result, newFrom)
		for _, arc := range core.States[i].Arcs {
			result.AddArc(newFrom, arc.Next+sigmaOffset,
				coreMapping[arc.ILabel], coreMapping[arc.OLabel], arc.Weight)
		}
		if core.States[i].Final {
			result.SetFinal(newFrom, core.States[i].Weight)
		}
	}

	// Step 3: connect sigma's final states to core's start
	for sID := int32(0); sID < sigmaOffset; sID++ {
		if result.States[sID].Final {
			result.States[sID].Final = false
			result.AddArc(sID, core.Start+sigmaOffset, EpsilonLabel, EpsilonLabel, 0)
		}
	}

	// Step 4: add sigma states again at the end (second sigma* in cdrewrite),
	// also with weighted character arcs
	sigma2 := sigma.copy()
	for i := range sigma2.States {
		for j := range sigma2.States[i].Arcs {
			if sigma2.States[i].Arcs[j].ILabel != EpsilonLabel {
				sigma2.States[i].Arcs[j].Weight += sigmaArcPenalty
			}
		}
	}
	coreOffset := int32(result.NumStates())
	for i := range sigma2.States {
		newFrom := int32(i) + coreOffset
		ensureState(result, newFrom)
		for _, arc := range sigma2.States[i].Arcs {
			result.AddArc(newFrom, arc.Next+coreOffset, arc.ILabel, arc.OLabel, arc.Weight)
		}
		if sigma2.States[i].Final {
			result.SetFinal(newFrom, sigma2.States[i].Weight)
		}
	}

	// Step 5: connect core's final states to sigma's start
	for sID := sigmaOffset; sID < coreOffset; sID++ {
		if result.States[sID].Final {
			result.States[sID].Final = false
			result.AddArc(sID, sigma2.Start+coreOffset, EpsilonLabel, EpsilonLabel, 0)
		}
	}

	return result
}

// BuildRule is a convenience wrapper for Cdrewrite.
func BuildRule(fst, sigma *Fst, l, r string) *Fst {
	if l == "" && r == "" {
		return Cdrewrite(fst, "", "", sigma)
	}
	return Cdrewrite(fst, l, r, sigma)
}

// =============================================================================
// Escape and String Utilities
// =============================================================================

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

// =============================================================================
// Serialization
// =============================================================================

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

// =============================================================================
// Internal Helpers
// =============================================================================

// ensureState ensures that the FST has at least from+1 states.
// Unlike AddState, it only adds states when necessary.
func ensureState(f *Fst, from int32) {
	for int(from) >= len(f.States) {
		f.AddState()
	}
}

// copy creates a deep copy of the FST.
func (f *Fst) copy() *Fst {
	result := &Fst{
		States:  make([]State, len(f.States)),
		Start:   f.Start,
		Symbols: f.Symbols.Copy(),
		Props:   f.Props,
	}
	for i := range f.States {
		result.States[i] = State{
			Final:   f.States[i].Final,
			Weight:  f.States[i].Weight,
			NumIEps: f.States[i].NumIEps,
			NumOEps: f.States[i].NumOEps,
		}
		if len(f.States[i].Arcs) > 0 {
			result.States[i].Arcs = make([]Arc, len(f.States[i].Arcs))
			copy(result.States[i].Arcs, f.States[i].Arcs)
		}
	}
	return result
}

// maxStateID returns the maximum state ID in the FST.
func maxStateID(f *Fst) int32 {
	return int32(len(f.States) - 1)
}

// =============================================================================
// ShortestPath and ComposeShortestPath
// =============================================================================

// ShortestPath uses Dijkstra's algorithm to find the path with minimum
// total weight from start to a final state, returning concatenated output labels.
func (f *Fst) ShortestPath() string {
	if f == nil {
		return ""
	}

	type node struct {
		state  int32
		output string
		weight float32
	}

	pq := []node{{state: f.Start, output: "", weight: 0}}
	best := map[int32]float32{f.Start: 0}

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

		if int(cur.state) >= len(f.States) {
			continue
		}
		st := &f.States[cur.state]

		if st.Final {
			return cur.output
		}

		for _, arc := range st.Arcs {
			nw := cur.weight + arc.Weight
			no := cur.output + f.Symbols.Symbol(arc.OLabel)
			if prev, ok := best[arc.Next]; !ok || nw < prev {
				best[arc.Next] = nw
				pq = append(pq, node{state: arc.Next, output: no, weight: nw})
			}
		}
	}

	return ""
}

// ComposeInputWithFst composes input with the FST and returns the shortest output.
// Uses on-the-fly composition with int label matching.
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

	type entry struct {
		output string
		weight float32
	}

	posBest := make([]map[int32]entry, len(runes)+1)
	for i := range posBest {
		posBest[i] = make(map[int32]entry)
	}
	posBest[0][other.Start] = entry{output: "", weight: 0}

	for pos := 0; pos <= len(runes); pos++ {
		// Epsilon closure: follow all epsilon arcs at this position
		q := make([]int32, 0, len(posBest[pos]))
		for s := range posBest[pos] {
			q = append(q, s)
		}
		const maxEpsIter = 10000
		epsIter := 0
		for len(q) > 0 && epsIter < maxEpsIter {
			s := q[0]
			q = q[1:]
			cur := posBest[pos][s]
			if int(s) >= len(other.States) {
				continue
			}
			st := &other.States[s]
			if !st.HasIEpsilons() {
				continue
			}
			for _, arc := range st.Arcs {
				if arc.ILabel == EpsilonLabel {
					nw := cur.weight + arc.Weight
					no := cur.output + other.Symbols.Symbol(arc.OLabel)
					prev, ok := posBest[pos][arc.Next]
					if !ok || nw < prev.weight || (nw == prev.weight && len(no) > len(prev.output)) {
						posBest[pos][arc.Next] = entry{output: no, weight: nw}
						q = append(q, arc.Next)
					}
				}
			}
			epsIter++
		}

		if pos < len(runes) {
			matchLabel := other.Symbols.Find(string(runes[pos]))
			if matchLabel < 0 {
				continue
			}
			for s, cur := range posBest[pos] {
				if int(s) >= len(other.States) {
					continue
				}
				st := &other.States[s]
				for _, arc := range st.Arcs {
					if arc.ILabel == matchLabel {
						nw := cur.weight + arc.Weight
						no := cur.output + other.Symbols.Symbol(arc.OLabel)
						if prev, ok := posBest[pos+1][arc.Next]; !ok || nw < prev.weight || (nw == prev.weight && len(no) > len(prev.output)) {
							posBest[pos+1][arc.Next] = entry{output: no, weight: nw}
						}
					}
				}
			}
		}
	}

	// Find best final state at the end position.
	// Only consider states with Final=true as valid end states.
	// States with len(Arcs)==0 but Final=false are dead ends (partial matches)
	// and should not be considered as valid outputs.
	minWeight := float32(1e30)
	bestOutput := ""
	bestEndState := int32(-1)

	for s, cur := range posBest[len(runes)] {
		if int(s) < len(other.States) {
			st := &other.States[s]
			if st.Final {
				totalW := cur.weight + st.Weight
				if totalW < minWeight || (totalW == minWeight && len(cur.output) > len(bestOutput)) {
					minWeight = totalW
					bestOutput = cur.output
					bestEndState = s
				}
			}
		}
	}

	// Follow epsilon chains from the best final state to collect any
	// trailing epsilon output labels (e.g., from Insert operations).
	// Only follow epsilon chains that lead to a final state.
	if bestEndState >= 0 {
		type epsEntry struct {
			state  int32
			output string
		}
		const maxEpsFollow = 50000
		epsVisited := map[int32]string{bestEndState: bestOutput}
		epsQ := []epsEntry{{state: bestEndState, output: bestOutput}}
		for len(epsQ) > 0 && len(epsVisited) < maxEpsFollow {
			be := epsQ[0]
			epsQ = epsQ[1:]
			if int(be.state) >= len(other.States) {
				continue
			}
			st := &other.States[be.state]
			if !st.HasIEpsilons() {
				continue
			}
			for _, arc := range st.Arcs {
				if arc.ILabel == EpsilonLabel {
					no := be.output + other.Symbols.Symbol(arc.OLabel)
					prev, seen := epsVisited[arc.Next]
					if !seen || len(no) > len(prev) {
						epsVisited[arc.Next] = no
						epsQ = append(epsQ, epsEntry{state: arc.Next, output: no})
						// Only update bestOutput if the epsilon chain leads to a final state
						if int(arc.Next) < len(other.States) && other.States[arc.Next].Final {
							if len(no) > len(bestOutput) {
								bestOutput = no
							}
						}
					}
				}
			}
		}
	}

	return bestOutput
}

// ComposePrefixResult holds the result of a prefix composition.
type ComposePrefixResult struct {
	Output   string // output string
	Consumed int    // number of input runes consumed
	Weight   float32
}

// ComposePrefixShortestPath composes the input string with the FST and finds
// the best match that consumes a prefix of the input. Returns the output,
// the number of input characters consumed, and the weight.
func ComposePrefixShortestPath(inputStr string, other *Fst) ComposePrefixResult {
	if other == nil || inputStr == "" {
		return ComposePrefixResult{}
	}

	runes := []rune(inputStr)

	type entry struct {
		output string
		weight float32
	}

	posBest := make([]map[int32]entry, len(runes)+1)
	for i := range posBest {
		posBest[i] = make(map[int32]entry)
	}
	posBest[0][other.Start] = entry{output: "", weight: 0}

	// Track the best final match at each position
	bestConsumed := 0
	bestOutput := ""
	bestWeight := float32(1e30)

	for pos := 0; pos <= len(runes); pos++ {
		// Epsilon closure
		q := make([]int32, 0, len(posBest[pos]))
		for s := range posBest[pos] {
			q = append(q, s)
		}
		const maxEpsIter = 10000
		epsIter := 0
		for len(q) > 0 && epsIter < maxEpsIter {
			s := q[0]
			q = q[1:]
			cur := posBest[pos][s]
			if int(s) >= len(other.States) {
				continue
			}
			st := &other.States[s]
			if !st.HasIEpsilons() {
				continue
			}
			for _, arc := range st.Arcs {
				if arc.ILabel == EpsilonLabel {
					nw := cur.weight + arc.Weight
					no := cur.output + other.Symbols.Symbol(arc.OLabel)
					prev, ok := posBest[pos][arc.Next]
					if !ok || nw < prev.weight {
						posBest[pos][arc.Next] = entry{output: no, weight: nw}
						q = append(q, arc.Next)
					}
				}
			}
			epsIter++
		}

		// Check for final states at this position
		for s, cur := range posBest[pos] {
			if int(s) < len(other.States) && other.States[s].Final {
				totalW := cur.weight + other.States[s].Weight
				// Prefer longer match; for same length, prefer lower weight
				if pos > bestConsumed || (pos == bestConsumed && totalW < bestWeight) {
					bestWeight = totalW
					bestOutput = cur.output
					bestConsumed = pos
				}
			}
		}

		if pos < len(runes) {
			matchLabel := other.Symbols.Find(string(runes[pos]))
			if matchLabel < 0 {
				continue
			}
			for s, cur := range posBest[pos] {
				if int(s) >= len(other.States) {
					continue
				}
				st := &other.States[s]
				for _, arc := range st.Arcs {
					if arc.ILabel == matchLabel {
						nw := cur.weight + arc.Weight
						no := cur.output + other.Symbols.Symbol(arc.OLabel)
						if prev, ok := posBest[pos+1][arc.Next]; !ok || nw < prev.weight {
							posBest[pos+1][arc.Next] = entry{output: no, weight: nw}
						}
					}
				}
			}
		}
	}

	// Follow epsilon chains from the best final state
	if bestConsumed > 0 {
		// Find the best final state at bestConsumed position
		bestState := int32(-1)
		bestStateWeight := float32(1e30)
		for s, cur := range posBest[bestConsumed] {
			if int(s) < len(other.States) && other.States[s].Final {
				totalW := cur.weight + other.States[s].Weight
				if totalW < bestStateWeight {
					bestStateWeight = totalW
					bestState = s
				}
			}
		}
		if bestState >= 0 {
			type epsEntry struct {
				state  int32
				output string
			}
			epsVisited := map[int32]string{bestState: bestOutput}
			epsQ := []epsEntry{{state: bestState, output: bestOutput}}
			for len(epsQ) > 0 && len(epsVisited) < 50000 {
				be := epsQ[0]
				epsQ = epsQ[1:]
				if int(be.state) >= len(other.States) {
					continue
				}
				st := &other.States[be.state]
				if !st.HasIEpsilons() {
					continue
				}
				for _, arc := range st.Arcs {
					if arc.ILabel == EpsilonLabel {
						no := be.output + other.Symbols.Symbol(arc.OLabel)
						prev, seen := epsVisited[arc.Next]
						if !seen || len(no) > len(prev) {
							epsVisited[arc.Next] = no
							epsQ = append(epsQ, epsEntry{state: arc.Next, output: no})
							if int(arc.Next) < len(other.States) && other.States[arc.Next].Final {
								if len(no) > len(bestOutput) {
									bestOutput = no
								}
							}
						}
					}
				}
			}
		}
	}

	return ComposePrefixResult{
		Output:   bestOutput,
		Consumed: bestConsumed,
		Weight:   bestWeight,
	}
}
func (f *Fst) ComposeShortestPath(other *Fst) string {
	if f == nil || other == nil {
		return ""
	}

	input := extractLinearInput(f)
	// Allow empty input (e.g., Accep("")) to compose with other FST
	return ComposeInputWithFst(input, f, other)
}

// extractLinearInput extracts the concatenated ILabels from a linear-chain acceptor.
func extractLinearInput(f *Fst) string {
	if f == nil {
		return ""
	}
	var sb strings.Builder
	state := f.Start
	visited := make(map[int32]bool)
	for {
		if visited[state] {
			break
		}
		visited[state] = true
		if int(state) >= len(f.States) {
			break
		}
		st := &f.States[state]
		if len(st.Arcs) == 0 {
			break
		}
		// Pick the first arc (linear chain assumption)
		a := &st.Arcs[0]
		if a.ILabel != EpsilonLabel {
			sb.WriteString(f.Symbols.Symbol(a.ILabel))
		}
		state = a.Next
	}
	return sb.String()
}

// =============================================================================
// Gob encoding registration
// =============================================================================

func init() {
	gob.Register(&Fst{})
	gob.Register(&SymbolTable{})
}
