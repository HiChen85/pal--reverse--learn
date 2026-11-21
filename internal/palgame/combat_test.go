package palgame

import (
	"os"
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func numericCombatFixture(t *testing.T, forcedLoss bool) (Source, *palflow.WorldState) {
	t.Helper()
	g, r := fixture()
	g.Objects = make([][7]uint16, 12)
	g.Scripts = make([]paldata.Instruction, 16)
	g.Scripts[1] = paldata.Instruction{Opcode: 7, Operands: [3]uint16{0, 12, 13}}
	g.Scripts[2] = paldata.Instruction{Opcode: 0x1f, Operands: [3]uint16{1, 1, 0}}
	g.Scripts[12] = paldata.Instruction{Opcode: 0x1f, Operands: [3]uint16{2, 1, 0}}
	g.Scripts[14] = paldata.Instruction{Opcode: 0x89, Operands: [3]uint16{1}}
	if forcedLoss {
		g.Objects[10][4] = 14
	}
	r.HasBattleTables = true
	r.Enemies = []paldata.Enemy{{HP: 5, PhysicalResistance: 2, Exp: 2, Cash: 3}}
	if forcedLoss {
		r.Enemies[0].HP = 9999
	}
	r.EnemyTeams = [][5]uint16{{10}}
	r.Battlefields = []paldata.Battlefield{{}}
	for i := range r.LevelUpExp {
		r.LevelUpExp[i] = 1000
	}
	for i := range r.Roles {
		raw := [75]uint16{}
		raw[7], raw[8], raw[9], raw[10], raw[17], raw[20] = 100, 20, 100, 20, 50, 100
		r.Roles[i] = combatRole(uint16(i), raw)
	}
	w, err := InitialSnapshot(g, r, 1)
	if err != nil {
		t.Fatal(err)
	}
	w.BattleMode, w.BattleRandomState = "auto_attack", 1
	return Source{Game: g, Runtime: r, RedactText: true}, w
}

func TestNumericBattleReturnsActualVictoryAndForcedLossBranches(t *testing.T) {
	for _, lose := range []bool{false, true} {
		source, w := numericCombatFixture(t, lose)
		before := palflow.CloneWorld(w)
		runner, err := palflow.New(source, palflow.Options{MaxSteps: 1000})
		if err != nil {
			t.Fatal(err)
		}
		report, err := runner.Trace(palflow.Request{Entry: 1, World: w})
		if err != nil {
			t.Fatal(err)
		}
		if !report.Completed || report.BattleOutcomesStubbed || report.State.CombatWork < 1 || report.Trace[0].Combat == nil || !reflect.DeepEqual(w, before) {
			t.Fatalf("numeric battle was not executed transactionally: %+v", report)
		}
		world := report.State.World
		if lose {
			if world.Inventory[2] != 1 || world.Inventory[1] != 0 || report.Trace[0].Combat.RawResult != 1 || world.Experience[0][0].Exp != 0 {
				t.Fatal("script-forced defeat did not follow loss branch")
			}
		}
		if !lose {
			if world.Inventory[1] != 1 || world.Inventory[2] != 0 || report.Trace[0].Combat.RawResult != 3 || world.Experience[0][0].Exp != 2 || world.Cash != 3 {
				t.Fatal("numeric victory/rewards did not follow success branch")
			}
		}
	}
}

func TestNumericBattleFailureDoesNotCommitRoundsOrConsumeOutcomeStub(t *testing.T) {
	source, w := numericCombatFixture(t, false)
	before := palflow.CloneWorld(w)
	runner, _ := palflow.New(source, palflow.Options{MaxSteps: 1})
	report, err := runner.Trace(palflow.Request{Entry: 1, World: w, Inputs: []palflow.Input{{Kind: palflow.InputBattle, Value: palflow.BattleWin}}})
	if err == nil || report.Completed || report.InputsConsumed != 0 || !reflect.DeepEqual(report.State.World, before) {
		t.Fatal("numeric battle failure committed state")
	}
}

func TestNumericUnhandledDefeatCannotContinueQuest(t *testing.T) {
	source, w := numericCombatFixture(t, true)
	source.Game.Scripts[1].Operands[1] = 0
	runner, _ := palflow.New(source, palflow.Options{MaxSteps: 1000})
	r, err := runner.Trace(palflow.Request{Entry: 1, World: w})
	if err == nil || r.Completed || r.State.World.Inventory[1] != 0 || r.State.World.Inventory[2] != 0 || r.BattleOutcomesStubbed {
		t.Fatal("unhandled defeat advanced the quest", err)
	}
}

func TestLocalImportedNumericCombat(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR for local numeric combat")
	}
	g, err := paldata.Load(dir, paldata.Options{Layout: "dos", Encoding: "big5"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := paldata.LoadRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	w, err := InitialSnapshot(g, r, 1)
	if err != nil {
		t.Fatal(err)
	}
	w.BattleMode, w.BattleRandomState = "auto_attack", 1
	before := palflow.CloneWorld(w)
	result, err := (Source{Game: g, Runtime: r, RedactText: true}).RunBattle(w, 0, false, nil, palflow.Options{MaxSteps: 10000})
	if err != nil {
		t.Fatal(err)
	}
	if result.RawResult != 3 || len(result.Reports) == 0 || result.World == nil || result.World.InBattle || result.World.Experience[0][0].Exp != 2 || result.World.Cash != 2 || !reflect.DeepEqual(w, before) {
		t.Fatal("imported combat/rewards mismatch")
	}
	t.Logf("numeric imported team0: rounds=%d work=%d HP=%d XP=%d cash=%d rawResult=%d", len(result.Reports), result.Work, result.World.Roles[0][9], result.World.Experience[0][0].Exp, result.World.Cash, result.RawResult)
}
