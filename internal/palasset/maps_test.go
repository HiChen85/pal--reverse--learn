package palasset

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// All default test fixtures are generated here and contain no game data.
func mapRawYJ1(plain []byte) []byte {
	out := make([]byte, 16)
	copy(out, "YJ_1")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(plain)))
	blocks := (len(plain) + 0x3fff) / 0x4000
	binary.LittleEndian.PutUint16(out[12:], uint16(blocks))
	for len(plain) > 0 {
		n := min(len(plain), 0x4000)
		header := [4]byte{}
		binary.LittleEndian.PutUint16(header[:], uint16(n))
		out = append(out, header[:]...)
		out = append(out, plain[:n]...)
		plain = plain[n:]
	}
	binary.LittleEndian.PutUint32(out[8:], uint32(len(out)))
	return out
}

func mapSprite(frames int) []byte {
	words := frames + 1
	out := make([]byte, words*2+frames*4)
	for i := 0; i < words; i++ {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(words+i*2))
	}
	for i := 0; i < frames; i++ {
		binary.LittleEndian.PutUint16(out[words*2+i*4:], uint16(i+1))
		binary.LittleEndian.PutUint16(out[words*2+i*4+2:], 1)
	}
	return out
}

func mapFixtureTiles() []byte {
	tiles := make([]byte, MapDecodedBytes)
	put := func(x, y, h int, word uint32) {
		binary.LittleEndian.PutUint32(tiles[((y*64+x)*2+h)*4:], word)
	}
	// Bottom frame 257, height 11, blocked. Top frame 2, height 10.
	put(2, 3, 1, 0x0a033b01)
	put(63, 127, 1, 0x00000702)
	return tiles
}

func openMapFixture(t *testing.T, maps, gops [][]byte) (*MapCatalog, string, string) {
	t.Helper()
	dir := t.TempDir()
	mp, gp := filepath.Join(dir, "map.mkf"), filepath.Join(dir, "gop.mkf")
	for path, data := range map[string][]byte{mp: mkfBytes(maps...), gp: mkfBytes(gops...)} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	c, err := OpenMaps(mp, gp)
	if err != nil {
		t.Fatal(err)
	}
	return c, mp, gp
}

func TestMapsTileLayoutLayersAndImmutableSnapshots(t *testing.T) {
	tiles := mapFixtureTiles()
	packed, gop := mapRawYJ1(tiles), mapSprite(258)
	maps, gops := [][]byte{nil, packed}, [][]byte{nil, gop}
	c, mp, gp := openMapFixture(t, maps, gops)
	if c.Count() != 2 || c.MAPSourceSize() != int64(len(mkfBytes(maps...))) || c.GOPSourceSize() != int64(len(mkfBytes(gops...))) || c.MAPSourceSHA256() != fmt.Sprintf("%x", sha256.Sum256(mkfBytes(maps...))) || c.GOPSourceSHA256() != fmt.Sprintf("%x", sha256.Sum256(mkfBytes(gops...))) {
		t.Fatal("source identity differs")
	}
	for _, path := range []string{mp, gp} {
		if err := os.WriteFile(path, []byte("replaced after snapshot"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m, err := c.Map(1)
	if err != nil {
		t.Fatal(err)
	}
	info := m.Info()
	if info.ID != 1 || info.DecodedSize != MapDecodedBytes || info.CompressedSize != 65568 || info.GOPSize != 1550 || info.FrameCount != 258 || info.BlockedTiles != 1 || len(info.UnsafeFrames) != 0 || info.SHA256 != "541ff8c106d494a5c33963eec293a8810a5b25bbf4dac75053ba93afb5af2221" || info.GOPSHA256 != "649f2ca0ed925120e98dec9c72c6f7387948f03b102600e14e5cd386eaf9e1bf" {
		t.Fatalf("synthetic map summary changed: %+v", info)
	}
	if m.Tile(2, 3, 1) != 0x0a033b01 || m.Tile(2, 3, 0) != 0 || m.Tile(63, 127, 1) != 0x702 || !m.IsBlocked(2, 3, 1) || m.IsBlocked(2, 3, 0) || m.TileHeight(2, 3, 1, 0) != 11 || m.TileHeight(2, 3, 1, 1) != 10 {
		t.Fatal("tile coordinate, collision or height layout differs")
	}
	bottom, ok := m.TileFrame(2, 3, 1, 0)
	if !ok || bottom.Index != 257 || bottom.Offset != 1546 || bottom.End != 1550 {
		t.Fatalf("ninth bottom-index bit lost: %+v %v", bottom, ok)
	}
	top, ok := m.TileFrame(2, 3, 1, 1)
	if !ok || top.Index != 2 {
		t.Fatalf("top frame one-based index differs: %+v %v", top, ok)
	}
	if _, ok := m.TileFrame(0, 0, 0, 1); ok {
		t.Fatal("absent top layer returned a frame")
	}
	frame, err := m.Frame(bottom.Index)
	if err != nil || !bytes.Equal(frame, []byte{2, 1, 1, 0}) {
		t.Fatalf("frame bytes differ: %x %v", frame, err)
	}
	frame[0] = 0xff
	info.Frames[257].Safe = false
	info.Frames[257].Offset = -1
	again, err := c.Map(1)
	second, frameErr := again.Frame(257)
	if err != nil || frameErr != nil || second[0] != 2 || !again.Info().Frames[257].Safe {
		t.Fatal("caller can mutate retained frame or metadata")
	}
	serialized, err := json.Marshal(m)
	wantJSON, wantErr := json.Marshal(m.Info())
	if err != nil || wantErr != nil || !bytes.Equal(serialized, wantJSON) || bytes.Contains(serialized, []byte("tiles")) || bytes.Contains(serialized, []byte("gop")) {
		t.Fatal("MapData JSON must contain only public summary")
	}
}

func TestMapsRejectInvalidCoordinatesAndIndexes(t *testing.T) {
	c, _, _ := openMapFixture(t, [][]byte{nil, mapRawYJ1(mapFixtureTiles())}, [][]byte{nil, mapSprite(3)})
	m, err := c.Map(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, coordinate := range [][3]int{{-1, 0, 0}, {64, 0, 0}, {0, -1, 0}, {0, 128, 0}, {0, 0, -1}, {0, 0, 2}} {
		x, y, h := coordinate[0], coordinate[1], coordinate[2]
		if m.Tile(x, y, h) != 0 || !m.IsBlocked(x, y, h) || m.TileHeight(x, y, h, 0) != 0 {
			t.Errorf("unsafe invalid coordinate %v", coordinate)
		}
		if _, ok := m.TileFrame(x, y, h, 0); ok {
			t.Errorf("invalid coordinate has frame %v", coordinate)
		}
	}
	for _, layer := range []int{-1, 2} {
		if m.TileHeight(2, 3, 1, layer) != 0 {
			t.Fatal("invalid layer has height")
		}
		if _, ok := m.TileFrame(2, 3, 1, layer); ok {
			t.Fatal("invalid layer has frame")
		}
	}
	for _, id := range []uint16{0, 2, 65535} {
		if _, err := c.Map(id); err == nil {
			t.Errorf("invalid or reserved map %d accepted", id)
		}
	}
	for _, index := range []int{-1, 3, 65535} {
		if _, err := m.Frame(index); err == nil {
			t.Errorf("invalid frame %d accepted", index)
		}
	}
	if _, ok := m.TileFrame(2, 3, 1, 0); ok {
		t.Fatal("tile bitmap outside GOP directory accepted")
	}
	var nilMap *MapData
	var nilCatalog *MapCatalog
	if nilMap.Tile(0, 0, 0) != 0 || !nilMap.IsBlocked(0, 0, 0) || nilMap.TileHeight(0, 0, 0, 0) != 0 || nilCatalog.Count() != 0 || nilCatalog.MAPSourceSize() != 0 || nilCatalog.GOPSourceSize() != 0 || nilCatalog.MAPSourceSHA256() != "" || nilCatalog.GOPSourceSHA256() != "" {
		t.Fatal("nil query differs")
	}
	if _, err := nilMap.Frame(0); err == nil {
		t.Fatal("nil map has frame")
	}
	if _, err := nilCatalog.Map(1); err == nil {
		t.Fatal("nil catalog has map")
	}
}

func TestMapsKeepMalformedGOPFramesAndMapsLocal(t *testing.T) {
	packed := mapRawYJ1(mapFixtureTiles())
	badFrame := mapSprite(3)
	binary.LittleEndian.PutUint16(badFrame[6:], 1000)
	c, _, _ := openMapFixture(t, [][]byte{nil, packed, packed, packed, []byte("bad YJ1"), packed}, [][]byte{nil, badFrame, {0xff, 0xff}, {0, 0}, mapSprite(3), mapSprite(3)})
	m, err := c.Map(1)
	if err != nil {
		t.Fatal(err)
	}
	info := m.Info()
	if info.FrameCount != 3 || len(info.UnsafeFrames) != 1 || info.UnsafeFrames[0].Index != 2 || !info.Frames[0].Safe || !info.Frames[1].Safe || info.Frames[2].Safe {
		t.Fatalf("one unsafe GOP frame affected its neighbors: %+v", info)
	}
	if _, err := m.Frame(2); err == nil {
		t.Fatal("unsafe frame copied")
	}
	if _, err := m.Frame(1); err != nil {
		t.Fatal("safe neighboring frame rejected", err)
	}
	if _, ok := m.TileFrame(2, 3, 1, 1); ok {
		t.Fatal("tile returned unsafe top frame")
	}
	info.UnsafeFrames[0].Reason = "caller mutation"
	if m.Info().UnsafeFrames[0].Reason == "caller mutation" {
		t.Fatal("issue metadata aliases immutable map")
	}
	for _, id := range []uint16{2, 3} {
		bad, err := c.Map(id)
		if err != nil || len(bad.Info().UnsafeFrames) != 1 || bad.Info().UnsafeFrames[0].Index != -1 || len(bad.Info().Frames) != 0 {
			t.Fatalf("malformed GOP directory %d was not localized: %v %v", id, bad, err)
		}
		if _, err := bad.Frame(0); err == nil {
			t.Fatal("malformed directory has frame")
		}
	}
	if _, err := c.Map(4); err == nil {
		t.Fatal("bad compression accepted")
	}
	good, err := c.Map(5)
	if err != nil || len(good.Info().UnsafeFrames) != 0 {
		t.Fatalf("neighbor map affected by corruption: %v %v", good, err)
	}
}

func TestMapsRequireExactDecodedLengthAndBoundedChunks(t *testing.T) {
	good := mapRawYJ1(mapFixtureTiles())
	tooLong := append([]byte(nil), good...)
	binary.LittleEndian.PutUint32(tooLong[4:], MapDecodedBytes+1)
	truncated := good[:len(good)-1]
	maps := [][]byte{nil, mapRawYJ1(make([]byte, MapDecodedBytes-1)), tooLong, truncated, nil, make([]byte, MaxMapChunkBytes+1), good, good}
	gops := make([][]byte, len(maps))
	for i := range gops {
		gops[i] = mapSprite(1)
	}
	gops[6], gops[7] = nil, make([]byte, MaxMapChunkBytes+1)
	c, _, _ := openMapFixture(t, maps, gops)
	for id := 1; id < len(maps); id++ {
		if _, err := c.Map(uint16(id)); err == nil {
			t.Errorf("invalid map/GOP resource %d accepted", id)
		}
	}
	// A map index must exist in both source archives.
	c, _, _ = openMapFixture(t, [][]byte{nil, good, good}, [][]byte{nil, mapSprite(1)})
	if c.Count() != 2 {
		t.Fatal("shared chunk count differs")
	}
	if _, err := c.Map(2); err == nil {
		t.Fatal("map with missing GOP index accepted")
	}
}

func TestMapsRejectNonregularOversizedAndMalformedArchives(t *testing.T) {
	_, mp, gp := openMapFixture(t, [][]byte{nil}, [][]byte{nil})
	dir := t.TempDir()
	link := filepath.Join(dir, "link.mkf")
	if err := os.Symlink(mp, link); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.mkf")
	if err := os.WriteFile(bad, []byte{8, 0, 0, 0, 4, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(dir, "large.mkf")
	f, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxArchiveBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	tooMany := filepath.Join(dir, "many.mkf")
	if err := os.WriteFile(tooMany, mkfBytes(make([][]byte, MaxSpriteCount+1)...), 0600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{dir, link, bad, large, tooMany, filepath.Join(dir, "missing")} {
		if _, err := OpenMaps(invalid, gp); err == nil {
			t.Errorf("invalid MAP path %s accepted", invalid)
		}
		if _, err := OpenMaps(mp, invalid); err == nil {
			t.Errorf("invalid GOP path %s accepted", invalid)
		}
	}
}

func TestMapsCacheAndConcurrentReaders(t *testing.T) {
	const count = maxCacheEntries + 10
	maps, gops := make([][]byte, count), make([][]byte, count)
	for i := 1; i < count; i++ {
		maps[i], gops[i] = mapRawYJ1(mapFixtureTiles()), mapSprite(3)
	}
	c, _, _ := openMapFixture(t, maps, gops)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for n := 1; n < count; n++ {
				id := uint16(1 + (n+offset)%(count-1))
				m, err := c.Map(id)
				if err != nil || m.Info().ID != id || m.Tile(2, 3, 1) != 0x0a033b01 {
					t.Errorf("concurrent map %d: %v", id, err)
					return
				}
				m.Info().Frames[0].Offset = -1
				frame, err := m.Frame(0)
				if err != nil {
					t.Error(err)
					return
				}
				frame[0] = 255
			}
		}(worker)
	}
	wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	if cap(c.loading) != MaxConcurrentMapLoads || len(c.loading) != 0 || len(c.cache) > maxCacheEntries || c.cacheBytes > maxCacheBytes || len(c.cacheOrder) != len(c.cache) {
		t.Fatalf("cache/load bound differs: entries=%d bytes=%d order=%d", len(c.cache), c.cacheBytes, len(c.cacheOrder))
	}
	cost := 0
	for _, m := range c.cache {
		cost += mapDataCost(m)
		if m.info.Frames[0].Offset != 8 || m.gop[8] != 1 {
			t.Fatal("caller mutation reached cached map")
		}
	}
	if cost != c.cacheBytes {
		t.Fatal("cache accounting differs")
	}
}

func TestMapsCacheByteBudgetEvictsBeforeEntryLimit(t *testing.T) {
	const count = 20
	maps, gops := make([][]byte, count), make([][]byte, count)
	packed, gop := mapRawYJ1(mapFixtureTiles()), mapSprite(10000)
	for i := 1; i < count; i++ {
		maps[i], gops[i] = packed, gop
	}
	c, _, _ := openMapFixture(t, maps, gops)
	first, err := c.Map(1)
	if err != nil {
		t.Fatal(err)
	}
	for i := 2; i < count; i++ {
		if _, err := c.Map(uint16(i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.cache) >= count-1 || c.cache[1] != nil || c.cacheBytes > maxCacheBytes {
		t.Fatalf("byte budget did not evict old maps: entries=%d bytes=%d", len(c.cache), c.cacheBytes)
	}
	// Eviction cannot invalidate a map that a caller still owns.
	if first.Tile(2, 3, 1) != 0x0a033b01 {
		t.Fatal("eviction invalidated retained tile data")
	}
	if _, err := first.Frame(9999); err != nil {
		t.Fatal("eviction invalidated retained GOP frame", err)
	}
}

// Real game assets are optional, live outside this repository and never become
// test fixtures. Set PAL_MAP_ASSET_DIR to verify a locally owned resource set.
func TestMapsLocalAssets(t *testing.T) {
	dir := os.Getenv("PAL_MAP_ASSET_DIR")
	if dir == "" {
		t.Skip("PAL_MAP_ASSET_DIR is not set")
	}
	c, err := OpenMaps(filepath.Join(dir, "map.mkf"), filepath.Join(dir, "gop.mkf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint16{3, 12} {
		m, err := c.Map(id)
		if err != nil {
			t.Fatal(err)
		}
		info := m.Info()
		safe := 0
		for _, frame := range info.Frames {
			if frame.Safe {
				safe++
				data, err := m.Frame(frame.Index)
				if err != nil || len(data) != frame.End-frame.Offset {
					t.Fatal("checked real frame read differs", err)
				}
				img, err := DecodeRLE(data, 1<<20)
				if err != nil || img.Width != 32 || img.Height != 15 {
					t.Fatalf("GOP %d frame %d has invalid bitmap: %v", id, frame.Index, err)
				}
			}
		}
		t.Logf("map=%d compressed=%d decoded=%d sha256=%s blocked=%d gop=%d gopsha256=%s frames=%d safe=%d unsafe=%v", id, info.CompressedSize, info.DecodedSize, info.SHA256, info.BlockedTiles, info.GOPSize, info.GOPSHA256, info.FrameCount, safe, info.UnsafeFrames)
		if info.DecodedSize != MapDecodedBytes || safe == 0 {
			t.Fatal("real map summary differs")
		}
		// Identities select the known PAL98 archive pair without rejecting a
		// caller's different, locally owned PAL edition. Only digests/counts
		// are recorded in source; no original tile or bitmap data is included.
		if c.MAPSourceSHA256() == "1f1fdf71257e3769d1ee3494336a8ae3a5d7690dce710803fa357bbc48023d88" && c.GOPSourceSHA256() == "dbb17aeb8fcef6e24232b05abb58d5e9acf06a868b73bc0dc6c9a9e955fffb9c" {
			want := map[uint16]struct {
				packed, blocked, gop, frames int
				tileSHA, gopSHA              string
			}{
				3:  {6790, 447, 43010, 169, "50059eb95665d50eded5d6c85a4393eca020055bf3aca4207259e3e363503827", "88d3da2bb7f941f6f13d7f293108f87840dc2b0adbbe6dc9bf6142e3d301e8a9"},
				12: {3960, 692, 65440, 310, "84a89c015b354ce75ef73df63839598e264303675f371de88f06272584bd2c5b", "6392a7abfc01c7128447a9d7ebdf46d14d172a616ee918e5514d480afdb17c11"},
			}[id]
			if info.CompressedSize != int64(want.packed) || info.BlockedTiles != want.blocked || info.GOPSize != want.gop || int(info.FrameCount) != want.frames || info.SHA256 != want.tileSHA || info.GOPSHA256 != want.gopSHA || safe != want.frames || len(info.UnsafeFrames) != 0 || !info.Frames[want.frames-1].TerminalFallback {
				t.Fatal("known PAL98 map identity or counts differ")
			}
		}
	}
}
