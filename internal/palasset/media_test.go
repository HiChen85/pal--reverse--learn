package palasset

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func mediaFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func rawMediaYJ1(data []byte) []byte {
	out := make([]byte, 16)
	copy(out, "YJ_1")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(data)))
	blocks := 0
	for len(data) > 0 {
		n := min(len(data), 16384)
		header := make([]byte, 4)
		binary.LittleEndian.PutUint16(header, uint16(n))
		out = append(out, header...)
		out = append(out, data[:n]...)
		data = data[n:]
		blocks++
	}
	binary.LittleEndian.PutUint16(out[12:], uint16(blocks))
	binary.LittleEndian.PutUint32(out[8:], uint32(len(out)))
	return out
}

func TestRNGCommandValidation(t *testing.T) {
	valid := [][]byte{nil, {0}, {0x13, 6}, {0xff, 6, 1, 2}, {2, 3, 0, 4, 0, 0, 6, 1, 2, 0},
		{7, 1, 2, 3, 4, 8, 1, 2, 3, 4, 5, 6, 9, 1, 2, 3, 4, 5, 6, 7, 8, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		{11, 1, 1, 2, 3, 4, 12, 0, 0, 1, 2, 13, 1, 2, 14, 1, 2, 15, 1, 2, 16, 1, 2, 17, 1, 1, 2, 18, 0, 0, 1, 2},
		{4, 255, 255, 0}, // Skipping without subsequent writes touches no pixels.
	}
	for i, data := range valid {
		if err := ValidateRNGCommands(data); err != nil {
			t.Errorf("valid %d: %v", i, err)
		}
	}
	invalid := [][]byte{{3}, {4, 0}, {6}, {10, 1, 2}, {11, 2, 1, 2}, {12, 0}, {13, 1}, {17}, {18, 0, 0, 1}, {4, 255, 255, 6, 1, 2}, {18, 255, 255, 1, 2}}
	for i, data := range invalid {
		if err := ValidateRNGCommands(data); err == nil {
			t.Errorf("unsafe %d accepted", i)
		}
	}
}

func TestRNGNestedDirectoryStopsAndImmutableSnapshot(t *testing.T) {
	tooLarge := make([]byte, 65001)
	declaredTooLarge := rawSprite([]byte{0})
	binary.LittleEndian.PutUint32(declaredTooLarge[4:], 65001)
	movie := mkfBytes(rawSprite([]byte{6, 1, 2, 0}), nil, tooLarge, declaredTooLarge)
	path := mediaFile(t, "rng.mkf", mkfBytes(movie, nil))
	c, err := OpenRNG(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Count() != 2 || c.SourceSHA256() == "" || c.SourceSize() == 0 {
		t.Fatal("missing source identity")
	}
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		movie  uint16
		frame  uint32
		status string
	}{{0, 0, "playable"}, {0, 1, "empty_frame"}, {0, 2, "frame_exceeds_read_buffer"}, {0, 3, "decompression_failed"}, {0, 4, "missing_frame"}, {1, 0, "empty_movie"}, {2, 0, "missing_movie"}} {
		got, err := c.Frame(tc.movie, tc.frame)
		if err != nil || got.Status != tc.status {
			t.Fatalf("%d/%d: %+v %v", tc.movie, tc.frame, got, err)
		}
		if got.Status == "playable" && (got.DecodedSize != 4 || got.SHA256 == "" || got.FrameCount != 4) {
			t.Fatalf("wrong decode evidence: %+v", got)
		}
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				if _, err := c.Frame(0, 0); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}

func TestRNGRejectsUnsafeLocalResources(t *testing.T) {
	badDirectory := []byte{9, 0, 0, 0, 0, 0, 0, 0, 0}
	for _, movie := range [][]byte{badDirectory, mkfBytes([]byte("unknown compression")), mkfBytes(rawSprite([]byte{6, 1})), mkfBytes(rawSprite([]byte{4, 255, 255, 6, 1, 2}))} {
		c, err := OpenRNG(mediaFile(t, "rng.mkf", mkfBytes(movie)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Frame(0, 0); err == nil {
			t.Fatal("unsafe movie accepted")
		}
	}
	if _, err := OpenRNG(mediaFile(t, "rng.mkf", badDirectory)); err == nil {
		t.Fatal("malformed top directory accepted")
	}
}

func TestFBPCompletePictureBlackFallbackAndShortDecode(t *testing.T) {
	large := rawSprite([]byte{0})
	binary.LittleEndian.PutUint32(large[4:], 64001)
	path := mediaFile(t, "fbp.mkf", mkfBytes(rawMediaYJ1(make([]byte, 64000)), nil, large, rawMediaYJ1(nil), rawSprite([]byte{1})))
	c, err := OpenFBP(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	for id, status := range []string{"picture", "black_empty_picture", "black_decompression_failed", "black_empty_decode"} {
		got, err := c.Picture(uint16(id))
		if err != nil || got.Status != status {
			t.Fatalf("picture %d: %+v %v", id, got, err)
		}
		if id == 0 && (got.DecodedSize != 64000 || got.SHA256 == "") {
			t.Fatalf("missing decode identity %+v", got)
		}
	}
	if _, err := c.Picture(4); err == nil {
		t.Fatal("short successful decode accepted")
	}
	if got, err := c.Picture(5); err != nil || got.Status != "black_missing_picture" {
		t.Fatalf("missing picture %+v %v", got, err)
	}
}

// Local corpus validation is opt-in and emits metadata counts only. Nothing
// from either archive is copied to the repository or executed.
func TestMediaLocalCorpus(t *testing.T) {
	dir := os.Getenv("PAL_MEDIA_DIR")
	if dir == "" {
		t.Skip("set PAL_MEDIA_DIR to locally supplied DOS RNG/FBP archives")
	}
	rng, err := OpenRNG(filepath.Join(dir, "rng.mkf"))
	if err != nil {
		t.Fatal(err)
	}
	frames, stops := 0, 0
	for movie := 0; movie < rng.Count(); movie++ {
		first, err := rng.Frame(uint16(movie), 0)
		if err != nil {
			t.Fatal(err)
		}
		if first.Status == "empty_movie" {
			stops++
			continue
		}
		for frame := uint32(0); frame <= uint32(first.FrameCount); frame++ {
			info, err := rng.Frame(uint16(movie), frame)
			if err != nil {
				t.Fatal(err)
			}
			if info.Status != "playable" {
				stops++
				continue
			}
			frames++
		}
	}
	fbp, err := OpenFBP(filepath.Join(dir, "fbp.mkf"))
	if err != nil {
		t.Fatal(err)
	}
	pictures := 0
	for id := 0; id < fbp.Count(); id++ {
		info, err := fbp.Picture(uint16(id))
		if err != nil {
			t.Fatal(err)
		}
		if info.Status == "picture" {
			pictures++
		}
	}
	t.Logf("validated RNG movies=%d playable_frames=%d terminal_stops=%d archive_sha256=%s; FBP chunks=%d pictures=%d archive_sha256=%s", rng.Count(), frames, stops, rng.SourceSHA256(), fbp.Count(), pictures, fbp.SourceSHA256())
}
