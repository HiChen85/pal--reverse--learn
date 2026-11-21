// Package pal provides bounded, read-only inspection of PAL resource containers.
// It does not decompress chunks or execute game scripts.
package pal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	// MaxChunks limits directory memory consumption when inspecting untrusted files.
	MaxChunks = 1 << 20
	// MaxChunkBytes limits the allocation made by a single ReadChunk call.
	// Open and ChunkSize can inventory larger chunks without loading them.
	MaxChunkBytes = 64 << 20
)

// Archive is an MKF offset directory. The caller owns the underlying reader and
// must keep it open and unchanged for the archive's lifetime. Trailing bytes
// beyond the last directory offset are allowed, but are not part of any chunk.
// There is no write API and the archive does not own or close its reader.
type Archive struct {
	reader  io.ReaderAt
	offsets []uint32
}

// Open validates an MKF directory consisting of little-endian uint32 offsets.
// The first offset is the directory byte length; consecutive offsets delimit
// chunks. Equal consecutive offsets represent an empty chunk. All offsets must
// lie within the supplied file size. No chunk data is loaded by Open.
func Open(reader io.ReaderAt, size int64) (*Archive, error) {
	if reader == nil {
		return nil, errors.New("MKF: nil reader")
	}
	if size < 4 || size > math.MaxUint32 {
		return nil, fmt.Errorf("MKF: unsupported file size %d", size)
	}
	var head [4]byte
	if err := readExact(reader, head[:], 0); err != nil {
		return nil, fmt.Errorf("MKF: read header: %w", err)
	}
	first := binary.LittleEndian.Uint32(head[:])
	if first < 4 || first%4 != 0 || int64(first) > size {
		return nil, fmt.Errorf("MKF: invalid directory length %d", first)
	}
	count := uint64(first)/4 - 1
	if count > MaxChunks {
		return nil, fmt.Errorf("MKF: chunk count %d exceeds limit %d", count, MaxChunks)
	}
	table := make([]byte, int(first))
	if err := readExact(reader, table, 0); err != nil {
		return nil, fmt.Errorf("MKF: read directory: %w", err)
	}
	offsets := make([]uint32, int(count)+1)
	previous := first
	for i := range offsets {
		offset := binary.LittleEndian.Uint32(table[i*4:])
		if offset < previous || int64(offset) > size {
			return nil, fmt.Errorf("MKF: invalid offset %d at index %d", offset, i)
		}
		// A changed header between reads must not change the directory layout.
		if i == 0 && offset != first {
			return nil, errors.New("MKF: directory changed while reading")
		}
		offsets[i] = offset
		previous = offset
	}
	return &Archive{reader: reader, offsets: offsets}, nil
}

// Count reports the number of chunks, including empty chunks.
func (a *Archive) Count() int {
	if a == nil || len(a.offsets) == 0 {
		return 0
	}
	return len(a.offsets) - 1
}

// ChunkSize returns the compressed/on-disk size. It does not interpret payloads.
func (a *Archive) ChunkSize(index int) (int64, error) {
	if a == nil || index < 0 || index >= a.Count() {
		return 0, fmt.Errorf("MKF: chunk index %d out of range", index)
	}
	return int64(a.offsets[index+1]) - int64(a.offsets[index]), nil
}

// ReadChunk returns an independent byte slice. Empty chunks succeed with an
// empty slice. Chunks larger than MaxChunkBytes must be inspected by size only.
func (a *Archive) ReadChunk(index int) ([]byte, error) {
	size, err := a.ChunkSize(index)
	if err != nil {
		return nil, err
	}
	if size > MaxChunkBytes {
		return nil, fmt.Errorf("MKF: chunk %d size %d exceeds read limit %d", index, size, MaxChunkBytes)
	}
	data := make([]byte, int(size))
	if len(data) > 0 {
		if err := readExact(a.reader, data, int64(a.offsets[index])); err != nil {
			return nil, fmt.Errorf("MKF: read chunk %d: %w", index, err)
		}
	}
	return data, nil
}

func readExact(reader io.ReaderAt, data []byte, offset int64) error {
	n, err := reader.ReadAt(data, offset)
	if n == len(data) {
		return nil
	}
	if err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}
