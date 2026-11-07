package palflow

import (
	"reflect"
	"strings"
	"testing"
)

func setPlayerRaw(world *WorldState, role uint16, values map[int]uint16) {
	raw := world.Roles[role]
	for attribute, value := range values {
		raw[attribute] = value
	}
	world.Roles[role] = raw
}

func TestEquipSwapsInPlaceAndPreservesInventoryReservation(t *testing.T) {
	world := playerWorld()
	world.Inventory = nil
	world.InventorySlots = []InventorySlot{{Item: 12, Amount: 4}, {Item: 20, Amount: 1, InUse: 1}, {Item: 30, Amount: 7}}
	world.LastUnequippedItem = 99
	setPlayerRaw(world, 4, map[int]uint16{14: 10})
	world.EquipmentEffects[3][17][4] = 77
	world.PlayerStatus[4][8] = 1000
	state := playerState(world, 4)
	if _, err := applyPlayerForTest(t, &state, ins(0x18, 0xe, 20)); err != nil {
		t.Fatal(err)
	}
	if state.World.Roles[4][14] != 20 || state.World.LastUnequippedItem != 10 || state.CurEquipPart != 3 || state.World.EquipmentEffects[3][17][4] != 0 || state.World.PlayerStatus[4][8] != 0 {
		t.Fatal("equipment swap did not update the role, context, last item or effects")
	}
	if state.World.InventorySlots[1] != (InventorySlot{Item: 10, Amount: 1, InUse: 1}) || state.World.Inventory[10] != 1 || len(state.World.Inventory) != 3 {
		t.Fatalf("one-for-one swap did not preserve its inventory position/reservation: %+v", state.World.InventorySlots[:3])
	}
	if world.InventorySlots[1].Item != 20 || world.Roles[4][14] != 10 || world.LastUnequippedItem != 99 {
		t.Fatal("caller snapshot was aliased")
	}
}

func TestEquipFallbackHonorsRetainedOldSlotAndIgnoredInventoryFailures(t *testing.T) {
	world := playerWorld()
	world.Inventory = nil
	world.InventorySlots = []InventorySlot{{Item: 10, Amount: 0}, {Item: 20, Amount: 1}}
	setPlayerRaw(world, 0, map[int]uint16{11: 10})
	state := playerState(world, 0)
	if _, err := applyPlayerForTest(t, &state, ins(0x18, 0xb, 20)); err != nil {
		t.Fatal(err)
	}
	if state.World.InventorySlots[0].Amount != 1 || state.World.InventorySlots[1] != (InventorySlot{Item: 20, Amount: 0}) {
		t.Fatal("an existing zero-quantity old item must prevent the in-place fast path")
	}
	world = playerWorld()
	world.Inventory = nil
	world.InventorySlots = make([]InventorySlot, 256)
	for index := range world.InventorySlots {
		world.InventorySlots[index] = InventorySlot{Item: uint16(index + 1), Amount: 1}
	}
	setPlayerRaw(world, 0, map[int]uint16{11: 600})
	state = playerState(world, 0)
	if _, err := applyPlayerForTest(t, &state, ins(0x18, 0xb, 500)); err != nil {
		t.Fatal(err)
	}
	if state.World.Roles[0][11] != 500 || state.World.LastUnequippedItem != 600 || len(state.World.Inventory) != 256 {
		t.Fatal("source ignores inventory removal/addition failures during equipment assignment")
	}
}

func TestAttributeSignedDeltaIgnoresEquipmentContextAndTemporaryPercent(t *testing.T) {
	world := playerWorld()
	setPlayerRaw(world, 3, map[int]uint16{2: 0, 17: 101})
	world.PlayerSprites = map[uint16]uint16{3: 0}
	state := playerState(world, 5)
	state.CurEquipPart = 2
	if _, err := applyPlayerForTest(t, &state, ins(0x19, 2, 0xffff, 4)); err != nil {
		t.Fatal(err)
	}
	if state.World.Roles[3][2] != 65535 || state.World.PlayerSprites[3] != 65535 || state.World.EquipmentEffects[2][2][3] != 0 {
		t.Fatal("signed raw delta must wrap and update explicit role, ignoring current equipment part")
	}
	if _, err := applyPlayerForTest(t, &state, ins(0x30, 17, 0xffce, 4)); err != nil {
		t.Fatal(err)
	}
	want := uint16(65536 - 50)
	if state.World.EquipmentEffects[6][17][3] != want || state.World.Roles[3][17] != 101 {
		t.Fatal("temporary percentage must truncate signed product toward zero into extra effect")
	}
}

func TestHPMPUsesWrappedWordThenSignedLowerBoundAndOriginalAliveCheck(t *testing.T) {
	for _, test := range []struct {
		name                                        string
		hp, mp, maxHP, maxMP, delta, wantHP, wantMP uint16
		op                                          uint16
		wantSuccess                                 bool
	}{
		{"both capped", 95, 90, 100, 100, 20, 100, 100, 0x1d, true},
		{"signed negative", 5, 7, 100, 100, 0xfff6, 0, 0, 0x1d, true},
		{"dead ignored", 0, 7, 100, 100, 20, 0, 7, 0x1c, false},
		{"no treatment", 100, 100, 100, 100, 20, 100, 100, 0x1b, false},
		{"signed high becomes zero", 32760, 10, 65535, 100, 20, 0, 10, 0x1b, true},
		{"wrapped positive", 65530, 10, 65535, 100, 20, 14, 10, 0x1b, true},
		{"mp changed after hp reaches zero", 5, 10, 100, 100, 0xfffb, 0, 5, 0x1d, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			world := playerWorld()
			setPlayerRaw(world, 4, map[int]uint16{7: test.maxHP, 8: test.maxMP, 9: test.hp, 10: test.mp})
			state := playerState(world, 4)
			if _, err := applyPlayerForTest(t, &state, ins(test.op, 0, test.delta)); err != nil {
				t.Fatal(err)
			}
			if raw := state.World.Roles[4]; raw[9] != test.wantHP || raw[10] != test.wantMP || state.ScriptSuccess != test.wantSuccess {
				t.Fatalf("got HP=%d MP=%d success=%v", raw[9], raw[10], state.ScriptSuccess)
			}
		})
	}
}

func TestPartyHPMPUsesOrderedRolesAndOpcodeSpecificSuccess(t *testing.T) {
	for _, opcode := range []uint16{0x1b, 0x1c, 0x1d} {
		world := playerWorld()
		world.PartyRoles = []uint16{4, 2}
		setPlayerRaw(world, 4, map[int]uint16{7: 100, 8: 100, 9: 0, 10: 0})
		setPlayerRaw(world, 2, map[int]uint16{7: 100, 8: 100, 9: 10, 10: 10})
		state := playerState(world, 99)
		state.ScriptSuccess = false
		if _, err := applyPlayerForTest(t, &state, ins(opcode, 1, 20)); err != nil {
			t.Fatal(err)
		}
		if state.ScriptSuccess != (opcode == 0x1b) || state.World.Roles[4][9] != 0 || state.World.Roles[0][9] != 100 {
			t.Fatalf("party targeting or success rules differ for %04x", opcode)
		}
		if opcode != 0x1c && state.World.Roles[2][9] != 30 {
			t.Fatal("party member's HP not changed")
		}
		if opcode != 0x1b && state.World.Roles[2][10] != 30 {
			t.Fatal("party member's MP not changed")
		}
	}
}

func TestRemoveItemsUsesInventoryShortageThenEquipmentInPartySlotOrder(t *testing.T) {
	world := playerWorld()
	world.PartyRoles = []uint16{2, 5}
	world.Inventory, world.InventorySlots = nil, []InventorySlot{{Item: 10, Amount: 1, InUse: 1}}
	setPlayerRaw(world, 0, map[int]uint16{11: 10})
	setPlayerRaw(world, 2, map[int]uint16{11: 10, 14: 10})
	setPlayerRaw(world, 5, map[int]uint16{11: 10})
	world.PlayerStatus[2][8] = 1000
	world.EquipmentEffects[0][17][2] = 99
	state := playerState(world, 99)
	if _, err := applyPlayerForTest(t, &state, ins(0x20, 10, 3, 40)); err != nil {
		t.Fatal(err)
	}
	if state.World.InventorySlots[0] != (InventorySlot{Item: 10, Amount: 0, InUse: 1}) || state.World.Roles[2][11] != 0 || state.World.Roles[2][14] != 0 || state.World.Roles[5][11] != 10 || state.World.Roles[0][11] != 10 || state.World.EquipmentEffects[0][17][2] != 0 || state.World.PlayerStatus[2][8] != 0 {
		t.Fatal("partial inventory removal did not consume exact remainder from ordered party equipment")
	}
	if state.Frames[0].PC != 2 {
		t.Fatal("sufficient aggregate availability must fall through")
	}
}

func TestRemoveItemsInsufficientBranchNeedsNoOrderedSlotsAndPreservesWorld(t *testing.T) {
	world := playerWorld()
	world.PartyRoles = []uint16{5}
	setPlayerRaw(world, 5, map[int]uint16{11: 10})
	state := playerState(world, 99)
	state.Frames[0].PC = 65535
	before := CloneWorld(world)
	if _, err := applyPlayerForTest(t, &state, ins(0x20, 10, 6, 42)); err != nil {
		t.Fatal(err)
	}
	if state.Frames[0].PC != 42 || !reflect.DeepEqual(state.World, before) {
		t.Fatal("insufficient aggregate count must jump before mutation, including at maximum PC")
	}
	world = playerWorld()
	world.Inventory, world.InventorySlots = nil, []InventorySlot{}
	setPlayerRaw(world, 0, map[int]uint16{11: 10})
	state = playerState(world, 0)
	if _, err := applyPlayerForTest(t, &state, ins(0x20, 10, 65535, 42)); err != nil {
		t.Fatal(err)
	}
	if state.Frames[0].PC != 42 || state.World.Roles[0][11] != 10 {
		t.Fatal("removal amount is unsigned rather than signed")
	}
}

func TestRevivePreservesEquipmentStatusesAndSparseHighLevelPoisons(t *testing.T) {
	world := playerWorld()
	world.PartyRoles = []uint16{2, 5}
	setPlayerRaw(world, 5, map[int]uint16{7: 101, 9: 0, 10: 17})
	world.Objects[80], world.Objects[81] = [7]uint16{3}, [7]uint16{4}
	world.PlayerPoisons[0][1] = WorldPoison{80, 9}
	world.PlayerPoisons[2][1] = WorldPoison{81, 8}
	world.PlayerPoisons[15][1] = WorldPoison{80, 7}
	world.PlayerStatus[5][0], world.PlayerStatus[5][8] = 999, 1000
	state := playerState(world, 5)
	if _, err := applyPlayerForTest(t, &state, ins(0x22, 0, 5)); err != nil {
		t.Fatal(err)
	}
	if state.World.Roles[5][9] != 50 || state.World.Roles[5][10] != 17 || state.World.PlayerStatus[5][0] != 0 || state.World.PlayerStatus[5][8] != 1000 || state.World.PlayerPoisons[0][1] != (WorldPoison{}) || state.World.PlayerPoisons[15][1] != (WorldPoison{}) || state.World.PlayerPoisons[2][1] != (WorldPoison{81, 8}) {
		t.Fatal("revive healing, status retention or full sparse poison scan differs")
	}
	if _, err := applyPlayerForTest(t, &state, ins(0x22, 0, 5)); err != nil || state.ScriptSuccess {
		t.Fatal("reviving a living role must report failure", err)
	}
}

func TestUnequipUsesExplicitZeroBasedRoleAndOneBasedPart(t *testing.T) {
	world := playerWorld()
	world.Inventory, world.InventorySlots = nil, []InventorySlot{}
	setPlayerRaw(world, 4, map[int]uint16{14: 20})
	world.PlayerStatus[4][8] = 1000
	state := playerState(world, 99)
	if _, err := applyPlayerForTest(t, &state, ins(0x23, 4, 4)); err != nil {
		t.Fatal(err)
	}
	if state.World.Roles[4][14] != 0 || state.World.Inventory[20] != 1 || state.World.PlayerStatus[4][8] != 0 || state.CurEquipPart != -1 {
		t.Fatal("unequip role/part mapping or cleanup differs")
	}
	state.World.EquipmentEffects[3][17][4] = 77
	if _, err := applyPlayerForTest(t, &state, ins(0x23, 4, 4)); err != nil || state.World.EquipmentEffects[3][17][4] != 77 {
		t.Fatal("specific empty equipment part must be ignored", err)
	}
	if _, err := applyPlayerForTest(t, &state, ins(0x23, 4, 0)); err != nil || state.World.EquipmentEffects[3][17][4] != 0 {
		t.Fatal("all-part removal must clear effects even from empty parts", err)
	}
}

func TestRemoveStatusPreservesPermanentEquipmentDuration(t *testing.T) {
	world := playerWorld()
	world.PlayerStatus[3][7], world.PlayerStatus[3][8] = 999, 1000
	state := playerState(world, 3)
	for _, status := range []uint16{7, 8} {
		if _, err := applyPlayerForTest(t, &state, ins(0x2f, status)); err != nil {
			t.Fatal(err)
		}
	}
	if state.World.PlayerStatus[3][7] != 0 || state.World.PlayerStatus[3][8] != 1000 {
		t.Fatal("status removal must preserve durations above 999")
	}
}

func TestMagicSlotsPreserveHolesAndCheckDuplicatesBeyondFirstHole(t *testing.T) {
	world := playerWorld()
	setPlayerRaw(world, 4, map[int]uint16{32: 10, 34: 20, 35: 10})
	state := playerState(world, 99)
	state.ScriptSuccess = false
	for _, instruction := range []Instruction{ins(0x55, 20, 5), ins(0x56, 10, 5), ins(0x55, 30, 5)} {
		if _, err := applyPlayerForTest(t, &state, instruction); err != nil {
			t.Fatal(err)
		}
	}
	raw := state.World.Roles[4]
	if raw[32] != 30 || raw[33] != 0 || raw[34] != 20 || raw[35] != 10 || state.ScriptSuccess {
		t.Fatal("magic slot duplicate, hole, first-match or ignored success rules differ")
	}
	setPlayerRaw(state.World, 4, map[int]uint16{32: 1})
	raw = state.World.Roles[4]
	for index := 32; index < 64; index++ {
		raw[index] = uint16(index)
	}
	state.World.Roles[4] = raw
	if _, err := applyPlayerForTest(t, &state, ins(0x55, 500, 5)); err != nil || state.World.Roles[4] != raw {
		t.Fatal("full magic slots must ignore failed add", err)
	}
}

func TestMagicDamageUsesObjectMagicNumberAndWordArithmetic(t *testing.T) {
	world := playerWorld()
	world.Objects[200] = [7]uint16{7}
	world.MagicBaseDamage = map[uint16]uint16{7: 11, 200: 22}
	setPlayerRaw(world, 3, map[int]uint16{9: 0, 10: 10000})
	state := playerState(world, 3)
	if _, err := applyPlayerForTest(t, &state, ins(0x57, 200, 0)); err != nil {
		t.Fatal(err)
	}
	if state.World.MagicBaseDamage[7] != 14464 || state.World.MagicBaseDamage[200] != 22 || state.World.Roles[3][10] != 0 || world.MagicBaseDamage[7] != 11 {
		t.Fatal("MP multiplier default, wrapped assignment, magic indirection or clone differs")
	}
}

func TestFullHPConditionOnlyUsesPartyAndStrictLessThan(t *testing.T) {
	world := playerWorld()
	world.PartyRoles = []uint16{5, 3}
	setPlayerRaw(world, 5, map[int]uint16{7: 100, 9: 101})
	setPlayerRaw(world, 3, map[int]uint16{7: 100, 9: 100})
	setPlayerRaw(world, 0, map[int]uint16{7: 100, 9: 0})
	state := playerState(world, 99)
	if _, err := applyPlayerForTest(t, &state, ins(0x74, 77)); err != nil || state.Frames[0].PC != 2 {
		t.Fatal("equal or above maximum party HP must fall through", err)
	}
	setPlayerRaw(state.World, 3, map[int]uint16{9: 99})
	state.Frames[0].PC = 65535
	if _, err := applyPlayerForTest(t, &state, ins(0x74, 77)); err != nil || state.Frames[0].PC != 77 {
		t.Fatal("injured later party member must branch without fallthrough overflow", err)
	}
}

func TestObjectScriptWritesNormalizedDOSFieldsWithoutRemappingFlags(t *testing.T) {
	world := playerWorld()
	world.Objects[10] = [7]uint16{0, 1, 2, 3, 4, 0, 77}
	state := playerState(world, 99)
	if _, err := applyPlayerForTest(t, &state, ins(0x90, 10, 123, 3)); err != nil {
		t.Fatal(err)
	}
	if state.World.Objects[10][5] != 123 || state.World.Objects[10][6] != 77 {
		t.Fatal("runtime normalized raw operand index must not remap DOS flags")
	}
	if _, err := applyPlayerForTest(t, &state, ins(0x90, 10, 456, 4)); err != nil || state.World.Objects[10][6] != 456 {
		t.Fatal("last bounded normalized field was rejected", err)
	}
}

func TestPlayerNewFailuresRollbackWorldContextAndPC(t *testing.T) {
	for _, test := range []struct {
		name        string
		instruction Instruction
		prepare     func(*WorldState)
		needle      string
	}{
		{"raw index", ins(0x19, 75, 1), nil, "attribute"},
		{"party context", ins(0x1b, 1, 1), func(w *WorldState) { w.PartyRoles = nil }, "party"},
		{"late party role", ins(0x1d, 1, 1), func(w *WorldState) { w.PartyRoles = []uint16{0, 9} }, "role"},
		{"revive poison", ins(0x22, 0, 5), func(w *WorldState) {
			setPlayerRaw(w, 0, map[int]uint16{7: 100, 9: 0})
			w.PlayerPoisons[15][0] = WorldPoison{99, 1}
		}, "poison"},
		{"revive int overflow", ins(0x22, 0, 65535), func(w *WorldState) { setPlayerRaw(w, 0, map[int]uint16{7: 65535, 9: 0}) }, "overflows"},
		{"unequip slot", ins(0x23, 0, 7), nil, "part"},
		{"status", ins(0x2f, 9), nil, "status"},
		{"missing magic", ins(0x57, 10), func(w *WorldState) { w.Objects[10] = [7]uint16{3} }, "base damage"},
		{"object field", ins(0x90, 10, 1, 5), nil, "selector"},
		{"equipment poison late removal", ins(0x20, 10, 2), func(w *WorldState) {
			w.Inventory = nil
			w.InventorySlots = []InventorySlot{{Item: 10, Amount: 1}}
			setPlayerRaw(w, 0, map[int]uint16{16: 10})
			w.PlayerPoisons[0][0] = WorldPoison{99, 1}
		}, "poison"},
	} {
		t.Run(test.name, func(t *testing.T) {
			world := playerWorld()
			if test.prepare != nil {
				test.prepare(world)
			}
			state := playerState(world, 0)
			before := state
			before.World, before.Frames = CloneWorld(world), append([]Frame(nil), state.Frames...)
			_, err := applyPlayerForTest(t, &state, test.instruction)
			if err == nil || !strings.Contains(err.Error(), test.needle) || !reflect.DeepEqual(state, before) {
				t.Fatalf("failure was guessed, or partially committed: %v", err)
			}
		})
	}
}

func TestExpandedLinearEquipmentOpcodesRemainAtomicAndBudgeted(t *testing.T) {
	world := playerWorld()
	setPlayerRaw(world, 0, map[int]uint16{11: 10, 17: 100})
	world.Objects[10] = [7]uint16{0, 0, 0, 1}
	scripts := []Instruction{ins(0), ins(0x19, 17, 2), ins(0x30, 17, 50), ins(0x55, 200), ins(0x2f, 8), ins(0)}
	runner, err := New(equipmentOnlySource{tables{scripts: scripts}}, Options{MaxSteps: 5})
	if err != nil {
		t.Fatal(err)
	}
	state, evidence, err := runner.InitializeEquipment(world)
	if err != nil || len(evidence) != 5 || state.PlayerSubsteps != 5 || state.World.Roles[0][17] != 102 || state.World.EquipmentEffects[6][17][0] != 51 || state.World.Roles[0][32] != 200 {
		t.Fatal("new deterministic linear operations did not share budget or state", err)
	}
	runner, err = New(equipmentOnlySource{tables{scripts: scripts}}, Options{MaxSteps: 4})
	if err != nil {
		t.Fatal(err)
	}
	state, evidence, err = runner.InitializeEquipment(world)
	if err == nil || state.World != nil || len(evidence) != 0 || world.Roles[0][17] != 100 {
		t.Fatal("expanded equipment subset did not rollback on shared budget exhaustion")
	}
}
