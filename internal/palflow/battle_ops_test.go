package palflow

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

type battleTablesSource struct {
	tables
	runtime *paldata.RuntimeTables
}

func (s battleTablesSource) RuntimeTables() *paldata.RuntimeTables { return s.runtime }

func battleWorld() *WorldState {
	return &WorldState{
		InBattle: true, PartyRoles: []uint16{2, 0}, BattleMovingPlayer: 0,
		InventorySlots: []InventorySlot{},
		Roles:          map[uint16][75]uint16{2: {7: 100, 9: 40}, 0: {7: 80, 9: 30}},
		Objects:        map[uint16][7]uint16{10: {0: 0, 1: 5}, 11: {0: 1, 1: 9}},
		Battle: &palcombat.Snapshot{Outcome: palcombat.Ongoing, MaxEnemySlot: 3, Enemies: []palcombat.EnemyState{
			{Slot: 3, ObjectID: 10, HP: 100, Enemy: paldata.Enemy{HP: 100, CollectValue: 10, Raw: [35]uint16{11: 100, 34: 10}}},
			{Slot: 1, ObjectID: 11, HP: 60, Enemy: paldata.Enemy{ID: 1, HP: 60, Raw: [35]uint16{11: 60}}},
		}},
	}
}

func battleRuntime() *paldata.RuntimeTables {
	return &paldata.RuntimeTables{
		Enemies: []paldata.Enemy{{HP: 100}, {ID: 1, HP: 60}},
		Stores:  [][paldata.StoreSize]uint16{{20, 21, 22, 23, 24, 25, 26, 27, 28}},
	}
}

func battleEvaluate(t *testing.T, w *WorldState, event uint16, inst Instruction, inputs ...Input) (State, TraceStep) {
	t.Helper()
	r, err := New(battleTablesSource{runtime: battleRuntime()}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := State{World: w, ScriptSuccess: true, Frames: []Frame{{PC: 10, EventID: event}}}
	step := TraceStep{}
	handled, err := r.evaluateBattleOps(&s, &step, inst, inputs)
	if !handled || err != nil {
		t.Fatalf("opcode %04x: handled=%t err=%v", inst.Opcode, handled, err)
	}
	if step.Effect == "" {
		t.Fatal("missing battle evidence")
	}
	return s, step
}

// Numeric constants are independently calculated from the classic WORD
// assignments and exact script predicates in the pinned reference.
func TestBattleEnemyHPSourceGoldens(t *testing.T) {
	for _, tc := range []struct {
		name string
		hp   int
		inst Instruction
		want int
	}{
		{"damage", 100, ins(0x21, 0, 30), 70},
		{"damage WORD underflow", 100, ins(0x21, 0, 101), 65535},
		{"half rounds damage up", 10, ins(0x5b, 100), 4},
		{"half bounded", 10, ins(0x5b, 3), 7},
		{"half zero cap", 10, ins(0x5b, 0), 10},
		{"half dead WORD underflow", 0, ins(0x5b, 1), 65535},
		{"half largest WORD", 65535, ins(0x5b, 65535), 32767},
		{"immediate KO", 100, ins(0x60), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := battleWorld()
			setBattleEnemyHP(&w.Battle.Enemies[0], uint16(tc.hp))
			before := CloneWorld(w)
			s, _ := battleEvaluate(t, w, 3, tc.inst)
			x := s.World.Battle.Enemies[0]
			if x.HP != tc.want || int(x.Enemy.HP) != tc.want || int(x.Enemy.Raw[11]) != tc.want {
				t.Fatalf("HP fields diverged: %+v", x)
			}
			if s.Frames[0].PC != 11 || !reflect.DeepEqual(w, before) {
				t.Fatal("wrong fallthrough or caller snapshot mutated")
			}
		})
	}
	w := battleWorld()
	w.Battle.Enemies = append(w.Battle.Enemies, palcombat.EnemyState{Slot: 4, HP: 30})
	s, _ := battleEvaluate(t, w, 0xffff, ins(0x21, 1, 70))
	if s.World.Battle.Enemies[0].HP != 30 || s.World.Battle.Enemies[1].HP != 65526 || s.World.Battle.Enemies[2].HP != 30 {
		t.Fatal("all-target damage did not skip empty object or retain WORD underflow")
	}
}

func TestBattleDrainUsesMovingPartyRoleAndWORDHealing(t *testing.T) {
	w := battleWorld()
	w.BattleMovingPlayer = 1
	s, _ := battleEvaluate(t, w, 3, ins(0x39, 60))
	if s.World.Battle.Enemies[0].HP != 40 || s.World.Roles[0][9] != 80 || s.World.Roles[2][9] != 40 {
		t.Fatal("drain must heal moving player's role and cap after WORD addition")
	}
	w = battleWorld()
	raw := w.Roles[2]
	raw[7], raw[9] = 65535, 65530
	w.Roles[2] = raw
	setBattleEnemyHP(&w.Battle.Enemies[0], 5)
	s, _ = battleEvaluate(t, w, 3, ins(0x39, 10))
	if s.World.Roles[2][9] != 4 || s.World.Battle.Enemies[0].HP != 65531 {
		t.Fatal("drain silently changed source WORD assignments")
	}
}

func TestBattleEnemyStatusResistanceSourceGoldens(t *testing.T) {
	for _, tc := range []struct {
		name string
		draw int
		pc   uint16
		want uint16
	}{
		{"below", 4, 20, 0},
		{"equal resists", 5, 20, 0},
		{"above", 6, 11, 7},
		{"division tail", 10, 11, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, step := battleEvaluate(t, battleWorld(), 3, ins(0x2e, palcombat.Sleep, 7, 20), Input{InputRandom, tc.draw})
			if s.Frames[0].PC != tc.pc || s.World.Battle.Enemies[0].Status[palcombat.Sleep] != tc.want || s.InputIndex != 1 || step.Input.Input.Value != tc.draw || !s.ScriptSuccess {
				t.Fatalf("wrong status result: %+v", s)
			}
		})
	}
	w := battleWorld()
	w.Battle.Enemies[0].Status[palcombat.Sleep] = 999
	s, _ := battleEvaluate(t, w, 3, ins(0x2e, palcombat.Sleep, 0, 20), Input{InputRandom, 6})
	if s.World.Battle.Enemies[0].Status[palcombat.Sleep] != 0 {
		t.Fatal("enemy status assignment must replace even a longer duration")
	}
}

func TestBattleCollectAndExchangeSourceGoldens(t *testing.T) {
	w := battleWorld()
	w.CollectValue = 65530
	s, _ := battleEvaluate(t, w, 3, ins(0x33, 20))
	if s.World.CollectValue != 4 {
		t.Fatal("collect counter did not wrap as WORD")
	}
	s, _ = battleEvaluate(t, w, 1, ins(0x33, 20))
	if s.Frames[0].PC != 20 || s.World.CollectValue != 65530 {
		t.Fatal("zero collect value must branch without changing pool")
	}
	for _, tc := range []struct {
		name string
		pool uint16
		draw []Input
		left uint16
		item uint16
	}{
		{"one no random", 1, nil, 0, 20},
		{"capped index", 10, []Input{{InputRandom, 11}}, 1, 28},
		{"division tail wraps pool", 4, []Input{{InputRandom, 5}}, 65535, 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := battleWorld()
			w.CollectValue = tc.pool
			s, step := battleEvaluate(t, w, 0xffff, ins(0x34, 20), tc.draw...)
			if s.World.CollectValue != tc.left || s.World.Inventory[tc.item] != 1 || s.InputIndex != len(tc.draw) || step.Screen == nil || step.Screen.Rendered {
				t.Fatalf("wrong exchange: %+v", s.World)
			}
		})
	}
	s, _ = battleEvaluate(t, battleWorld(), 0xffff, ins(0x34, 0))
	if s.Frames[0].PC != 0 || s.InputIndex != 0 {
		t.Fatal("empty pool must branch directly to operand zero")
	}
}

func TestBattleStealingSourceGoldens(t *testing.T) {
	w := battleWorld()
	w.Battle.Enemies[0].Enemy.StealCount = 9
	w.Cash = 0xffffffff
	s, step := battleEvaluate(t, w, 3, ins(0x6a, 0), Input{InputRandom, 11}, Input{InputRandom, 4})
	x := s.World.Battle.Enemies[0]
	if x.Enemy.StealCount != 7 || x.Enemy.Raw[20] != 7 || s.World.Cash != 1 || s.InputIndex != 2 || step.Screen.Kind != "battle_steal" {
		t.Fatal("rate zero must still draw chance; coin divisor tail and DWORD cash wrap must survive")
	}
	w = battleWorld()
	w.Battle.Enemies[0].Enemy.StealItem, w.Battle.Enemies[0].Enemy.StealCount = 20, 2
	s, _ = battleEvaluate(t, w, 3, ins(0x6a, 5), Input{InputRandom, 5})
	if s.World.Inventory[20] != 1 || s.World.Battle.Enemies[0].Enemy.StealCount != 1 || s.InputIndex != 1 {
		t.Fatal("item theft must succeed at equality with one random draw")
	}
	s, _ = battleEvaluate(t, w, 3, ins(0x6a, 5), Input{InputRandom, 6})
	if len(s.World.Inventory) != 0 || s.World.Battle.Enemies[0].Enemy.StealCount != 2 || s.InputIndex != 1 {
		t.Fatal("failed theft changed enemy or inventory")
	}
	s, _ = battleEvaluate(t, battleWorld(), 3, ins(0x6a, 0))
	if s.InputIndex != 0 {
		t.Fatal("empty enemy stock must not draw")
	}
}

func TestBattleBaselineAndConditionalSourceGoldens(t *testing.T) {
	for _, tc := range []struct {
		name string
		inst Instruction
		edit func(*WorldState)
		pc   uint16
	}{
		{"percentage above", ins(0x64, 50, 20), func(w *WorldState) { setBattleEnemyHP(&w.Battle.Enemies[0], 51) }, 20},
		{"percentage equal", ins(0x64, 50, 20), func(w *WorldState) { setBattleEnemyHP(&w.Battle.Enemies[0], 50) }, 11},
		{"enemy moving", ins(0x68, 20), func(w *WorldState) { w.BattleEnemyMoving = true }, 20},
		{"party moving", ins(0x68, 20), nil, 11},
		{"first enemy kind", ins(0x91, 20), nil, 11},
		{"later same kind", ins(0x91, 20), func(w *WorldState) { w.Battle.Enemies[1].ObjectID = 10 }, 20},
		{"not in battle", ins(0x91, 20), func(w *WorldState) { w.InBattle = false; w.Battle = nil }, 11},
		{"boss escape fails", ins(0x3a, 20), func(w *WorldState) { w.BattleBoss = true }, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := battleWorld()
			if tc.edit != nil {
				tc.edit(w)
			}
			s, _ := battleEvaluate(t, w, 3, tc.inst)
			if s.Frames[0].PC != tc.pc || !s.ScriptSuccess {
				t.Fatalf("PC=%d success=%t", s.Frames[0].PC, s.ScriptSuccess)
			}
		})
	}
}

func TestBattleScalarRawValuesAndPresentation(t *testing.T) {
	w := battleWorld()
	s, _ := battleEvaluate(t, w, 3, ins(0x5c, 65535))
	if s.World.BattleHidingTime != -65535 {
		t.Fatal("hiding duration was truncated to SHORT")
	}
	s, _ = battleEvaluate(t, w, 3, ins(0x6b, 65535))
	if s.World.BattleBlow != -1 {
		t.Fatal("blow must cast operand to signed SHORT")
	}
	s, _ = battleEvaluate(t, w, 3, ins(0x67, 21, 0))
	x := s.World.Battle.Enemies[0]
	if x.Enemy.Magic != 21 || x.Enemy.MagicRate != 10 || x.Enemy.Raw[15] != 21 || x.Enemy.Raw[16] != 10 {
		t.Fatal("enemy magic fields did not stay normalized")
	}
	for _, result := range []uint16{0, 1, 3, 0xffff, 1000, 1001, 1002, 42} {
		s, _ := battleEvaluate(t, w, 3, ins(0x89, result))
		if !s.World.BattleResultSet || s.World.BattleResult != result || s.World.Battle.Outcome != palcombat.Ongoing {
			t.Fatal("raw result must be preserved for the combat bridge")
		}
	}
	s, step := battleEvaluate(t, w, 3, ins(0x3a, 20))
	if s.World.BattleResult != 0xffff || !s.World.BattleResultSet || step.Screen.Kind != "battle_player_escape" {
		t.Fatal("non-boss script escape did not request fled result")
	}
	s, step = battleEvaluate(t, w, 3, ins(0x69))
	if s.World.BattleResult != 0 || !s.World.BattleResultSet || step.Audio.ID != 45 || step.Audio.Executed || step.Screen.Rendered {
		t.Fatal("enemy escape must terminate without executing presentation")
	}
	s, step = battleEvaluate(t, w, 3, ins(0x92, 2))
	if step.Screen == nil || step.Screen.Rendered || s.WorldTicks != 0 || s.AutoSubsteps != 0 {
		t.Fatal("magic animation advanced the world")
	}
	if battleOpcodeName(0x26) != "" || battleOpcodeName(0x27) != "" || battleOpcodeName(0x32) != "" || battleOpcodeName(0x93) != "" {
		t.Fatal("misclassified nonbattle or absent opcode")
	}
}

func TestBattleInstructionFailureIsAtomic(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inst   Instruction
		edit   func(*State)
		inputs []Input
		want   string
	}{
		{"missing enemy", ins(0x21, 0, 10), func(s *State) { s.Frames[0].EventID = 2 }, nil, "absent"},
		{"no battle", ins(0x60), func(s *State) { s.World.Battle = nil }, nil, "snapshot"},
		{"duplicate slots", ins(0x60), func(s *State) { s.World.Battle.Enemies[1].Slot = 3 }, nil, "duplicated"},
		{"invalid enemy HP", ins(0x60), func(s *State) { s.World.Battle.Enemies[0].HP = -1 }, nil, "WORD"},
		{"bad status before draw", ins(0x2e, 9, 1, 20), nil, []Input{{InputRandom, 6}}, "status"},
		{"bad random range", ins(0x2e, 0, 1, 20), nil, []Input{{InputRandom, 11}}, "random"},
		{"steal second draw absent", ins(0x6a, 0), func(s *State) { s.World.Battle.Enemies[0].Enemy.StealCount = 9 }, []Input{{InputRandom, 0}}, "random"},
		{"heal role absent", ins(0x39, 10), func(s *State) { delete(s.World.Roles, 2) }, nil, "absent"},
		{"fallthrough overflow", ins(0x21, 1, 10), func(s *State) { s.Frames[0].PC = 0xffff }, nil, "overflow"},
		{"random then fallthrough overflow", ins(0x2e, 0, 1, 20), func(s *State) { s.Frames[0].PC = 0xffff }, []Input{{InputRandom, 6}}, "overflow"},
		{"bad casting player", ins(0x92, 3), nil, nil, "party index"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := New(battleTablesSource{runtime: battleRuntime()}, Options{})
			s := State{World: battleWorld(), ScriptSuccess: true, Frames: []Frame{{PC: 10, EventID: 3}}}
			if tc.edit != nil {
				tc.edit(&s)
			}
			before := s
			before.World, before.Frames = CloneWorld(s.World), append([]Frame(nil), s.Frames...)
			step := TraceStep{Effect: "unchanged"}
			handled, err := r.evaluateBattleOps(&s, &step, tc.inst, tc.inputs)
			if !handled || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("handled=%t err=%v", handled, err)
			}
			if !reflect.DeepEqual(s, before) || step.Effect != "unchanged" || step.Input != nil {
				t.Fatal("failed instruction published partial state, input or evidence")
			}
		})
	}
}

type battleScriptRandom struct {
	values []int
	ranges [][2]int
}

func (r *battleScriptRandom) RandomLong(low, high int) (int, error) {
	r.ranges = append(r.ranges, [2]int{low, high})
	if len(r.values) == 0 {
		return 0, fmt.Errorf("battle test random exhausted")
	}
	v := r.values[0]
	r.values = r.values[1:]
	return v, nil
}

func TestBattleGeneratedRandomSharesProviderAndRetainsEveryDraw(t *testing.T) {
	r, _ := New(tables{}, Options{})
	rng := &battleScriptRandom{values: []int{2}}
	w := battleWorld()
	w.Battle.Enemies[0].Enemy.StealCount = 9
	s := State{World: w, Random: rng, Frames: []Frame{{PC: 10, EventID: 3}}}
	step := TraceStep{}
	// The explicit chance wins over the provider. The provider supplies the
	// second, currently unknown draw without consuming the confirmation tape.
	inputs := []Input{{InputRandom, 7}, {InputConfirm, 1}}
	if handled, err := r.evaluateBattleOps(&s, &step, ins(0x6a, 0), inputs); !handled || err != nil {
		t.Fatalf("handled=%t error=%v", handled, err)
	}
	if !reflect.DeepEqual(rng.ranges, [][2]int{{2, 3}}) || s.InputIndex != 1 || s.World.Cash != 4 || len(step.RandomInputs) != 2 {
		t.Fatalf("provider or draw records wrong: ranges=%v inputs=%d step=%+v", rng.ranges, s.InputIndex, step)
	}
	if step.RandomInputs[0].Generated || step.RandomInputs[0].Index != 0 || !step.RandomInputs[1].Generated || step.RandomInputs[1].Index != -1 {
		t.Fatal("explicit and generated random evidence was conflated")
	}
}

func TestBattleInventoryFullStillConsumesStolenItem(t *testing.T) {
	w := battleWorld()
	w.InventorySlots = make([]InventorySlot, inventoryCapacity)
	for i := range w.InventorySlots {
		w.InventorySlots[i] = InventorySlot{Item: uint16(100 + i), Amount: 0}
	}
	w.Battle.Enemies[0].Enemy.StealItem, w.Battle.Enemies[0].Enemy.StealCount = 20, 1
	s, _ := battleEvaluate(t, w, 3, ins(0x6a, 0), Input{InputRandom, 11})
	if s.World.Battle.Enemies[0].Enemy.StealCount != 0 || s.World.Inventory[20] != 0 || len(s.World.InventorySlots) != inventoryCapacity {
		t.Fatal("ignored inventory insertion failure changed source steal behavior")
	}
}

func TestBattlePercentageRejectsUndefinedSignedProduct(t *testing.T) {
	rt := battleRuntime()
	rt.Enemies[0].HP = 65535
	r, _ := New(battleTablesSource{runtime: rt}, Options{})
	s := State{World: battleWorld(), Frames: []Frame{{PC: 10, EventID: 3}}}
	before := CloneWorld(s.World)
	if handled, err := r.evaluateBattleOps(&s, &TraceStep{}, ins(0x64, 65535, 20), nil); !handled || err == nil || !strings.Contains(err.Error(), "signed int") {
		t.Fatalf("handled=%t error=%v", handled, err)
	}
	if s.Frames[0].PC != 10 || !reflect.DeepEqual(s.World, before) {
		t.Fatal("undefined source arithmetic changed state")
	}
}
