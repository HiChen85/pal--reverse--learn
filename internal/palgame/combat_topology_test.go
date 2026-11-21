package palgame

import (
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func TestNumericVMTopologyAndMoneyChangesAreTransactional(t *testing.T) {
	for _, opcode := range []uint16{0x9c, 0x9e, 0x9f, 0x88} {
		for _, fail := range []bool{false, true} {
			source, w := numericCombatFixture(t, false)
			source.Runtime.Enemies = []paldata.Enemy{{HP: 101, PhysicalResistance: 2}, {ID: 1, HP: 400, Attack: 1}}
			source.Runtime.Magics = make([]paldata.Magic, 2)
			source.Runtime.EnemyTeams[0] = [5]uint16{10, 0, 0xffff, 0xffff, 0xffff}
			source.Game.Objects[10][4] = 1
			source.Game.Objects[11] = [7]uint16{0: 1, 2: 5, 3: 6, 4: 7}
			w.Objects[10], w.Objects[11] = source.Game.Objects[10], source.Game.Objects[11]
			source.Game.Scripts[1] = paldata.Instruction{Opcode: opcode}
			switch opcode {
			case 0x9e:
				source.Game.Scripts[1].Operands = [3]uint16{11, 1, 10}
			case 0x9f, 0x88:
				source.Game.Scripts[1].Operands[0] = 11
			}
			// Stop-and-advance gives the invoker continuation 3. A division
			// clone must keep the pre-return Ready=1; a summon imports Ready=7.
			source.Game.Scripts[2] = paldata.Instruction{Opcode: 1}
			w.InBattle, w.Cash = true, 5001
			party, err := combatParty(w)
			if err != nil {
				t.Fatal(err)
			}
			random := &combatRandom{state: 1}
			limit := 1000
			if fail {
				limit = 1
			}
			cb := &combatCallbacks{source: source, world: palflow.CloneWorld(w), random: random, options: palflow.Options{MaxSteps: limit, MaxCallDepth: 32, MaxTextBytes: palflow.DefaultMaxTextBytes}}
			e, err := palcombat.New(palcombat.Config{Game: source.Game, Tables: source.Runtime, TeamID: 0, Party: party, Random: random, Callbacks: cb, Cash: 5001})
			if err != nil {
				t.Fatal(err)
			}
			before, worldBefore := e.Snapshot(), palflow.CloneWorld(cb.world)
			report, err := e.Submit([]palcombat.Action{{Actor: 0, Kind: palcombat.Defend}})
			if fail {
				if err == nil || report.Committed || !reflect.DeepEqual(e.Snapshot(), before) || !reflect.DeepEqual(cb.world, worldBefore) || cb.work != 1 {
					t.Fatalf("opcode %04x failed VM leaked world: %+v %v", opcode, report, err)
				}
				continue
			}
			if err != nil || !report.Committed {
				t.Fatalf("opcode %04x: %+v %v", opcode, report, err)
			}
			after := e.Snapshot()
			wantScripts := 2
			if opcode == 0x9e {
				wantScripts++ // The new slot's turn-start hook runs at round end.
			}
			if after.Enemies[0].Ready != 3 || cb.world.Battle.Enemies[0].Ready != 3 || len(report.Scripts) != wantScripts || report.Scripts[0].Opcode != opcode {
				t.Fatalf("opcode %04x wrong continuation/trace %+v", opcode, after)
			}
			switch opcode {
			case 0x9c:
				if len(after.Enemies) != 2 || after.Enemies[0].HP != 51 || after.Enemies[1].HP != 51 || after.Enemies[1].Ready != 1 {
					t.Fatal("VM division lost old/new continuation")
				}
			case 0x9e:
				if len(after.Enemies) != 2 || after.Enemies[1].HP != 400 || after.Enemies[1].ObjectID != 11 || after.Enemies[1].Ready != 7 {
					t.Fatal("VM summon lost imported initialization")
				}
			case 0x9f:
				if after.Enemies[0].HP != 101 || after.Enemies[0].ObjectID != 11 || after.Enemies[0].Enemy.ID != 1 || after.Enemies[0].TurnStart != 0 {
					t.Fatal("VM transform lost old HP/scripts")
				}
			case 0x88:
				if after.Cash != 1 || after.MagicBaseDamage[1] != 2000 || cb.world.Cash != 1 || cb.world.MagicBaseDamage[1] != 2000 || source.Runtime.Magics[1].BaseDamage != 0 {
					t.Fatal("VM money magic did not publish mapped damage atomically")
				}
			}
		}
	}
}
