package palgame

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

// This opt-in test retains no original resource data, dialogue or screenshots.
// A loaded checkpoint is still not evidence of an original-game route.
func TestLocalSaveResumeAfterOpening(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR to the documented local DOS sample")
	}
	g, err := paldata.Load(dir, paldata.Options{Encoding: "big5", Layout: "dos"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := paldata.LoadRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := palasset.OpenMGO(filepath.Join(dir, "mgo.mkf"), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	maps, err := palasset.OpenMaps(filepath.Join(dir, "map.mkf"), filepath.Join(dir, "gop.mkf"))
	if err != nil {
		t.Fatal(err)
	}
	palettes, err := palasset.OpenPAT(filepath.Join(dir, "pat.mkf"))
	if err != nil {
		t.Fatal(err)
	}
	rng, err := palasset.OpenRNG(filepath.Join(dir, "rng.mkf"))
	if err != nil {
		t.Fatal(err)
	}
	fbp, err := palasset.OpenFBP(filepath.Join(dir, "fbp.mkf"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(g, r, SessionOptions{Assets: assets, Maps: maps, Palettes: palettes, RNG: rng, FBP: fbp, RedactText: true})
	if err != nil {
		t.Fatal(err)
	}
	// Initial pending OnEnter is saveable without trying to load or execute it.
	s = saveRoundTrip(t, s)
	if _, err := s.Enter(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(3, nil); err != nil {
		t.Fatal(err)
	}
	next := saveRoundTrip(t, s)
	want, err := s.Interact(nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := next.Interact(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) || !reflect.DeepEqual(s.Snapshot(), next.Snapshot()) {
		t.Fatal("local resumed trace differs from uninterrupted session")
	}
}
