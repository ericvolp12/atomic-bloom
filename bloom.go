/*
Package bloom provides data structures and methods for creating Bloom filters.

A Bloom filter is a representation of a set of _n_ items, where the main
requirement is to make membership queries; _i.e._, whether an item is a
member of a set.

This implementation uses an atomic bitset for thread-safety.
*/
package bloom

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

// A BloomFilter is a representation of a set of _n_ items, where the main
// requirement is to make membership queries; _i.e._, whether an item is a
// member of a set.
type BloomFilter struct {
	m uint          // Number of bits
	k uint          // Number of hash functions
	b *atomicBitSet // The atomic bitset
}

func max(x, y uint) uint {
	if x > y {
		return x
	}
	return y
}

// New creates a new Bloom filter with _m_ bits and _k_ hashing functions
// We force _m_ and _k_ to be at least one to avoid panics.
func New(m uint, k uint) *BloomFilter {
	m = max(1, m)
	k = max(1, k)
	return &BloomFilter{m, k, newAtomicBitSet(m)}
}

// From creates a new Bloom filter with len(_data_) * 64 bits and _k_ hashing
// functions, initialized with the provided data.
func From(data []int64, k uint) *BloomFilter {
	m := uint(len(data) * 64)
	return FromWithM(data, m, k)
}

// FromWithM creates a new Bloom filter with _m_ length, _k_ hashing functions,
// initialized with the provided data.
func FromWithM(data []int64, m, k uint) *BloomFilter {
	k = max(1, k)
	return &BloomFilter{m, k, fromAtomicBitSet(data, m)}
}

// baseHashes returns the four hash values of data that are used to create k
// hashes
func baseHashes(data []byte) [4]uint64 {
	var d Digest128 // murmur hashing
	hash1, hash2, hash3, hash4 := d.Sum256(data)
	return [4]uint64{
		hash1, hash2, hash3, hash4,
	}
}

// location returns the ith hashed location using the four base hash values
func location(h [4]uint64, i uint) uint64 {
	ii := uint64(i)
	return h[ii%2] + ii*h[2+(((ii+(ii%2))%4)/2)]
}

// location returns the ith hashed location specific to this filter's size
func (f *BloomFilter) location(h [4]uint64, i uint) uint {
	return uint(location(h, i) % uint64(f.m))
}

// EstimateParameters estimates requirements for m and k.
func EstimateParameters(n uint, p float64) (m uint, k uint) {
	m = uint(math.Ceil(-1 * float64(n) * math.Log(p) / math.Pow(math.Log(2), 2)))
	k = uint(math.Ceil(math.Log(2) * float64(m) / float64(n)))
	// Ensure k is at least 1
	if k < 1 {
		k = 1
	}
	return
}

// NewWithEstimates creates a new Bloom filter for about n items with fp
// false positive rate
func NewWithEstimates(n uint, fp float64) *BloomFilter {
	m, k := EstimateParameters(n, fp)
	return New(m, k)
}

// Cap returns the capacity, _m_, of a Bloom filter
func (f *BloomFilter) Cap() uint {
	return f.m
}

// K returns the number of hash functions used in the BloomFilter
func (f *BloomFilter) K() uint {
	return f.k
}

// BitSet returns the underlying atomic bitset for this filter.
func (f *BloomFilter) BitSet() *atomicBitSet {
	return f.b
}

// Add data to the Bloom Filter. Returns the filter (allows chaining)
func (f *BloomFilter) Add(data []byte) *BloomFilter {
	h := baseHashes(data)
	for i := uint(0); i < f.k; i++ {
		f.b.Set(f.location(h, i))
	}
	return f
}

// Add precomputed hash values to the Bloom Filter. Returns the filter (allows chaining)
func (f *BloomFilter) AddHash(h [4]uint64) *BloomFilter {
	for i := uint(0); i < f.k; i++ {
		f.b.Set(f.location(h, i))
	}
	return f
}

// Merge the data from another Bloom Filter. Returns error if parameters don't match.
func (f *BloomFilter) Merge(g *BloomFilter) error {
	if f.m != g.m {
		return fmt.Errorf("m's don't match: %d != %d", f.m, g.m)
	}
	if f.k != g.k {
		return fmt.Errorf("k's don't match: %d != %d", f.k, g.k) // Corrected error message
	}

	f.b.InPlaceUnion(g.b)
	return nil
}

// Copy creates a copy of a Bloom filter.
func (f *BloomFilter) Copy() *BloomFilter {
	fc := New(f.m, f.k)
	// Manually copy the bitset data for a deep copy
	for i := range f.b.data {
		fc.b.data[i].Store(f.b.data[i].Load())
	}
	return fc
}

// AddString adds a string to the Bloom Filter.
func (f *BloomFilter) AddString(data string) *BloomFilter {
	return f.Add([]byte(data))
}

// Test returns true if the data is *probably* in the BloomFilter, false otherwise.
func (f *BloomFilter) Test(data []byte) bool {
	h := baseHashes(data)
	for i := uint(0); i < f.k; i++ {
		if !f.b.Test(f.location(h, i)) {
			return false
		}
	}
	return true
}

// TestHash returns true if the hash is *probably* in the BloomFilter.
func (f *BloomFilter) TestHash(h [4]uint64) bool {
	for i := uint(0); i < f.k; i++ {
		if !f.b.Test(f.location(h, i)) {
			return false
		}
	}
	return true
}

// TestString returns true if the string is *probably* in the BloomFilter.
func (f *BloomFilter) TestString(data string) bool {
	return f.Test([]byte(data))
}

// TestLocations returns true if all locations are set in the BloomFilter.
func (f *BloomFilter) TestLocations(locs []uint64) bool {
	for _, loc := range locs {
		if !f.b.Test(uint(loc % uint64(f.m))) {
			return false
		}
	}
	return true
}

// TestAndAdd checks membership and adds the data unconditionally.
// Returns true if the element was *probably* present before adding.
func (f *BloomFilter) TestAndAdd(data []byte) bool {
	present := true
	h := baseHashes(data)
	for i := uint(0); i < f.k; i++ {
		l := f.location(h, i)
		if !f.b.Test(l) {
			present = false
		}
		f.b.Set(l) // Set the bit regardless
	}
	return present
}

// TestAndAddString is the string version of TestAndAdd.
func (f *BloomFilter) TestAndAddString(data string) bool {
	return f.TestAndAdd([]byte(data))
}

// TestOrAdd checks membership and adds the data only if not present.
// Returns true if the element was *probably* present before adding.
// Note: Due to the nature of atomics, this isn't truly conditional on *all*
// bits being present beforehand if run concurrently. It ensures each bit
// is set if it wasn't already.
func (f *BloomFilter) TestOrAdd(data []byte) bool {
	present := true
	h := baseHashes(data)
	for i := uint(0); i < f.k; i++ {
		l := f.location(h, i)
		if !f.b.Test(l) {
			present = false
			f.b.Set(l) // Set the bit if not present
		}
	}
	return present
}

// TestOrAddString is the string version of TestOrAdd.
func (f *BloomFilter) TestOrAddString(data string) bool {
	return f.TestOrAdd([]byte(data))
}

// ClearAll clears all the data in a Bloom filter.
func (f *BloomFilter) ClearAll() *BloomFilter {
	f.b.ClearAll()
	return f
}

// EstimateFalsePositiveRate estimates the empirical false positive rate.
// Uses a temporary filter.
func EstimateFalsePositiveRate(m, k, n uint) (fpRate float64) {
	rounds := uint32(100000)
	f := New(m, k) // Uses the new atomic-backed filter
	n1 := make([]byte, 4)
	for i := uint32(0); i < uint32(n); i++ {
		binary.BigEndian.PutUint32(n1, i)
		f.Add(n1)
	}
	fp := 0
	for i := uint32(0); i < rounds; i++ {
		binary.BigEndian.PutUint32(n1, i+uint32(n)+1) // Test elements not added
		if f.Test(n1) {
			fp++
		}
	}
	fpRate = float64(fp) / float64(rounds)
	return
}

// ApproximatedSize estimates the number of items added to the filter.
func (f *BloomFilter) ApproximatedSize() int64 {
	m := float64(f.Cap())
	k := float64(f.K())
	x := float64(f.b.Count())       // Use the Count method of atomicBitSet
	if m == 0 || k == 0 || m == x { // Avoid division by zero or log(0)
		// Cannot estimate, or filter is full.
		// Returning 0 or an indicator might be appropriate.
		// Or return an estimate based on m/k if x is close to m.
		// For simplicity, returning 0 here.
		if m == x && m > 0 && k > 0 {
			// A rough upper bound guess if full, though inaccurate
			return int64(m / k)
		}
		return 0
	}
	// Formula: - (m / k) * ln(1 - x / m)
	return int64(-m / k * math.Log(1-x/m))
}

// bloomFilterJSON is an unexported type for marshaling/unmarshaling BloomFilter struct.
type bloomFilterJSON struct {
	M uint          `json:"m"`
	K uint          `json:"k"`
	B *atomicBitSet `json:"b"` // Use atomicBitSet
}

// MarshalJSON implements json.Marshaler interface.
func (f BloomFilter) MarshalJSON() ([]byte, error) {
	return json.Marshal(bloomFilterJSON{f.m, f.k, f.b})
}

// UnmarshalJSON implements json.Unmarshaler interface.
func (f *BloomFilter) UnmarshalJSON(data []byte) error {
	var j bloomFilterJSON
	// Need to initialize B so UnmarshalJSON on atomicBitSet works
	j.B = &atomicBitSet{}
	err := json.Unmarshal(data, &j)
	if err != nil {
		return err
	}
	f.m = j.M
	f.k = j.K
	f.b = j.B
	return nil
}

// WriteTo writes a binary representation of the BloomFilter to an i/o stream.
func (f *BloomFilter) WriteTo(stream io.Writer) (int64, error) {
	var totalBytes int64

	// Write m
	err := binary.Write(stream, binary.BigEndian, uint64(f.m))
	if err != nil {
		return totalBytes, err
	}
	totalBytes += int64(binary.Size(uint64(0)))

	// Write k
	err = binary.Write(stream, binary.BigEndian, uint64(f.k))
	if err != nil {
		return totalBytes, err
	}
	totalBytes += int64(binary.Size(uint64(0)))

	// Write the atomicBitSet
	numBytes, err := f.b.WriteTo(stream)
	totalBytes += numBytes
	return totalBytes, err
}

// ReadFrom reads a binary representation of the BloomFilter from an i/o stream.
func (f *BloomFilter) ReadFrom(stream io.Reader) (int64, error) {
	var totalBytes int64
	var m, k uint64

	// Read m
	err := binary.Read(stream, binary.BigEndian, &m)
	if err != nil {
		return totalBytes, err
	}
	f.m = uint(m)
	totalBytes += int64(binary.Size(uint64(0)))

	// Read k
	err = binary.Read(stream, binary.BigEndian, &k)
	if err != nil {
		return totalBytes, err
	}
	f.k = uint(k)
	totalBytes += int64(binary.Size(uint64(0)))

	// Read the atomicBitSet
	f.b = &atomicBitSet{} // Initialize before reading into it
	numBytes, err := f.b.ReadFrom(stream)
	totalBytes += numBytes
	return totalBytes, err
}

// GobEncode implements gob.GobEncoder interface.
func (f *BloomFilter) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	_, err := f.WriteTo(&buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder interface.
func (f *BloomFilter) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	_, err := f.ReadFrom(buf)
	return err
}

// MarshalBinary implements encoding.BinaryMarshaler interface.
func (f *BloomFilter) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	_, err := f.WriteTo(&buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler interface.
func (f *BloomFilter) UnmarshalBinary(data []byte) error {
	buf := bytes.NewBuffer(data)
	_, err := f.ReadFrom(buf)
	return err
}

// Equal tests for the equality of two Bloom filters
func (f *BloomFilter) Equal(g *BloomFilter) bool {
	return f.m == g.m && f.k == g.k && f.b.Equal(g.b)
}

// Locations returns a list of hash locations representing a data item.
// This function remains independent of the bitset implementation.
func Locations(data []byte, k uint) []uint64 {
	locs := make([]uint64, k)
	h := baseHashes(data)
	for i := uint(0); i < k; i++ {
		locs[i] = location(h, i)
	}
	return locs
}

// --- Murmur hash implementation (digest128, etc.) remains unchanged ---
// ... (Keep the existing murmur hash code from murmur.go or include it here) ...
// NOTE: For this example, assuming murmur.go exists and provides digest128 and its methods.
// If murmur.go is not separate, its contents should be included here.
