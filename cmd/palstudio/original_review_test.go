package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/pal"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
)

func rewriteOriginalSSS(t *testing.T, dir string, mutate func([][]byte)) {
	t.Helper()
	path := filepath.Join(dir, "SSS.MKF")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := pal.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	chunks := make([][]byte, archive.Count())
	for i := range chunks {
		chunks[i], err = archive.ReadChunk(i)
		if err != nil {
			t.Fatal(err)
		}
	}
	mutate(chunks)
	if err := os.WriteFile(path, originalMKF(chunks...), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestOriginalSuccessfulReloadClearsSourceBoundInputsAndSession(t *testing.T) {
	for _, layout := range []string{"dos", "windows"} {
		t.Run(layout, func(t *testing.T) {
			o := newOriginalData()
			o.directory = originalRuntimeFixture(t)
			if err := o.load(); err != nil {
				t.Fatal(err)
			}
			o.useSession = true
			o.assumeEntered = true
			if err := o.resetSession(); err != nil {
				t.Fatal(err)
			}
			o.event = "1"
			if err := o.run("event_trigger"); err != nil {
				t.Fatal(err)
			}
			o.inputsJSON = `[{"kind":"battle","value":0}]`
			o.lastEvent = 2
			next := originalRuntimeFixture(t)
			if layout == "windows" {
				rewriteOriginalSSS(t, next, func(c [][]byte) { c[2] = make([]byte, 3*7*2) })
			}
			messagePath := filepath.Join(next, "M.MSG")
			data, err := os.ReadFile(messagePath)
			if err != nil {
				t.Fatal(err)
			}
			data[0] = 'T'
			if err := os.WriteFile(messagePath, data, 0600); err != nil {
				t.Fatal(err)
			}
			o.directory = next
			o.encoding = "gbk"
			o.layout = layout
			if err := o.load(); err != nil {
				t.Fatal(err)
			}
			if o.inputsJSON != "[]" || o.lastEvent != 0 || o.useSession || o.assumeEntered || len(o.history) != 0 || o.scene != "1" || o.event != "" {
				t.Fatal("new source inherited a prior source's interaction state")
			}
			if o.session == nil || !o.session.Snapshot().NeedsSceneEnter || len(o.session.Snapshot().World.Inventory) != 0 {
				t.Fatal("new source inherited checkpoint inventory or entry gate")
			}
			if o.game.Encoding != "gbk" || o.game.Layout != layout || o.loadedDirectory != next || !strings.HasPrefix(o.game.Messages[0], "T") {
				t.Fatal("new source/format/encoding were not installed")
			}
		})
	}
}
func TestOriginalViewModelsShowCommittedWorldFields(t *testing.T) {
	dir := originalRuntimeFixture(t)
	rewriteOriginalSSS(t, dir, func(c [][]byte) {
		c[4] = originalWords(0, 0, 0, 0, 0x13, 1, 99, 101, 0x24, 1, 4, 0, 0x6d, 1, 0, 5, 0, 0, 0, 0, 0, 0, 0, 0)
	})
	o := newOriginalData()
	o.directory = dir
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	o.assumeEntered = true
	if err := o.resetSession(); err != nil {
		t.Fatal(err)
	}
	o.useSession = true
	o.event = "1"
	if err := o.run("event_trigger"); err != nil {
		t.Fatal(err)
	}
	event, err := o.eventDisplay(1)
	if err != nil {
		t.Fatal(err)
	}
	if event.Original.X != 1 || event.Original.Y != 2 || event.Original.AutoScript != 0 || event.Current.X != 99 || event.Current.Y != 101 || event.Current.AutoScript != 4 {
		t.Fatal("event view model confused source with current state", event)
	}
	if !strings.Contains(event.text(), "原始 (1,2) / 当前 (99,101)") || !strings.Contains(event.text(), "Auto 原始 0000 / 当前 0004") {
		t.Fatal("event labels do not distinguish source/current fields", event.text())
	}
	scene, err := o.sceneDisplay()
	if err != nil {
		t.Fatal(err)
	}
	if scene.Original.OnTeleport != 0 || scene.Current.OnTeleport != 5 || !strings.Contains(scene.text(), "OnTeleport 原始 0000 / 当前 0005") {
		t.Fatal("scene labels show stale teleport script", scene.text())
	}
}
func TestOriginalGateRecordsIntentWithoutInventingInvocation(t *testing.T) {
	o := newOriginalData()
	o.directory = originalRuntimeFixture(t)
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	o.useSession = true
	o.event = "1"
	o.inputsJSON = `[{"kind":"confirm","value":1}]`
	if err := o.run("event_trigger"); err == nil {
		t.Fatal("unentered scene trigger accepted")
	}
	record := o.history[len(o.history)-1]
	if record.Invoked || record.Request != nil || record.Options != nil || record.Report.Coverage != "" || record.ActionIntent.EventID != 1 || len(record.ActionIntent.Inputs) != 1 || !reflect.DeepEqual(record.SessionBefore, record.SessionAfter) {
		t.Fatal("gate refusal was exported as a VM invocation")
	}
	o.exportPath = filepath.Join(t.TempDir(), "gate.json")
	if err := o.export(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(o.exportPath)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Invocation map[string]json.RawMessage `json:"invocation"`
	}
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if _, exists := output.Invocation["request"]; exists {
		t.Fatal("non-executed request leaked into exported invocation")
	}
	if _, exists := output.Invocation["options"]; exists {
		t.Fatal("non-executed runner options exported")
	}
	// Entry zero is a genuine empty invocation, despite its empty trace.
	o.inputsJSON = "[]"
	if err := o.run("on_enter"); err != nil {
		t.Fatal(err)
	}
	zero := o.history[len(o.history)-1]
	if !zero.Invoked || zero.Request == nil || zero.Request.Entry != 0 || zero.Options == nil || !zero.Report.Completed || len(zero.Report.Trace) != 0 {
		t.Fatal("entry zero was mistaken for an administrative gate refusal")
	}
}
func TestOriginalExportedRequestAndOptionsReplay(t *testing.T) {
	for _, sessionMode := range []bool{false, true} {
		t.Run(fmt.Sprint(sessionMode), func(t *testing.T) {
			o := newOriginalData()
			o.directory = originalRuntimeFixture(t)
			if err := o.load(); err != nil {
				t.Fatal(err)
			}
			if sessionMode {
				o.assumeEntered = true
				if err := o.resetSession(); err != nil {
					t.Fatal(err)
				}
				o.useSession = true
			}
			o.event = "1"
			if err := o.run("event_trigger"); err != nil {
				t.Fatal(err)
			}
			record := o.history[len(o.history)-1]
			if !record.Invoked || record.Request == nil || record.Options == nil {
				t.Fatal("invoked action omitted request/options")
			}
			runner, err := palflow.New(palgame.Source{Game: o.game, Assets: o.assets}, *record.Options)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := runner.Trace(*record.Request)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(replay.Trace, record.Report.Trace) || !reflect.DeepEqual(replay.State, record.Report.State) || replay.InputsConsumed != record.Report.InputsConsumed || replay.Completed != record.Report.Completed {
				t.Fatal("exported request/options do not reproduce the actual runner result")
			}
			want := originalDiagnosticOptions()
			if sessionMode {
				want = originalSessionOptions()
			}
			if !reflect.DeepEqual(*record.Options, want) {
				t.Fatal("incorrect runner limits exported")
			}
		})
	}
}

// One handcrafted YJ1 raw block contains a two-entry frame directory and two
// opaque frame bytes. It certifies only safe directory bounds, not rendered RLE.
func originalSyntheticMGO() []byte {
	decoded := originalWords(2, 3, 0)
	packed := make([]byte, 20+len(decoded))
	copy(packed, "YJ_1")
	binary.LittleEndian.PutUint32(packed[4:], uint32(len(decoded)))
	binary.LittleEndian.PutUint32(packed[8:], uint32(len(packed)))
	binary.LittleEndian.PutUint16(packed[12:], 1)
	binary.LittleEndian.PutUint16(packed[16:], uint16(len(decoded)))
	copy(packed[20:], decoded)
	return originalMKF(packed)
}
func originalSpriteFixture(t *testing.T, name string) string {
	t.Helper()
	dir := originalRuntimeFixture(t)
	rewriteOriginalSSS(t, dir, func(c [][]byte) {
		c[4] = originalWords(0, 0, 0, 0, 0x65, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	})
	if name != "" {
		if err := os.WriteFile(filepath.Join(dir, name), originalSyntheticMGO(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func TestOriginalOptionalMGOCatalogAndRunnerBinding(t *testing.T) {
	for _, available := range []bool{false, true} {
		t.Run(fmt.Sprint(available), func(t *testing.T) {
			name := ""
			if available {
				name = "mGo.MkF"
			}
			o := newOriginalData()
			o.directory = originalSpriteFixture(t, name)
			if err := o.load(); err != nil {
				t.Fatal(err)
			}
			if o.runtime == nil || o.session == nil {
				t.Fatal("optional MGO absence disabled logic checkpoints", o.runtimeError)
			}
			if available {
				sum := sha256.Sum256(originalSyntheticMGO())
				if o.assets == nil || o.assets.Count() != 1 || o.mgoIdentity == nil || o.mgoIdentity.Name != name || o.mgoIdentity.SHA256 != fmt.Sprintf("%x", sum) {
					t.Fatal("MGO name/hash/count not bound")
				}
			} else if o.assets != nil || o.assetError == "" {
				t.Fatal("missing MGO was silently available")
			}
			// Both diagnostic and logical-session invocations must receive the catalog.
			for _, sessionMode := range []bool{false, true} {
				if sessionMode {
					o.assumeEntered = true
					if err := o.resetSession(); err != nil {
						t.Fatal(err)
					}
					o.useSession = true
				}
				o.event = "1"
				err := o.run("event_trigger")
				record := o.history[len(o.history)-1]
				if available {
					if err != nil || !record.Report.Completed || len(record.Report.Trace[0].Resources) != 1 {
						t.Fatal("loaded MGO metadata not supplied to runner", err)
					}
					resource := record.Report.Trace[0].Resources[0]
					if resource.ArchiveSHA256 != o.mgoIdentity.SHA256 || resource.FrameCount != 1 || resource.DecodedSize != 6 {
						t.Fatal("sprite decode metadata does not match catalog identity")
					}
				} else {
					if err == nil || record.Report.Blocker == nil || record.Report.Blocker.PC != 1 || record.Report.Blocker.Opcode != 0x65 {
						t.Fatal("missing MGO silently skipped sprite reload")
					}
					if sessionMode && o.session.Snapshot().Actions != 0 {
						t.Fatal("missing resource committed partial session")
					}
				}
			}
		})
	}
}
func TestOriginalMGOCatalogRejectsAmbiguousOrAliasedNames(t *testing.T) {
	for _, kind := range []string{"duplicate", "symlink", "directory", "malformed", "wrong-name"} {
		t.Run(kind, func(t *testing.T) {
			dir := originalRuntimeFixture(t)
			switch kind {
			case "duplicate":
				for _, name := range []string{"MGO.MKF", "mgo.mkf"} {
					if err := os.WriteFile(filepath.Join(dir, name), originalSyntheticMGO(), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "sprite")
				if err := os.WriteFile(target, originalSyntheticMGO(), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(dir, "MGO.MKF")); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(filepath.Join(dir, "MGO.MKF"), 0700); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				if err := os.WriteFile(filepath.Join(dir, "MGO.MKF"), []byte{0, 0, 0, 0}, 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong-name":
				if err := os.WriteFile(filepath.Join(dir, "MGO.MKF.bak"), originalSyntheticMGO(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			o := newOriginalData()
			o.directory = dir
			if err := o.load(); err != nil {
				t.Fatal(err)
			}
			if o.assets != nil || o.mgoIdentity != nil || o.assetError == "" || o.session == nil {
				t.Fatal("bad optional catalog was accepted or destroyed non-resource checkpoint", kind, o.assetError)
			}
		})
	}
}
func TestOriginalExportIncludesAndProtectsMGOIdentity(t *testing.T) {
	o := newOriginalData()
	o.directory = originalSpriteFixture(t, "MGO.MKF")
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	o.event = "1"
	if err := o.run("event_trigger"); err != nil {
		t.Fatal(err)
	}
	o.exportPath = filepath.Join(t.TempDir(), "resource.paltrace.json")
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
	if evidence.MGOIdentity == nil || evidence.MGOIdentity.SHA256 != o.assets.SourceSHA256() || evidence.SpriteDecodeLimit != originalSpriteDecodeLimit || len(evidence.Files) != 4 {
		t.Fatal("resource identity/decoder bound missing from export")
	}
	alias := filepath.Join(t.TempDir(), "asset-alias.json")
	source := filepath.Join(o.directory, "MGO.MKF")
	if err := os.Link(source, alias); err != nil {
		t.Fatal(err)
	}
	o.exportPath = alias
	if err := o.export(); err == nil {
		t.Fatal("MGO alias accepted as JSON output")
	}
	after, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, originalSyntheticMGO()) {
		t.Fatal("resource source bytes were changed")
	}
}

// Optional local integration: the supplied directory must contain this PAL1
// table fixture and MGO. No local resource or decoded dialogue is stored in Git.
func TestLocalOriginalUIAssetCheckpoint(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR to the matching authorized PAL1 data and MGO directory")
	}
	o := newOriginalData()
	o.directory = dir
	if err := o.load(); err != nil {
		t.Fatal(err)
	}
	if o.assets == nil || o.runtime == nil || o.session == nil {
		t.Fatal("MGO/runtime session unavailable", o.assetError, o.runtimeError)
	}
	t.Logf("MGO count=%d size=%d sha256=%s", o.assets.Count(), o.mgoIdentity.Size, o.mgoIdentity.SHA256)
	// Use the real new-game session. No assumed-entry checkpoint and no reset may
	// bypass its OnEnter gate before the subsequent inventory actions.
	o.useSession = true
	beforeOpening := o.session.Snapshot()
	sourceEntry := o.game.Scenes[0].OnEnter
	if !beforeOpening.NeedsSceneEnter || beforeOpening.Actions != 0 || o.assumeEntered || o.checkpoint.AssumeSceneEntered {
		t.Fatal("opening test did not start at the real entry gate")
	}
	if err := o.run("on_enter"); err != nil {
		t.Fatal("opening must complete and commit before any pickups", err)
	}
	opening := o.history[len(o.history)-1]
	afterOpening := o.session.Snapshot()
	if !opening.Invoked || opening.Request == nil || opening.Options == nil || !opening.Report.Completed || opening.Report.Blocker != nil || !opening.SessionCommitted || opening.Checkpoint == nil || opening.Checkpoint.AssumeSceneEntered {
		t.Fatal("opening was not a completed real session invocation")
	}
	if opening.Report.State.ReturnedEntry != 8145 || afterOpening.World.Scenes[1].OnEnter != 8145 || afterOpening.NeedsSceneEnter || afterOpening.Actions != 1 || o.game.Scenes[0].OnEnter != sourceEntry {
		t.Fatal("opening entry replacement/gate/source isolation mismatch")
	}
	if !reflect.DeepEqual(opening.SessionBefore, &beforeOpening) || !reflect.DeepEqual(opening.SessionAfter, &afterOpening) || reflect.DeepEqual(beforeOpening, afterOpening) {
		t.Fatal("opening before/after proof does not describe the committed session")
	}
	if len(opening.Report.DialogueIDs) != 67 || opening.Report.State.ExecutedSteps != 193 || opening.Report.State.PlayerSubsteps != 130 || opening.Report.State.WorldTicks != 76 {
		t.Fatal("opening instruction/dialogue/tick baseline changed")
	}
	autoWork := 0
	for _, step := range opening.Report.Trace {
		for _, auto := range step.AutoSteps {
			autoWork += int(auto.Work)
		}
		if step.PartyWalk != nil {
			for _, movement := range step.PartyWalk.Steps {
				for _, auto := range movement.AutoSteps {
					autoWork += int(auto.Work)
				}
			}
		}
	}
	if opening.Report.State.AutoSubsteps != 60 || autoWork != 60 {
		t.Fatalf("opening auto-work baseline changed: state=%d trace=%d", opening.Report.State.AutoSubsteps, autoWork)
	}
	if opening.Report.State.World == nil || len(opening.Report.State.World.PlayerResources) == 0 {
		t.Fatal("opening did not actually load sprite metadata")
	}
	for _, resource := range opening.Report.State.World.PlayerResources {
		if resource.ArchiveSHA256 != o.mgoIdentity.SHA256 || resource.DecodedSize < 2 || resource.FrameCount < 1 {
			t.Fatal("resource evidence does not bind loaded MGO")
		}
	}
	t.Logf("opening committed=true returned=%d main=%d player=%d ticks=%d auto=%d messages=%d", opening.Report.State.ReturnedEntry, opening.Report.State.ExecutedSteps, opening.Report.State.PlayerSubsteps, opening.Report.State.WorldTicks, opening.Report.State.AutoSubsteps, len(opening.Report.DialogueIDs))
	for i, id := range []string{"5", "6", "7"} {
		before := o.session.Snapshot()
		o.event = id
		if err := o.run("event_trigger"); err != nil {
			t.Fatal(err)
		}
		record := o.history[len(o.history)-1]
		after := o.session.Snapshot()
		if !record.Invoked || record.Request == nil || !record.Report.Completed || !record.SessionCommitted || !reflect.DeepEqual(record.Report.DialogueIDs, []uint16{uint16(1025 + i)}) || after.Actions != i+2 {
			t.Fatal("opening-to-pickup sequence failed", id)
		}
		if !reflect.DeepEqual(record.SessionBefore, &before) || !reflect.DeepEqual(record.SessionAfter, &after) || record.Checkpoint == nil || record.Checkpoint.AssumeSceneEntered {
			t.Fatal("pickup did not extend the real opening session", id)
		}
	}
	for _, item := range []uint16{99, 201, 236} {
		if o.session.Snapshot().World.Inventory[item] != 1 {
			t.Fatalf("wrong committed pickup count for object %d", item)
		}
	}
	beforeRepeat := o.session.Snapshot()
	o.event = "5"
	if err := o.run("event_trigger"); err == nil {
		t.Fatal("hidden local pickup repeated")
	}
	rejected := o.history[len(o.history)-1]
	if rejected.Invoked || rejected.Request != nil || rejected.SessionCommitted || !reflect.DeepEqual(beforeRepeat, o.session.Snapshot()) || !reflect.DeepEqual(rejected.SessionBefore, &beforeRepeat) || !reflect.DeepEqual(rejected.SessionAfter, &beforeRepeat) {
		t.Fatal("rejected repeat invented invocation or changed the opening/pickup state")
	}
	// The next unsupported boundary executes a proposal, then rolls the entire
	// proposal back rather than discarding the already committed opening/pickups.
	o.event = "1"
	if err := o.run("event_trigger"); err == nil {
		t.Fatal("update the departure boundary after its implementation changes")
	}
	departure := o.history[len(o.history)-1]
	if !departure.Invoked || departure.Request == nil || departure.SessionCommitted || departure.Report.Blocker == nil || departure.Report.Blocker.PC != 4669 || departure.Report.Blocker.Opcode != 0x50 || !reflect.DeepEqual(beforeRepeat, o.session.Snapshot()) || !reflect.DeepEqual(departure.SessionBefore, &beforeRepeat) || !reflect.DeepEqual(departure.SessionAfter, &beforeRepeat) {
		t.Fatal("departure blocker/rollback evidence mismatch")
	}
}
