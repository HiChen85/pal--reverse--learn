// Package palasset imports bounded, read-only metadata from local PAL assets.
// It does not render pixels, execute resource contents, or distribute game data.
package palasset

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/HiChen85/pal--reverse--learn/internal/pal"
	"github.com/HiChen85/pal--reverse--learn/internal/palcodec"
)

const (
	MaxArchiveBytes = 64 << 20
	MaxSpriteCount  = 65536
	maxCacheBytes   = 8 << 20
	maxCacheEntries = 64
)

// FrameInfo describes a directory span. Safe means its byte range is within the
// decoded sprite and outside its directory. It does not certify an RLE payload.
type FrameInfo struct {
	Index       int
	Offset, End int
	Safe        bool
}

type UnsafeFrame struct {
	Index  int
	Reason string
}

type SpriteInfo struct {
	ID             uint16
	CompressedSize int64
	DecodedSize    int
	SHA256         string
	FrameCount     uint16
	Frames         []FrameInfo
	// Index -1 describes a malformed sprite-level header/directory. Individual
	// malformed frame offsets do not prevent inspection of other sprite IDs.
	UnsafeFrames []UnsafeFrame
}

type SpriteCatalog struct {
	archive      *pal.Archive
	maxDecoded   int
	sourceSize   int64
	sourceSHA256 string
	mu           sync.Mutex
	cache        map[uint16]SpriteInfo
	cacheOrder   []uint16
	cacheBytes   int
}

// OpenMGO takes an immutable in-memory snapshot of one regular, non-symlink
// archive of at most 64 MiB. maxDecoded bounds each on-demand YJ1 decode and
// must be in 1..64 MiB. There is no file handle to close. Resource payloads are
// decoded only by Sprite; opening an archive cannot fail because a different
// sprite's compressed stream or frame directory is malformed.
func OpenMGO(path string, maxDecoded int) (*SpriteCatalog, error) {
	if maxDecoded < 1 || maxDecoded > palcodec.MaxYJ1Bytes {
		return nil, fmt.Errorf("MGO: maxDecoded outside 1..%d", palcodec.MaxYJ1Bytes)
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("MGO: requires regular file; symlinks are not accepted")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return nil, errors.New("MGO: file changed identity while opening")
	}
	if info.Size() > MaxArchiveBytes {
		return nil, fmt.Errorf("MGO: file size %d exceeds limit %d", info.Size(), MaxArchiveBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxArchiveBytes || int64(len(data)) != info.Size() {
		return nil, errors.New("MGO: resource exceeds bound or changed size while reading")
	}
	archive, err := pal.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("MGO: %w", err)
	}
	if archive.Count() > MaxSpriteCount {
		return nil, fmt.Errorf("MGO: sprite count %d exceeds %d", archive.Count(), MaxSpriteCount)
	}
	sum := sha256.Sum256(data)
	return &SpriteCatalog{archive: archive, maxDecoded: maxDecoded, sourceSize: int64(len(data)), sourceSHA256: fmt.Sprintf("%x", sum), cache: make(map[uint16]SpriteInfo)}, nil
}

func (c *SpriteCatalog) Count() int {
	if c == nil {
		return 0
	}
	return c.archive.Count()
}
func (c *SpriteCatalog) SourceSize() int64 {
	if c == nil {
		return 0
	}
	return c.sourceSize
}
func (c *SpriteCatalog) SourceSHA256() string {
	if c == nil {
		return ""
	}
	return c.sourceSHA256
}

// Sprite returns independent metadata slices. Successfully decoded sprites can
// contain UnsafeFrames, which callers should report instead of applying the
// reference engine's broken-sprite offset hacks. Pixel data is not retained.
// The metadata-only FIFO cache is bounded to 8 MiB and 64 entries. Cache access
// is synchronized; independent decodes can execute concurrently.
func (c *SpriteCatalog) Sprite(id uint16) (SpriteInfo, error) {
	if c == nil || c.archive == nil {
		return SpriteInfo{}, errors.New("MGO: nil sprite catalog")
	}
	c.mu.Lock()
	cached, ok := c.cache[id]
	c.mu.Unlock()
	if ok {
		return cloneInfo(cached), nil
	}
	compressed, err := c.archive.ReadChunk(int(id))
	if err != nil {
		return SpriteInfo{}, fmt.Errorf("MGO sprite %d: %w", id, err)
	}
	if len(compressed) == 0 {
		return SpriteInfo{}, fmt.Errorf("MGO sprite %d: empty chunk", id)
	}
	decoded, err := palcodec.DecodeYJ1(compressed, c.maxDecoded)
	if err != nil {
		return SpriteInfo{}, fmt.Errorf("MGO sprite %d: %w", id, err)
	}
	meta := inspectSprite(id, int64(len(compressed)), decoded)
	cost := metadataCost(meta)
	if cost <= maxCacheBytes {
		c.mu.Lock()
		if _, exists := c.cache[id]; !exists {
			for len(c.cacheOrder) > 0 && (len(c.cache) >= maxCacheEntries || c.cacheBytes+cost > maxCacheBytes) {
				old := c.cacheOrder[0]
				c.cacheOrder = c.cacheOrder[1:]
				c.cacheBytes -= metadataCost(c.cache[old])
				delete(c.cache, old)
			}
			c.cache[id] = meta
			c.cacheOrder = append(c.cacheOrder, id)
			c.cacheBytes += cost
		}
		c.mu.Unlock()
	}
	return cloneInfo(meta), nil
}

func cloneInfo(info SpriteInfo) SpriteInfo {
	info.Frames = append([]FrameInfo(nil), info.Frames...)
	info.UnsafeFrames = append([]UnsafeFrame(nil), info.UnsafeFrames...)
	return info
}
func metadataCost(info SpriteInfo) int {
	// Conservative allocation estimate: integer slots, struct padding, slice/
	// string headers and immutable reason bytes. Archive data is separately bound.
	n := 256 + len(info.Frames)*40 + len(info.UnsafeFrames)*40
	for _, issue := range info.UnsafeFrames {
		n += len(issue.Reason)
	}
	return n
}

func inspectSprite(id uint16, compressed int64, data []byte) SpriteInfo {
	sum := sha256.Sum256(data)
	x := SpriteInfo{ID: id, CompressedSize: compressed, DecodedSize: len(data), SHA256: fmt.Sprintf("%x", sum)}
	if len(data) < 2 {
		x.UnsafeFrames = append(x.UnsafeFrames, UnsafeFrame{-1, "sprite header shorter than one WORD"})
		return x
	}
	words := int(binary.LittleEndian.Uint16(data))
	if words == 0 {
		x.UnsafeFrames = append(x.UnsafeFrames, UnsafeFrame{-1, "zero sprite directory length"})
		return x
	}
	// Public PAL_SpriteGetNumFrames returns firstWORD - 1. The first WORD is
	// simultaneously the first frame's WORD offset; the last entry is an end.
	x.FrameCount = uint16(words - 1)
	directoryBytes := words * 2
	if directoryBytes > len(data) {
		x.UnsafeFrames = append(x.UnsafeFrames, UnsafeFrame{-1, "frame directory extends outside decoded sprite"})
		return x
	}
	x.Frames = make([]FrameInfo, int(x.FrameCount))
	for i := range x.Frames {
		start := int(binary.LittleEndian.Uint16(data[i*2:])) * 2
		end := int(binary.LittleEndian.Uint16(data[(i+1)*2:])) * 2
		frame := FrameInfo{Index: i, Offset: start, End: end, Safe: true}
		reason := ""
		switch {
		case start < directoryBytes:
			reason = "frame starts inside directory"
		case start >= len(data):
			reason = "frame starts outside decoded sprite"
		case end <= start:
			reason = "frame end does not follow its start"
		case end > len(data):
			reason = "frame ends outside decoded sprite"
		}
		if reason != "" {
			frame.Safe = false
			x.UnsafeFrames = append(x.UnsafeFrames, UnsafeFrame{i, reason})
		}
		x.Frames[i] = frame
	}
	return x
}
