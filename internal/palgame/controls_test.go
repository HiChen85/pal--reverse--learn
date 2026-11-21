package palgame

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func TestControlFrameSharesDialogueBudgetAcrossTouchTriggers(t *testing.T) {
	s := controlFixture(t, []paldata.Event{{X: 320, Y: 160, State: 1, TriggerMode: 4, Trigger: 1}, {X: 320, Y: 160, State: 1, TriggerMode: 4, Trigger: 1}})
	s.game.Scripts[1] = paldata.Instruction{Opcode: 0xffff}
	s.game.Messages[0] = strings.Repeat("x", 600<<10)
	before := s.Snapshot()
	r, err := s.Interact(nil)
	if err == nil || r.Completed || !reflect.DeepEqual(before, s.Snapshot()) || len(r.Triggers) != 2 || r.Triggers[1].Blocker == nil || r.Triggers[1].Blocker.Kind != "text_limit" {
		t.Fatal("second touch bypassed the frame-wide dialogue budget", err)
	}
}

// The default fixtures are generated data. The fixed expectations below follow
// SDLPAL d4bf69b64d335cd680555af9df90e7ec4f3006a3, scene.c PAL_UpdateParty /
// PAL_UpdatePartyGestures and play.c PAL_GameUpdate / PAL_Search. No original
// executable, APK, copyrighted map or dialogue is needed by these goldens.
func controlRawYJ1(plain []byte) []byte {
	out := make([]byte, 16)
	copy(out, "YJ_1")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(plain)))
	binary.LittleEndian.PutUint16(out[12:], uint16((len(plain)+0x3fff)/0x4000))
	for len(plain) > 0 {
		n := min(len(plain), 0x4000)
		header := make([]byte, 4)
		binary.LittleEndian.PutUint16(header, uint16(n))
		out = append(out, header...)
		out = append(out, plain[:n]...)
		plain = plain[n:]
	}
	binary.LittleEndian.PutUint32(out[8:], uint32(len(out)))
	return out
}

func controlMKF(chunks ...[]byte) []byte {
	out := make([]byte, 4*(len(chunks)+1))
	for i, chunk := range chunks {
		binary.LittleEndian.PutUint32(out[i*4:], uint32(len(out)))
		out = append(out, chunk...)
	}
	binary.LittleEndian.PutUint32(out[len(chunks)*4:], uint32(len(out)))
	return out
}

func controlFixture(t *testing.T, events []paldata.Event, blocked ...[3]int) *Session {
	t.Helper()
	tiles := make([]byte, palasset.MapDecodedBytes)
	for _, p := range blocked {
		binary.LittleEndian.PutUint32(tiles[((p[1]*64+p[0])*2+p[2])*4:], 0x2000)
	}
	dir := t.TempDir()
	paths := map[string][]byte{
		"map.mkf": controlMKF(nil, controlRawYJ1(tiles)),
		"gop.mkf": controlMKF(nil, []byte{2, 0, 4, 0, 1, 0, 1, 0}),
		"mgo.mkf": controlMKF(controlRawYJ1([]byte{2, 0, 4, 0, 1, 0, 1, 0}), controlRawYJ1([]byte{2, 0, 4, 0, 1, 0, 1, 0})),
	}
	for name, data := range paths {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	maps, err := palasset.OpenMaps(filepath.Join(dir, "map.mkf"), filepath.Join(dir, "gop.mkf"))
	if err != nil {
		t.Fatal(err)
	}
	assets, err := palasset.OpenMGO(filepath.Join(dir, "mgo.mkf"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	g, runtime := fixture()
	g.Events = events
	g.Scenes = []paldata.Scene{{Map: 1}, {EventStart: uint16(len(events))}}
	g.Scripts = make([]paldata.Instruction, 32)
	s, err := NewSession(g, runtime, SessionOptions{Maps: maps, Assets: assets, AssumeSceneEntered: true})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.source().LoadResources(s.current.World, true, false)
	if err != nil {
		t.Fatal(err)
	}
	w.ViewportX, w.ViewportY = 160, 48
	w.FrameNum, w.ChaseCycles, w.ChaseRange = 40, 0, 4
	w.Trail = []palflow.WorldTrail{{X: 288, Y: 144, Direction: 3}, {X: 272, Y: 136, Direction: 3}, {X: 256, Y: 128, Direction: 2}, {X: 240, Y: 120, Direction: 1}, {X: 224, Y: 112}}
	syncControlPosition(w)
	s.current.World = w
	return s
}

func TestControlsMovementSourceGoldens(t *testing.T) {
	for direction, expected := range []palflow.WorldPosition{{X: 304, Y: 168}, {X: 304, Y: 152}, {X: 336, Y: 152}, {X: 336, Y: 168}} {
		t.Run(string(rune('0'+direction)), func(t *testing.T) {
			s := controlFixture(t, nil)
			trail := append([]palflow.WorldTrail(nil), s.current.World.Trail...)
			report, err := s.Move(uint16(direction), nil)
			if err != nil {
				t.Fatal(err)
			}
			w := s.Snapshot().World
			if !report.Completed || !report.Moved || report.Blocked || report.Before != (palflow.WorldPosition{X: 320, Y: 160}) || report.After != expected || report.BeforeFrame != 40 || report.AfterFrame != 41 || report.Work != 3 || report.State.Actions != 1 {
				t.Fatalf("movement golden: %+v", report)
			}
			if w.PartyX != int(expected.X) || w.PartyY != int(expected.Y) || w.PartyStepFrame != 1 || w.PartyFrames[0] != uint16(direction)*3+1 || w.PartyPositions[0] != (palflow.WorldPosition{X: 160, Y: 112}) || w.ChaseCycles != 65535 || w.ChaseRange != 4 {
				t.Fatalf("movement state golden: %+v", w)
			}
			if w.Trail[0] != (palflow.WorldTrail{X: 320, Y: 160, Direction: uint16(direction)}) || !reflect.DeepEqual(w.Trail[1:], trail[:4]) {
				t.Fatal("movement did not shift the exact pre-move trail and new input direction")
			}
			report.State.World.Trail[0].X = 999
			if s.current.World.Trail[0].X != 320 {
				t.Fatal("control evidence aliases committed world")
			}
		})
	}
}

func TestControlsBlockedMovementSourceGoldens(t *testing.T) {
	for _, kind := range []string{"map", "event", "range"} {
		t.Run(kind, func(t *testing.T) {
			var s *Session
			switch kind {
			case "map":
				s = controlFixture(t, nil, [3]int{10, 10, 1})
			case "event":
				s = controlFixture(t, []paldata.Event{{X: 336, Y: 168, State: 2, VanishTime: 9}})
			case "range":
				s = controlFixture(t, nil)
				s.current.World.ViewportX = 1728 // target tile 59 >= 64-5-1
				syncControlPosition(s.current.World)
			}
			before := s.Snapshot()
			report, err := s.Move(3, nil)
			if err != nil {
				t.Fatal(err)
			}
			w := s.Snapshot().World
			if !report.Completed || report.Moved || !report.Blocked || report.Before != report.After || w.Direction != 3 || w.PartyFrames[0] != 9 || w.PartyStepFrame != 2 || report.Work != 2 || !reflect.DeepEqual(before.World.Trail, w.Trail) {
				t.Fatalf("blocked %s golden: %+v", kind, report)
			}
			if kind == "event" && w.Events[1].VanishTime != 8 {
				t.Fatal("vanished state>=2 object must still block")
			}
		})
	}
}

func TestControlsCoordinatePackingWrapsBeforePartyCache(t *testing.T) {
	s := controlFixture(t, nil)
	w := s.current.World
	w.ViewportX, w.ViewportY, w.PartyOffsetX, w.PartyOffsetY = 32760, 32760, -32768, -32760
	syncControlPosition(w)
	report, err := s.Move(3, nil)
	if err != nil {
		t.Fatal(err)
	}
	w = s.current.World
	if !report.Moved || w.ViewportX != -32760 || w.ViewportY != -32768 || w.PartyX != 8 || w.PartyY != 8 || w.Trail[0] != (palflow.WorldTrail{X: 65528, Y: 0, Direction: 3}) {
		t.Fatalf("PAL_XY signed16 packing changed: viewport=%d,%d party=%d,%d trail=%+v", w.ViewportX, w.ViewportY, w.PartyX, w.PartyY, w.Trail[0])
	}
}

func TestControlsPartyAndFollowerSourceGolden(t *testing.T) {
	// These fixed coordinates/frames were also checked by compiling the five
	// original scene.c/play.c functions with only rendering, script and collision
	// boundaries stubbed. The first trailing member's candidate is blocked.
	s := controlFixture(t, []paldata.Event{{X: 336, Y: 168, State: 1, TriggerMode: 1, SpriteFrames: 3}}, [3]int{8, 8, 1})
	w := s.current.World
	w.PartyRoles, w.FollowerSprites = []uint16{0, 1, 2}, []uint16{1, 1}
	for _, id := range []uint16{0, 2} {
		role := w.Roles[id]
		role[64] = 4
		w.Roles[id] = role
	}
	if _, err := s.Move(3, nil); err != nil {
		t.Fatal(err)
	}
	w = s.current.World
	positions := []palflow.WorldPosition{{X: 160, Y: 112}, {X: 112, Y: 88}, {X: 96, Y: 96}, {X: 80, Y: 72}, {X: 64, Y: 64}}
	if !reflect.DeepEqual(w.PartyPositions, positions) || !reflect.DeepEqual(w.PartyFrames, []uint16{13, 10, 13, 8, 5}) || w.PartyStepFrame != 1 {
		t.Fatalf("party/follower walking source golden: positions=%+v frames=%+v phase=%d", w.PartyPositions, w.PartyFrames, w.PartyStepFrame)
	}
	report, err := s.Interact(nil)
	if err != nil {
		t.Fatal(err)
	}
	w = s.current.World
	if report.SearchEventID != 1 || !reflect.DeepEqual(w.PartyPositions, positions) || !reflect.DeepEqual(w.PartyFrames, []uint16{9, 9, 9, 6, 3}) || w.PartyStepFrame != 2 || w.Events[1].Direction != 1 {
		t.Fatalf("party/follower search source golden: positions=%+v frames=%+v phase=%d", w.PartyPositions, w.PartyFrames, w.PartyStepFrame)
	}
}

var eastSearchGolden = []palflow.WorldPosition{{X: 320, Y: 160}, {X: 336, Y: 168}, {X: 320, Y: 176}, {X: 352, Y: 160}, {X: 352, Y: 176}, {X: 336, Y: 184}, {X: 368, Y: 168}, {X: 368, Y: 184}, {X: 352, Y: 192}, {X: 384, Y: 176}, {X: 384, Y: 192}, {X: 368, Y: 200}, {X: 400, Y: 184}}

func TestControlsSearchRangeAndPositionsSourceGoldens(t *testing.T) {
	for mode, limit := range map[uint16]int{0: 0, 1: 2, 2: 8, 3: 14, 4: 0} {
		for i, position := range eastSearchGolden {
			s := controlFixture(t, []paldata.Event{{X: position.X, Y: position.Y, State: 1, TriggerMode: mode, SpriteFrames: 3, CurrentFrame: 2}})
			s.current.World.Direction = 3
			report, err := s.Interact(nil)
			if err != nil {
				t.Fatalf("mode=%d index=%d: %v", mode, i, err)
			}
			// Touch mode at the exact party position clears the search key;
			// other touch distances are exactly at or beyond its strict radius.
			if mode == 4 && i == 0 {
				if !report.SearchSuppressed || !reflect.DeepEqual(report.TriggeredEvents, []uint16{1}) {
					t.Fatal("touch did not clear manual search")
				}
				continue
			}
			if !reflect.DeepEqual(report.SearchPositions, eastSearchGolden) || report.SearchEventID != map[bool]uint16{true: 1, false: 0}[i < limit] {
				t.Fatalf("mode=%d index=%d search golden: %+v", mode, i, report)
			}
			if i < limit {
				e := s.current.World.Events[1]
				if e.Direction != 1 || e.Frame != 0 || s.current.World.PartyFrames[0] != 9 {
					t.Fatalf("search facing/standing golden mode=%d index=%d: %+v", mode, i, e)
				}
			}
		}
	}
}

func TestControlsSearchPrefersPositionThenEventOrderAndPreservesVanishQuirk(t *testing.T) {
	s := controlFixture(t, []paldata.Event{
		{X: 336, Y: 168, State: 1, TriggerMode: 1},
		{X: 320, Y: 160, State: 1, TriggerMode: 1, VanishTime: 5},
		{X: 320, Y: 160, State: 1, TriggerMode: 1},
	})
	s.current.World.Direction = 3
	report, err := s.Interact(nil)
	if err != nil || report.SearchEventID != 2 || !reflect.DeepEqual(report.TriggeredEvents, []uint16{2}) || s.current.World.Events[2].VanishTime != 4 {
		t.Fatalf("search order/vanish source golden: %+v %v", report, err)
	}
	// An event with no directional frame interval must retain its pose.
	s = controlFixture(t, []paldata.Event{{X: 320, Y: 160, State: 1, TriggerMode: 1, SpriteFrames: 3, CurrentFrame: 12, Direction: 2}})
	s.current.World.Direction = 3
	if _, err = s.Interact(nil); err != nil {
		t.Fatal(err)
	}
	if e := s.current.World.Events[1]; e.Frame != 12 || e.Direction != 2 {
		t.Fatal("PAL_Search's strict spriteFrames*4 > frame gate changed")
	}
}

func TestControlsTouchRunsBeforeMovementAndUpdatesVanishAndHiddenState(t *testing.T) {
	s := controlFixture(t, []paldata.Event{
		{X: 320, Y: 160, State: 1, TriggerMode: 4, SpriteFrames: 3, CurrentFrame: 2},
		{X: 336, Y: 168, State: 1, TriggerMode: 4},
		{X: 320, Y: 160, State: 1, TriggerMode: 4, VanishTime: 1},
		{X: 320, Y: 160, State: 1, TriggerMode: 4, VanishTime: -1},
		{X: 481, Y: 160, State: -2, CurrentFrame: 3},
		{X: 480, Y: 368, State: -2, CurrentFrame: 3},
	})
	s.current.World.ChaseCycles = 1
	report, err := s.Move(3, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := s.current.World
	if !reflect.DeepEqual(report.TriggeredEvents, []uint16{1}) || !report.Moved || report.After != (palflow.WorldPosition{X: 336, Y: 168}) || w.Events[1].Direction != 1 || w.Events[1].Frame != 0 || w.Events[3].VanishTime != 0 || w.Events[4].VanishTime != 0 || w.Events[5].State != 2 || w.Events[5].Frame != 0 || w.Events[6].State != -2 || w.Events[6].Frame != 3 || w.ChaseCycles != 0 || w.ChaseRange != 1 {
		t.Fatalf("trigger/timer/offscreen source golden: %+v", report)
	}
	// At the new location, event 2 is touched on the following frame.
	report, err = s.Interact(nil)
	if err != nil || !reflect.DeepEqual(report.TriggeredEvents, []uint16{2}) || !report.SearchSuppressed || len(report.SearchPositions) != 0 {
		t.Fatalf("next frame touch/search clearing golden: %+v %v", report, err)
	}
}

func TestControlsNPCDisplacementTriesFourDirectionsBeforeInputMovement(t *testing.T) {
	s := controlFixture(t, []paldata.Event{{X: 320, Y: 160, State: 2, Sprite: 1, Direction: 0}}, [3]int{9, 9, 1})
	trail := append([]palflow.WorldTrail(nil), s.current.World.Trail...)
	report, err := s.Interact(nil)
	if err != nil {
		t.Fatal(err)
	}
	// NPC south(0)+1=west is blocked. North(2) is the next attempt.
	w := s.current.World
	if report.After != (palflow.WorldPosition{X: 336, Y: 152}) || report.Work != 3 || !reflect.DeepEqual(w.Trail, trail) || w.Direction != 0 || w.PartyFrames[0] != 0 {
		t.Fatalf("NPC push source golden: %+v", report)
	}
}

func TestControlsTriggersAndAutoConsumeOneOrderedInputStream(t *testing.T) {
	s := controlFixture(t, []paldata.Event{
		{X: 320, Y: 160, State: 1, TriggerMode: 4, Trigger: 1, AutoScript: 7},
		{X: 320, Y: 160, State: 1, TriggerMode: 4, Trigger: 4},
	})
	s.game.Scripts[1] = paldata.Instruction{Opcode: 0xa}
	s.game.Scripts[2] = paldata.Instruction{Opcode: 1}
	s.game.Scripts[4] = paldata.Instruction{Opcode: 6, Operands: [3]uint16{50, 6, 0}}
	s.game.Scripts[5] = paldata.Instruction{Opcode: 1}
	s.game.Scripts[6] = paldata.Instruction{Opcode: 1}
	s.game.Scripts[7] = paldata.Instruction{Opcode: 6, Operands: [3]uint16{50, 0, 0}}
	inputs := []palflow.Input{{Kind: palflow.InputConfirm, Value: 1}, {Kind: palflow.InputRandom, Value: 75}, {Kind: palflow.InputRandom, Value: 25}}
	report, err := s.Interact(inputs)
	if err != nil {
		t.Fatal(err)
	}
	w := s.current.World
	if report.InputsConsumed != 3 || len(report.UnusedInputs) != 0 || !reflect.DeepEqual(report.TriggeredEvents, []uint16{1, 2}) || len(report.AutoSteps) != 1 || report.AutoSteps[0].Input.Index != 2 || report.AutoSteps[0].Input.Input.Value != 25 || w.Events[1].Trigger != 3 || w.Events[2].Trigger != 7 || w.Events[1].AutoScript != 8 || s.current.LastEventID != 2 || report.Work != 7 || s.current.Actions != 1 {
		t.Fatalf("ordered control input/pointer golden: %+v", report)
	}
}

func TestControlsFailureRollsBackEntireFrame(t *testing.T) {
	for _, failure := range []string{"unused", "wrong-input", "unsupported-second-trigger", "unsupported-auto", "shared-budget"} {
		t.Run(failure, func(t *testing.T) {
			s := controlFixture(t, []paldata.Event{
				{X: 320, Y: 160, State: 1, TriggerMode: 4, Trigger: 1, VanishTime: 0},
				{X: 320, Y: 160, State: 1, TriggerMode: 4, Trigger: 4},
				{State: 1, VanishTime: 10},
			})
			// Trigger 1 changes inventory and entry; a later failure must undo both.
			first := paldata.Instruction{Opcode: 0x1f, Operands: [3]uint16{1, 1, 0}}
			s.game.Scripts[1], s.game.Scripts[2] = first, paldata.Instruction{Opcode: 1}
			s.game.Scripts[4] = paldata.Instruction{Opcode: 1}
			var inputs []palflow.Input
			switch failure {
			case "unused":
				inputs = []palflow.Input{{Kind: palflow.InputConfirm, Value: 1}}
			case "wrong-input":
				s.game.Scripts[4].Opcode = 6
				inputs = []palflow.Input{{Kind: palflow.InputConfirm, Value: 1}}
			case "unsupported-second-trigger":
				s.game.Scripts[4].Opcode = 0x7fff
			case "unsupported-auto":
				e := s.current.World.Events[2]
				e.AutoScript = 10
				s.current.World.Events[2] = e
				s.game.Scripts[10].Opcode = 0x7fff
			case "shared-budget":
				s.maxSteps = 4 // control + world tick + first two instructions
			}
			before := s.Snapshot()
			report, err := s.Move(3, inputs)
			if err == nil || report.Completed || !reflect.DeepEqual(before, s.Snapshot()) || len(report.Triggers) == 0 {
				t.Fatalf("%s failed to reject/rollback complete action: %+v %v", failure, report, err)
			}
		})
	}
}

func TestControlsRejectUnenteredMissingResourcesAndInvalidState(t *testing.T) {
	for _, invalid := range []string{"enter", "changed", "pending", "no-resources", "other-scene", "no-catalog", "resource-digest", "coordinate", "direction", "event-boundary", "battle"} {
		t.Run(invalid, func(t *testing.T) {
			s := controlFixture(t, nil)
			w := s.current.World
			switch invalid {
			case "enter":
				s.current.NeedsSceneEnter = true
			case "changed":
				w.SceneChanged = true
			case "pending":
				w.SceneLoadPending = true
			case "no-resources":
				w.SceneResources = nil
			case "other-scene":
				w.SceneResources.SceneID = 2
			case "no-catalog":
				s.maps = nil
			case "resource-digest":
				w.SceneResources.MapSHA256 = "different"
			case "coordinate":
				w.ViewportX = 32768
			case "direction":
				w.Direction = 4
			case "event-boundary":
				delete(w.Scenes, 2)
			case "battle":
				w.InBattle = true
			}
			before := s.Snapshot()
			if report, err := s.Interact(nil); err == nil || report.Completed || !reflect.DeepEqual(before, s.Snapshot()) {
				t.Fatal("invalid controls state accepted or mutated")
			}
		})
	}
	s := controlFixture(t, nil)
	before := s.Snapshot()
	if _, err := s.Move(4, nil); err == nil || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("invalid movement direction accepted or mutated")
	}
}

func TestControlsSceneSwitchCommitsTriggerAndStopsFrame(t *testing.T) {
	s := controlFixture(t, []paldata.Event{{X: 320, Y: 160, State: 1, TriggerMode: 4, Trigger: 1}, {State: 1, VanishTime: 5}})
	s.game.Scripts[1] = paldata.Instruction{Opcode: 0x59, Operands: [3]uint16{2, 0, 0}}
	s.game.Scripts[2] = paldata.Instruction{Opcode: 1}
	trail := append([]palflow.WorldTrail(nil), s.current.World.Trail...)
	report, err := s.Move(3, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := s.current.World
	if !report.Completed || report.Moved || !s.current.NeedsSceneEnter || !w.SceneLoadPending || w.Scene != 2 || w.Events[1].Trigger != 3 || w.Events[2].VanishTime != 5 || w.FrameNum != 40 || w.ChaseCycles != 0 || !reflect.DeepEqual(w.Trail, trail) || s.current.Actions != 1 {
		t.Fatalf("scene-switch early-return golden: %+v", report)
	}
	before := s.Snapshot()
	if _, err := s.Move(3, nil); err == nil || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("controls bypassed next scene OnEnter")
	}
}

func TestControlsMapTableChangeDoesNotImplicitlyReload(t *testing.T) {
	s := controlFixture(t, nil)
	r := s.current.World.Scenes[1]
	r.Map = 65535
	s.current.World.Scenes[1] = r
	if report, err := s.Move(3, nil); err != nil || !report.Moved || s.current.World.SceneResources.MapID != 1 {
		t.Fatalf("scene table map edit replaced current loaded map: %+v %v", report, err)
	}
}

func TestLocalSpatialControlsAfterOpening(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR to the documented local DOS sample")
	}
	g, err := paldata.Load(dir, paldata.Options{Encoding: "big5", Layout: "dos"})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := paldata.LoadRuntime(dir)
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
	s, err := NewSession(g, runtime, SessionOptions{Assets: assets, Maps: maps, RedactText: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enter(nil); err != nil {
		t.Fatal(err)
	}
	// This is a short local key replay from the imported opening, not a claim
	// about story completion or a comparison against the original executable.
	before := s.Snapshot()
	if _, err := s.Move(3, []palflow.Input{{Kind: palflow.InputRandom, Value: 50}}); err == nil || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("local movement with unused input did not roll back")
	}
	expectedPositions := []palflow.WorldPosition{{X: 1360, Y: 296}, {X: 1344, Y: 304}, {X: 1328, Y: 296}, {X: 1344, Y: 288}}
	for index, direction := range []uint16{3, 0, 1, 2} {
		report, err := s.Move(direction, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !report.Completed || !report.Moved || report.Blocked || report.After != expectedPositions[index] || report.AfterFrame != uint32(77+index) || len(report.TriggeredEvents) != 0 || report.Work != 3 {
			t.Fatalf("local move %d differs: after=%+v frame=%d moved=%v blocked=%v trigger=%v work=%d", direction, report.After, report.AfterFrame, report.Moved, report.Blocked, report.TriggeredEvents, report.Work)
		}
	}
	report, err := s.Interact(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Completed || report.After != (palflow.WorldPosition{X: 1360, Y: 280}) || report.AfterFrame != 81 || report.SearchEventID != 10 || !reflect.DeepEqual(report.TriggeredEvents, []uint16{10}) || len(report.Triggers) != 1 || !report.Triggers[0].Completed || report.Work != 33 || s.current.Actions != 6 || s.current.NeedsSceneEnter {
		t.Fatalf("local spatial search differs: after=%+v frame=%d search=%d trigger=%v work=%d", report.After, report.AfterFrame, report.SearchEventID, report.TriggeredEvents, report.Work)
	}
}
