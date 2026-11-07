package palgame

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func bindSaveFixture(t *testing.T, s *Session) {
	t.Helper()
	s.game.Encoding, s.game.Layout = "big5", "windows"
	s.game.Files = nil
	for _, name := range []string{"SSS.MKF", "DATA.MKF", "M.MSG", "WORD.DAT"} {
		sum := sha256.Sum256([]byte("synthetic-" + name))
		s.game.Files = append(s.game.Files, paldata.FileInfo{Name: name, SHA256: fmt.Sprintf("%x", sum), Size: int64(len(name))})
	}
	s.runtime.Source = s.game.Files[1]
}
func saveRoundTrip(t *testing.T, s *Session) *Session {
	t.Helper()
	data, err := s.MarshalSave()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(": null")) {
		t.Fatal("save emits null")
	}
	next, err := RestoreSession(s.game, s.runtime, SessionOptions{Assets: s.assets, Maps: s.maps, Palettes: s.palettes, RNG: s.rng, FBP: s.fbp}, data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Snapshot(), next.Snapshot()) || !reflect.DeepEqual(s.Initialization(), next.Initialization()) || s.ExecutionOptions() != next.ExecutionOptions() || s.InitialCheckpoint() != next.InitialCheckpoint() {
		t.Fatal("round trip changed complete session state or limits")
	}
	if next.LastInvocation() != nil {
		t.Fatal("load invented a VM invocation")
	}
	return next
}

func TestSaveResumeControlsTraceAndStateExactly(t *testing.T) {
	s := controlFixture(t, []paldata.Event{{X: 336, Y: 168, State: 1, TriggerMode: 1, Trigger: 1}})
	s.game.Scripts[1] = paldata.Instruction{Opcode: 6, Operands: [3]uint16{50, 3}}
	s.game.Scripts[2] = paldata.Instruction{Opcode: 0x1f, Operands: [3]uint16{1, 1}}
	s.maxSteps = 73
	s.current.LastEventID = 1
	s.current.IdleFrames[1] = 7
	s.redactText = true
	bindSaveFixture(t, s)
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
		t.Fatal("resumed search trace or complete session differs")
	}
	want, err = s.Move(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err = next.Move(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) || !reflect.DeepEqual(s.Snapshot(), next.Snapshot()) {
		t.Fatal("resumed movement differs")
	}
}

func TestSaveResumeItemAndSeededTriggerExactly(t *testing.T) {
	s := itemSession(t)
	bindSaveFixture(t, s)
	if _, err := s.UseItem(1, 0, nil); err != nil {
		t.Fatal(err)
	}
	next := saveRoundTrip(t, s)
	want, err := s.UseItem(1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := next.UseItem(1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) || !reflect.DeepEqual(s.Snapshot(), next.Snapshot()) {
		t.Fatal("resumed item callback/consumption differs")
	}
	g, r := fixture()
	g.Scripts = []paldata.Instruction{{}, {Opcode: 6, Operands: [3]uint16{50, 3}}, {Opcode: 0x1f, Operands: [3]uint16{1, 1}}, {}}
	s, err = NewSession(g, r, SessionOptions{AssumeSceneEntered: true, BattleSeed: 8371, MaxSteps: 11})
	if err != nil {
		t.Fatal(err)
	}
	bindSaveFixture(t, s)
	if _, err := s.Trigger(1, nil); err != nil {
		t.Fatal(err)
	}
	next = saveRoundTrip(t, s)
	if s.Snapshot().World.BattleRandomState == 8371 {
		t.Fatal("fixture did not advance random state")
	}
	want, err = s.Trigger(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err = next.Trigger(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) || !reflect.DeepEqual(s.Snapshot(), next.Snapshot()) {
		t.Fatal("resumed seeded random trace differs")
	}
}

func TestSaveResumePendingSceneEntryWithoutLoadingOrReset(t *testing.T) {
	s := controlFixture(t, []paldata.Event{{X: 320, Y: 160, State: 1, TriggerMode: 4, Trigger: 1}})
	s.game.Scenes = append(s.game.Scenes, paldata.Scene{EventStart: 1})
	sc := s.current.World.Scenes[2]
	sc.Map = 1
	s.current.World.Scenes[2] = sc
	s.current.World.Scenes[3] = palflow.WorldScene{EventStart: 1}
	s.game.Scripts[1] = paldata.Instruction{Opcode: 0x59, Operands: [3]uint16{2}}
	s.game.Scripts[2] = paldata.Instruction{Opcode: 1}
	bindSaveFixture(t, s)
	// Rebuild detached initialization after extending the fixture's source table.
	initial, err := InitialState(s.game, s.runtime, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.initialization = initial
	if _, err := s.Move(3, nil); err != nil {
		t.Fatal(err)
	}
	if !s.current.NeedsSceneEnter || !s.current.World.SceneLoadPending || s.current.World.SceneResources.SceneID != 1 {
		t.Fatal("fixture did not reach pending boundary")
	}
	s.current.World.PartyOffsetX, s.current.World.PartyOffsetY = 144, 104
	next := saveRoundTrip(t, s)
	if next.current.World.PartyOffsetX != 144 || !next.current.World.SceneLoadPending {
		t.Fatal("restore called LoadResources or reset pending state")
	}
	want, err := s.Enter(nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := next.Enter(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) || !reflect.DeepEqual(s.Snapshot(), next.Snapshot()) {
		t.Fatal("pending OnEnter resume differs")
	}
}

// Rehash deliberately malformed payloads so these tests exercise semantic
// validation in addition to the checksum's accidental-corruption protection.
func mutateSave(t *testing.T, data []byte, change func(map[string]any)) []byte {
	t.Helper()
	var out map[string]any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&out); err != nil {
		t.Fatal(err)
	}
	change(out)
	payload, err := json.Marshal(out["payload"])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	out["payload_sha256"] = fmt.Sprintf("%x", sum)
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestSaveStrictRejectionPreservesExistingSession(t *testing.T) {
	s := controlFixture(t, []paldata.Event{{State: 1, TriggerMode: 1}})
	bindSaveFixture(t, s)
	data, err := s.MarshalSave()
	if err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	last := s.LastInvocation()
	cases := map[string][]byte{
		"duplicate": bytes.Replace(data, []byte(`"version": 1`), []byte(`"version": 1, "version": 1`), 1),
		"trailing":  append(append([]byte{}, data...), []byte(`{}`)...),
		"oversize":  []byte(strings.Repeat(" ", MaxSaveBytes+1)),
		"checksum":  bytes.Replace(data, []byte(`"actions": 0`), []byte(`"actions": 1`), 1),
	}
	mutations := map[string]func(map[string]any){
		"unknown":       func(p map[string]any) { p["surprise"] = true },
		"missing-world": func(p map[string]any) { delete(p["snapshot"].(map[string]any), "world") },
		"null":          func(p map[string]any) { p["snapshot"].(map[string]any)["world"] = nil },
		"missing-events": func(p map[string]any) {
			p["snapshot"].(map[string]any)["world"].(map[string]any)["events"] = map[string]any{}
		},
		"missing-roles": func(p map[string]any) { delete(p["snapshot"].(map[string]any)["world"].(map[string]any), "roles") },
		"policy": func(p map[string]any) {
			p["snapshot"].(map[string]any)["world"].(map[string]any)["random_mode"] = "unknown"
		},
		"battle-policy": func(p map[string]any) {
			p["snapshot"].(map[string]any)["world"].(map[string]any)["battle_mode"] = "win"
		},
		"battle": func(p map[string]any) { p["snapshot"].(map[string]any)["world"].(map[string]any)["in_battle"] = true },
		"array": func(p map[string]any) {
			p["snapshot"].(map[string]any)["world"].(map[string]any)["experience"] = []any{}
		},
		"budget": func(p map[string]any) { p["options"].(map[string]any)["max_steps"] = 0 },
		"key-alias": func(p map[string]any) {
			p["snapshot"].(map[string]any)["world"].(map[string]any)["roles"].(map[string]any)["00"] = p["snapshot"].(map[string]any)["world"].(map[string]any)["roles"].(map[string]any)["0"]
		},
		"resource": func(p map[string]any) {
			p["source"].(map[string]any)["mgo"].(map[string]any)["SHA256"] = strings.Repeat("0", 64)
		},
		"nil-path": func(p map[string]any) { p["nil_collections"] = []string{"/snapshot/world/roles"} },
	}
	for name, change := range mutations {
		cases[name] = mutateSave(t, data, func(out map[string]any) { change(out["payload"].(map[string]any)) })
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			if restored, err := RestoreSession(s.game, s.runtime, SessionOptions{Assets: s.assets, Maps: s.maps, Palettes: s.palettes}, bad); err == nil || restored != nil {
				t.Fatal("malformed save accepted")
			}
			if !reflect.DeepEqual(before, s.Snapshot()) || !reflect.DeepEqual(last, s.LastInvocation()) {
				t.Fatal("restore failure mutated active session")
			}
		})
	}
}

func TestSaveBindsAllSourcesAndMemoryTables(t *testing.T) {
	s := controlFixture(t, nil)
	bindSaveFixture(t, s)
	data, err := s.MarshalSave()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"file", "encoding", "layout", "runtime", "scripts", "mgo", "maps"} {
		t.Run(kind, func(t *testing.T) {
			g := *s.game
			g.Files = append([]paldata.FileInfo(nil), s.game.Files...)
			g.Scripts = append([]paldata.Instruction(nil), s.game.Scripts...)
			r := *s.runtime
			options := SessionOptions{Assets: s.assets, Maps: s.maps}
			switch kind {
			case "file":
				g.Files[0].SHA256 = strings.Repeat("0", 64)
			case "encoding":
				g.Encoding = "gbk"
			case "layout":
				g.Layout = "dos"
			case "runtime":
				r.Roles[0].Raw[20] = 1
			case "scripts":
				g.Scripts[1].Opcode = 1
			case "mgo":
				options.Assets = nil
			case "maps":
				options.Maps = nil
			}
			if _, err := RestoreSession(&g, &r, options, data); err == nil {
				t.Fatal("different source accepted")
			}
		})
	}
}

func TestSaveBindsOptionalPaletteTimingSource(t *testing.T) {
	s := controlFixture(t, nil)
	bindSaveFixture(t, s)
	path := filepath.Join(t.TempDir(), "pat.mkf")
	if err := os.WriteFile(path, controlMKF(make([]byte, palasset.PaletteBytes)), 0600); err != nil {
		t.Fatal(err)
	}
	palettes, err := palasset.OpenPAT(path)
	if err != nil {
		t.Fatal(err)
	}
	s.palettes = palettes
	data, err := s.MarshalSave()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreSession(s.game, s.runtime, SessionOptions{Assets: s.assets, Maps: s.maps}, data); err == nil {
		t.Fatal("palette timing source was not bound")
	}
	next := saveRoundTrip(t, s)
	if next.palettes != palettes {
		t.Fatal("restored session lost palette provider")
	}
}

func TestSaveResumeAfterCompletedNumericCombat(t *testing.T) {
	source, _ := numericCombatFixture(t, false)
	s, err := NewSession(source.Game, source.Runtime, SessionOptions{AssumeSceneEntered: true, MaxSteps: 1000})
	if err != nil {
		t.Fatal(err)
	}
	bindSaveFixture(t, s)
	if _, err := s.Trigger(1, nil); err != nil {
		t.Fatal(err)
	}
	if s.current.World.Battle == nil || s.current.World.InBattle {
		t.Fatal("fixture did not finish numeric combat")
	}
	next := saveRoundTrip(t, s)
	want, err := s.Trigger(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := next.Trigger(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) || !reflect.DeepEqual(s.Snapshot(), next.Snapshot()) {
		t.Fatal("resuming after terminal battle summary changed later combat")
	}
}

func TestSaveResumePreservesBudgetFailureAndRollback(t *testing.T) {
	s := controlFixture(t, []paldata.Event{{X: 320, Y: 160, State: 1, TriggerMode: 4, Trigger: 1}})
	s.game.Scripts[1] = paldata.Instruction{Opcode: 0x1f, Operands: [3]uint16{1, 1}}
	s.game.Scripts[2] = paldata.Instruction{Opcode: 1}
	s.maxSteps = 3
	bindSaveFixture(t, s)
	next := saveRoundTrip(t, s)
	before := s.Snapshot()
	want, wantErr := s.Interact(nil)
	got, gotErr := next.Interact(nil)
	if wantErr == nil || gotErr == nil || wantErr.Error() != gotErr.Error() || !reflect.DeepEqual(want, got) || !reflect.DeepEqual(before, s.Snapshot()) || !reflect.DeepEqual(before, next.Snapshot()) {
		t.Fatal("save changed shared budget blocker, trace or rollback")
	}
}

func TestSaveEncodedByteBoundaryIncludesTrailingNewline(t *testing.T) {
	s := itemSession(t)
	bindSaveFixture(t, s)
	s.current.World.ScreenTint = "x"
	base, err := s.MarshalSave()
	if err != nil {
		t.Fatal(err)
	}
	s.current.World.ScreenTint = strings.Repeat("x", MaxSaveBytes-len(base)+1)
	data, err := s.MarshalSave()
	if err != nil || len(data) != MaxSaveBytes || data[len(data)-1] != '\n' {
		t.Fatal("exact encoded byte boundary failed", len(data), err)
	}
	if restored, err := RestoreSession(s.game, s.runtime, SessionOptions{}, data); err != nil || len(restored.Snapshot().World.ScreenTint) != len(s.current.World.ScreenTint) {
		t.Fatal("reader rejected the exact writer byte boundary", err)
	}
	s.current.World.ScreenTint += "x"
	if _, err := s.MarshalSave(); err == nil {
		t.Fatal("save emitted a file larger than the reader permits")
	}
}

func TestSaveBindsOptionalMediaExecutionSources(t *testing.T) {
	s := controlFixture(t, nil)
	bindSaveFixture(t, s)
	dir := t.TempDir()
	rngPath, fbpPath := filepath.Join(dir, "rng.mkf"), filepath.Join(dir, "fbp.mkf")
	if err := os.WriteFile(rngPath, controlMKF(controlMKF(controlRawYJ1([]byte{0x13}))), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fbpPath, controlMKF(nil), 0600); err != nil {
		t.Fatal(err)
	}
	rng, err := palasset.OpenRNG(rngPath)
	if err != nil {
		t.Fatal(err)
	}
	fbp, err := palasset.OpenFBP(fbpPath)
	if err != nil {
		t.Fatal(err)
	}
	s.rng, s.fbp = rng, fbp
	data, err := s.MarshalSave()
	if err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"rng", "fbp"} {
		options := SessionOptions{Assets: s.assets, Maps: s.maps, RNG: rng, FBP: fbp}
		if missing == "rng" {
			options.RNG = nil
		} else {
			options.FBP = nil
		}
		if _, err := RestoreSession(s.game, s.runtime, options, data); err == nil {
			t.Fatal("missing media execution source was accepted", missing)
		}
	}
	next := saveRoundTrip(t, s)
	if next.rng != rng || next.fbp != fbp {
		t.Fatal("restore lost media execution providers")
	}
}
