package pynini

import (
	"container/heap"
	"encoding/gob"
	"os"
	"strings"
	"sync"
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

// ilabelRangeMap stores precomputed ilabel-to-arc-index mapping for a state.
// For sorted arcs, this allows O(log n) lookup of arcs by ilabel.
type ilabelRangeMap struct {
	labels []int32 // sorted distinct ilabels
	starts []int   // start index in Arcs for each label
	counts []int   // number of arcs for each label
}

// Fst represents a finite-state transducer.
// States are stored in a dense slice (index = state ID) for O(1) access
// and contiguous memory layout. Labels are int32 IDs from the SymbolTable.
type Fst struct {
	States  []State
	Start   int32
	Symbols *SymbolTable
	Props   uint64

	// Compose cache (lazily initialized, not serialized by gob)
	epsArcIdx    [][]int          // epsArcIdx[s] = indices of epsilon arcs from state s (nil if none)
	ilabelRanges []ilabelRangeMap // ilabelRanges[s] = precomputed ilabel mapping
	composeReady bool

	// Rune label cache (lazily initialized, not serialized by gob)
	runeLabelCache map[rune]int32
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

// FindRuneLabel returns the symbol table ID for a rune, using a cache to avoid
// repeated string(r) allocations and map lookups. Returns -1 if not found.
func (f *Fst) FindRuneLabel(r rune) int32 {
	if f.runeLabelCache == nil {
		f.runeLabelCache = make(map[rune]int32, 256)
	}
	if id, ok := f.runeLabelCache[r]; ok {
		return id
	}
	id := f.Symbols.Find(string(r))
	f.runeLabelCache[r] = id
	return id
}

// ComputeInputLabels computes the symbol table label IDs for each rune in the
// input string. Uses the FST's rune label cache for efficiency.
func (f *Fst) ComputeInputLabels(input string) []int32 {
	runes := []rune(input)
	labels := make([]int32, len(runes))
	for i, r := range runes {
		labels[i] = f.FindRuneLabel(r)
	}
	return labels
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

// maxComposeStates limits the number of states in a composed FST to prevent OOM.
// When the limit is exceeded, composition stops early and returns what was built.
const maxComposeStates = 500000

// Compose composes this FST with another FST.
// Uses int label matching for O(1) comparison instead of string matching.
// Includes a state limit (maxComposeStates) to prevent OOM from state explosion.
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
						if nextID >= int32(maxComposeStates) {
							continue // skip new state, limit reached
						}
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
					if nextID >= int32(maxComposeStates) {
						continue
					}
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
					if nextID >= int32(maxComposeStates) {
						continue
					}
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

	// Try to extract single-character strings from other and use
	// char-union-aware difference. This handles cases like
	// VCHAR.Difference(Union(Accep("\\"), Accep("\""))) where other
	// is a union of single-character acceptors.
	excludeChars := collectSingleCharStrings(other)
	if len(excludeChars) > 0 {
		return charUnionDifferenceByChars(f, excludeChars)
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

// collectSingleCharStrings collects all single-character strings accepted by an FST.
// It performs a BFS from the start state, following epsilon and non-epsilon arcs,
// and records any single-character strings that lead to a final state.
func collectSingleCharStrings(f *Fst) map[string]bool {
	result := make(map[string]bool)
	if f == nil || len(f.States) == 0 {
		return result
	}

	// BFS to find all paths of length 1 (single character) from start to final
	type stateWithChar struct {
		state int32
		char  string // empty means we haven't consumed a character yet
	}

	visited := make(map[stateWithChar]bool)
	queue := []stateWithChar{{state: f.Start, char: ""}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if visited[cur] {
			continue
		}
		visited[cur] = true

		st := &f.States[cur.state]
		for _, arc := range st.Arcs {
			isym := f.Symbols.Symbol(arc.ILabel)
			if isym == "" { // epsilon arc
				next := stateWithChar{state: arc.Next, char: cur.char}
				if !visited[next] {
					queue = append(queue, next)
				}
			} else if cur.char == "" && len([]rune(isym)) == 1 {
				// First non-epsilon character on this path
				next := stateWithChar{state: arc.Next, char: isym}
				if !visited[next] {
					queue = append(queue, next)
				}
			}
			// Skip paths with more than one character
		}
	}

	// Now check which single-char paths reach a final state (via epsilon arcs)
	for swc := range visited {
		if swc.char == "" {
			continue
		}
		// Check if we can reach a final state from swc.state via epsilon arcs only
		if canReachFinalViaEpsilon(f, swc.state) {
			result[swc.char] = true
		}
	}

	return result
}

// canReachFinalViaEpsilon checks if a final state is reachable from the given
// state following only epsilon arcs.
func canReachFinalViaEpsilon(f *Fst, start int32) bool {
	if f.States[start].Final {
		return true
	}
	visited := make(map[int32]bool)
	queue := []int32{start}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		if visited[s] {
			continue
		}
		visited[s] = true
		if f.States[s].Final {
			return true
		}
		for _, arc := range f.States[s].Arcs {
			if arc.ILabel == EpsilonLabel && !visited[arc.Next] {
				queue = append(queue, arc.Next)
			}
		}
	}
	return false
}

// charUnionDifferenceByChars removes characters in the exclude set from a
// char-union-like FST. It follows epsilon arcs from the start state to find
// all character arcs, and filters out those matching the exclude set.
// This handles both simple char-unions and Union-of-char-unions (like CJK VCHAR).
func charUnionDifferenceByChars(f *Fst, exclude map[string]bool) *Fst {
	result := f.copy()

	// Find all states reachable from start via epsilon arcs
	epsilonReachable := []int32{f.Start}
	visited := make(map[int32]bool)
	{
		queue := []int32{f.Start}
		for len(queue) > 0 {
			s := queue[0]
			queue = queue[1:]
			if visited[s] {
				continue
			}
			visited[s] = true
			for _, arc := range result.States[s].Arcs {
				if arc.ILabel == EpsilonLabel && !visited[arc.Next] {
					queue = append(queue, arc.Next)
					epsilonReachable = append(epsilonReachable, arc.Next)
				}
			}
		}
	}

	// Filter character arcs from all epsilon-reachable states
	for _, stateID := range epsilonReachable {
		st := &result.States[stateID]
		newArcs := make([]Arc, 0, len(st.Arcs))
		removedIEps := int32(0)
		removedOEps := int32(0)
		for _, arc := range st.Arcs {
			sym := result.Symbols.Symbol(arc.ILabel)
			if arc.ILabel == EpsilonLabel || !exclude[sym] {
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
		st.Arcs = newArcs
		st.NumIEps -= removedIEps
		st.NumOEps -= removedOEps
	}

	return result
}

// =============================================================================
// Cdrewrite
// =============================================================================

// Cdrewrite creates a context-dependent rewrite rule transducer.
// sigma should already be a starred alphabet (e.g. VSIGMA = VCHAR.Star()).
//
// Builds: sigma* concat L concat phi concat R concat sigma*
//
// The sigma* copies have a small weight penalty on character-consuming arcs
// to ensure the rule (phi) is preferred over consuming input via sigma*.
// This matches the C++ Cdrewrite semantics where the rule application path
// should win when it can match.
//
// Optimization: when sigma has the "star pattern" (a hub state with character
// arcs looping back through it), we build the cdrewrite directly without
// copying the entire sigma FST. Instead, we extract the character arcs from
// sigma's hub and add them as self-loops to the start and end states.
// This reduces state count from ~2*sigma_states+core to ~2+core.
func Cdrewrite(fst *Fst, l, r string, sigma *Fst) *Fst {
	if fst == nil || sigma == nil {
		return NewFst()
	}

	// Build the core: Accep(l) concat fst concat Accep(r)
	left := Accep(l)
	right := Accep(r)
	core := left.Concat(fst).Concat(right)

	// Try optimized path: extract character arcs from sigma's star pattern
	charArcs := extractStarCharArcs(sigma)
	if charArcs != nil {
		return cdrewriteFromCharArcs(core, charArcs, sigma.Symbols)
	}

	// General path: fall back to copying sigma
	return cdrewriteGeneral(core, sigma)
}

// extractStarCharArcs extracts character arcs from a sigma* FST that has the
// "star pattern": a hub state reachable from start via epsilon, with character
// arcs reachable via an epsilon tree from the hub, and all character arc targets
// are final with epsilon arcs back to the hub.
//
// The star pattern (from VCHAR.Star()) can have two forms:
//
// Simple (small character sets):
//
//	State 0 (start, final): epsilon to state 1 (hub)
//	State 1 (hub): character arcs to states 2..N+1
//	States 2..N+1: final, epsilon back to state 1
//
// Tree (large character sets from binary Union):
//
//	State 0 (start, final): epsilon to state 1 (hub)
//	State 1 (hub): epsilon to state 2, epsilon to state 7
//	State 2: epsilon to state 3, epsilon to state 5
//	State 3: "a" -> state 4
//	State 4 (final): epsilon back to state 1
//	State 5: "b" -> state 6
//	State 6 (final): epsilon back to state 1
//	State 7: "c" -> state 8
//	State 8 (final): epsilon back to state 1
//
// For cdrewrite, we only need the character arcs (as self-loops), not the
// full state structure. This avoids copying 8000+ states twice.
func extractStarCharArcs(sigma *Fst) []Arc {
	if sigma == nil || len(sigma.States) < 2 {
		return nil
	}

	startState := &sigma.States[sigma.Start]
	if !startState.Final {
		return nil
	}

	// Find the hub state: the state reachable via epsilon from start
	var hub int32 = -1
	epsilonCount := 0
	for _, arc := range startState.Arcs {
		if arc.ILabel == EpsilonLabel && arc.OLabel == EpsilonLabel {
			hub = arc.Next
			epsilonCount++
		}
	}
	// Hub must be reachable via exactly one pure-epsilon arc from start
	if hub < 0 || epsilonCount != 1 || int(hub) >= len(sigma.States) {
		return nil
	}

	// Walk the epsilon tree from the hub, collecting all character arcs.
	// The tree structure comes from binary Union: each Union creates a new
	// state with two epsilon arcs to the two operands.
	var charArcs []Arc
	visited := make(map[int32]bool)
	queue := []int32{hub}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		if visited[s] || int(s) >= len(sigma.States) {
			continue
		}
		visited[s] = true
		st := &sigma.States[s]
		for _, arc := range st.Arcs {
			if arc.ILabel == EpsilonLabel && arc.OLabel == EpsilonLabel {
				// Follow epsilon arcs within the tree
				queue = append(queue, arc.Next)
			} else if arc.ILabel != EpsilonLabel {
				// Collect character arcs
				charArcs = append(charArcs, Arc{
					ILabel: arc.ILabel,
					OLabel: arc.OLabel,
					Weight: arc.Weight,
				})
			}
		}
	}

	if len(charArcs) == 0 {
		return nil
	}

	// Verify the star pattern: each character arc target should be final
	// and have an epsilon arc back to the hub
	for _, arc := range charArcs {
		if int(arc.Next) >= len(sigma.States) {
			return nil
		}
		target := &sigma.States[arc.Next]
		if !target.Final {
			return nil
		}
		// Check that target has an epsilon arc back to hub
		foundBack := false
		for _, backArc := range target.Arcs {
			if backArc.ILabel == EpsilonLabel && backArc.OLabel == EpsilonLabel && backArc.Next == hub {
				foundBack = true
				break
			}
		}
		if !foundBack {
			return nil
		}
	}

	return charArcs
}

// cdrewriteFromCharArcs builds a cdrewrite FST efficiently using extracted
// character arcs from sigma*. Instead of copying the entire sigma FST twice,
// it adds character self-loops to the start and end states.
//
// Result structure:
//
//	State 0 (start, final): char self-loops (weight+penalty) + epsilon to core
//	Core states: as-is from core
//	State N (final): char self-loops (weight+penalty)
//	Core's final states: epsilon to state N
func cdrewriteFromCharArcs(core *Fst, charArcs []Arc, sigmaSymbols *SymbolTable) *Fst {
	const sigmaArcPenalty = float32(0.0001)

	result := NewFst()
	// Merge sigma's symbols first so char arc labels are valid
	result.Symbols.Merge(sigmaSymbols)
	// Merge core's symbols
	coreMapping := result.Symbols.Merge(core.Symbols)

	// State 0: start state with character self-loops
	// (also final, matching sigma*'s "match zero characters" behavior)
	result.States[0].Final = true

	for _, arc := range charArcs {
		result.AddArc(0, 0, arc.ILabel, arc.OLabel, arc.Weight+sigmaArcPenalty)
	}

	// Add core states starting from state 1
	coreOffset := int32(1)
	for i := range core.States {
		newFrom := int32(i) + coreOffset
		ensureState(result, newFrom)
		for _, arc := range core.States[i].Arcs {
			result.AddArc(newFrom, arc.Next+coreOffset,
				coreMapping[arc.ILabel], coreMapping[arc.OLabel], arc.Weight)
		}
		if core.States[i].Final {
			result.SetFinal(newFrom, core.States[i].Weight)
		}
	}

	// Connect start state to core's start
	result.AddArc(0, core.Start+coreOffset, EpsilonLabel, EpsilonLabel, 0)

	// Add trailing sigma* state (character self-loops + final)
	trailingState := int32(result.NumStates())
	result.AddState()
	result.SetFinal(trailingState, 0)
	for _, arc := range charArcs {
		result.AddArc(trailingState, trailingState, arc.ILabel, arc.OLabel, arc.Weight+sigmaArcPenalty)
	}

	// Connect core's final states to trailing sigma* state
	for sID := coreOffset; sID < trailingState; sID++ {
		if result.States[sID].Final {
			result.States[sID].Final = false
			result.States[sID].Weight = 0
			result.AddArc(sID, trailingState, EpsilonLabel, EpsilonLabel, 0)
		}
	}

	return result
}

// cdrewriteGeneral builds a cdrewrite FST by copying sigma twice.
// This is the fallback for sigma FSTs that don't match the star pattern.
func cdrewriteGeneral(core *Fst, sigma *Fst) *Fst {
	const sigmaArcPenalty = float32(0.0001)

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
		case '\\', '[', ']':
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
		return nil, err
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
// Uses a heap-based priority queue for O((V+E) log V) complexity and
// backpointers to reconstruct the output path.
func (f *Fst) ShortestPath() string {
	if f == nil || len(f.States) == 0 {
		return ""
	}

	// backptr stores the arc used to reach a state and the predecessor state.
	type backptr struct {
		olabel int32
		from   int32
	}

	dist := make([]float32, len(f.States))
	for i := range dist {
		dist[i] = float32(1e30)
	}
	dist[f.Start] = 0

	prev := make([]backptr, len(f.States))
	for i := range prev {
		prev[i] = backptr{olabel: -1, from: -1}
	}

	// Min-heap entries: (weight, state)
	h := &shortestPathHeap{{weight: 0, state: f.Start}}
	visited := make([]bool, len(f.States))

	for h.Len() > 0 {
		cur := heap.Pop(h).(shortestPathEntry)
		if visited[cur.state] {
			continue
		}
		visited[cur.state] = true

		if int(cur.state) >= len(f.States) {
			continue
		}
		st := &f.States[cur.state]

		if st.Final {
			// Reconstruct output path via backpointers
			var labels []int32
			s := cur.state
			for prev[s].from >= 0 {
				labels = append(labels, prev[s].olabel)
				s = prev[s].from
			}
			// Reverse labels and build output string
			for i, j := 0, len(labels)-1; i < j; i, j = i+1, j-1 {
				labels[i], labels[j] = labels[j], labels[i]
			}
			var sb strings.Builder
			for _, l := range labels {
				if l != EpsilonLabel {
					sb.WriteString(f.Symbols.Symbol(l))
				}
			}
			return sb.String()
		}

		for _, arc := range st.Arcs {
			nw := cur.weight + arc.Weight
			if nw < dist[arc.Next] {
				dist[arc.Next] = nw
				prev[arc.Next] = backptr{olabel: arc.OLabel, from: cur.state}
				heap.Push(h, shortestPathEntry{weight: nw, state: arc.Next})
			}
		}
	}

	return ""
}

// shortestPathEntry is a heap element for Dijkstra's algorithm.
type shortestPathEntry struct {
	weight float32
	state  int32
}

// shortestPathHeap implements heap.Interface for shortestPathEntry.
type shortestPathHeap []shortestPathEntry

func (h shortestPathHeap) Len() int           { return len(h) }
func (h shortestPathHeap) Less(i, j int) bool { return h[i].weight < h[j].weight }
func (h shortestPathHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *shortestPathHeap) Push(x interface{}) {
	*h = append(*h, x.(shortestPathEntry))
}

func (h *shortestPathHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// isILabelSorted checks whether all states in the FST have arcs sorted by ILabel.
func isILabelSorted(f *Fst) bool {
	if f == nil {
		return false
	}
	if f.Props&propILabelSorted != 0 {
		return true
	}
	if f.Props&propNotILabelSorted != 0 {
		return false
	}
	for i := range f.States {
		arcs := f.States[i].Arcs
		for j := 1; j < len(arcs); j++ {
			if arcs[j].ILabel < arcs[j-1].ILabel {
				return false
			}
		}
	}
	return len(f.States) > 0
}

// PrepareForComposition precomputes internal indices for efficient composition.
// Must be called after ArcSort("input"). Lazily initialized; safe to call multiple times.
func (f *Fst) PrepareForComposition() {
	if f.composeReady {
		return
	}

	numStates := len(f.States)
	f.epsArcIdx = make([][]int, numStates)
	f.ilabelRanges = make([]ilabelRangeMap, numStates)

	for s := range f.States {
		arcs := f.States[s].Arcs
		if len(arcs) == 0 {
			continue
		}

		// Build epsilon arc index
		var epsIdx []int
		for i, arc := range arcs {
			if arc.ILabel == EpsilonLabel {
				epsIdx = append(epsIdx, i)
			}
		}
		f.epsArcIdx[s] = epsIdx

		// Build ilabel range map (for sorted arcs)
		// Group consecutive arcs by ilabel
		var labels []int32
		var starts []int
		var counts []int
		curLabel := arcs[0].ILabel
		curStart := 0
		curCount := 1
		for i := 1; i < len(arcs); i++ {
			if arcs[i].ILabel == curLabel {
				curCount++
			} else {
				labels = append(labels, curLabel)
				starts = append(starts, curStart)
				counts = append(counts, curCount)
				curLabel = arcs[i].ILabel
				curStart = i
				curCount = 1
			}
		}
		labels = append(labels, curLabel)
		starts = append(starts, curStart)
		counts = append(counts, curCount)

		f.ilabelRanges[s] = ilabelRangeMap{
			labels: labels,
			starts: starts,
			counts: counts,
		}
	}

	f.composeReady = true
}

// =============================================================================
// Composition types (shared by ComposeInputWithFst and ComposePrefixShortestPath)
// =============================================================================

// ComposeInputWithFst composes input with the FST and returns the shortest output.
// Uses on-the-fly composition with int label matching.
// Optimized with:
//   - Precomputed epsilon arc indices (no linear scan)
//   - Precomputed ilabel range maps (binary search)
//   - Dense weight arrays with generation counters (fast epsilon closure)
//   - Active state lists maintained through epsilon closure (no full scan)
//   - Rune label cache (avoids string(r) allocation per character)
//   - sync.Pool for scratch array reuse
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
	numRunes := len(runes)
	numStates := len(other.States)
	if numStates == 0 {
		return ""
	}

	// Ensure compose cache is ready
	other.PrepareForComposition()

	// Pre-compute input label IDs for each rune position using rune cache.
	inputLabels := make([]int32, numRunes)
	for i, r := range runes {
		inputLabels[i] = other.FindRuneLabel(r)
	}

	// Dense weight arrays with generation counters.
	// Pooled to avoid per-call allocation of large arrays.
	scratch := getComposeScratch(numStates)
	defer putComposeScratch(scratch)
	curWeight := scratch.curWeight
	curGen := scratch.curGen
	nextWeight := scratch.nextWeight
	nextGen := scratch.nextGen

	// Get pooled compose context (posStates maps, active slices)
	ctx := getComposeContext(numRunes + 1)
	defer putComposeContext(ctx)
	posStates := ctx.posStates

	// Initialize start state
	posStates[0][other.Start] = composeStateEntry{
		weight: 0,
		bp:     composeBP{fromPos: -1, fromState: -1, olabel: EpsilonLabel},
	}

	var gen uint32 = 1

	// Active states at current position — maintained through epsilon closure
	curActive := ctx.curActive
	curActive = append(curActive, other.Start)

	// Epsilon closure queue
	epsQueue := ctx.epsQueue

	for pos := 0; pos <= numRunes; pos++ {
		// Initialize dense arrays from posStates for fast epsilon closure
		curMap := posStates[pos]
		for s, entry := range curMap {
			curWeight[s] = entry.weight
			curGen[s] = gen
		}

		// Epsilon closure: follow all epsilon arcs at this position.
		// New states discovered via epsilon are added to curActive.
		epsQueue = epsQueue[:0]
		epsQueue = append(epsQueue, curActive...)
		epsHead := 0
		for epsHead < len(epsQueue) {
			s := epsQueue[epsHead]
			epsHead++
			if int(s) >= numStates {
				continue
			}
			curW := curWeight[s]
			epsIdx := other.epsArcIdx[s]
			if epsIdx == nil {
				continue
			}
			arcs := other.States[s].Arcs
			for _, idx := range epsIdx {
				arc := &arcs[idx]
				nw := curW + arc.Weight
				t := arc.Next
				if int(t) >= numStates {
					continue
				}
				if curGen[t] != gen || nw < curWeight[t] {
					curWeight[t] = nw
					wasNew := curGen[t] != gen
					curGen[t] = gen
					epsQueue = append(epsQueue, t)
					if wasNew {
						curActive = append(curActive, t)
					}
					curMap[t] = composeStateEntry{weight: nw, bp: composeBP{fromPos: int32(pos), fromState: s, olabel: arc.OLabel}}
				}
			}
		}

		// Character matching: advance to next position.
		if pos < numRunes {
			matchLabel := inputLabels[pos]
			if matchLabel < 0 {
				gen++
				curActive = curActive[:0]
				continue
			}
			gen++
			nextGen2 := gen
			nextMap := posStates[pos+1]

			for _, s := range curActive {
				if int(s) >= numStates {
					continue
				}
				curW := curWeight[s]
				// Use precomputed ilabel range map for O(log n) lookup
				irm := &other.ilabelRanges[s]
				if len(irm.labels) == 0 {
					continue
				}
				// Binary search on precomputed labels
				lo, hi := 0, len(irm.labels)
				for lo < hi {
					mid := lo + (hi-lo)/2
					if irm.labels[mid] < matchLabel {
						lo = mid + 1
					} else {
						hi = mid
					}
				}
				if lo >= len(irm.labels) || irm.labels[lo] != matchLabel {
					continue
				}
				// Iterate over all arcs with this ilabel
				arcs := other.States[s].Arcs
				start := irm.starts[lo]
				end := start + irm.counts[lo]
				for i := start; i < end; i++ {
					arc := &arcs[i]
					nw := curW + arc.Weight
					t := arc.Next
					if int(t) >= numStates {
						continue
					}
					if nextGen[t] != nextGen2 || nw < nextWeight[t] {
						nextWeight[t] = nw
						nextGen[t] = nextGen2
						nextMap[t] = composeStateEntry{weight: nw, bp: composeBP{fromPos: int32(pos), fromState: s, olabel: arc.OLabel}}
					}
				}
			}

			// Prepare next active states
			curActive = curActive[:0]
			for s := range nextMap {
				curActive = append(curActive, s)
			}
		}
	}

	// Find best final state at the end position.
	minWeight := float32(1e30)
	bestEndState := int32(-1)

	for s, entry := range posStates[numRunes] {
		if int(s) < numStates && other.States[s].Final {
			totalW := entry.weight + other.States[s].Weight
			if totalW < minWeight {
				minWeight = totalW
				bestEndState = s
			}
		}
	}

	if bestEndState < 0 {
		return ""
	}

	// Reconstruct output by following backpointers from the best final state.
	var olabels []int32
	curState := bestEndState
	curPos := int32(numRunes)
	for {
		entry, ok := posStates[curPos][curState]
		if !ok || entry.bp.fromPos < 0 {
			break
		}
		if entry.bp.olabel != EpsilonLabel {
			olabels = append(olabels, entry.bp.olabel)
		}
		curState = entry.bp.fromState
		curPos = entry.bp.fromPos
	}

	// Reverse and build output string
	if len(olabels) == 0 {
		return ""
	}
	for i, j := 0, len(olabels)-1; i < j; i, j = i+1, j-1 {
		olabels[i], olabels[j] = olabels[j], olabels[i]
	}
	var sb strings.Builder
	for _, l := range olabels {
		sb.WriteString(other.Symbols.Symbol(l))
	}
	return sb.String()
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
// Optimized with:
//   - Precomputed epsilon arc indices and ilabel range maps
//   - Reusable scratch weight arrays (no per-call allocation)
//   - Active state lists maintained through epsilon closure (no full scan)
//   - Rune label cache (avoids string(r) allocation per character)
func ComposePrefixShortestPath(inputStr string, other *Fst) ComposePrefixResult {
	if other == nil || inputStr == "" {
		return ComposePrefixResult{}
	}

	runes := []rune(inputStr)
	numRunes := len(runes)
	numStates := len(other.States)
	if numStates == 0 {
		return ComposePrefixResult{}
	}

	// Ensure compose cache is ready
	other.PrepareForComposition()

	// Pre-compute input label IDs using rune cache
	inputLabels := make([]int32, numRunes)
	for i, r := range runes {
		inputLabels[i] = other.FindRuneLabel(r)
	}

	// Dense weight arrays with generation counters for epsilon closure
	// Pooled to avoid per-call allocation of large arrays.
	scratch := getComposeScratch(numStates)
	defer putComposeScratch(scratch)
	curWeight := scratch.curWeight
	curGen := scratch.curGen
	nextWeight := scratch.nextWeight
	nextGen := scratch.nextGen

	var gen uint32 = 1

	// Get pooled compose context (posStates maps, active slices)
	ctx := getComposeContext(numRunes + 1)
	defer putComposeContext(ctx)
	posStates := ctx.posStates

	// Initialize start state
	posStates[0][other.Start] = composeStateEntry{
		weight: 0,
		bp:     composeBP{fromPos: -1, fromState: -1, olabel: EpsilonLabel},
	}

	// Active states at current position
	curActive := ctx.curActive
	curActive = append(curActive, other.Start)

	// Track the best final match at each position
	bestConsumed := 0
	bestEndState := int32(-1)
	bestWeight := float32(1e30)

	epsQueue := ctx.epsQueue

	for pos := 0; pos <= numRunes; pos++ {
		// Initialize dense arrays from posStates
		curMap := posStates[pos]
		for s, entry := range curMap {
			curWeight[s] = entry.weight
			curGen[s] = gen
		}

		// Epsilon closure
		epsQueue = epsQueue[:0]
		epsQueue = append(epsQueue, curActive...)
		epsHead := 0
		for epsHead < len(epsQueue) {
			s := epsQueue[epsHead]
			epsHead++
			if int(s) >= numStates {
				continue
			}
			curW := curWeight[s]
			epsIdx := other.epsArcIdx[s]
			if epsIdx == nil {
				continue
			}
			arcs := other.States[s].Arcs
			for _, idx := range epsIdx {
				arc := &arcs[idx]
				nw := curW + arc.Weight
				t := arc.Next
				if int(t) >= numStates {
					continue
				}
				if curGen[t] != gen || nw < curWeight[t] {
					curWeight[t] = nw
					wasNew := curGen[t] != gen
					curGen[t] = gen
					epsQueue = append(epsQueue, t)
					if wasNew {
						curActive = append(curActive, t)
					}
					newBP := composeBP{fromPos: int32(pos), fromState: s, olabel: arc.OLabel}
					curMap[t] = composeStateEntry{weight: nw, bp: newBP}
				}
			}
		}

		// Check for final states at this position
		for _, s := range curActive {
			if int(s) < numStates && other.States[s].Final {
				entry := curMap[s]
				totalW := entry.weight + other.States[s].Weight
				if pos > bestConsumed || (pos == bestConsumed && totalW < bestWeight) {
					bestWeight = totalW
					bestConsumed = pos
					bestEndState = s
				}
			}
		}

		if pos < numRunes {
			matchLabel := inputLabels[pos]
			if matchLabel < 0 {
				gen++
				curActive = curActive[:0]
				continue
			}
			gen++
			nextGen2 := gen
			nextMap := posStates[pos+1]

			for _, s := range curActive {
				if int(s) >= numStates {
					continue
				}
				curW := curWeight[s]
				irm := &other.ilabelRanges[s]
				if len(irm.labels) == 0 {
					continue
				}
				lo, hi := 0, len(irm.labels)
				for lo < hi {
					mid := lo + (hi-lo)/2
					if irm.labels[mid] < matchLabel {
						lo = mid + 1
					} else {
						hi = mid
					}
				}
				if lo >= len(irm.labels) || irm.labels[lo] != matchLabel {
					continue
				}
				arcs := other.States[s].Arcs
				start := irm.starts[lo]
				end := start + irm.counts[lo]
				for i := start; i < end; i++ {
					arc := &arcs[i]
					nw := curW + arc.Weight
					t := arc.Next
					if int(t) >= numStates {
						continue
					}
					if nextGen[t] != nextGen2 || nw < nextWeight[t] {
						nextWeight[t] = nw
						nextGen[t] = nextGen2
						newBP := composeBP{fromPos: int32(pos), fromState: s, olabel: arc.OLabel}
						nextMap[t] = composeStateEntry{weight: nw, bp: newBP}
					}
				}
			}

			// Prepare next active states
			curActive = curActive[:0]
			for s := range nextMap {
				curActive = append(curActive, s)
			}
		}
	}

	if bestEndState < 0 || bestConsumed == 0 {
		return ComposePrefixResult{}
	}

	// Reconstruct output by following backpointers from the best final state.
	var olabels []int32
	curState := bestEndState
	curPos := int32(bestConsumed)
	for {
		entry, ok := posStates[curPos][curState]
		if !ok || entry.bp.fromPos < 0 {
			break
		}
		if entry.bp.olabel != EpsilonLabel {
			olabels = append(olabels, entry.bp.olabel)
		}
		curState = entry.bp.fromState
		curPos = entry.bp.fromPos
	}

	var output string
	if len(olabels) > 0 {
		for i, j := 0, len(olabels)-1; i < j; i, j = i+1, j-1 {
			olabels[i], olabels[j] = olabels[j], olabels[i]
		}
		var sb strings.Builder
		for _, l := range olabels {
			sb.WriteString(other.Symbols.Symbol(l))
		}
		output = sb.String()
	}

	return ComposePrefixResult{
		Output:   output,
		Consumed: bestConsumed,
		Weight:   bestWeight,
	}
}

// ComposePrefixShortestPathWithMaxLen is like ComposePrefixShortestPath but
// limits the search depth to maxLen characters. When maxLen > 0, only the
// first maxLen characters of the input are considered.
func ComposePrefixShortestPathWithMaxLen(inputStr string, other *Fst, maxLen int) ComposePrefixResult {
	if other == nil || inputStr == "" {
		return ComposePrefixResult{}
	}

	runes := []rune(inputStr)
	effectiveRunes := len(runes)
	if maxLen > 0 && maxLen < effectiveRunes {
		effectiveRunes = maxLen
	}

	// Truncate the input string to the effective length
	truncated := string(runes[:effectiveRunes])
	return ComposePrefixShortestPath(truncated, other)
}

// ComposePrefixShortestPathWithLabels is like ComposePrefixShortestPath but
// accepts pre-computed input label IDs. Note: the inputLabels must be computed
// using the same FST's symbol table as `other`, otherwise results will be incorrect.
func ComposePrefixShortestPathWithLabels(inputStr string, inputLabels []int32, other *Fst) ComposePrefixResult {
	if other == nil || len(inputLabels) == 0 {
		return ComposePrefixResult{}
	}
	// Fall back to regular ComposePrefixShortestPath since label IDs
	// are FST-specific and cannot be shared across different FSTs.
	return ComposePrefixShortestPath(inputStr, other)
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

// composeBP stores backpointer information for path reconstruction.
type composeBP struct {
	fromPos   int32
	fromState int32
	olabel    int32
}

// composeStateEntry stores weight and backpointer for a state at a position.
type composeStateEntry struct {
	weight float32
	bp     composeBP
}

// composeContext holds reusable per-call state for composition operations.
// Pooled via sync.Pool to avoid massive per-call allocation of posStates maps.
type composeContext struct {
	posStates  []map[int32]composeStateEntry
	curActive  []int32
	epsQueue   []int32
	nextActive []int32
}

var composeContextPool = sync.Pool{
	New: func() interface{} {
		return &composeContext{}
	},
}

// maxMapReuseCap is the maximum number of entries a reused map may have
// before we discard it and allocate a fresh smaller one.
const maxMapReuseCap = 128

// getComposeContext returns a composeContext from the pool, ensuring it has
// enough posStates maps for numPositions positions. Existing maps are cleared
// (entries deleted) rather than reallocated.
func getComposeContext(numPositions int) *composeContext {
	ctx := composeContextPool.Get().(*composeContext)

	// Ensure posStates slice is long enough
	if len(ctx.posStates) < numPositions {
		// Extend the slice
		oldLen := len(ctx.posStates)
		ctx.posStates = append(ctx.posStates, make([]map[int32]composeStateEntry, numPositions-oldLen)...)
	}
	posStates := ctx.posStates[:numPositions]

	// Clear each map: delete all entries, or replace if too large
	for i := range posStates {
		m := posStates[i]
		if m == nil {
			if i == 0 {
				m = make(map[int32]composeStateEntry, 16)
			} else {
				m = make(map[int32]composeStateEntry, 8)
			}
			posStates[i] = m
		} else if len(m) > 0 {
			if len(m) > maxMapReuseCap {
				// Map grew too large; replace with a fresh smaller one
				if i == 0 {
					posStates[i] = make(map[int32]composeStateEntry, 16)
				} else {
					posStates[i] = make(map[int32]composeStateEntry, 8)
				}
			} else {
				// Clear by deleting all entries (cheaper than reallocating)
				for k := range m {
					delete(m, k)
				}
			}
		}
	}

	// Reset slices (keep capacity)
	ctx.curActive = ctx.curActive[:0]
	ctx.epsQueue = ctx.epsQueue[:0]
	if ctx.nextActive == nil {
		ctx.nextActive = make([]int32, 0, 128)
	} else {
		ctx.nextActive = ctx.nextActive[:0]
	}

	return ctx
}

func putComposeContext(ctx *composeContext) {
	composeContextPool.Put(ctx)
}

// composeScratch holds reusable arrays for composition operations.
// Pooled via sync.Pool to avoid per-call allocation of large arrays.
type composeScratch struct {
	curWeight  []float32
	curGen     []uint32
	nextWeight []float32
	nextGen    []uint32
}

var composeScratchPool = sync.Pool{
	New: func() interface{} {
		return &composeScratch{}
	},
}

func getComposeScratch(numStates int) *composeScratch {
	s := composeScratchPool.Get().(*composeScratch)
	// Ensure arrays are large enough (grow if needed, but don't shrink)
	if cap(s.curWeight) < numStates {
		s.curWeight = make([]float32, numStates)
		s.curGen = make([]uint32, numStates)
		s.nextWeight = make([]float32, numStates)
		s.nextGen = make([]uint32, numStates)
	} else {
		// Reuse existing capacity — just set length
		s.curWeight = s.curWeight[:numStates]
		s.curGen = s.curGen[:numStates]
		s.nextWeight = s.nextWeight[:numStates]
		s.nextGen = s.nextGen[:numStates]
		// Zero the gen arrays to avoid stale generation matches
		for i := range s.curGen {
			s.curGen[i] = 0
		}
		for i := range s.nextGen {
			s.nextGen[i] = 0
		}
	}
	return s
}

func putComposeScratch(s *composeScratch) {
	composeScratchPool.Put(s)
}

func init() {
	gob.Register(&Fst{})
	gob.Register(&SymbolTable{})
}
