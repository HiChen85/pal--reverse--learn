package palasset

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/HiChen85/pal--reverse--learn/internal/pal"
	"github.com/HiChen85/pal--reverse--learn/internal/palcodec"
)

// Format research: SDLPAL map.h/map.c and palcommon.c (GPL-3.0-or-later).
// This importer independently reads the little-endian tile layout and WORD
// sprite directory. It does not reproduce the reference's broken-frame hacks.
const (
	MapColumns      = 64
	MapRows         = 128
	MapHalfRows     = 2
	MapDecodedBytes = MapColumns * MapRows * MapHalfRows * 4
	// Both compressed MAP and raw GOP chunks are independently bounded. A GOP
	// WORD directory can address at most 131070 bytes; this also leaves room for
	// uninterpreted trailing data without allowing archive-sized allocations.
	MaxMapChunkBytes      = 1 << 20
	MaxConcurrentMapLoads = 4
)

// MapInfo is safe to include in an inspection report. It contains identities,
// counts and frame spans, never tile words, compressed streams or pixel bytes.
type MapInfo struct {
	ID             uint16
	CompressedSize int64
	DecodedSize    int
	SHA256         string
	BlockedTiles   int
	GOPSize        int
	GOPSHA256      string
	FrameCount     uint16
	Frames         []FrameInfo
	UnsafeFrames   []UnsafeFrame
}

// MapData is immutable. Tile and Frame methods read its private snapshot;
// Info and Frame return independent slices. JSON serialization emits Info only.
type MapData struct {
	tiles []byte
	gop   []byte
	info  MapInfo
}

type mapSource struct {
	archive *pal.Archive
	size    int64
	sha256  string
}

// MapCatalog owns immutable MAP/GOP archive snapshots. Its FIFO cache retains
// at most 64 maps and 8 MiB including decoded tiles, GOP bytes and metadata.
// At most four uncached loads run concurrently; callers may keep returned maps
// alive independently of cache eviction. There are no file handles to close.
type MapCatalog struct {
	maps, gops mapSource
	loading    chan struct{}
	mu         sync.Mutex
	cache      map[uint16]*MapData
	cacheOrder []uint16
	cacheBytes int
}

// OpenMaps snapshots two regular, non-symlink files of at most 64 MiB each.
// Resource payloads are inspected lazily by Map, so one corrupt map does not
// prevent loading another. Count is the number of shared chunk indexes; slot
// zero is reserved, following the PAL map format, and cannot be loaded.
func OpenMaps(mapPath, gopPath string) (*MapCatalog, error) {
	maps, err := readMapSource(mapPath, "MAP")
	if err != nil {
		return nil, err
	}
	gops, err := readMapSource(gopPath, "GOP")
	if err != nil {
		return nil, err
	}
	return &MapCatalog{maps: maps, gops: gops, loading: make(chan struct{}, MaxConcurrentMapLoads), cache: make(map[uint16]*MapData)}, nil
}

func readMapSource(path, label string) (mapSource, error) {
	fail := func(err error) (mapSource, error) {
		return mapSource{}, fmt.Errorf("%s: %w", label, err)
	}
	before, err := os.Lstat(path)
	if err != nil {
		return fail(err)
	}
	if !before.Mode().IsRegular() {
		return fail(errors.New("requires regular file; symlinks are not accepted"))
	}
	f, err := os.Open(path)
	if err != nil {
		return fail(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return fail(errors.New("file changed identity while opening"))
	}
	if info.Size() > MaxArchiveBytes {
		return fail(fmt.Errorf("file size %d exceeds limit %d", info.Size(), MaxArchiveBytes))
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxArchiveBytes+1))
	if err != nil {
		return fail(err)
	}
	if len(data) > MaxArchiveBytes || int64(len(data)) != info.Size() {
		return fail(errors.New("resource exceeds bound or changed size while reading"))
	}
	after, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !os.SameFile(info, after) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return fail(errors.New("resource changed while reading"))
	}
	archive, err := pal.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fail(err)
	}
	if archive.Count() > MaxSpriteCount {
		return fail(fmt.Errorf("chunk count %d exceeds %d", archive.Count(), MaxSpriteCount))
	}
	sum := sha256.Sum256(data)
	return mapSource{archive: archive, size: int64(len(data)), sha256: fmt.Sprintf("%x", sum)}, nil
}

func (c *MapCatalog) Count() int {
	if c == nil {
		return 0
	}
	return min(c.maps.archive.Count(), c.gops.archive.Count())
}

func (c *MapCatalog) MAPSourceSize() int64 {
	if c == nil {
		return 0
	}
	return c.maps.size
}

func (c *MapCatalog) MAPSourceSHA256() string {
	if c == nil {
		return ""
	}
	return c.maps.sha256
}

func (c *MapCatalog) GOPSourceSize() int64 {
	if c == nil {
		return 0
	}
	return c.gops.size
}

func (c *MapCatalog) GOPSourceSHA256() string {
	if c == nil {
		return ""
	}
	return c.gops.sha256
}

func (c *MapCatalog) cachedMap(id uint16) *MapData {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cache[id]
}

// Map requires exactly 65536 decoded MAP bytes in [128][64][2] uint32 order.
// GOP is a raw sprite, not YJ1 data. An unsafe GOP directory or individual frame
// is retained in Info, while Frame and TileFrame refuse unsafe spans.
func (c *MapCatalog) Map(id uint16) (*MapData, error) {
	if c == nil || c.maps.archive == nil || c.gops.archive == nil {
		return nil, errors.New("MAP: nil map catalog")
	}
	if id == 0 || int(id) >= c.Count() {
		return nil, fmt.Errorf("MAP: map index %d out of range or reserved", id)
	}
	if cached := c.cachedMap(id); cached != nil {
		return cached, nil
	}
	c.loading <- struct{}{}
	defer func() { <-c.loading }()
	if cached := c.cachedMap(id); cached != nil {
		return cached, nil
	}
	packed, err := readMapChunk(c.maps.archive, id, "MAP")
	if err != nil {
		return nil, err
	}
	tiles, err := palcodec.DecodeYJ1(packed, MapDecodedBytes)
	if err != nil {
		return nil, fmt.Errorf("MAP %d: %w", id, err)
	}
	if len(tiles) != MapDecodedBytes {
		return nil, fmt.Errorf("MAP %d: decoded length %d, want %d", id, len(tiles), MapDecodedBytes)
	}
	gop, err := readMapChunk(c.gops.archive, id, "GOP")
	if err != nil {
		return nil, err
	}
	sprite := inspectSprite(id, 0, gop)
	sum := sha256.Sum256(tiles)
	info := MapInfo{ID: id, CompressedSize: int64(len(packed)), DecodedSize: len(tiles), SHA256: fmt.Sprintf("%x", sum), GOPSize: len(gop), GOPSHA256: sprite.SHA256, FrameCount: sprite.FrameCount, Frames: sprite.Frames, UnsafeFrames: sprite.UnsafeFrames}
	for i := 0; i < len(tiles); i += 4 {
		if binary.LittleEndian.Uint32(tiles[i:])&0x2000 != 0 {
			info.BlockedTiles++
		}
	}
	m := &MapData{tiles: tiles, gop: gop, info: info}
	cost := mapDataCost(m)
	if cost <= maxCacheBytes {
		c.mu.Lock()
		if existing := c.cache[id]; existing != nil {
			m = existing
		} else {
			for len(c.cacheOrder) > 0 && (len(c.cache) >= maxCacheEntries || c.cacheBytes+cost > maxCacheBytes) {
				old := c.cacheOrder[0]
				c.cacheOrder = c.cacheOrder[1:]
				c.cacheBytes -= mapDataCost(c.cache[old])
				delete(c.cache, old)
			}
			c.cache[id] = m
			c.cacheOrder = append(c.cacheOrder, id)
			c.cacheBytes += cost
		}
		c.mu.Unlock()
	}
	return m, nil
}

func readMapChunk(a *pal.Archive, id uint16, label string) ([]byte, error) {
	size, err := a.ChunkSize(int(id))
	if err != nil {
		return nil, fmt.Errorf("%s %d: %w", label, id, err)
	}
	if size < 1 || size > MaxMapChunkBytes {
		return nil, fmt.Errorf("%s %d: chunk size %d outside 1..%d", label, id, size, MaxMapChunkBytes)
	}
	data, err := a.ReadChunk(int(id))
	if err != nil {
		return nil, fmt.Errorf("%s %d: %w", label, id, err)
	}
	return data, nil
}

func mapDataCost(m *MapData) int {
	n := 512 + len(m.tiles) + len(m.gop) + len(m.info.Frames)*40 + len(m.info.UnsafeFrames)*40
	for _, issue := range m.info.UnsafeFrames {
		n += len(issue.Reason)
	}
	return n
}

func (m *MapData) Info() MapInfo {
	if m == nil {
		return MapInfo{}
	}
	info := m.info
	info.Frames = append([]FrameInfo(nil), info.Frames...)
	info.UnsafeFrames = append([]UnsafeFrame(nil), info.UnsafeFrames...)
	return info
}

// DecodedSHA256 avoids copying the frame-directory metadata in repeated
// collision queries. The digest belongs to this immutable tile snapshot.
func (m *MapData) DecodedSHA256() string {
	if m == nil {
		return ""
	}
	return m.info.SHA256
}

func (m *MapData) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Info())
}

func (m *MapData) validTile(x, y, h int) bool {
	return m != nil && len(m.tiles) == MapDecodedBytes && x >= 0 && x < MapColumns && y >= 0 && y < MapRows && h >= 0 && h < MapHalfRows
}

// Tile returns the original tile word, or zero for an invalid coordinate.
// Use IsBlocked for collision queries, since invalid coordinates are blocked.
func (m *MapData) Tile(x, y, h int) uint32 {
	if !m.validTile(x, y, h) {
		return 0
	}
	offset := ((y*MapColumns+x)*MapHalfRows + h) * 4
	return binary.LittleEndian.Uint32(m.tiles[offset:])
}

func (m *MapData) IsBlocked(x, y, h int) bool {
	return !m.validTile(x, y, h) || m.Tile(x, y, h)&0x2000 != 0
}

// TileHeight returns the logical occlusion height, not screen pixels. Layer
// must be 0 (bottom) or 1 (top); invalid coordinates and layers return zero.
func (m *MapData) TileHeight(x, y, h, layer int) uint8 {
	if !m.validTile(x, y, h) || layer < 0 || layer > 1 {
		return 0
	}
	return uint8((m.Tile(x, y, h) >> uint(8+16*layer)) & 0xf)
}

// TileFrame returns a checked span in the raw GOP sprite. Top-layer zero means
// no bitmap; its other indexes are one-based. Bottom indexes are zero-based.
// False covers missing bitmaps, invalid coordinates and unsafe frame spans.
func (m *MapData) TileFrame(x, y, h, layer int) (FrameInfo, bool) {
	if !m.validTile(x, y, h) || layer < 0 || layer > 1 {
		return FrameInfo{}, false
	}
	word := m.Tile(x, y, h) >> uint(16*layer)
	index := int((word&0xff)|((word>>4)&0x100)) - layer
	if index < 0 || index >= len(m.info.Frames) {
		return FrameInfo{}, false
	}
	frame := m.info.Frames[index]
	return frame, frame.Safe
}

// Frame copies one directory-checked GOP frame. Safe describes its span only;
// an RLE decoder must additionally validate dimensions, runs and pixel bounds.
func (m *MapData) Frame(index int) ([]byte, error) {
	if m == nil || index < 0 || index >= len(m.info.Frames) {
		return nil, fmt.Errorf("GOP: frame index %d out of range", index)
	}
	frame := m.info.Frames[index]
	if !frame.Safe || frame.Offset < 0 || frame.End <= frame.Offset || frame.End > len(m.gop) {
		return nil, fmt.Errorf("GOP: unsafe frame %d", index)
	}
	return append([]byte(nil), m.gop[frame.Offset:frame.End]...), nil
}
