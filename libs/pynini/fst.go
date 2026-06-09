package pynini

import (
	"container/heap"
	"encoding/gob"
	"os"
	"sort"
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
	Final      bool
	Weight     float32
	Arcs       []Arc
	NumIEps    int32
	NumOEps    int32
	FinalOLabel int32 // output label for final state (EpsilonLabel=0 means none)
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
	epsArcIdx      [][]int             // epsArcIdx[s] = indices of epsilon arcs from state s (nil if none)
	epsFlat        [][]epsFlatArc      // epsFlat[s] = flattened epsilon arcs for direct iteration
	ilabelRanges   []ilabelRangeMap    // ilabelRanges[s] = precomputed ilabel mapping
	epsClosureList [][]epsClosureEntry // epsClosureList[s] = precomputed epsilon closure entries for state s
	composeReady   bool
	hasEpsArcs     bool // true if any state has epsilon arcs

	// Rune label cache (lazily initialized, not serialized by gob)
	runeLabelCache map[rune]int32

	// Per-FST compose context and scratch (lazily initialized, not serialized by gob).
	// Stored in the FST to avoid GC-triggered reallocation of large arrays.
	composeCtx     *composeContext
	composeScratch *composeScratch
}

// epsClosureEntry represents one step in the precomputed epsilon closure.
// For state s, epsClosureList[s] contains entries for all states reachable
// via input-epsilon arcs from s, in BFS order. Each entry records the
// immediate predecessor and arc output label for backpointer reconstruction.
type epsClosureEntry struct {
	target    int32
	from      int32   // immediate predecessor in epsilon chain
	olabel    int32   // output label of arc from -> target
	relWeight float32 // accumulated weight from source to this target
}

// epsFlatArc is a flattened representation of an input-epsilon arc,
// storing (target, weight, olabel) directly to avoid indirection
// through States[s].Arcs[epsArcIdx[s][i]] during epsilon closure BFS.
type epsFlatArc struct {
	next   int32
	weight float32
	olabel int32
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
		// Build the cross by replacing all output labels in aFst with bStr characters
		return crossFstToStr(aFst, bStr)
	}

	if bIsFst {
		// Cross(str, fst): map input string to output labels of fst
		// Create a transducer where input is aStr and output follows bFst's output paths
		aStr := toString(a)
		if aStr == "" {
			// Insert: copy the FST and set all ILabels to epsilon
			return withInputEpsilon(bFst)
		}
		// Build the cross by replacing all input labels in bFst with aStr characters
		// For each path through bFst, replace the input labels with the characters of aStr
		// This is equivalent to: for each accepting path in bFst, create a parallel path
		// where input is aStr and output is the path's output
		return crossStrToFst(aStr, bFst)
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

// crossStrToFst creates a transducer where input is aStr and output follows
// all output paths of bFst. For each accepting path in bFst, we create a
// parallel path where the input is aStr (character by character, with epsilons
// padding if aStr is shorter than the output path) and the output is the path's
// output labels.
func crossStrToFst(aStr string, bFst *Fst) *Fst {
	if bFst == nil || len(bFst.States) == 0 {
		return NewFst()
	}
	// Find all output strings from bFst via DFS
	type pathEntry struct {
		state  int32
		output string
	}
	var outputStrings []string
	visited := make(map[int32]bool)
	var stack []pathEntry
	stack = append(stack, pathEntry{state: bFst.Start, output: ""})

	for len(stack) > 0 {
		entry := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if int(entry.state) >= len(bFst.States) {
			continue
		}

		if bFst.States[entry.state].Final {
			outputStrings = append(outputStrings, entry.output)
		}

		for _, arc := range bFst.States[entry.state].Arcs {
			if visited[arc.Next] && arc.ILabel == EpsilonLabel && arc.OLabel == EpsilonLabel {
				continue
			}
			newOutput := entry.output
			if arc.OLabel != EpsilonLabel {
				newOutput += bFst.Symbols.Symbol(arc.OLabel)
			}
			stack = append(stack, pathEntry{state: arc.Next, output: newOutput})
		}
		visited[entry.state] = true
	}

	if len(outputStrings) == 0 {
		return NewFst()
	}

	// Build the cross FST by union of crossFromStrings for each output string
	var result *Fst
	for _, outStr := range outputStrings {
		cross := crossFromStrings(aStr, outStr)
		if result == nil {
			result = cross
		} else {
			result = Union(result, cross)
		}
	}
	return result
}

// crossFstToStr creates a transducer where input follows all input paths of aFst
// and output is bStr. For each accepting path in aFst, we create a parallel path
// where the input is the path's input labels and the output is bStr.
func crossFstToStr(aFst *Fst, bStr string) *Fst {
	if aFst == nil || len(aFst.States) == 0 {
		return NewFst()
	}

	// Find all input strings from aFst via DFS
	type pathEntry struct {
		state int32
		input string
	}
	var inputStrings []string
	visited := make(map[int32]bool)
	var stack []pathEntry
	stack = append(stack, pathEntry{state: aFst.Start, input: ""})

	for len(stack) > 0 {
		entry := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if int(entry.state) >= len(aFst.States) {
			continue
		}

		if aFst.States[entry.state].Final {
			inputStrings = append(inputStrings, entry.input)
		}

		for _, arc := range aFst.States[entry.state].Arcs {
			if visited[arc.Next] && arc.ILabel == EpsilonLabel && arc.OLabel == EpsilonLabel {
				continue
			}
			newInput := entry.input
			if arc.ILabel != EpsilonLabel {
				newInput += aFst.Symbols.Symbol(arc.ILabel)
			}
			stack = append(stack, pathEntry{state: arc.Next, input: newInput})
		}
		visited[entry.state] = true
	}

	if len(inputStrings) == 0 {
		return NewFst()
	}

	// Build the cross FST by union of crossFromStrings for each input string
	var result *Fst
	for _, inStr := range inputStrings {
		cross := crossFromStrings(inStr, bStr)
		if result == nil {
			result = cross
		} else {
			result = Union(result, cross)
		}
	}
	return result
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
// Optimized: pre-computes s2's ilabel index once, avoiding per-pair map allocation.
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

	// Pre-compute s2's ilabel index for all states (avoid per-pair map allocation)
	type ilabelArcs struct {
		label int32
		arcs  []Arc
	}
	s2ILabelIndex := make([][]ilabelArcs, len(other.States))
	for s := range other.States {
		arcs := other.States[s].Arcs
		if len(arcs) == 0 {
			continue
		}
		// Group arcs by mapped ilabel
		labelMap := make(map[int32][]Arc)
		for _, arc := range arcs {
			mappedLabel := otherMapping[arc.ILabel]
			labelMap[mappedLabel] = append(labelMap[mappedLabel], arc)
		}
		idx := make([]ilabelArcs, 0, len(labelMap))
		for label, arcList := range labelMap {
			idx = append(idx, ilabelArcs{label: label, arcs: arcList})
		}
		// Sort by label for binary search
		sort.Slice(idx, func(i, j int) bool {
			return idx[i].label < idx[j].label
		})
		s2ILabelIndex[s] = idx
	}

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

		if s1.Final && other.States[p.s2].Final {
			result.SetFinal(resultStateID, s1.Weight+other.States[p.s2].Weight)
		}

		// Use pre-computed s2 ilabel index with binary search
		s2Idx := s2ILabelIndex[p.s2]

		// Process s1 arcs
		for _, a1 := range s1.Arcs {
			// Binary search for matching ilabel in s2 index
			lo, hi := 0, len(s2Idx)
			for lo < hi {
				mid := lo + (hi-lo)/2
				if s2Idx[mid].label < a1.OLabel {
					lo = mid + 1
				} else {
					hi = mid
				}
			}
			if lo < len(s2Idx) && s2Idx[lo].label == a1.OLabel {
				for _, a2 := range s2Idx[lo].arcs {
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
		for _, a2 := range other.States[p.s2].Arcs {
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
			Final:      f.States[i].Final,
			Weight:     f.States[i].Weight,
			NumIEps:    f.States[i].NumIEps,
			NumOEps:    f.States[i].NumOEps,
			FinalOLabel: f.States[i].FinalOLabel,
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

	// Check if any state has epsilon arcs
	for s := range f.States {
		if len(f.epsArcIdx[s]) > 0 {
			f.hasEpsArcs = true
			break
		}
	}

	// Build flattened epsilon arcs for direct iteration during BFS.
	// This avoids the indirection through States[s].Arcs[epsArcIdx[s][i]].
	if f.hasEpsArcs {
		f.epsFlat = make([][]epsFlatArc, numStates)
		for s := range f.States {
			epsIdx := f.epsArcIdx[s]
			if len(epsIdx) == 0 {
				continue
			}
			flat := make([]epsFlatArc, len(epsIdx))
			arcs := f.States[s].Arcs
			for i, idx := range epsIdx {
				arc := &arcs[idx]
				flat[i] = epsFlatArc{
					next:   arc.Next,
					weight: arc.Weight,
					olabel: arc.OLabel,
				}
			}
			f.epsFlat[s] = flat
		}
	}

	// Pre-compute epsilon closures for runtime BFS acceleration.
	// For each state s, epsClosureList[s] contains entries for all states
	// reachable via input-epsilon arcs from s, in BFS order.
	// Only pre-compute for states with small closures (<= maxEpsClosureSize)
	// to limit memory usage. States with large closures fall back to runtime BFS.
	// For large FSTs (> 10000 states), reduce the closure size limit to save memory.
	if f.hasEpsArcs {
		maxEpsClosureSize := 500 // limit per-state closure size
		if numStates > 10000 {
			maxEpsClosureSize = 300 // reduce for large FSTs
		}
		if numStates > 50000 {
			maxEpsClosureSize = 150 // further reduce for very large FSTs
		}
		f.epsClosureList = make([][]epsClosureEntry, numStates)
		// Reusable arrays for BFS (avoid allocation per state)
		visited := make([]uint32, numStates) // generation-based visited
		bestWeight := make([]float32, numStates)
		var queue []int32
		var gen uint32

		for s := 0; s < numStates; s++ {
			if len(f.epsArcIdx[s]) == 0 {
				continue // no epsilon arcs from this state
			}

			// BFS from state s over input-epsilon arcs
			gen++
			queue = queue[:0]
			queue = append(queue, int32(s))
			visited[s] = gen
			bestWeight[s] = 0
			var entries []epsClosureEntry
			tooLarge := false

			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				curW := bestWeight[cur]

				for _, idx := range f.epsArcIdx[cur] {
					arc := &f.States[cur].Arcs[idx]
					nw := curW + arc.Weight
					t := arc.Next
					if visited[t] != gen || nw < bestWeight[t] {
						bestWeight[t] = nw
						entries = append(entries, epsClosureEntry{
							target:    t,
							from:      cur,
							olabel:    arc.OLabel,
							relWeight: nw,
						})
						if len(entries) > maxEpsClosureSize {
							tooLarge = true
							break
						}
						if visited[t] != gen {
							visited[t] = gen
							queue = append(queue, t)
						}
					}
				}
				if tooLarge {
					break
				}
			}

			if !tooLarge {
				f.epsClosureList[s] = entries
			}
			// If tooLarge, epsClosureList[s] remains nil -> runtime BFS fallback
		}

		// Always pre-compute the start state's epsilon closure with a higher limit,
		// since it's the most commonly accessed (used at position 0 of every composition).
		if f.epsClosureList[f.Start] == nil && len(f.epsArcIdx[f.Start]) > 0 {
			const startEpsClosureLimit = 5000
			gen++
			queue = queue[:0]
			queue = append(queue, f.Start)
			visited[f.Start] = gen
			bestWeight[f.Start] = 0
			var startEntries []epsClosureEntry
			startTooLarge := false

			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				curW := bestWeight[cur]

				for _, idx := range f.epsArcIdx[cur] {
					arc := &f.States[cur].Arcs[idx]
					nw := curW + arc.Weight
					t := arc.Next
					if visited[t] != gen || nw < bestWeight[t] {
						bestWeight[t] = nw
						startEntries = append(startEntries, epsClosureEntry{
							target:    t,
							from:      cur,
							olabel:    arc.OLabel,
							relWeight: nw,
						})
						if len(startEntries) > startEpsClosureLimit {
							startTooLarge = true
							break
						}
						if visited[t] != gen {
							visited[t] = gen
							queue = append(queue, t)
						}
					}
				}
				if startTooLarge {
					break
				}
			}

			if !startTooLarge {
				f.epsClosureList[f.Start] = startEntries
			}
		}
	}

	f.composeReady = true
}

// GetEpsArcIdxPublic returns the epsilon arc indices for a state (for debugging).
func (f *Fst) GetEpsArcIdxPublic(s int) []int {
	if s < 0 || s >= len(f.epsArcIdx) {
		return nil
	}
	return f.epsArcIdx[s]
}

// EpsClosureStats holds statistics about the precomputed epsilon closure list.
type EpsClosureStats struct {
	TotalEntries      int
	StatesWithClosure int
	MinSize           int
	MaxSize           int
	AvgSize           float64
	TopStates         []EpsClosureStateInfo // top states by closure size
	MemoryEstimate    int64                 // estimated bytes (entries * 16)
}

// EpsClosureStateInfo holds info about a single state's epsilon closure.
type EpsClosureStateInfo struct {
	StateID     int
	ClosureSize int
}

// GetEpsClosureStats computes and returns statistics about the epsilon closure list.
func (f *Fst) GetEpsClosureStats() EpsClosureStats {
	var stats EpsClosureStats
	if len(f.epsClosureList) == 0 {
		return stats
	}

	stats.MinSize = -1 // sentinel: not yet set
	for s, entries := range f.epsClosureList {
		n := len(entries)
		stats.TotalEntries += n
		if n > 0 {
			stats.StatesWithClosure++
			if stats.MinSize < 0 || n < stats.MinSize {
				stats.MinSize = n
			}
			if n > stats.MaxSize {
				stats.MaxSize = n
			}
			stats.TopStates = append(stats.TopStates, EpsClosureStateInfo{
				StateID:     s,
				ClosureSize: n,
			})
		}
	}
	if stats.StatesWithClosure > 0 {
		stats.AvgSize = float64(stats.TotalEntries) / float64(stats.StatesWithClosure)
	} else {
		stats.MinSize = 0
	}
	stats.MemoryEstimate = int64(stats.TotalEntries) * 16

	// Sort top states by closure size descending, keep top 10
	sort.Slice(stats.TopStates, func(i, j int) bool {
		return stats.TopStates[i].ClosureSize > stats.TopStates[j].ClosureSize
	})
	if len(stats.TopStates) > 10 {
		stats.TopStates = stats.TopStates[:10]
	}

	return stats
}

// =============================================================================
// Composition types (shared by ComposeInputWithFst and ComposePrefixShortestPath)
// =============================================================================

// ComposeInputWithFst composes input with the FST and returns the shortest output.
// Uses on-the-fly composition with dense arrays + generation counters.
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

	other.PrepareForComposition()

	inputLabels := make([]int32, numRunes)
	for i, r := range runes {
		inputLabels[i] = other.FindRuneLabel(r)
	}

	scratch := other.getOrCreateComposeScratch(numStates)
	curWeight := scratch.curWeight
	curGen := scratch.curGen
	nextWeight := scratch.nextWeight
	nextGen := scratch.nextGen

	numPositions := numRunes + 1
	ctx := other.getOrCreateComposeContext(numPositions, numStates)

	// Initialize start state
	ctx.ctxSet(0, other.Start, 0, -1, -1, EpsilonLabel)
	curWeight[other.Start] = 0
	curGen[other.Start] = scratch.callGen + 1

	var gen uint32 = scratch.callGen + 1

	curActive := ctx.curActive
	curActive = append(curActive, other.Start)
	nextActive := ctx.nextActive
	epsQueue := ctx.epsQueue

	for pos := 0; pos <= numRunes; pos++ {
		// Epsilon closure (only if FST has epsilon arcs)
		// Optimization: use pre-computed epsilon closure when there is exactly
		// one active state (the start state at position 0).
		if other.hasEpsArcs {
			intP := int32(pos)
			usePrecomputed := len(curActive) == 1 && pos == 0 && other.epsClosureList[curActive[0]] != nil
			if usePrecomputed {
				s := curActive[0]
				entries := other.epsClosureList[s]
				startW := curWeight[s]
				for i := range entries {
					entry := &entries[i]
					t := entry.target
					nw := startW + entry.relWeight
					if curGen[t] != gen || nw < curWeight[t] {
						wasNew := curGen[t] != gen
						curWeight[t] = nw
						curGen[t] = gen
						if wasNew {
							curActive = append(curActive, t)
						}
						ctx.ctxSet(intP, t, nw, intP, entry.from, entry.olabel)
					}
				}
			} else {
				epsQueue = epsQueue[:0]
				for i := 0; i < len(curActive); i++ {
					s := curActive[i]
					if int(s) < numStates && len(other.epsFlat[s]) > 0 {
						epsQueue = append(epsQueue, s)
					}
				}
				epsHead := 0
				for epsHead < len(epsQueue) {
					s := epsQueue[epsHead]
					epsHead++
					if int(s) >= numStates {
						continue
					}
					curW := curWeight[s]
					epsFlat := other.epsFlat[s]
					if len(epsFlat) == 0 {
						continue
					}
					// Active state limit for epsilon BFS
				const maxActiveInputEpsStates = 500
				for i := range epsFlat {
						e := &epsFlat[i]
						nw := curW + e.weight
						t := e.next
						if int(t) >= numStates {
							continue
						}
						if curGen[t] != gen || nw < curWeight[t] {
							curWeight[t] = nw
							wasNew := curGen[t] != gen
							curGen[t] = gen
							epsQueue = append(epsQueue, t)
							if wasNew && len(curActive) < maxActiveInputEpsStates {
								curActive = append(curActive, t)
							}
							ctx.ctxSet(intP, t, nw, intP, s, e.olabel)
						}
					}
				}
			}
		}

		// Character matching
		if pos < numRunes {
			matchLabel := inputLabels[pos]
			if matchLabel < 0 {
				gen++
				curActive = curActive[:0]
				continue
			}
			gen++
			nextGen2 := gen
			nextActive = nextActive[:0]

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
						isNew := nextGen[t] != nextGen2
						nextWeight[t] = nw
						nextGen[t] = nextGen2
						if isNew {
							nextActive = append(nextActive, t)
						}
						ctx.ctxSet(int32(pos+1), t, nw, int32(pos), s, arc.OLabel)
					}
				}
			}

			// Swap: nextActive becomes curActive for the next position
			curActive, nextActive = nextActive, curActive[:0]

			// Copy weights from nextWeight to curWeight for next iteration
			for _, s := range curActive {
				curWeight[s] = nextWeight[s]
				curGen[s] = gen
			}
		}
	}

	// Find best final state at the end position
	minWeight := float32(1e30)
	bestEndState := int32(-1)
	for _, s := range curActive {
		if int(s) < numStates && other.States[s].Final {
			totalW := curWeight[s] + other.States[s].Weight
			if totalW < minWeight {
				minWeight = totalW
				bestEndState = s
			}
		}
	}

	if bestEndState < 0 {
		return ""
	}

	// Reconstruct output
	var olabels []int32
	curState := bestEndState
	curPos := int32(numRunes)
	for {
		_, fromPos, fromState, olabel, ok := ctx.ctxGet(curPos, curState)
		if !ok || fromPos < 0 {
			break
		}
		if olabel != EpsilonLabel {
			olabels = append(olabels, olabel)
		}
		curState = fromState
		curPos = fromPos
	}

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
// Optimized with dense arrays and generation counters for cache efficiency.
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
	scratch := other.getOrCreateComposeScratch(numStates)
	curWeight := scratch.curWeight
	curGen := scratch.curGen
	nextWeight := scratch.nextWeight
	nextGen := scratch.nextGen

	var gen uint32 = scratch.callGen + 1

	// Get compose context from FST cache (dense arrays + active lists)
	numPositions := numRunes + 1
	ctx := other.getOrCreateComposeContext(numPositions, numStates)

	// Initialize start state
	ctx.ctxSet(0, other.Start, 0, -1, -1, EpsilonLabel)
	curWeight[other.Start] = 0
	curGen[other.Start] = gen

	curActive := ctx.curActive
	curActive = append(curActive, other.Start)
	nextActive := ctx.nextActive
	epsQueue := ctx.epsQueue

	// Track the best final match at each position
	bestConsumed := 0
	bestEndState := int32(-1)
	bestWeight := float32(1e30)

	for pos := 0; pos <= numRunes; pos++ {
		// Epsilon closure (only if FST has epsilon arcs)
		if other.hasEpsArcs {
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
						ctx.ctxSet(int32(pos), t, nw, int32(pos), s, arc.OLabel)
					}
				}
			}
		}

		// Check for final states at this position (after epsilon closure)
		for _, s := range curActive {
			if int(s) < numStates && other.States[s].Final {
				totalW := curWeight[s] + other.States[s].Weight
				if pos > bestConsumed || (pos == bestConsumed && totalW < bestWeight) {
					bestWeight = totalW
					bestConsumed = pos
					bestEndState = s
				}
			}
		}

		// Character matching
		if pos < numRunes {
			matchLabel := inputLabels[pos]
			if matchLabel < 0 {
				gen++
				curActive = curActive[:0]
				continue
			}
			gen++
			nextGen2 := gen
			nextActive = nextActive[:0]

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
						isNew := nextGen[t] != nextGen2
						nextWeight[t] = nw
						nextGen[t] = nextGen2
						if isNew {
							nextActive = append(nextActive, t)
						}
						ctx.ctxSet(int32(pos+1), t, nw, int32(pos), s, arc.OLabel)
					}
				}
			}

			// Swap: nextActive becomes curActive for the next position
			curActive, nextActive = nextActive, curActive[:0]

			// Copy weights from nextWeight to curWeight for next iteration
			for _, s := range curActive {
				curWeight[s] = nextWeight[s]
				curGen[s] = gen
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
		_, fromPos, fromState, olabel, ok := ctx.ctxGet(curPos, curState)
		if !ok || fromPos < 0 {
			break
		}
		if olabel != EpsilonLabel {
			olabels = append(olabels, olabel)
		}
		curState = fromState
		curPos = fromPos
	}

	var output string
	if len(olabels) > 0 || other.States[bestEndState].FinalOLabel != EpsilonLabel {
		for i, j := 0, len(olabels)-1; i < j; i, j = i+1, j-1 {
			olabels[i], olabels[j] = olabels[j], olabels[i]
		}
		// Append final state output label (from RmOutputEpsilon)
		if other.States[bestEndState].FinalOLabel != EpsilonLabel {
			olabels = append(olabels, other.States[bestEndState].FinalOLabel)
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

// ComposePrefixShortestPathRunes composes input runes with the FST and finds
// the best match that consumes a prefix of the input. This version accepts a
// rune slice directly, avoiding string allocation and conversion overhead.
// The pos parameter specifies the starting position in the runes slice,
// and maxLen limits the number of runes to consider from pos.
// Optimized with dense arrays + generation counters (no map overhead).
func ComposePrefixShortestPathRunes(runes []rune, pos int, maxLen int, other *Fst) ComposePrefixResult {
	if other == nil || len(runes) == 0 || pos >= len(runes) {
		return ComposePrefixResult{}
	}

	effectiveLen := len(runes) - pos
	if maxLen > 0 && maxLen < effectiveLen {
		effectiveLen = maxLen
	}
	if effectiveLen <= 0 {
		return ComposePrefixResult{}
	}

	// Pre-compute input label IDs
	inputLabels := make([]int32, effectiveLen)
	for i := 0; i < effectiveLen; i++ {
		inputLabels[i] = other.FindRuneLabel(runes[pos+i])
	}
	return composePrefixShortestPathCore(inputLabels, effectiveLen, other)
}

// ComposePrefixShortestPathRunesWithLabels is like ComposePrefixShortestPathRunes
// but accepts pre-computed input label IDs. This avoids redundant label computation
// when composing the same input against multiple FSTs.
func ComposePrefixShortestPathRunesWithLabels(inputLabels []int32, effectiveLen int, other *Fst) ComposePrefixResult {
	if other == nil || len(inputLabels) == 0 || effectiveLen <= 0 {
		return ComposePrefixResult{}
	}
	if effectiveLen > len(inputLabels) {
		effectiveLen = len(inputLabels)
	}
	return composePrefixShortestPathCore(inputLabels, effectiveLen, other)
}

// composePrefixShortestPathCore is the shared core of ComposePrefixShortestPath*
// functions. It composes pre-computed input labels with the FST and finds the
// best match prefix using dense arrays and generation counters.
func composePrefixShortestPathCore(inputLabels []int32, effectiveLen int, other *Fst) ComposePrefixResult {
	numStates := len(other.States)
	if numStates == 0 {
		return ComposePrefixResult{}
	}

	// Ensure compose cache is ready
	other.PrepareForComposition()

	// Dense weight arrays with generation counters for epsilon closure
	scratch := other.getOrCreateComposeScratch(numStates)
	curWeight := scratch.curWeight
	curGen := scratch.curGen
	nextWeight := scratch.nextWeight
	nextGen := scratch.nextGen

	var gen uint32 = scratch.callGen + 1

	// Get compose context from FST cache (dense arrays + active lists)
	numPositions := effectiveLen + 1
	ctx := other.getOrCreateComposeContext(numPositions, numStates)

	// Initialize start state
	ctx.ctxSet(0, other.Start, 0, -1, -1, EpsilonLabel)
	curWeight[other.Start] = 0
	curGen[other.Start] = gen

	curActive := ctx.curActive
	curActive = append(curActive, other.Start)
	nextActive := ctx.nextActive

	bestConsumed := 0
	bestEndState := int32(-1)
	bestWeight := float32(1e30)

	for p := 0; p <= effectiveLen; p++ {
		// Epsilon closure via BFS.
		// Optimization: use pre-computed epsilon closure when there is exactly
		// one active state (the start state at position 0). This avoids the
		// expensive BFS over the entire epsilon closure of the start state.
		// Active state limit: cap the number of active states to avoid
		// exponential blowup in epsilon BFS for large FSTs.
		// Must be large enough for date tagger's epsilon closure (~571 states).
		const maxActiveStates = 2000
		if other.hasEpsArcs {
			intP := int32(p)
			usePrecomputed := len(curActive) == 1 && p == 0 && other.epsClosureList[curActive[0]] != nil
			if usePrecomputed {
				s := curActive[0]
				entries := other.epsClosureList[s]
				startW := curWeight[s]
				for i := range entries {
					entry := &entries[i]
					t := entry.target
					nw := startW + entry.relWeight
					if curGen[t] != gen || nw < curWeight[t] {
						wasNew := curGen[t] != gen
						curWeight[t] = nw
						curGen[t] = gen
						if wasNew {
							if len(curActive) < maxActiveStates {
								curActive = append(curActive, t)
							}
						}
						ctx.ctxSet(intP, t, nw, intP, entry.from, entry.olabel)
					}
				}
			} else {
				epsQueue := ctx.epsQueue
				epsQueue = epsQueue[:0]
				for i := 0; i < len(curActive); i++ {
					s := curActive[i]
					if int(s) < numStates && len(other.epsFlat[s]) > 0 {
						epsQueue = append(epsQueue, s)
					}
				}
				epsHead := 0
				const maxActiveEpsStates = 2000
				for epsHead < len(epsQueue) {
					s := epsQueue[epsHead]
					epsHead++
					if int(s) >= numStates {
						continue
					}
					curW := curWeight[s]
					epsFlat := other.epsFlat[s]
					if len(epsFlat) == 0 {
						continue
					}
					for i := range epsFlat {
						e := &epsFlat[i]
						nw := curW + e.weight
						t := e.next
						if int(t) >= numStates {
							continue
						}
						if curGen[t] != gen || nw < curWeight[t] {
							curWeight[t] = nw
							wasNew := curGen[t] != gen
							curGen[t] = gen
							epsQueue = append(epsQueue, t)
							if wasNew && len(curActive) < maxActiveEpsStates {
								curActive = append(curActive, t)
							}
							ctx.ctxSet(intP, t, nw, intP, s, e.olabel)
						}
					}
				}
			}
		}

		// Check for final states at this position
		for _, s := range curActive {
			if int(s) < numStates && other.States[s].Final {
				totalW := curWeight[s] + other.States[s].Weight
				if p > bestConsumed || (p == bestConsumed && totalW < bestWeight) {
					bestWeight = totalW
					bestConsumed = p
					bestEndState = s
				}
			}
		}

		// Character matching
		if p < effectiveLen {
			// Early termination: if no active states, no further matches possible
			if len(curActive) == 0 {
				break
			}
			matchLabel := inputLabels[p]
			if matchLabel < 0 {
				gen++
				curActive = curActive[:0]
				continue
			}
			gen++
			nextGen2 := gen
			nextActive = nextActive[:0]

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
						isNew := nextGen[t] != nextGen2
						nextWeight[t] = nw
						nextGen[t] = nextGen2
						if isNew {
							nextActive = append(nextActive, t)
						}
						ctx.ctxSet(int32(p+1), t, nw, int32(p), s, arc.OLabel)
					}
				}
			}

			// Swap: nextActive becomes curActive for the next position
			curActive, nextActive = nextActive, curActive[:0]

			// Copy weights from nextWeight to curWeight for next iteration
			for _, s := range curActive {
				curWeight[s] = nextWeight[s]
				curGen[s] = gen
			}
		}
	}

	if bestEndState < 0 || bestConsumed == 0 {
		return ComposePrefixResult{}
	}

	// Reconstruct output
	var olabels []int32
	curState := bestEndState
	curPos := int32(bestConsumed)
	for {
		_, fromPos, fromState, olabel, ok := ctx.ctxGet(curPos, curState)
		if !ok || fromPos < 0 {
			break
		}
		if olabel != EpsilonLabel {
			olabels = append(olabels, olabel)
		}
		curState = fromState
		curPos = fromPos
	}

	var output string
	if len(olabels) > 0 || other.States[bestEndState].FinalOLabel != EpsilonLabel {
		for i, j := 0, len(olabels)-1; i < j; i, j = i+1, j-1 {
			olabels[i], olabels[j] = olabels[j], olabels[i]
		}
		// Append final state output label (from RmOutputEpsilon)
		if other.States[bestEndState].FinalOLabel != EpsilonLabel {
			olabels = append(olabels, other.States[bestEndState].FinalOLabel)
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

// composeContext holds reusable data for composition operations.
// Uses dense arrays with generation counters instead of hash maps
// for better cache locality and performance.
type composeContext struct {
	stride      int
	bpFromPos   []int32
	bpFromState []int32
	bpOLabel    []int32
	posGen      []uint32
	curGen      uint32

	curActive  []int32
	nextActive []int32
	epsQueue   []int32
}

// getOrCreateComposeScratch returns a composeScratch for this FST, reusing cached
// arrays to avoid GC-triggered allocation. The scratch is stored in the FST and
// reused across calls. Generation counters avoid the need to clear arrays.
func (f *Fst) getOrCreateComposeScratch(numStates int) *composeScratch {
	if f.composeScratch == nil {
		f.composeScratch = &composeScratch{}
	}
	s := f.composeScratch
	if cap(s.curWeight) < numStates {
		s.curWeight = make([]float32, numStates)
		s.curGen = make([]uint32, numStates)
		s.nextWeight = make([]float32, numStates)
		s.nextGen = make([]uint32, numStates)
	} else {
		s.curWeight = s.curWeight[:numStates]
		s.curGen = s.curGen[:numStates]
		s.nextWeight = s.nextWeight[:numStates]
		s.nextGen = s.nextGen[:numStates]
	}
	s.callGen += 1000
	return s
}

// getOrCreateComposeContext returns a composeContext for this FST, reusing cached
// arrays to avoid GC-triggered allocation.
func (f *Fst) getOrCreateComposeContext(numPositions, numStates int) *composeContext {
	if f.composeCtx == nil {
		f.composeCtx = &composeContext{}
	}
	ctx := f.composeCtx
	ctx.curGen++
	if ctx.curGen == 0 {
		ctx.curGen = 1
	}

	totalSize := numPositions * numStates
	ctx.stride = numStates

	if cap(ctx.bpFromPos) < totalSize {
		ctx.bpFromPos = make([]int32, totalSize)
		ctx.bpFromState = make([]int32, totalSize)
		ctx.bpOLabel = make([]int32, totalSize)
		ctx.posGen = make([]uint32, totalSize)
	} else {
		ctx.bpFromPos = ctx.bpFromPos[:totalSize]
		ctx.bpFromState = ctx.bpFromState[:totalSize]
		ctx.bpOLabel = ctx.bpOLabel[:totalSize]
		ctx.posGen = ctx.posGen[:totalSize]
	}

	if cap(ctx.curActive) < numStates {
		ctx.curActive = make([]int32, 0, numStates)
	} else {
		ctx.curActive = ctx.curActive[:0]
	}
	if cap(ctx.nextActive) < numStates {
		ctx.nextActive = make([]int32, 0, numStates)
	} else {
		ctx.nextActive = ctx.nextActive[:0]
	}
	if cap(ctx.epsQueue) < numStates*2 {
		ctx.epsQueue = make([]int32, 0, numStates*2)
	} else {
		ctx.epsQueue = ctx.epsQueue[:0]
	}

	return ctx
}

func (ctx *composeContext) ctxSet(pos, state int32, weight float32, fromPos, fromState, olabel int32) {
	idx := int(pos)*ctx.stride + int(state)
	ctx.bpFromPos[idx] = fromPos
	ctx.bpFromState[idx] = fromState
	ctx.bpOLabel[idx] = olabel
	ctx.posGen[idx] = ctx.curGen
}

func (ctx *composeContext) ctxGet(pos, state int32) (float32, int32, int32, int32, bool) {
	idx := int(pos)*ctx.stride + int(state)
	if ctx.posGen[idx] != ctx.curGen {
		return 0, 0, 0, 0, false
	}
	return 0, ctx.bpFromPos[idx], ctx.bpFromState[idx], ctx.bpOLabel[idx], true
}

// composeScratch holds reusable arrays for composition operations.
// Uses dense arrays with generation counters instead of hash maps
// for better cache locality and performance. Stored in the FST to
// avoid GC-triggered reallocation of large arrays.
type composeScratch struct {
	curWeight  []float32
	curGen     []uint32
	nextWeight []float32
	nextGen    []uint32
	callGen    uint32 // per-instance generation base, incremented each call
}

func init() {
	gob.Register(&Fst{})
	gob.Register(&SymbolTable{})
}
