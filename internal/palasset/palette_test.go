package palasset

import (
	"crypto/sha256"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func paletteBytes(seed byte) []byte {
	out := make([]byte, PaletteBytes)
	for i := range out {
		out[i] = byte((int(seed) + i) % 64)
	}
	return out
}

func openPATFixture(t *testing.T, chunks ...[]byte) (*PaletteCatalog, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pat.mkf")
	if err := os.WriteFile(path, mkfBytes(chunks...), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := OpenPAT(path)
	if err != nil {
		t.Fatal(err)
	}
	return c, path
}

func TestPATDayNightFallbackAndSnapshot(t *testing.T) {
	day, night := paletteBytes(0), paletteBytes(19)
	dual := append(append([]byte(nil), day...), night...)
	archive := mkfBytes(day, dual)
	c, path := openPATFixture(t, day, dual)
	if c.Count() != 2 || c.SourceSize() != int64(len(archive)) || c.SourceSHA256() != fmt.Sprintf("%x", sha256.Sum256(archive)) {
		t.Fatal("PAT source identity changed")
	}
	if err := os.WriteFile(path, []byte("changed after snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct {
		id    uint16
		night bool
		bytes []byte
	}{{0, false, day}, {0, true, day}, {1, false, day}, {1, true, night}} {
		palette, err := c.Palette(request.id, request.night)
		if err != nil {
			t.Fatal(err)
		}
		for i, got := range palette {
			want := color.RGBA{request.bytes[i*3] << 2, request.bytes[i*3+1] << 2, request.bytes[i*3+2] << 2, 255}
			if got != want {
				t.Fatalf("palette %d night=%v index=%d: %v want %v", request.id, request.night, i, got, want)
			}
		}
		palette[0] = color.RGBA{}
		again, err := c.Palette(request.id, request.night)
		if err != nil || again[0].A != 255 {
			t.Fatal("returned palette aliases retained data", err)
		}
	}
	palette, err := c.Palette(0, false)
	if err != nil || palette[21].R != 252 {
		t.Fatal("six-bit maximum must expand to 252", err)
	}
}

func TestPATRejectsIncompleteSetsAndInvalidComponents(t *testing.T) {
	invalid := [][]byte{nil, make([]byte, PaletteBytes-1), make([]byte, PaletteBytes+1), make([]byte, 2*PaletteBytes-1), make([]byte, 2*PaletteBytes+1)}
	badDay := paletteBytes(0)
	badDay[767] = 64
	badNight := append(paletteBytes(0), paletteBytes(0)...)
	badNight[PaletteBytes+500] = 255
	invalid = append(invalid, badDay, badNight)
	c, _ := openPATFixture(t, append(invalid, paletteBytes(0))...)
	for id := range invalid {
		for _, night := range []bool{false, true} {
			if _, err := c.Palette(uint16(id), night); err == nil {
				t.Errorf("invalid palette %d night=%v accepted", id, night)
			}
		}
	}
	if _, err := c.Palette(uint16(len(invalid)), true); err != nil {
		t.Fatal("invalid palette affected neighbor", err)
	}
	if _, err := c.Palette(65535, false); err == nil {
		t.Fatal("out-of-range palette accepted")
	}
	var nilCatalog *PaletteCatalog
	if nilCatalog.Count() != 0 || nilCatalog.SourceSize() != 0 || nilCatalog.SourceSHA256() != "" {
		t.Fatal("nil PAT query differs")
	}
	if _, err := nilCatalog.Palette(0, false); err == nil {
		t.Fatal("nil PAT accepted")
	}
}

func TestPATRejectsNonregularAndMalformedArchives(t *testing.T) {
	_, path := openPATFixture(t, paletteBytes(0))
	dir := t.TempDir()
	link := filepath.Join(dir, "link.mkf")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.mkf")
	if err := os.WriteFile(bad, []byte{8, 0, 0, 0, 4, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, link, bad, filepath.Join(dir, "missing")} {
		if _, err := OpenPAT(path); err == nil {
			t.Errorf("invalid PAT path %s accepted", path)
		}
	}
}

func TestPATConcurrentReaders(t *testing.T) {
	c, _ := openPATFixture(t, append(paletteBytes(0), paletteBytes(19)...))
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func(night bool) {
			defer wg.Done()
			for range 40 {
				palette, err := c.Palette(0, night)
				if err != nil || palette[0].A != 255 {
					t.Error("concurrent palette", err)
					return
				}
				palette[0] = color.RGBA{}
			}
		}(worker%2 != 0)
	}
	wg.Wait()
}

func TestPATLocalAssets(t *testing.T) {
	dir := os.Getenv("PAL_PIXEL_ASSET_DIR")
	if dir == "" {
		t.Skip("PAL_PIXEL_ASSET_DIR is not set")
	}
	c, err := OpenPAT(filepath.Join(dir, "pat.mkf"))
	if err != nil {
		t.Fatal(err)
	}
	for id := range c.Count() {
		for _, night := range []bool{false, true} {
			palette, err := c.Palette(uint16(id), night)
			if err != nil {
				t.Fatal(err)
			}
			for _, rgb := range palette {
				if rgb.R > 252 || rgb.G > 252 || rgb.B > 252 || rgb.A != 255 {
					t.Fatal("real palette conversion differs")
				}
			}
		}
	}
	t.Logf("PAT size=%d sha256=%s count=%d", c.SourceSize(), c.SourceSHA256(), c.Count())
	if c.SourceSHA256() == "78b08ef4c4630b703c714da25ab6482d4524a577c56c9554a369bbf30cc67eab" && (c.Count() != 9 || c.SourceSize() != 8488) {
		t.Fatal("known PAT98 archive identity differs")
	}
}
