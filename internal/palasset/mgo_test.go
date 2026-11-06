package palasset

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palcodec"
)

// These tiny fixtures are constructed here. They contain no game assets.
func rawSprite(data []byte) []byte {
	out := make([]byte, 20)
	copy(out, "YJ_1")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(data)))
	binary.LittleEndian.PutUint32(out[8:], uint32(len(out)+len(data)))
	binary.LittleEndian.PutUint16(out[12:], 1)
	binary.LittleEndian.PutUint16(out[16:], uint16(len(data)))
	return append(out, data...)
}

func spriteBytes() []byte {
	// Two frames, directory WORD offsets 3,5,8: spans [6,10) and [10,16).
	out := make([]byte, 16)
	for i, offset := range []uint16{3, 5, 8} {
		binary.LittleEndian.PutUint16(out[i*2:], offset)
	}
	copy(out[6:], []byte{1, 0, 1, 0, 2, 0, 1, 0, 0x11, 0x22})
	return out
}

func mkfBytes(chunks ...[]byte) []byte {
	out := make([]byte, (len(chunks)+1)*4)
	pos := len(out)
	for i, chunk := range chunks {
		binary.LittleEndian.PutUint32(out[i*4:], uint32(pos))
		out = append(out, chunk...)
		pos += len(chunk)
	}
	binary.LittleEndian.PutUint32(out[len(chunks)*4:], uint32(pos))
	return out
}

func openFixture(t *testing.T, data []byte, limit int) (*SpriteCatalog, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mgo.mkf")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := OpenMGO(path, limit)
	if err != nil {
		t.Fatal(err)
	}
	return catalog, path
}

func TestCatalogUsesImmutableSnapshotAndIndependentMetadata(t *testing.T) {
	plain := spriteBytes()
	packed := rawSprite(plain)
	archive := mkfBytes(nil, packed)
	c, path := openFixture(t, archive, 16)
	if c.Count() != 2 || c.SourceSize() != int64(len(archive)) || c.SourceSHA256() != fmt.Sprintf("%x", sha256.Sum256(archive)) {
		t.Fatal("archive identity or direct indexes differ")
	}
	// Mutating the file after OpenMGO must not mix data from different versions.
	if err := os.WriteFile(path, []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := c.Sprite(1)
	if err != nil {
		t.Fatal(err)
	}
	wantFrames := []FrameInfo{{Index: 0, Offset: 6, End: 10, Safe: true}, {Index: 1, Offset: 10, End: 16, Safe: true}}
	if got.ID != 1 || got.CompressedSize != int64(len(packed)) || got.DecodedSize != 16 || got.FrameCount != 2 || got.SHA256 != fmt.Sprintf("%x", sha256.Sum256(plain)) || !reflect.DeepEqual(got.Frames, wantFrames) || len(got.UnsafeFrames) != 0 {
		t.Fatalf("wrong sprite metadata: %+v", got)
	}
	got.Frames[0].Offset = 999
	again, err := c.Sprite(1)
	if err != nil || !reflect.DeepEqual(again.Frames, wantFrames) {
		t.Fatalf("cached metadata aliases caller: %+v %v", again, err)
	}
	if _, err = c.Sprite(0); err == nil {
		t.Fatal("empty sprite accepted")
	}
	if _, err = c.Sprite(2); err == nil {
		t.Fatal("out-of-range sprite accepted")
	}
}

func TestCatalogKeepsMalformedSpriteIssuesLocal(t *testing.T) {
	badOffset := spriteBytes()
	binary.LittleEndian.PutUint16(badOffset[4:], 1000)
	badDirectory := []byte{0xff, 0xff}
	c, _ := openFixture(t, mkfBytes(rawSprite(badOffset), rawSprite(badDirectory), []byte("bad compression"), rawSprite(spriteBytes())), 1<<20)
	got, err := c.Sprite(0)
	if err != nil || got.FrameCount != 2 || len(got.UnsafeFrames) != 1 || got.UnsafeFrames[0].Index != 1 || !got.Frames[0].Safe || got.Frames[1].Safe {
		t.Fatalf("frame issue not isolated: %+v %v", got, err)
	}
	got.UnsafeFrames[0].Reason = "caller changed"
	again, err := c.Sprite(0)
	if err != nil || again.UnsafeFrames[0].Reason == "caller changed" {
		t.Fatal("issue slice aliases cache")
	}
	header, err := c.Sprite(1)
	if err != nil || len(header.UnsafeFrames) != 1 || header.UnsafeFrames[0].Index != -1 || len(header.Frames) != 0 {
		t.Fatalf("malformed directory not identified: %+v %v", header, err)
	}
	if _, err = c.Sprite(2); err == nil {
		t.Fatal("bad compression accepted")
	}
	valid, err := c.Sprite(3)
	if err != nil || len(valid.UnsafeFrames) != 0 || valid.FrameCount != 2 {
		t.Fatalf("neighbor corruption affected valid sprite: %+v %v", valid, err)
	}
}

func TestCatalogRejectsInvalidFilesAndDecodeBounds(t *testing.T) {
	data := mkfBytes(rawSprite(spriteBytes()))
	c, path := openFixture(t, data, 15)
	if _, err := c.Sprite(0); err == nil {
		t.Fatal("per-sprite decode bound ignored")
	}
	for _, limit := range []int{0, -1, palcodec.MaxYJ1Bytes + 1} {
		if _, err := OpenMGO(path, limit); err == nil {
			t.Fatalf("invalid decoded budget %d accepted", limit)
		}
	}
	dir := t.TempDir()
	if _, err := OpenMGO(dir, 16); err == nil {
		t.Fatal("directory accepted")
	}
	link := filepath.Join(dir, "linked.mkf")
	if err := os.Symlink(path, link); err == nil {
		if _, err := OpenMGO(link, 16); err == nil {
			t.Fatal("symlink accepted")
		}
	}
	bad := filepath.Join(dir, "bad.mkf")
	if err := os.WriteFile(bad, []byte{8, 0, 0, 0, 4, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenMGO(bad, 16); err == nil {
		t.Fatal("descending directory accepted")
	}
	oversized := filepath.Join(dir, "too-large.mkf")
	f, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	err = f.Truncate(MaxArchiveBytes + 1)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("prepare sparse oversized input: %v %v", err, closeErr)
	}
	if _, err := OpenMGO(oversized, 16); err == nil {
		t.Fatal("oversized archive accepted")
	}
}

func TestCatalogCacheBoundAndConcurrentReaders(t *testing.T) {
	chunks := make([][]byte, maxCacheEntries+9)
	for i := range chunks {
		chunks[i] = rawSprite(spriteBytes())
	}
	c, _ := openFixture(t, mkfBytes(chunks...), 16)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for n := range chunks {
				id := uint16((n + offset) % len(chunks))
				got, err := c.Sprite(id)
				if err != nil || got.FrameCount != 2 || got.ID != id {
					t.Errorf("concurrent sprite %d: %+v %v", id, got, err)
					return
				}
				got.Frames[0].Offset = -1
			}
		}(worker)
	}
	wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) > maxCacheEntries || c.cacheBytes > maxCacheBytes || len(c.cacheOrder) != len(c.cache) {
		t.Fatalf("metadata cache not bounded: entries=%d bytes=%d order=%d", len(c.cache), c.cacheBytes, len(c.cacheOrder))
	}
	wantDigest := fmt.Sprintf("%x", sha256.Sum256(spriteBytes()))
	for _, got := range c.cache {
		if got.Frames[0].Offset != 6 || got.SHA256 != wantDigest {
			t.Fatal("cached data changed by concurrent callers")
		}
	}
}
