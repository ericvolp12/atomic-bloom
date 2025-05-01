package bloom

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/bits"
	"sync/atomic"
)

// atomicBitSet is a thread-safe bitset implementation using atomic operations.
type atomicBitSet struct {
	data []atomic.Int64
	size uint
}

// newAtomicBitSet creates a new atomicBitSet with a given size in bits.
func newAtomicBitSet(size uint) *atomicBitSet {
	numInts := (size + 63) / 64
	return &atomicBitSet{
		data: make([]atomic.Int64, numInts),
		size: size,
	}
}

// fromAtomicBitSet creates a new atomicBitSet from existing data.
// The data slice represents the bitset content.
func fromAtomicBitSet(data []int64, size uint) *atomicBitSet {
	abs := newAtomicBitSet(size)
	for i, v := range data {
		if i < len(abs.data) {
			abs.data[i].Store(v)
		}
	}
	return abs
}

// Set sets the bit at the given index i.
func (bs *atomicBitSet) Set(i uint) {
	if i >= bs.size {
		// Or handle error/panic as appropriate
		return
	}
	index := i / 64
	pos := i % 64
	mask := int64(1) << pos
	bs.data[index].Or(mask)
}

// Test checks if the bit at the given index i is set.
func (bs *atomicBitSet) Test(i uint) bool {
	if i >= bs.size {
		return false
	}
	index := i / 64
	pos := i % 64
	mask := int64(1) << pos
	return (bs.data[index].Load() & mask) != 0
}

// ClearAll resets all bits to zero.
func (bs *atomicBitSet) ClearAll() {
	for i := range bs.data {
		bs.data[i].Store(0)
	}
}

// Equal checks if two atomicBitSets are equal.
func (bs *atomicBitSet) Equal(other *atomicBitSet) bool {
	if bs.size != other.size || len(bs.data) != len(other.data) {
		return false
	}
	for i := range bs.data {
		if bs.data[i].Load() != other.data[i].Load() {
			return false
		}
	}
	return true
}

// InPlaceUnion performs a bitwise OR operation with another atomicBitSet.
// Assumes both bitsets have the same size.
func (bs *atomicBitSet) InPlaceUnion(other *atomicBitSet) {
	for i := range bs.data {
		bs.data[i].Or(other.data[i].Load())
	}
}

// Count returns the number of set bits.
func (bs *atomicBitSet) Count() uint {
	var count uint
	for i := range bs.data {
		count += uint(bits.OnesCount64(uint64(bs.data[i].Load())))
	}
	return count
}

// WriteTo writes the bitset data to a stream.
func (bs *atomicBitSet) WriteTo(stream io.Writer) (int64, error) {
	var totalBytes int64
	// Write size first
	err := binary.Write(stream, binary.BigEndian, uint64(bs.size))
	if err != nil {
		return totalBytes, err
	}
	totalBytes += int64(binary.Size(uint64(0)))

	// Write data length
	dataLen := uint64(len(bs.data))
	err = binary.Write(stream, binary.BigEndian, dataLen)
	if err != nil {
		return totalBytes, err
	}
	totalBytes += int64(binary.Size(uint64(0)))

	// Write data content
	for i := range bs.data {
		val := bs.data[i].Load()
		err = binary.Write(stream, binary.BigEndian, val)
		if err != nil {
			return totalBytes, err
		}
		totalBytes += int64(binary.Size(val))
	}
	return totalBytes, nil
}

// ReadFrom reads the bitset data from a stream.
func (bs *atomicBitSet) ReadFrom(stream io.Reader) (int64, error) {
	var totalBytes int64
	var size uint64
	// Read size
	err := binary.Read(stream, binary.BigEndian, &size)
	if err != nil {
		return totalBytes, err
	}
	bs.size = uint(size)
	totalBytes += int64(binary.Size(uint64(0)))

	// Read data length
	var dataLen uint64
	err = binary.Read(stream, binary.BigEndian, &dataLen)
	if err != nil {
		return totalBytes, err
	}
	totalBytes += int64(binary.Size(uint64(0)))

	// Read data content
	bs.data = make([]atomic.Int64, dataLen)
	for i := uint64(0); i < dataLen; i++ {
		var val int64
		err = binary.Read(stream, binary.BigEndian, &val)
		if err != nil {
			return totalBytes, err
		}
		bs.data[i].Store(val)
		totalBytes += int64(binary.Size(val))
	}
	return totalBytes, nil
}

// MarshalJSON implements json.Marshaler interface.
func (bs *atomicBitSet) MarshalJSON() ([]byte, error) {
	rawData := make([]int64, len(bs.data))
	for i := range bs.data {
		rawData[i] = bs.data[i].Load()
	}
	return json.Marshal(map[string]interface{}{
		"size": bs.size,
		"data": rawData,
	})
}

// UnmarshalJSON implements json.Unmarshaler interface.
func (bs *atomicBitSet) UnmarshalJSON(data []byte) error {
	var j map[string]interface{}
	err := json.Unmarshal(data, &j)
	if err != nil {
		return err
	}

	sizeFloat, ok := j["size"].(float64)
	if !ok {
		return fmt.Errorf("invalid size type in JSON")
	}
	bs.size = uint(sizeFloat)

	rawDataInterface, ok := j["data"].([]interface{})
	if !ok {
		return fmt.Errorf("invalid data type in JSON")
	}

	bs.data = make([]atomic.Int64, len(rawDataInterface))
	for i, v := range rawDataInterface {
		valFloat, ok := v.(float64)
		if !ok {
			return fmt.Errorf("invalid data element type in JSON")
		}
		bs.data[i].Store(int64(valFloat))
	}
	return nil
}
