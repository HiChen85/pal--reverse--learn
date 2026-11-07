package palflow

import (
	"reflect"
	"strings"
	"testing"
)

func playerWorld() *WorldState {
	w := &WorldState{Roles: map[uint16][75]uint16{}, Objects: map[uint16][7]uint16{}, PartyRoles: []uint16{0}, PlayerStatusMode: "classic", Inventory: map[uint16]int{10: 4}}
	for role := uint16(0); role < 6; role++ {
		raw := [75]uint16{}
		raw[9] = 100
		w.Roles[role] = raw
	}
	return w
}

func TestPartyLengthChangeWithFollowersIsRejectedWithoutPartialCommit(t *testing.T) {
	w := playerWorld()
	w.PartyRoles = []uint16{0, 1}
	w.FollowerSprites = []uint16{7}
	state := playerState(w, 0)
	before := CloneWorld(w)
	_, err := applyPlayerForTest(t, &state, ins(0x75, 1, 0, 0))
	if err == nil || !strings.Contains(err.Error(), "overlapping raw party slots") || !reflect.DeepEqual(state.World, before) {
		t.Fatalf("follower alias shift was accepted: %v", err)
	}
}
func playerState(world *WorldState, role uint16) State {
	return State{World: world, Frames: []Frame{{PC: 1, Entry: 1, NextEntry: 1, EventID: role}}, CurEquipPart: -1, ScriptSuccess: true}
}
func applyPlayerForTest(t *testing.T, state *State, instruction Instruction) (TraceStep, error) {
	t.Helper()
	r, err := New(tables{scripts: []Instruction{ins(0), ins(0)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	step := TraceStep{}
	handled, err := r.evaluatePlayer(state, &step, instruction)
	if !handled {
		t.Fatal("player opcode unhandled")
	}
	return step, err
}
func TestPlayerExtraAndRawAttributesPreserveBitsAndRoleAddressing(t *testing.T) {
	w := playerWorld()
	s := playerState(w, 5)
	_, err := applyPlayerForTest(t, &s, ins(0x17, 0x11, 74, 0xffff))
	if err != nil {
		t.Fatal(err)
	}
	if s.World.EquipmentEffects[6][74][5] != 0xffff || w.EquipmentEffects[6][74][5] != 0 || s.CurEquipPart != -1 {
		t.Fatal("extra attribute alias/sign/slot semantics differ")
	}
	_, err = applyPlayerForTest(t, &s, ins(0x1a, 9, 0xfffe, 1))
	if err != nil {
		t.Fatal(err)
	}
	if s.World.Roles[0][9] != 0xfffe || s.World.Roles[5][9] != 100 {
		t.Fatal("explicit role is not operand2 minus one")
	}
	s.CurEquipPart = 4
	_, err = applyPlayerForTest(t, &s, ins(0x1a, 17, 65535, 0))
	if err != nil {
		t.Fatal(err)
	}
	if s.World.EquipmentEffects[4][17][5] != 65535 || s.World.Roles[5][17] != 0 {
		t.Fatal("current equipment part not used")
	}
}
func TestSameItemReequipClearsOnlyRoleSlotAndDualAttack(t *testing.T) {
	w := playerWorld()
	raw := w.Roles[0]
	raw[14] = 10
	w.Roles[0] = raw
	for attr := range 75 {
		w.EquipmentEffects[3][attr][0] = 99
		w.EquipmentEffects[3][attr][1] = 88
	}
	w.PlayerStatus[0][8] = 1000
	s := playerState(w, 0)
	_, err := applyPlayerForTest(t, &s, ins(0x18, 0xe, 10))
	if err != nil {
		t.Fatal(err)
	}
	if s.CurEquipPart != 3 || s.World.PlayerStatus[0][8] != 0 || s.World.Roles[0][14] != 10 || !reflect.DeepEqual(s.World.Inventory, w.Inventory) {
		t.Fatal("same-item equip changed inventory or did not clear status")
	}
	for attr := range 75 {
		if s.World.EquipmentEffects[3][attr][0] != 0 || s.World.EquipmentEffects[3][attr][1] != 88 {
			t.Fatal("role effect clear exceeded its slot")
		}
	}
	before := s
	before.World = CloneWorld(s.World)
	before.Frames = append([]Frame(nil), s.Frames...)
	_, err = applyPlayerForTest(t, &s, ins(0x18, 0xe, 11))
	if err == nil || !strings.Contains(err.Error(), "swap") || !reflect.DeepEqual(s, before) {
		t.Fatal("unsupported swap committed partial effects")
	}
}
func TestWearCleanupFiltersPoisonObjectsAndHonorsPartyIndex(t *testing.T) {
	w := playerWorld()
	w.PartyRoles = []uint16{2, 0}
	raw := w.Roles[0]
	raw[16] = 20
	w.Roles[0] = raw
	w.Objects[80] = [7]uint16{50}
	w.Objects[81] = [7]uint16{99}
	w.PlayerPoisons[0][1] = WorldPoison{80, 5}
	w.PlayerPoisons[1][1] = WorldPoison{81, 6}
	w.PlayerPoisons[3][1] = WorldPoison{80, 7}
	w.PlayerPoisons[0][0] = WorldPoison{81, 8}
	s := playerState(w, 0)
	_, err := applyPlayerForTest(t, &s, ins(0x18, 0x10, 20))
	if err != nil {
		t.Fatal(err)
	}
	if s.World.PlayerPoisons[0][1] != (WorldPoison{80, 5}) || s.World.PlayerPoisons[1][1] != (WorldPoison{}) || s.World.PlayerPoisons[3][1] != (WorldPoison{}) || s.World.PlayerPoisons[0][0] != (WorldPoison{81, 8}) {
		t.Fatal("wear cleanup differs from contiguous poison-list filtering")
	}
	w = playerWorld()
	raw = w.Roles[0]
	raw[16] = 20
	w.Roles[0] = raw
	w.PlayerPoisons[0][0] = WorldPoison{99, 1}
	s = playerState(w, 0)
	before := CloneWorld(w)
	_, err = applyPlayerForTest(t, &s, ins(0x18, 0x10, 20))
	if err == nil || !reflect.DeepEqual(s.World, before) {
		t.Fatal("unknown poison object was guessed or cleanup leaked")
	}
}
func TestPlayerStatusRulesAndExplicitMode(t *testing.T) {
	for _, test := range []struct {
		name                               string
		status, existing, rounds, hp, want uint16
		success                            bool
	}{
		{"bad_preserve", 0, 3, 8, 100, 3, true}, {"bad_set", 1, 0, 8, 0, 8, true}, {"good_extend", 8, 1000, 1200, 100, 1200, true}, {"good_dead", 8, 3, 8, 0, 3, true}, {"puppet_dead", 4, 2, 8, 0, 8, true}, {"puppet_alive", 4, 2, 8, 100, 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := playerWorld()
			raw := w.Roles[0]
			raw[9] = test.hp
			w.Roles[0] = raw
			w.PlayerStatus[0][test.status] = test.existing
			s := playerState(w, 0)
			_, err := applyPlayerForTest(t, &s, ins(0x2d, test.status, test.rounds))
			if err != nil {
				t.Fatal(err)
			}
			if s.World.PlayerStatus[0][test.status] != test.want || s.ScriptSuccess != test.success {
				t.Fatalf("status result: %+v", s)
			}
		})
	}
	w := playerWorld()
	w.PlayerStatusMode = ""
	s := playerState(w, 0)
	_, err := applyPlayerForTest(t, &s, ins(0x2d, 1, 3))
	if err == nil {
		t.Fatal("missing status mode accepted")
	}
	_, err = applyPlayerForTest(t, &s, ins(0x2d, 8, 1000))
	if err != nil {
		t.Fatal("mode-independent dual attack blocked", err)
	}
	w = playerWorld()
	w.PlayerStatusMode = "modern"
	w.PlayerStatus[0][7] = 3
	s = playerState(w, 0)
	before := CloneWorld(w)
	_, err = applyPlayerForTest(t, &s, ins(0x2d, 1, 3))
	if err == nil || !reflect.DeepEqual(s.World, before) {
		t.Fatal("undefined modern conflict return was guessed")
	}
}
func TestPartyRebuildRunsAllRolesAndPersistsEquipmentEntryPointers(t *testing.T) {
	w := playerWorld()
	w.PartyRoles = []uint16{2}
	w.PlayerPoisons[0][0] = WorldPoison{99, 9}
	w.EquipmentEffects[6][74][5] = 999
	raw := w.Roles[0]
	raw[14] = 10
	w.Roles[0] = raw
	raw = w.Roles[5]
	raw[11] = 11
	raw[12] = 12
	w.Roles[5] = raw
	w.Objects[10] = [7]uint16{0, 0, 0, 10}
	w.Objects[11] = [7]uint16{0, 0, 0, 20}
	w.Objects[12] = [7]uint16{0, 0, 0, 0}
	scripts := make([]Instruction, 24)
	scripts[10] = ins(0x18, 0xe, 10)
	scripts[11] = ins(0x17, 0xe, 17, 25)
	scripts[12] = ins(0x1a, 18, 30)
	scripts[13] = ins(0x2d, 8, 1000)
	scripts[14] = ins(0)
	scripts[20] = ins(0x18, 0xb, 11)
	scripts[21] = ins(0x17, 0xb, 19, 44)
	scripts[22] = ins(1)
	runner, err := New(tables{scripts: scripts}, Options{MaxSteps: 20})
	if err != nil {
		t.Fatal(err)
	}
	state := playerState(w, 99)
	step := TraceStep{}
	handled, err := runner.evaluatePlayer(&state, &step, ins(0x75, 1, 3, 0))
	if !handled || err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.World.PartyRoles, []uint16{0, 2}) || state.World.EquipmentEffects[3][17][0] != 25 || state.World.EquipmentEffects[3][18][0] != 30 || state.World.EquipmentEffects[0][19][5] != 44 || state.World.EquipmentEffects[6][74][5] != 0 {
		t.Fatal("party/equipment effects missing for party or non-party role")
	}
	if state.World.PlayerPoisons != ([16][5]WorldPoison{}) || state.World.PlayerStatus[0][8] != 1000 || state.World.Objects[11][3] != 23 || state.CurEquipPart != -1 || state.LastEventID != 5 || state.Frames[0].EventID != 99 || state.PlayerSubsteps != 8 || len(step.PlayerSubsteps) != 8 {
		t.Fatalf("equipment trigger lifecycle mismatch: state=%+v evidence=%+v", state, step.PlayerSubsteps)
	}
	if step.PlayerSubsteps[0].PC != 10 || step.PlayerSubsteps[0].Role != 0 || step.PlayerSubsteps[5].Role != 5 || step.PlayerSubsteps[7].ReturnedEntry == nil || *step.PlayerSubsteps[7].ReturnedEntry != 23 {
		t.Fatal("equipment instruction evidence lost")
	}
	if w.Objects[11][3] != 20 || w.PlayerPoisons[0][0].ID != 99 || w.EquipmentEffects[6][74][5] != 999 {
		t.Fatal("request world was aliased")
	}
}
func TestPartyRebuildUnknownChildAndBudgetRollbackWholeInstruction(t *testing.T) {
	for _, test := range []struct {
		name   string
		second Instruction
		budget int
		needle string
	}{{"unknown", ins(4, 20), 20, "outside bounded linear"}, {"budget", ins(0x17, 0xb, 17, 30), 2, "budget"}} {
		t.Run(test.name, func(t *testing.T) {
			w := playerWorld()
			raw := w.Roles[0]
			raw[11] = 10
			w.Roles[0] = raw
			w.Objects[10] = [7]uint16{0, 0, 0, 10}
			w.PlayerPoisons[0][0] = WorldPoison{99, 1}
			scripts := make([]Instruction, 13)
			scripts[10] = ins(0x18, 0xb, 10)
			scripts[11] = test.second
			scripts[12] = ins(0)
			runner, err := New(tables{scripts: scripts}, Options{MaxSteps: test.budget})
			if err != nil {
				t.Fatal(err)
			}
			state := playerState(w, 7)
			before := state
			before.World = CloneWorld(w)
			before.Frames = append([]Frame(nil), state.Frames...)
			step := TraceStep{}
			_, err = runner.evaluatePlayer(&state, &step, ins(0x75, 2))
			if err == nil || !strings.Contains(err.Error(), test.needle) || !reflect.DeepEqual(state, before) || len(step.PlayerSubsteps) != 0 {
				t.Fatalf("failed75 partially committed or wrong error: %v", err)
			}
		})
	}
	w := playerWorld()
	state := playerState(w, 9)
	runner, _ := New(tables{}, Options{})
	step := TraceStep{}
	_, err := runner.evaluatePlayer(&state, &step, ins(0x75))
	if err != nil || !reflect.DeepEqual(state.World.PartyRoles, []uint16{0}) {
		t.Fatal("empty party args must choose role0", err)
	}
}

func TestRawSpriteAttributeSynchronizesKnownMirrorOnlyOutsideEquipment(t *testing.T) {
	w := playerWorld()
	raw := w.Roles[0]
	raw[2] = 10
	w.Roles[0] = raw
	w.PlayerSprites = map[uint16]uint16{0: 10}
	state := playerState(w, 0)
	_, err := applyPlayerForTest(t, &state, ins(0x1a, 2, 65535, 0))
	if err != nil {
		t.Fatal(err)
	}
	if state.World.Roles[0][2] != 65535 || state.World.PlayerSprites[0] != 65535 || w.PlayerSprites[0] != 10 {
		t.Fatal("raw sprite write diverged from known mirror or aliased request")
	}
	state.CurEquipPart = 3
	_, err = applyPlayerForTest(t, &state, ins(0x1a, 2, 22, 0))
	if err != nil {
		t.Fatal(err)
	}
	if state.World.EquipmentEffects[3][2][0] != 22 || state.World.Roles[0][2] != 65535 || state.World.PlayerSprites[0] != 65535 {
		t.Fatal("equipment sprite effect changed canonical sprite/mirror")
	}
	state = playerState(playerWorld(), 0)
	_, err = applyPlayerForTest(t, &state, ins(0x1a, 2, 33, 0))
	if err != nil {
		t.Fatal(err)
	}
	if state.World.Roles[0][2] != 33 || state.World.PlayerSprites != nil {
		t.Fatal("omitted partial sprite cache was invented")
	}
}

func TestEquipmentBudgetIncludesPriorWorldTicks(t *testing.T) {
	w := playerWorld()
	raw := w.Roles[0]
	raw[11] = 10
	w.Roles[0] = raw
	w.Objects[10] = [7]uint16{0, 0, 0, 10}
	scripts := make([]Instruction, 12)
	scripts[10] = ins(0x18, 0xb, 10)
	scripts[11] = ins(0)
	runner, err := New(tables{scripts: scripts}, Options{MaxSteps: 4})
	if err != nil {
		t.Fatal(err)
	}
	state := playerState(w, 7)
	state.WorldTicks = 1
	step := TraceStep{}
	_, err = runner.evaluatePlayer(&state, &step, ins(0x75, 1))
	if err != nil || state.PlayerSubsteps != 2 || state.WorldTicks != 1 {
		t.Fatalf("exact shared budget should pass: %v", err)
	}
	state = playerState(w, 7)
	state.WorldTicks = 2
	before := state
	before.World = CloneWorld(w)
	before.Frames = append([]Frame(nil), state.Frames...)
	step = TraceStep{}
	_, err = runner.evaluatePlayer(&state, &step, ins(0x75, 1))
	if err == nil || !strings.Contains(err.Error(), "budget") || !reflect.DeepEqual(state, before) || len(step.PlayerSubsteps) != 0 {
		t.Fatal("prior world ticks were omitted from atomic equipment budget")
	}
}

// Hiding the optional resource interface ensures bootstrap cannot accidentally
// invoke the party-opcode resource-loading path.
type equipmentOnlySource struct{ Source }

func TestInitializeEquipmentRunsCallbacksWithoutPartyReloadOrPoisonClear(t *testing.T) {
	world := playerWorld()
	world.PartyRoles = []uint16{2}
	world.PlayerPoisons[0][0] = WorldPoison{ID: 55, Script: 66}
	world.EquipmentEffects[6][74][5] = 999
	raw := world.Roles[0]
	raw[11] = 10
	world.Roles[0] = raw
	raw = world.Roles[5]
	raw[11] = 11
	world.Roles[5] = raw
	world.Objects[10] = [7]uint16{0, 0, 0, 10}
	world.Objects[11] = [7]uint16{0, 0, 0, 20}
	scripts := make([]Instruction, 22)
	scripts[10] = ins(0x17, 0xb, 17, 25)
	scripts[11] = ins(1)
	scripts[20] = ins(0x2d, 4, 3) // Puppet on a living role returns false.
	scripts[21] = ins(0)
	source := equipmentOnlySource{tables{scripts: scripts}}
	runner, err := New(source, Options{MaxSteps: 4})
	if err != nil {
		t.Fatal(err)
	}
	original := CloneWorld(world)
	state, evidence, err := runner.InitializeEquipment(world)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(world, original) {
		t.Fatal("bootstrap mutated caller world")
	}
	if !reflect.DeepEqual(state.World.PartyRoles, []uint16{2}) || state.World.PlayerPoisons != world.PlayerPoisons || state.World.PlayerResources != nil {
		t.Fatal("bootstrap changed party, cleared poison or loaded sprites")
	}
	if state.World.EquipmentEffects[0][17][0] != 25 || state.World.EquipmentEffects[6][74][5] != 0 {
		t.Fatal("bootstrap did not rebuild equipment effects")
	}
	if state.World.Objects[10][3] != 12 || state.World.Objects[11][3] != 20 || state.LastEventID != 5 || state.ScriptSuccess || state.CurEquipPart != -1 {
		t.Fatal("bootstrap callback return/context/success semantics incorrect")
	}
	if state.PlayerSubsteps != 4 || state.ExecutedSteps != 0 || len(evidence) != 4 || len(state.Frames) != 0 || *evidence[1].ReturnedEntry != 12 {
		t.Fatal("bootstrap evidence or outer-instruction budget incorrect")
	}
	runner, err = New(source, Options{MaxSteps: 3})
	if err != nil {
		t.Fatal(err)
	}
	failed, partial, err := runner.InitializeEquipment(world)
	if err == nil || failed.World != nil || len(partial) != 0 || !reflect.DeepEqual(world, original) {
		t.Fatal("failed bootstrap returned or committed a callback prefix")
	}
}

func TestInitializeEquipmentZeroEntryStillSetsInvocationContext(t *testing.T) {
	world := playerWorld()
	raw := world.Roles[0]
	raw[11] = 10
	world.Roles[0] = raw
	raw = world.Roles[5]
	raw[11] = 11
	world.Roles[5] = raw
	world.Objects[10] = [7]uint16{0, 0, 0, 1}
	world.Objects[11] = [7]uint16{}
	runner, err := New(equipmentOnlySource{tables{scripts: []Instruction{ins(0), ins(0x2d, 4, 3), ins(0)}}}, Options{MaxSteps: 2})
	if err != nil {
		t.Fatal(err)
	}
	state, evidence, err := runner.InitializeEquipment(world)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastEventID != 5 || !state.ScriptSuccess || len(evidence) != 2 {
		t.Fatal("zero entry must still begin and return a trigger invocation")
	}
}

func TestPartyEquipmentBudgetIncludesAutoSubsteps(t *testing.T) {
	world := playerWorld()
	raw := world.Roles[0]
	raw[11] = 10
	world.Roles[0] = raw
	world.Objects[10] = [7]uint16{0, 0, 0, 1}
	runner, err := New(tables{scripts: []Instruction{ins(0), ins(0x17, 0xb, 17, 9), ins(0)}}, Options{MaxSteps: 4})
	if err != nil {
		t.Fatal(err)
	}
	state := playerState(world, 9)
	state.AutoSubsteps = 2
	before := state
	before.World = CloneWorld(world)
	before.Frames = append([]Frame(nil), state.Frames...)
	step := TraceStep{}
	if _, err := runner.evaluatePlayer(&state, &step, ins(0x75, 1)); err == nil || !reflect.DeepEqual(state, before) {
		t.Fatal("prior auto callback instructions omitted from shared budget")
	}
}
