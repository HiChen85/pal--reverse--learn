package palgame

import (
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

// The bridge rebuilds equipment and chooses an ordinary Enemy target. The
// numeric engine must promote it to all-target damage, rather than merely
// accepting an all-capable role whose pre-battle script ends the battle.
func TestNumericAutomaticAttacksExecuteEquipmentAllTargetSweeps(t *testing.T) {
	source, w := numericCombatFixture(t, false)
	source.Runtime.EnemyTeams[0] = [5]uint16{10, 10, 10, 0xffff, 0xffff}
	source.Game.Objects[1][3] = 4
	w.Objects[1] = source.Game.Objects[1]
	source.Game.Scripts[4] = paldata.Instruction{Opcode: 0x17, Operands: [3]uint16{0xb, 4, 1}}
	source.Game.Scripts[5] = paldata.Instruction{}
	raw := w.Roles[0]
	raw[11] = 1
	w.Roles[0] = raw
	w.PlayerStatus[0][palcombat.DualAttack] = 1
	before := palflow.CloneWorld(w)
	result, err := source.RunBattle(w, 0, false, nil, palflow.Options{MaxSteps: 1000})
	if err != nil || result.RawResult != 3 || result.World == nil || len(result.Reports) != 1 || !reflect.DeepEqual(w, before) {
		t.Fatalf("automatic equipment all-target combat failed: %+v %v", result, err)
	}
	sweeps := 0
	for _, draw := range result.Reports[0].Draws {
		if draw.Reason == "attack_all.critical" {
			sweeps++
		}
		if draw.Reason == "attack.damage_bonus" || draw.Reason == "attack.role0_bonus" || draw.Reason == "attack.variance" {
			t.Fatal("automatic bridge fell back to single-target attack", draw)
		}
	}
	if sweeps != 2 || result.World.Cash != 9 || result.World.Experience[0][0].Exp != 6 || result.World.EquipmentEffects[0][4][0] != 1 {
		t.Fatal("actual sweeps, rebuilt equipment or rewards missing", sweeps, result.World.Cash, result.World.Experience[0][0].Exp)
	}
}
