package pynini

import (
	"bytes"
	"encoding/gob"
)

// SymbolTable provides a bidirectional mapping between string labels and int32 IDs.
// Label 0 is always reserved for epsilon (empty string).
// This is the key optimization: arcs store int32 labels instead of strings,
// drastically reducing memory usage and improving comparison speed.
type SymbolTable struct {
	symToID map[string]int32
	idToSym []string
}

// GobEncode implements gob.GobEncoder for SymbolTable.
// Required because symToID and idToSym are unexported fields
// which gob cannot encode by default.
func (st *SymbolTable) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(st.idToSym); err != nil {
		return nil, err
	}
	// Encode symToID as a slice of key-value pairs for deterministic encoding
	type kv struct {
		K string
		V int32
	}
	pairs := make([]kv, 0, len(st.symToID))
	for k, v := range st.symToID {
		pairs = append(pairs, kv{K: k, V: v})
	}
	if err := enc.Encode(pairs); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder for SymbolTable.
func (st *SymbolTable) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	if err := dec.Decode(&st.idToSym); err != nil {
		return err
	}
	type kv struct {
		K string
		V int32
	}
	var pairs []kv
	if err := dec.Decode(&pairs); err != nil {
		return err
	}
	st.symToID = make(map[string]int32, len(pairs))
	for _, p := range pairs {
		st.symToID[p.K] = p.V
	}
	return nil
}

// NewSymbolTable creates a new symbol table with epsilon (label 0) pre-registered.
func NewSymbolTable() *SymbolTable {
	st := &SymbolTable{
		symToID: make(map[string]int32),
		idToSym: make([]string, 1),
	}
	st.idToSym[0] = "" // epsilon
	st.symToID[""] = 0
	return st
}

// Add adds a symbol and returns its ID. If the symbol already exists, returns its existing ID.
func (st *SymbolTable) Add(sym string) int32 {
	if id, ok := st.symToID[sym]; ok {
		return id
	}
	id := int32(len(st.idToSym))
	st.idToSym = append(st.idToSym, sym)
	st.symToID[sym] = id
	return id
}

// Find returns the ID for a symbol, or -1 if not found.
// Does NOT auto-add the symbol.
func (st *SymbolTable) Find(sym string) int32 {
	if id, ok := st.symToID[sym]; ok {
		return id
	}
	return -1
}

// FindOrAdd returns the ID for a symbol, adding it if not already present.
func (st *SymbolTable) FindOrAdd(sym string) int32 {
	if id, ok := st.symToID[sym]; ok {
		return id
	}
	return st.Add(sym)
}

// Symbol returns the string for a given ID. Returns "" for unknown IDs.
func (st *SymbolTable) Symbol(id int32) string {
	if id < 0 || int(id) >= len(st.idToSym) {
		return ""
	}
	return st.idToSym[id]
}

// Size returns the number of symbols (including epsilon).
func (st *SymbolTable) Size() int {
	return len(st.idToSym)
}

// Copy creates a deep copy of the symbol table.
func (st *SymbolTable) Copy() *SymbolTable {
	ns := &SymbolTable{
		symToID: make(map[string]int32, len(st.symToID)),
		idToSym: make([]string, len(st.idToSym)),
	}
	copy(ns.idToSym, st.idToSym)
	for k, v := range st.symToID {
		ns.symToID[k] = v
	}
	return ns
}

// Merge adds all symbols from another symbol table and returns a mapping
// from old IDs (in other) to new IDs (in this table).
func (st *SymbolTable) Merge(other *SymbolTable) []int32 {
	if other == nil {
		return nil
	}
	mapping := make([]int32, other.Size())
	for i, sym := range other.idToSym {
		mapping[i] = st.FindOrAdd(sym)
	}
	return mapping
}

// EpsilonLabel returns the epsilon label (always 0).
const EpsilonLabel int32 = 0