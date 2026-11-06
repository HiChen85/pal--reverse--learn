package palflow

import (
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
)

func poisonFixture() (*Runner, State) {
	code := make([]Instruction, 32)
	code[20] = ins(1)
	r, _ := New(tables{scripts: code}, Options{})
	w := playerWorld()
	w.Objects[0] = [7]uint16{}
	w.Objects[30] = [7]uint16{0: 2, 2: 20, 4: 20}
	w.Objects[31] = [7]uint16{0: 4}
	w.Objects[100] = [7]uint16{1: 0}
	w.Objects[101] = [7]uint16{1: 9}
	w.Battle = &palcombat.Snapshot{Enemies: []palcombat.EnemyState{{Slot: 3, ObjectID: 101, HP: 10}, {Slot: 1, ObjectID: 100, HP: 10}}}
	w.PartyRoles = []uint16{2, 0}
	return r, State{World: w, Frames: []Frame{{PC: 1, Entry: 1, NextEntry: 1, EventID: 3}}, CurEquipPart: -1, ScriptSuccess: true}
}

func TestEnemyPoisonUsesOriginalSlotOrderAndImmediateContinuation(t *testing.T) {
	r, s := poisonFixture()
	step := TraceStep{}
	_, err := r.evaluatePoison(&s, &step, ins(0x28, 1, 30), []Input{{Kind: InputRandom, Value: 0}, {Kind: InputRandom, Value: 8}})
	if err != nil {
		t.Fatal(err)
	}
	x, _ := battleEnemy(s.World, 1)
	other, _ := battleEnemy(s.World, 3)
	if x.Poisons[0] != (palcombat.Poison{ID: 30, Script: 21}) || other.Poisons[0].ID != 0 || s.InputIndex != 2 || s.LastEventID != 3 || len(step.Children) != 1 {
		t.Fatalf("enemy poison context/continuation: %+v", s)
	}
	_, before := poisonFixture()
	state := before
	original := CloneWorld(before.World)
	if _, err = r.evaluatePoison(&state, &TraceStep{}, ins(0x28, 1, 30), []Input{{Kind: InputRandom, Value: 0}}); err == nil || state.InputIndex != 0 || !reflect.DeepEqual(state.World, original) {
		t.Fatal("late missing input committed earlier enemy poison")
	}
}

func TestPlayerPoisonUsesPartyColumnsAndSelfCureReturnSlot(t *testing.T) {
	r, s := poisonFixture()
	raw := s.World.Roles[2]
	raw[22] = 5
	s.World.Roles[2] = raw
	s.World.EquipmentEffects[0][22][2] = 5
	raw = s.World.Roles[0]
	raw[22] = 100
	s.World.Roles[0] = raw
	_, err := r.evaluatePoison(&s, &TraceStep{}, ins(0x29, 1, 30), []Input{{Kind: InputRandom, Value: 11}, {Kind: InputRandom, Value: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if s.World.PlayerPoisons[0][0] != (WorldPoison{ID: 30, Script: 21}) || s.World.PlayerPoisons[0][1].ID != 0 || s.LastEventID != 2 {
		t.Fatal("poison was stored by role ID instead of party position")
	}
	// Clearing the current poison inside its initialization script still writes
	// the returned script to the same now-empty slot, as the source does.
	code := make([]Instruction, 32)
	code[20], code[21] = ins(0x2b, 0, 30), ins(1)
	r, _ = New(tables{scripts: code}, Options{})
	_, s = poisonFixture()
	s.Frames[0].EventID = 2
	if _, err = r.evaluatePoison(&s, &TraceStep{}, ins(0x29, 0, 30), []Input{{Kind: InputRandom, Value: 100}}); err != nil {
		t.Fatal(err)
	}
	if s.World.PlayerPoisons[0][0] != (WorldPoison{Script: 22}) {
		t.Fatal("self-cured slot lost its return continuation")
	}
}

func TestCureByLevelPreservesHigherPoisonAndClearsGhostScripts(t *testing.T) {
	r, s := poisonFixture()
	s.Frames[0].EventID = 2
	s.World.PlayerPoisons[0][0] = WorldPoison{ID: 30, Script: 20}
	s.World.PlayerPoisons[2][0] = WorldPoison{ID: 31, Script: 20}
	s.World.PlayerPoisons[3][0] = WorldPoison{Script: 21}
	if _, err := r.evaluatePoison(&s, &TraceStep{}, ins(0x2c, 0, 3), nil); err != nil {
		t.Fatal(err)
	}
	if s.World.PlayerPoisons[0][0] != (WorldPoison{}) || s.World.PlayerPoisons[2][0].ID != 31 || s.World.PlayerPoisons[3][0] != (WorldPoison{}) {
		t.Fatal("level cure changed the wrong poison slots")
	}
}

func TestGeneratedRandomPreservesDetachedPoisonMutation(t *testing.T) {
	r, s := poisonFixture()
	s.World.RandomMode = "seeded"
	s.World.BattleRandomState = 1
	before := CloneWorld(s.World)
	step := TraceStep{}
	if _, err := r.evaluatePoison(&s, &step, ins(0x28, 0, 30), nil); err != nil {
		t.Fatal(err)
	}
	// Slot 3 resists most draws; use the zero-resistance original slot 1.
	_, s = poisonFixture()
	s.Frames[0].EventID = 1
	s.World.RandomMode = "seeded"
	s.World.BattleRandomState = 1
	if _, err := r.evaluatePoison(&s, &step, ins(0x28, 0, 30), nil); err != nil {
		t.Fatal(err)
	}
	x, _ := battleEnemy(s.World, 1)
	if x.Poisons[0].ID != 30 || !step.RandomInputs[0].Generated || s.InputIndex != 0 || s.World.BattleRandomState == before.BattleRandomState {
		t.Fatal("generated random detached the poison write from the committed world")
	}
}
