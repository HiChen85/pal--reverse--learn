package pal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"
)

func directory(offsets ...uint32) []byte {
	b := make([]byte, 4*len(offsets))
	for i, offset := range offsets {
		binary.LittleEndian.PutUint32(b[i*4:], offset)
	}
	return b
}

func TestArchiveEmptyAndNonemptyChunks(t *testing.T) {
	data := append(directory(16, 19, 19, 21), []byte("abcde")...)
	a, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if a.Count() != 3 {
		t.Fatalf("count = %d", a.Count())
	}
	for i, want := range []string{"abc", "", "de"} {
		got, err := a.ReadChunk(i)
		if err != nil || string(got) != want {
			t.Fatalf("chunk %d = %q, %v", i, got, err)
		}
		size, err := a.ChunkSize(i)
		if err != nil || size != int64(len(want)) {
			t.Fatalf("size %d = %d, %v", i, size, err)
		}
	}
	chunk, _ := a.ReadChunk(0)
	chunk[0] = 'X'
	fresh, _ := a.ReadChunk(0)
	if string(fresh) != "abc" {
		t.Fatal("chunk aliases backing reader")
	}
	for _, index := range []int{-1, 3, math.MaxInt} {
		if _, err := a.ReadChunk(index); err == nil {
			t.Fatalf("accepted index %d", index)
		}
	}
}

func TestOpenRejectsMalformedDirectory(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		size int64
	}{
		{"short file", []byte{4}, 1},
		{"short header", []byte{4}, 4},
		{"zero offset", directory(0), 4},
		{"unaligned", directory(7, 8), 8},
		{"table outside file", directory(12, 12), 8},
		{"truncated table", directory(12, 12), 12},
		{"descending offsets", directory(12, 15, 14), 15},
		{"offset outside file", directory(8, 99), 8},
		{"too many chunks", directory((MaxChunks + 2) * 4), (MaxChunks + 2) * 4},
		{"too large file", directory(4), int64(math.MaxUint32) + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Open(bytes.NewReader(tc.data), tc.size); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	if _, err := Open(nil, 4); err == nil {
		t.Fatal("accepted nil reader")
	}
}

func TestZeroChunksAndTrailingBytes(t *testing.T) {
	for _, data := range [][]byte{directory(4), append(directory(4), 'x')} {
		a, err := Open(bytes.NewReader(data), int64(len(data)))
		if err != nil || a.Count() != 0 {
			t.Fatalf("zero chunk archive: %v", err)
		}
	}
}

func TestChunkReadLimitBeforeAllocation(t *testing.T) {
	data := directory(8, 8+MaxChunkBytes+1)
	a, err := Open(bytes.NewReader(data), 8+MaxChunkBytes+1)
	if err != nil {
		t.Fatal(err)
	}
	if size, _ := a.ChunkSize(0); size != MaxChunkBytes+1 {
		t.Fatal(size)
	}
	if _, err := a.ReadChunk(0); err == nil {
		t.Fatal("large allocation allowed")
	}
}

func TestTruncatedChunkReadFails(t *testing.T) {
	data := directory(8, 11)
	a, err := Open(bytes.NewReader(data), 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReadChunk(0); !errors.Is(err, io.EOF) {
		t.Fatalf("got %v", err)
	}
}

type shortReader struct{}

func (shortReader) ReadAt(p []byte, offset int64) (int, error) { return 0, nil }

func TestBrokenReaderShortRead(t *testing.T) {
	if _, err := Open(shortReader{}, 4); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %v", err)
	}
}

func FuzzOpen(f *testing.F) {
	f.Add(directory(4))
	f.Add(append(directory(8, 10), 'x', 'y'))
	f.Add([]byte("bad"))
	f.Fuzz(func(t *testing.T, data []byte) {
		a, err := Open(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return
		}
		if a.Count() < 0 || a.Count() > MaxChunks {
			t.Fatal("count limit")
		}
		// Keep fuzzing independent of input chunk count and avoid repeated copies.
		if a.Count() > 0 {
			i := a.Count() - 1
			chunk, err := a.ReadChunk(i)
			if err == nil && len(chunk) > MaxChunkBytes {
				t.Fatal("read limit")
			}
		}
	})
}
