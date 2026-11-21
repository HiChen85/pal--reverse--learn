package palrender

import (
	"encoding/binary"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func mkf(chunks ...[]byte) []byte {
	out := make([]byte, 4*(len(chunks)+1))
	for i, data := range chunks {
		binary.LittleEndian.PutUint32(out[i*4:], uint32(len(out)))
		out = append(out, data...)
	}
	binary.LittleEndian.PutUint32(out[len(chunks)*4:], uint32(len(out)))
	return out
}

func rawYJ(data []byte) []byte {
	out := make([]byte, 16)
	copy(out, "YJ_1")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(data)))
	blocks := 0
	for len(data) > 0 {
		n := min(len(data), 0x4000)
		header := make([]byte, 4)
		binary.LittleEndian.PutUint16(header, uint16(n))
		out = append(out, header...)
		out = append(out, data[:n]...)
		data, blocks = data[n:], blocks+1
	}
	binary.LittleEndian.PutUint16(out[12:], uint16(blocks))
	binary.LittleEndian.PutUint32(out[8:], uint32(len(out)))
	return out
}

func solidRLE(w, h int, index byte) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint16(out, uint16(w))
	binary.LittleEndian.PutUint16(out[2:], uint16(h))
	for y := 0; y < h; y++ {
		out = append(out, byte(w))
		for x := 0; x < w; x++ {
			out = append(out, index)
		}
	}
	return out
}

func directory(frames ...[]byte) []byte {
	out := make([]byte, (len(frames)+1)*2)
	for i, frame := range frames {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(len(out)/2))
		out = append(out, frame...)
		if len(out)%2 != 0 {
			out = append(out, 0)
		}
	}
	binary.LittleEndian.PutUint16(out[len(frames)*2:], uint16(len(out)/2))
	return out
}

func fixture(t *testing.T, raised bool, malformed bool) (Source, *palflow.WorldState) {
	t.Helper()
	dir := t.TempDir()
	tiles := make([]byte, palasset.MapDecodedBytes)
	if raised {
		binary.LittleEndian.PutUint32(tiles[((6*64+3)*2+1)*4:], (1<<8|2)<<16)
	}
	ground := solidRLE(32, 16, 1)
	if malformed {
		ground = []byte{32, 0, 16, 0, 32, 1}
	}
	pat := make([]byte, palasset.PaletteBytes)
	pat[3], pat[7], pat[11] = 63, 63, 63
	files := map[string][]byte{
		"MAP.MKF": mkf(nil, rawYJ(tiles)),
		"GOP.MKF": mkf(nil, directory(ground, solidRLE(32, 16, 3))),
		"PAT.MKF": mkf(pat),
		"MGO.MKF": mkf(rawYJ(directory(solidRLE(4, 8, 2))), rawYJ(directory(solidRLE(4, 8, 3)))),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	maps, err := palasset.OpenMaps(filepath.Join(dir, "MAP.MKF"), filepath.Join(dir, "GOP.MKF"))
	if err != nil {
		t.Fatal(err)
	}
	palettes, err := palasset.OpenPAT(filepath.Join(dir, "PAT.MKF"))
	if err != nil {
		t.Fatal(err)
	}
	sprites, err := palasset.OpenMGO(filepath.Join(dir, "MGO.MKF"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	m, err := maps.Map(1)
	if err != nil {
		t.Fatal(err)
	}
	info := m.Info()
	resource := func(id uint16) palflow.SpriteResource {
		i, err := sprites.Sprite(id)
		if err != nil {
			t.Fatal(err)
		}
		return palflow.SpriteResource{ID: id, ArchiveSHA256: sprites.SourceSHA256(), SHA256: i.SHA256, FrameCount: i.FrameCount, DecodedSize: i.DecodedSize}
	}
	w := &palflow.WorldState{Scene: 1, Scenes: map[uint16]palflow.WorldScene{1: {Map: 1}, 2: {EventStart: 1}}, PartyRoles: []uint16{0}, PartyFrames: []uint16{0}, PartyPositions: []palflow.WorldPosition{{X: 100, Y: 100}}, PlayerResources: []palflow.SpriteResource{resource(0)}, Events: map[uint16]palflow.WorldEvent{1: {Sprite: 1, X: 100, Y: 104, State: 1}}, SceneResources: &palflow.SceneResourceState{SceneID: 1, MapID: 1, MAPArchiveSHA256: maps.MAPSourceSHA256(), GOPArchiveSHA256: maps.GOPSourceSHA256(), MapSHA256: info.SHA256, GOPSHA256: info.GOPSHA256, EventSprites: map[uint16]palflow.SpriteResource{1: resource(1)}}}
	return Source{Maps: maps, Palettes: palettes, Sprites: sprites}, w
}

func TestScenePixelsCoordinatesLayeringAndSnapshotIsolation(t *testing.T) {
	s, w := fixture(t, false, false)
	before := palflow.CloneWorld(w)
	img, err := Render(s, w)
	if err != nil {
		t.Fatal(err)
	}
	for _, pixel := range []struct {
		x, y  int
		color color.RGBA
	}{{20, 20, color.RGBA{252, 0, 0, 255}}, {99, 97, color.RGBA{0, 252, 0, 255}}, {99, 104, color.RGBA{0, 0, 252, 255}}} {
		if got := img.RGBAAt(pixel.x, pixel.y); got != pixel.color {
			t.Fatalf("pixel (%d,%d): got %v want %v", pixel.x, pixel.y, got, pixel.color)
		}
	}
	if img.Bounds().Dx() != Width || img.Bounds().Dy() != Height || !reflect.DeepEqual(before, w) {
		t.Fatal("viewport dimensions or snapshot isolation changed")
	}
	w.ViewportX = 16
	shifted, err := Render(s, w)
	if err != nil || shifted.RGBAAt(83, 104) != (color.RGBA{0, 0, 252, 255}) {
		t.Fatal("event map coordinates were not translated by viewport", err)
	}
	// A changed map table is not itself a resource reload: retain loaded map.
	w.Scenes[1] = palflow.WorldScene{Map: 99}
	if _, err := Render(s, w); err != nil {
		t.Fatal("renderer read a pending scene-table map instead of loaded map", err)
	}
	raisedSource, raisedWorld := fixture(t, true, false)
	raisedImage, err := Render(raisedSource, raisedWorld)
	if err != nil || raisedImage.RGBAAt(99, 97) != (color.RGBA{0, 0, 252, 255}) {
		t.Fatal("raised upper tile did not occlude actor", err)
	}
}

func TestSceneRejectsMissingStaleAndMalformedResources(t *testing.T) {
	s, w := fixture(t, false, false)
	for _, change := range []func(*Source, *palflow.WorldState){
		func(s *Source, w *palflow.WorldState) { s.Palettes = nil },
		func(s *Source, w *palflow.WorldState) { s.Maps = nil },
		func(s *Source, w *palflow.WorldState) { w.SceneLoadPending = true },
		func(s *Source, w *palflow.WorldState) { w.SceneResources.MapSHA256 = "other" },
		func(s *Source, w *palflow.WorldState) { w.PlayerResources[0].ArchiveSHA256 = "other" },
		func(s *Source, w *palflow.WorldState) { w.PartyFrames[0] = 999 },
	} {
		source, world := s, palflow.CloneWorld(w)
		change(&source, world)
		if img, err := Render(source, world); err == nil || img != nil {
			t.Fatal("invalid resource state returned a misleading viewport")
		}
	}
	bad, badWorld := fixture(t, false, true)
	if img, err := Render(bad, badWorld); err == nil || img != nil || !strings.Contains(err.Error(), "RLE") {
		t.Fatal("malformed map pixels were hidden", err)
	}
}
