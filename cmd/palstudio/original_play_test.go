package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/content"
	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
	"github.com/HiChen85/pal--reverse--learn/internal/palrender"
	"github.com/egoist/mygo/ui"
)

func originalRawYJ(plain []byte) []byte {
	out := make([]byte, 16)
	copy(out, "YJ_1")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(plain)))
	count := 0
	for len(plain) != 0 {
		n := min(len(plain), 0x4000)
		header := originalWords(uint16(n), 0)
		out = append(out, header...)
		out = append(out, plain[:n]...)
		plain, count = plain[n:], count+1
	}
	binary.LittleEndian.PutUint16(out[12:], uint16(count))
	binary.LittleEndian.PutUint32(out[8:], uint32(len(out)))
	return out
}

func originalSolidRLE(w, h int, index byte) []byte {
	out := originalWords(uint16(w), uint16(h))
	for y := 0; y < h; y++ {
		out = append(out, byte(w))
		out = append(out, bytes.Repeat([]byte{index}, w)...)
	}
	return out
}

func originalFrames(frames ...[]byte) []byte {
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

func originalMapRuntimeFixture(t *testing.T) string {
	t.Helper()
	dir := originalRuntimeFixture(t)
	frames := make([][]byte, 12)
	for i := range frames {
		frames[i] = originalSolidRLE(2, 2, 2)
	}
	palette := make([]byte, palasset.PaletteBytes)
	palette[3], palette[7] = 63, 63
	files := map[string][]byte{
		"mAp.MkF": originalMKF(nil, originalRawYJ(make([]byte, palasset.MapDecodedBytes))),
		"gOp.MkF": originalMKF(nil, originalFrames(originalSolidRLE(32, 16, 1))),
		"PaT.MkF": originalMKF(palette),
		"MGO.MKF": originalMKF(originalRawYJ(originalFrames(frames...))),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	rewriteOriginalSSS(t, dir, func(c [][]byte) {
		binary.LittleEndian.PutUint16(c[0][2:], 176)
		binary.LittleEndian.PutUint16(c[0][4:], 120)
	})
	return dir
}

func TestOriginalPreparedEnterExportReplaysAndProtectsCatalogs(t *testing.T) {
	o := newOriginalData()
	o.directory = originalMapRuntimeFixture(t)
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	o.useSession = true
	if err := o.run("on_enter"); err != nil {
		t.Fatal(err)
	}
	r := o.history[len(o.history)-1]
	if o.maps == nil || o.palettes == nil || o.viewport == nil || r.Request == nil || r.Request.World.SceneResources == nil || len(r.Request.World.PlayerResources) != 1 {
		t.Fatal("actual prepared enter resources were omitted from UI evidence", o.mapError, o.paletteError, o.viewportError)
	}
	runner, err := palflow.New(palgame.Source{Game: o.game, Assets: o.assets, Maps: o.maps}, *r.Options)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := runner.Trace(*r.Request)
	if err != nil || !reflect.DeepEqual(replay.State, r.Report.State) || !reflect.DeepEqual(replay.Trace, r.Report.Trace) {
		t.Fatal("exported prepared request cannot replay actual Enter", err)
	}
	o.exportPath = filepath.Join(t.TempDir(), "catalogs.json")
	if err := o.export(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(o.exportPath)
	if err != nil {
		t.Fatal(err)
	}
	var evidence originalEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	if len(evidence.MapIdentities) != 2 || evidence.PaletteIdentity == nil || evidence.PaletteIdentity.SHA256 != o.palettes.SourceSHA256() || evidence.MapIdentities[0].SHA256 != o.maps.MAPSourceSHA256() || evidence.MapIdentities[1].SHA256 != o.maps.GOPSourceSHA256() {
		t.Fatal("export did not bind all rendering and collision catalogs")
	}
	for _, file := range append(o.mapIdentities, *o.paletteIdentity) {
		source := filepath.Join(o.directory, file.Name)
		before, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, symbolic := range []bool{false, true} {
			alias := filepath.Join(t.TempDir(), "alias.json")
			link := os.Link
			if symbolic {
				link = os.Symlink
			}
			if err := link(source, alias); err != nil {
				t.Fatal(err)
			}
			o.exportPath = alias
			if err := o.export(); err == nil {
				t.Fatal("catalog alias accepted as report output", file.Name)
			}
		}
		after, err := os.ReadFile(source)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("catalog source was changed", file.Name, err)
		}
	}
}

func TestOriginalNativeControlsSearchFromPositionAndRollback(t *testing.T) {
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	a.tab = 5
	a.original = newOriginalData()
	a.original.directory = originalMapRuntimeFixture(t)
	tt := ui.NewTester(a.view, 1400, 4000)
	for _, label := range []string{"读取原版数据", "逻辑会话", "执行场景 OnEnter", "向东 ↘"} {
		if err := tt.Click(label); err != nil || a.failed {
			t.Fatal("native action failed", label, err, a.message)
		}
	}
	o := a.original
	if o.viewport == nil || o.session.Snapshot().World.PartyX != 176 || o.session.Snapshot().World.PartyY != 120 {
		t.Fatal("native control did not move and render actual map", o.viewportError)
	}
	// Deliberately select a different diagnostic ID. The search must choose
	// event 1 at the real party position, independently of that selection.
	o.event = "2"
	if err := tt.Click("调查"); err != nil || a.failed {
		t.Fatal(err, a.message)
	}
	r := o.history[len(o.history)-1]
	if r.Mode != "control" || r.Control == nil || r.Control.SearchEventID != 1 || !r.SessionCommitted || o.session.Snapshot().World.Inventory[1] != 1 || r.Request != nil || r.Options != nil || r.ActionIntent.EventID != 0 {
		t.Fatal("native search was replaced with a diagnostic ID invocation")
	}
	before := o.session.Snapshot()
	o.inputsJSON = `[{"kind":"confirm","value":1}]`
	if err := tt.Click("向东 ↘"); err != nil || !a.failed {
		t.Fatal("unused input move was not rejected", err)
	}
	rejected := o.history[len(o.history)-1]
	if rejected.SessionCommitted || rejected.Control == nil || !rejected.Control.Moved || !reflect.DeepEqual(before, o.session.Snapshot()) || !reflect.DeepEqual(rejected.SessionBefore, rejected.SessionAfter) {
		t.Fatal("rejected control frame changed committed world or lost proposal")
	}
	// The full control evidence, not a fabricated VM request, is exported.
	o.exportPath = filepath.Join(t.TempDir(), "control.json")
	if err := o.export(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(o.exportPath)
	if err != nil {
		t.Fatal(err)
	}
	var evidence originalEvidence
	if err := json.Unmarshal(data, &evidence); err != nil || evidence.Invocation.Control == nil || evidence.Invocation.ActionIntent.Direction == nil || *evidence.Invocation.ActionIntent.Direction != 3 || evidence.Invocation.SessionCommitted {
		t.Fatal("control transaction export lost request intent or rollback", err)
	}
}

func TestOriginalMissingMapsAllowDiagnosticsButBlockPendingScene(t *testing.T) {
	o := newOriginalData()
	o.directory = originalRuntimeFixture(t)
	rewriteOriginalSSS(t, o.directory, func(c [][]byte) {
		c[1] = originalWords(1, 0, 0, 0, 2, 0, 0, 2, 3, 0, 0, 2)
		c[4] = originalWords(0, 0, 0, 0, 0x59, 2, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint16(c[0][32+8:], 0)
	})
	if err := o.load(); err != nil || o.runtime == nil || o.session == nil || o.maps != nil || o.mapError == "" {
		t.Fatal("optional maps disabled non-map diagnostics", err, o.runtimeError)
	}
	o.assumeEntered = true
	if err := o.resetSession(); err != nil {
		t.Fatal(err)
	}
	o.useSession, o.event = true, "1"
	if err := o.run("event_trigger"); err != nil || !o.session.Snapshot().NeedsSceneEnter || !o.session.Snapshot().World.SceneLoadPending {
		t.Fatal("diagnostic scene transition did not preserve pending load", err)
	}
	before := o.session.Snapshot()
	if err := o.run("on_enter"); err == nil || !strings.Contains(err.Error(), "MAP.MKF") {
		t.Fatal("pending scene silently entered without map catalog", err)
	}
	r := o.history[len(o.history)-1]
	if r.Invoked || r.Request != nil || r.Options != nil || r.SessionCommitted || !reflect.DeepEqual(before, o.session.Snapshot()) {
		t.Fatal("missing resource gate exported a stale invocation or committed")
	}
}

func TestLocalOriginalSceneViewport(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR to authorized local PAL1 data; no pixels are exported")
	}
	o := newOriginalData()
	o.directory = dir
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	o.useSession = true
	if err := o.run("on_enter"); err != nil {
		t.Fatal(err)
	}
	if o.viewport == nil {
		t.Fatal("local loaded scene did not render", o.viewportError)
	}
	t.Logf("viewport=320x200 palette=%d night=%t map=%d", o.session.Snapshot().World.Palette, o.session.Snapshot().World.NightPalette, o.session.Snapshot().World.SceneResources.MapID)
	writePreview := func(name string) {
		previewDir := os.Getenv("PAL_PREVIEW_DIR")
		if previewDir == "" {
			return
		}
		img, err := palrender.Render(palrender.Source{Maps: o.maps, Sprites: o.assets, Palettes: o.palettes}, o.session.Snapshot().World)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(previewDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	writePreview("preview-scene1.png")
	o.event = "1"
	if err := o.run("event_trigger"); err != nil {
		t.Fatal(err)
	}
	if err := o.run("on_enter"); err != nil {
		t.Fatal(err)
	}
	if o.session.Snapshot().World.Scene != 3 || o.session.Snapshot().World.SceneResources.MapID != 10 || o.viewport == nil {
		t.Fatal("scene 3 preview resource transition failed", o.viewportError)
	}
	writePreview("preview-scene3.png")
}

func TestOriginalInputRangesKeysAndConfigurableSessionBudget(t *testing.T) {
	for _, input := range []string{`[{"kind":"random","value":0}]`, `[{"kind":"random","value":101}]`, `[{"kind":"random","value":65535}]`, `[{"kind":"random","value":2147483647}]`, `[{"kind":"key","value":1}]`} {
		if _, err := decodeOriginalInputs(input); err != nil {
			t.Fatal("instruction-specific input was incorrectly rejected", input, err)
		}
	}
	for _, input := range []string{`[{"kind":"random","value":2147483648}]`, `[{"kind":"key","value":0}]`, `[{"kind":"key","value":2}]`} {
		if _, err := decodeOriginalInputs(input); err == nil {
			t.Fatal("invalid explicit input was accepted", input)
		}
	}
	o := newOriginalData()
	o.directory = originalRuntimeFixture(t)
	o.maxSteps = 71
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	o.useSession = true
	if err := o.run("on_enter"); err != nil {
		t.Fatal(err)
	}
	if o.history[len(o.history)-1].Options.MaxSteps != 71 {
		t.Fatal("configured budget did not reach actual session invocation")
	}
}
