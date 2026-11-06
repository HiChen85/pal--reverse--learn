package palgame

import (
	"reflect"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func fixture() (*paldata.Game, *paldata.RuntimeTables) {
	g := &paldata.Game{Files: []paldata.FileInfo{{Name: "data.mkf", SHA256: "synthetic"}}, Scenes: []paldata.Scene{{EventStart: 0}, {EventStart: 1}}, Events: []paldata.Event{{Trigger: 1, State: 1, TriggerMode: 1}}, Objects: make([][7]uint16, 3), Words: []string{"hero", "item"}, Messages: []string{"synthetic dialogue"}, RawMessages: [][]byte{[]byte("synthetic dialogue")}, Scripts: []paldata.Instruction{{}, {Opcode: 0x1f, Operands: [3]uint16{1, 1, 0}}, {Opcode: 0xffff}, {Opcode: 0x49, Operands: [3]uint16{0xffff, 0, 0}}, {}}}
	r := &paldata.RuntimeTables{Source: paldata.FileInfo{Name: "data.mkf", SHA256: "synthetic"}, Enemies: []paldata.Enemy{{}}}
	for i := range r.Roles {
		r.Roles[i].ID = uint16(i)
	}
	return g, r
}
func TestSnapshotIdentityAndIsolation(t *testing.T) {
	g, r := fixture()
	w, err := InitialSnapshot(g, r, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.InventorySlots) != 256 || len(w.PlayerSprites) != 6 || w.PartyOffsetX != 160 || w.PartyOffsetY != 112 {
		t.Fatal("incomplete initial snapshot")
	}
	e := w.Events[1]
	e.State = 0
	w.Events[1] = e
	if g.Events[0].State != 1 {
		t.Fatal("world aliases imported table")
	}
	r.Source.SHA256 = "other"
	if _, err = InitialSnapshot(g, r, 1); err == nil {
		t.Fatal("mixed runtime identity accepted")
	}
}
func TestSessionPreservesPickupStateAndRejectsRepeat(t *testing.T) {
	g, r := fixture()
	s, err := NewSession(g, r, SessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Trigger(1, nil); err == nil {
		t.Fatal("bypassed scene entry")
	}
	if _, err = s.Enter(nil); err != nil {
		t.Fatal(err)
	}
	report, err := s.Trigger(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Completed || !reflect.DeepEqual(report.DialogueIDs, []uint16{0}) {
		t.Fatal("missing dialogue")
	}
	state := s.Snapshot()
	if state.World.Inventory[1] != 1 || state.World.Events[1].State != 0 {
		t.Fatal("pickup state not preserved")
	}
	before := s.Snapshot()
	if _, err = s.Trigger(1, nil); err == nil {
		t.Fatal("hidden pickup repeated")
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("rejected repeat mutated world")
	}
	state.World.Inventory[1] = 99
	if s.Snapshot().World.Inventory[1] != 1 {
		t.Fatal("snapshot aliases session")
	}
}
func TestFailedOrInexactActionsDoNotCommitPrefix(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		g, r := fixture()
		if unknown {
			g.Scripts[2].Opcode = 0x7fff
		}
		s, err := NewSession(g, r, SessionOptions{AssumeSceneEntered: true})
		if err != nil {
			t.Fatal(err)
		}
		before := s.Snapshot()
		inputs := []palflow.Input{{Kind: "confirm", Value: 1}}
		if unknown {
			inputs = nil
		}
		report, err := s.Trigger(1, inputs)
		if err == nil || len(report.Trace) == 0 {
			t.Fatal("invalid action accepted or evidence lost")
		}
		if !reflect.DeepEqual(before, s.Snapshot()) {
			t.Fatal("partial action changed active world")
		}
	}
}
func TestSourceRedactsTextAndRejectsBounds(t *testing.T) {
	g, _ := fixture()
	src := Source{Game: g, RedactText: true}
	text, err := src.Message(0)
	if err != nil || strings.Contains(text, "synthetic dialogue") {
		t.Fatal("redaction failed")
	}
	if _, err = src.Message(1); err == nil {
		t.Fatal("bad message accepted")
	}
	if _, err = src.Instruction(9); err == nil {
		t.Fatal("bad PC accepted")
	}
}

func initializedFixture() (*paldata.Game, *paldata.RuntimeTables) {
	game, runtime := fixture()
	game.Layout = "dos"
	game.Scripts = make([]paldata.Instruction, 14)
	game.Scripts[3] = paldata.Instruction{Opcode: 0x18, Operands: [3]uint16{0xb, 1, 0}}
	game.Scripts[4] = paldata.Instruction{Opcode: 0x17, Operands: [3]uint16{0xb, 17, 15}}
	game.Scripts[5] = paldata.Instruction{Opcode: 1}
	game.Scripts[7] = paldata.Instruction{Opcode: 0x18, Operands: [3]uint16{0xb, 2, 0}}
	game.Scripts[8] = paldata.Instruction{Opcode: 0x2d, Operands: [3]uint16{4, 3, 0}}
	game.Scripts[12] = paldata.Instruction{Opcode: 0x17, Operands: [3]uint16{0xb, 17, 123}}
	game.Scenes[0].OnEnter = 12
	game.Objects[1][3] = 3
	game.Objects[2][3] = 7
	for i := range runtime.Roles {
		runtime.Roles[i].Raw[9] = 100
	}
	runtime.Roles[0].Equipment[0] = 1
	runtime.Roles[0].Raw[11] = 1
	runtime.Roles[5].Equipment[0] = 2
	runtime.Roles[5].Raw[11] = 2
	game.Events[0].SpriteFrames = 3
	game.Events[0].SpriteFramesAuto = 9
	game.Events[0].AutoIdleFrame = 7
	return game, runtime
}

func TestInitialStateAppliesEquipmentBeforeEnteringSceneAndPreservesContext(t *testing.T) {
	game, runtime := initializedFixture()
	initial, err := InitialState(game, runtime, 1)
	if err != nil {
		t.Fatal(err)
	}
	if initial.World.EquipmentEffects[0][17][0] != 15 || initial.World.Objects[1][3] != 6 || initial.World.Objects[2][3] != 7 {
		t.Fatal("new-game equipment effects or returned entries missing")
	}
	if initial.LastEventID != 5 || initial.ScriptSuccess || initial.Steps != 6 || len(initial.Evidence) != 6 {
		t.Fatalf("initial context/evidence missing: %+v", initial)
	}
	event := initial.World.Events[1]
	if event.SpriteFrames != 3 || event.SpriteFramesAuto != 9 || event.AutoIdleFrame != 7 || initial.World.AutoMessageMode != "dos" {
		t.Fatal("imported auto-script context fields missing")
	}
	if game.Objects[1][3] != 3 {
		t.Fatal("initialization mutated imported objects")
	}
	snapshot, err := InitialSnapshot(game, runtime, 1)
	if err != nil || snapshot.EquipmentEffects[0][17][0] != 15 {
		t.Fatal("world-only wrapper skipped initialization")
	}
	session, err := NewSession(game, runtime, SessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if session.Snapshot().LastEventID != 5 || session.Snapshot().ScriptSuccess {
		t.Fatal("session discarded equipment invocation context")
	}
	evidence := session.Initialization()
	evidence.World.Cash = 123
	*evidence.Evidence[2].ReturnedEntry = 99
	evidence.Evidence[0].PC = 99
	if session.Initialization().World.Cash != 0 || *session.Initialization().Evidence[2].ReturnedEntry != 6 || session.Initialization().Evidence[0].PC == 99 {
		t.Fatal("initialization evidence aliases session")
	}
	if _, err := session.Enter(nil); err != nil {
		t.Fatal(err)
	}
	current := session.Snapshot()
	if current.World.EquipmentEffects[0][17][5] != 123 || !current.ScriptSuccess {
		t.Fatal("first scene did not inherit actual last equipment role, or trigger did not reset success")
	}
	if session.Initialization().ScriptSuccess || session.Initialization().World.EquipmentEffects[0][17][5] != 0 {
		t.Fatal("scene action mutated immutable initialization evidence")
	}
}

func TestInitialStateUnsupportedEquipmentBlocksInsteadOfReturningDefaults(t *testing.T) {
	game, runtime := initializedFixture()
	game.Scripts[7].Opcode = 0x7fff
	if _, err := InitialState(game, runtime, 1); err == nil {
		t.Fatal("unsupported equipment callback silently skipped")
	}
	if session, err := NewSession(game, runtime, SessionOptions{}); err == nil || session != nil {
		t.Fatal("failed bootstrap created an active session")
	}
	if game.Objects[1][3] != 3 {
		t.Fatal("failed bootstrap committed returned entry prefix")
	}
}
