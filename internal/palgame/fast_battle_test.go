package palgame

import (
	"os"
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func TestFastVerifyUsesRealRoundsRewardsAndEndCallback(t *testing.T) {
	source, w := numericCombatFixture(t, false)
	source.Runtime.Enemies[0].HP, source.Runtime.Enemies[0].Raw[11] = 350, 350
	source.Game.Objects[10][3] = 8
	w.Objects[10] = source.Game.Objects[10]
	source.Game.Scripts[8] = paldata.Instruction{Opcode: 0x1f, Operands: [3]uint16{9, 2}}
	raw := w.Roles[0]
	raw[7], raw[9] = 1000, 1000
	w.Roles[0] = raw
	originalEnemies := append([]paldata.Enemy(nil), source.Runtime.Enemies...)
	originalWorld := palflow.CloneWorld(w)
	full, err := source.RunBattle(w, 0, false, nil, palflow.Options{MaxSteps: 10000})
	if err != nil {
		t.Fatal(err)
	}
	w.BattleMode = "fast_verify"
	fast, err := source.RunBattle(w, 0, false, nil, palflow.Options{MaxSteps: 10000})
	if err != nil {
		t.Fatal(err)
	}
	if fast.Policy != "fast_verify" || !reflect.DeepEqual(fast.Simplifications, []string{fastVerifyDescription}) || full.Policy != "auto_attack" || len(full.Simplifications) != 0 {
		t.Fatal("execution lost the declared verification policy")
	}
	if fast.RawResult != 3 || len(fast.Reports) != 1 || len(full.Reports) <= len(fast.Reports) || fast.Work >= full.Work || !fast.Reports[0].Committed {
		t.Fatalf("verification did not accelerate an actual numeric victory: full rounds=%d work=%d fast rounds=%d work=%d", len(full.Reports), full.Work, len(fast.Reports), fast.Work)
	}
	if fast.World.Experience[0][0].Exp != full.World.Experience[0][0].Exp || fast.World.Cash != full.World.Cash || fast.World.Cash != 3 || fast.World.Inventory[9] != 2 || !reflect.DeepEqual(fast.World.Inventory, full.World.Inventory) {
		t.Fatal("victory reward or enemy battle-end callback was bypassed")
	}
	if len(fast.Triggers) != 1 || len(fast.Reports[0].Draws) == 0 || fast.World.Battle == nil || fast.World.Battle.Enemies[0].Enemy.HP != 1 {
		t.Fatal("verification manufactured a victory without numeric or callback evidence")
	}
	for _, attribute := range []int{6, 7, 8, 17, 18, 19, 20, 21, 22} {
		if fast.World.Roles[0][attribute] != originalWorld.Roles[0][attribute] {
			t.Fatalf("verification changed permanent party base attribute %d", attribute)
		}
	}
	w.BattleMode = originalWorld.BattleMode
	if !reflect.DeepEqual(w, originalWorld) || !reflect.DeepEqual(source.Runtime.Enemies, originalEnemies) {
		t.Fatal("verification mutated the caller's world or imported enemy table")
	}
}

func TestFastVerifyHonorsDefeatAndUnimplementedCallbacks(t *testing.T) {
	for _, entryHook := range []int{2, 4} {
		for _, unsupported := range []bool{false, true} {
			source, w := numericCombatFixture(t, false)
			w.BattleMode = "fast_verify"
			source.Runtime.Enemies[0].Dexterity = 10000 // Ready runs before the party.
			source.Game.Objects[10][entryHook] = 14
			w.Objects[10] = source.Game.Objects[10]
			if unsupported {
				source.Game.Scripts[14] = paldata.Instruction{Opcode: 0xfffe}
			}
			before := palflow.CloneWorld(w)
			runner, err := palflow.New(source, palflow.Options{MaxSteps: 1000})
			if err != nil {
				t.Fatal(err)
			}
			report, err := runner.Trace(palflow.Request{Entry: 1, World: w})
			if unsupported {
				if err == nil || report.Completed || !reflect.DeepEqual(report.State.World, before) {
					t.Fatal("verification bypassed or committed an unsupported callback", err)
				}
			} else if err != nil || !report.Completed || report.BattleOutcomesStubbed || report.Trace[0].Combat.Policy != "fast_verify" || report.Trace[0].Combat.RawResult != 1 || report.State.World.Inventory[2] != 1 || report.State.World.Inventory[1] != 0 || report.State.World.Cash != 0 {
				t.Fatal("verification bypassed the actual defeat script or story loss branch", err)
			}
			if !reflect.DeepEqual(w, before) {
				t.Fatal("failed or successful verification mutated its caller's input")
			}
		}
	}
}

func TestFastVerifyCapsSummonAndMorphTemplates(t *testing.T) {
	for _, opcode := range []uint16{0x9e, 0x9f} {
		source, w := numericCombatFixture(t, false)
		w.BattleMode = "fast_verify"
		source.Runtime.Enemies = append(source.Runtime.Enemies, paldata.Enemy{ID: 1, HP: 999, Exp: 4, Cash: 5})
		source.Runtime.EnemyTeams[0] = [5]uint16{10, 0, 0xffff, 0xffff, 0xffff}
		source.Game.Objects[10][2] = 5
		source.Game.Objects[11] = [7]uint16{0: 1}
		w.Objects[10], w.Objects[11] = source.Game.Objects[10], source.Game.Objects[11]
		source.Game.Scripts[5] = paldata.Instruction{Opcode: opcode, Operands: [3]uint16{11, 1}}
		source.Game.Scripts[6] = paldata.Instruction{Opcode: 1}
		before := append([]paldata.Enemy(nil), source.Runtime.Enemies...)
		result, err := source.RunBattle(w, 0, false, nil, palflow.Options{MaxSteps: 1000})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, enemy := range result.World.Battle.Enemies {
			if enemy.ObjectID == 11 {
				found = true
				if enemy.Enemy.HP != 1 || enemy.Enemy.Raw[11] != 1 || !enemy.Rewarded {
					t.Fatalf("opcode %04x did not use capped private template: %+v", opcode, enemy)
				}
			}
		}
		if !found || result.RawResult != 3 || !reflect.DeepEqual(before, source.Runtime.Enemies) {
			t.Fatalf("opcode %04x did not execute a real capped topology mutation", opcode)
		}
	}
}

func TestFastVerifyKeepsInvalidEnemyHPInvalidAndBudgetAtomic(t *testing.T) {
	for _, invalidHP := range []bool{false, true} {
		source, w := numericCombatFixture(t, false)
		w.BattleMode = "fast_verify"
		limit := 1
		if invalidHP {
			source.Runtime.Enemies[0].HP = 0
			limit = 1000
		}
		before := palflow.CloneWorld(w)
		runner, _ := palflow.New(source, palflow.Options{MaxSteps: limit})
		report, err := runner.Trace(palflow.Request{Entry: 1, World: w, Inputs: []palflow.Input{{Kind: palflow.InputBattle, Value: palflow.BattleWin}}})
		if err == nil || report.Completed || report.InputsConsumed != 0 || report.BattleOutcomesStubbed || !reflect.DeepEqual(report.State.World, before) {
			t.Fatal("verification replaced invalid resources or a budget failure with supplied victory")
		}
	}
}

func TestFastVerifyDoesNotGuaranteeVictoryOrCommitPartialBattle(t *testing.T) {
	source, w := numericCombatFixture(t, false)
	w.BattleMode = "fast_verify"
	source.Runtime.Enemies[0].Dexterity = 10000
	source.Runtime.Enemies[0].Attack = 10000
	w.PlayerStatus[0][palcombat.Sleep] = 1 // Disabled players cannot auto-evade.
	result, err := source.RunBattle(w, 0, false, nil, palflow.Options{MaxSteps: 1000})
	if err != nil || result.RawResult != 1 || result.World.Battle.Outcome != palcombat.Defeat || result.World.Cash != 0 || result.World.Experience[0][0].Exp != 0 || len(result.Reports[0].Events) == 0 {
		t.Fatal("verification skipped an actual enemy attack/defeat", err)
	}

	source, w = numericCombatFixture(t, false)
	w.BattleMode = "fast_verify"
	source.Runtime.EnemyTeams[0] = [5]uint16{10, 10, 0xffff, 0xffff, 0xffff}
	before := palflow.CloneWorld(w)
	result, err = source.RunBattle(w, 0, false, nil, palflow.Options{MaxSteps: 1})
	if err == nil || result.World != nil || len(result.Reports) != 1 || !result.Reports[0].Committed || result.Reports[0].Outcome != palcombat.Ongoing || !reflect.DeepEqual(w, before) {
		t.Fatal("verification committed a partial fight after the shared budget was exhausted", err)
	}
}

func TestFastVerifyPolicyPersistsAcrossSessionSave(t *testing.T) {
	source, _ := numericCombatFixture(t, false)
	s, err := NewSession(source.Game, source.Runtime, SessionOptions{BattleMode: "fast_verify", AssumeSceneEntered: true, MaxSteps: 1000})
	if err != nil {
		t.Fatal(err)
	}
	bindSaveFixture(t, s)
	next := saveRoundTrip(t, s)
	if next.Snapshot().World.BattleMode != "fast_verify" {
		t.Fatal("save lost the explicit verification policy")
	}
	want, err := s.Trigger(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := next.Trigger(1, nil)
	if err != nil || !reflect.DeepEqual(want, got) || !reflect.DeepEqual(s.Snapshot(), next.Snapshot()) || got.Trace[0].Combat.Policy != "fast_verify" || got.Trace[0].Combat.RawResult != 3 {
		t.Fatal("restored verification policy did not reproduce its numeric battle", err)
	}
	if got.State.World.Battle.Outcome != palcombat.Victory {
		t.Fatal("restored verification fight did not use the actual combat engine")
	}
}

func TestLocalImportedFastVerifyCombat(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR for imported fast verification combat")
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
	source := Source{Game: g, Runtime: r, RedactText: true}
	w.BattleMode, w.BattleRandomState = "auto_attack", 1
	full, err := source.RunBattle(w, 0, false, nil, palflow.Options{MaxSteps: 10000})
	if err != nil {
		t.Fatal(err)
	}
	w.BattleMode = "fast_verify"
	before := append([]paldata.Enemy(nil), r.Enemies...)
	fast, err := source.RunBattle(w, 0, false, nil, palflow.Options{MaxSteps: 10000})
	if err != nil || fast.RawResult != 3 || fast.Policy != "fast_verify" || len(fast.Reports) >= len(full.Reports) || fast.World.Cash != full.World.Cash || fast.World.Experience[0][0].Exp != full.World.Experience[0][0].Exp || !reflect.DeepEqual(before, r.Enemies) {
		t.Fatal("imported verification battle failed or altered imported reward/table semantics", err)
	}
	t.Logf("team0 full rounds=%d work=%d; fast rounds=%d work=%d HP=%d XP=%d cash=%d", len(full.Reports), full.Work, len(fast.Reports), fast.Work, fast.World.Roles[0][9], fast.World.Experience[0][0].Exp, fast.World.Cash)
}
