package rules

import "sync"

// Shared singleton instances for Cardinal, Decimal, and Ordinal.
// Index 0 = deterministic=false, Index 1 = deterministic=true.
// Using sync.Once ensures each variant is created only once, eliminating
// redundant Processor and base FST construction across rule files.

var (
	sharedCardinals [2]*Cardinal
	sharedDecimals  [2]*Decimal
	sharedOrdinals  [2]*Ordinal
	cardinalOnce    [2]sync.Once
	decimalOnce     [2]sync.Once
	ordinalOnce     [2]sync.Once
)

func deterministicIndex(d bool) int {
	if d {
		return 1
	}
	return 0
}

// getSharedCardinal returns a singleton Cardinal instance for the given
// deterministic mode. The first call creates the instance; subsequent calls
// return the same instance.
func getSharedCardinal(deterministic bool) *Cardinal {
	idx := deterministicIndex(deterministic)
	cardinalOnce[idx].Do(func() {
		sharedCardinals[idx] = newCardinalInternal(deterministic)
		// Release base FSTs after construction — they are not needed at runtime.
		sharedCardinals[idx].ReleaseBaseFsts()
	})
	return sharedCardinals[idx]
}

// getSharedDecimal returns a singleton Decimal instance for the given
// deterministic mode.
func getSharedDecimal(deterministic bool) *Decimal {
	idx := deterministicIndex(deterministic)
	decimalOnce[idx].Do(func() {
		sharedDecimals[idx] = newDecimalInternal(deterministic)
		// Release base FSTs after construction — they are not needed at runtime.
		sharedDecimals[idx].ReleaseBaseFsts()
	})
	return sharedDecimals[idx]
}

// getSharedOrdinal returns a singleton Ordinal instance for the given
// deterministic mode.
func getSharedOrdinal(deterministic bool) *Ordinal {
	idx := deterministicIndex(deterministic)
	ordinalOnce[idx].Do(func() {
		sharedOrdinals[idx] = newOrdinalInternal(deterministic)
		// Release base FSTs after construction — they are not needed at runtime.
		sharedOrdinals[idx].ReleaseBaseFsts()
	})
	return sharedOrdinals[idx]
}

// ResetSharedInstances clears all shared singleton instances.
// This is only needed for testing to force re-creation.
func ResetSharedInstances() {
	sharedCardinals = [2]*Cardinal{}
	sharedDecimals = [2]*Decimal{}
	sharedOrdinals = [2]*Ordinal{}
	cardinalOnce = [2]sync.Once{}
	decimalOnce = [2]sync.Once{}
	ordinalOnce = [2]sync.Once{}
}
