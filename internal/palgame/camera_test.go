package palgame

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func cameraPAT(t *testing.T, chunks ...[]byte) *palasset.PaletteCatalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pat.mkf")
	if err := os.WriteFile(path, controlMKF(chunks...), 0600); err != nil {
		t.Fatal(err)
	}
	palettes, err := palasset.OpenPAT(path)
	if err != nil {
		t.Fatal(err)
	}
	return palettes
}

func TestSourcePaletteAvailabilityDistinguishesUnknownAbsentAndMalformed(t *testing.T) {
	badComponents := make([]byte, palasset.PaletteBytes)
	badComponents[0] = 64
	palettes := cameraPAT(t, make([]byte, palasset.PaletteBytes), make([]byte, 2*palasset.PaletteBytes), []byte{0}, badComponents)
	var _ palflow.PaletteAvailabilitySource = Source{}
	if _, err := (Source{}).PaletteAvailable(0, false); err == nil {
		t.Fatal("missing PAT was silently considered unavailable")
	}
	if _, err := (Source{Palettes: &palasset.PaletteCatalog{}}).PaletteAvailable(0, false); err == nil {
		t.Fatal("zero-value PAT was silently considered unavailable")
	}
	s := Source{Palettes: palettes}
	for _, night := range []bool{false, true} {
		for _, id := range []uint16{0, 1} {
			if available, err := s.PaletteAvailable(id, night); err != nil || !available {
				t.Fatal("day/night or day fallback rejected", id, night, available, err)
			}
		}
		for _, id := range []uint16{2, 3} {
			if available, err := s.PaletteAvailable(id, night); err == nil || available {
				t.Fatal("malformed palette converted into a NULL lookup", id, night, err)
			}
		}
		if available, err := s.PaletteAvailable(65535, night); err != nil || available {
			t.Fatal("out-of-range ID did not model the reference NULL lookup", available, err)
		}
	}
}

func TestSessionSceneFadeUsesBoundPaletteCatalogAndDoesNotCommitUnknownTiming(t *testing.T) {
	for _, available := range []bool{false, true} {
		g, runtime := fixture()
		g.Scripts = []paldata.Instruction{{}, {Opcode: 0x93, Operands: [3]uint16{32}}, {}}
		options := SessionOptions{AssumeSceneEntered: true}
		if available {
			options.Palettes = cameraPAT(t, make([]byte, palasset.PaletteBytes))
		}
		s, err := NewSession(g, runtime, options)
		if err != nil {
			t.Fatal(err)
		}
		before := s.Snapshot()
		report, err := s.Trigger(1, nil)
		if !available {
			if err == nil || !reflect.DeepEqual(before, s.Snapshot()) || report.State.WorldTicks != 0 {
				t.Fatal("session committed unknown fade timing", err)
			}
			continue
		}
		if err != nil || !report.Completed || report.State.WorldTicks != 2 || s.Snapshot().World.FrameNum != before.World.FrameNum+2 || len(report.Trace[0].WorldUpdates) != 2 || s.source().Palettes != options.Palettes {
			t.Fatal("session lost the palette catalog or fade updates", report.State.WorldTicks, err)
		}
		s.current.World.Palette = 65535
		s.game.Scripts[1].Operands[0] = 65472 // signed -64
		frame := s.current.World.FrameNum
		report, err = s.Trigger(1, nil)
		if err != nil || report.State.WorldTicks != 0 || s.Snapshot().World.FrameNum != frame || !s.Snapshot().World.NeedToFadeIn || report.Trace[0].SceneFade.PaletteLookup != "unavailable" {
			t.Fatal("unavailable reference palette did not take the early-return path", err)
		}
	}
}

func TestLocalPaletteAvailability(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR for local PAT availability checks")
	}
	palettes, err := palasset.OpenPAT(filepath.Join(dir, "pat.mkf"))
	if err != nil {
		t.Fatal(err)
	}
	source := Source{Palettes: palettes}
	for id := range palettes.Count() {
		for _, night := range []bool{false, true} {
			if available, err := source.PaletteAvailable(uint16(id), night); err != nil || !available {
				t.Fatal("local palette unavailable", id, night, err)
			}
		}
	}
}
