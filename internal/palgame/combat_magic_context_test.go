package palgame

import (
	"slices"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

func TestMagicBranchUsesActionSideAcrossNestedTriggers(t *testing.T) {
	source, w := numericCombatFixture(t, false)
	const (
		magicObject = 11
		sharedEntry = 20
		branch      = 24
		enemyBranch = 28
		roundHook   = 31
		friendHook  = 34
		invalid     = 39
	)
	source.Runtime.EnemyTeams[0] = [5]uint16{0, 10, 0xffff, 0xffff, 0xffff}
	source.Runtime.Enemies[0].Magic, source.Runtime.Enemies[0].MagicRate = magicObject, 10
	source.Runtime.Magics = []paldata.Magic{{CostMP: 2, BaseDamage: 0xffff}}
	source.Game.Scripts = make([]paldata.Instruction, 40)
	source.Game.Scripts[sharedEntry] = paldata.Instruction{Opcode: 4, Operands: [3]uint16{branch}}
	source.Game.Scripts[branch] = paldata.Instruction{Opcode: 0x68, Operands: [3]uint16{enemyBranch}}
	source.Game.Scripts[branch+1] = paldata.Instruction{Opcode: 0x60}
	source.Game.Scripts[enemyBranch] = paldata.Instruction{Opcode: 0x5f}
	source.Game.Scripts[roundHook] = paldata.Instruction{Opcode: 0x68, Operands: [3]uint16{invalid}}
	source.Game.Scripts[friendHook] = paldata.Instruction{Opcode: 0x68, Operands: [3]uint16{friendHook + 2}}
	source.Game.Scripts[friendHook+1] = paldata.Instruction{Opcode: 0xdead}
	source.Game.Scripts[invalid] = paldata.Instruction{Opcode: 0xdead}
	w.Objects[10] = [7]uint16{2: roundHook, 3: roundHook, 4: roundHook}
	w.Objects[magicObject] = [7]uint16{2: sharedEntry, 6: 2 | 8}
	w.Objects[9] = [7]uint16{2: friendHook}
	w.InBattle, w.PartyRoles = true, []uint16{2, 4}
	first, second := w.Roles[2], w.Roles[4]
	first[31] = 4 // Role 4 reacts when role 2 dies.
	second[3], second[32] = 9, magicObject
	w.Roles[2], w.Roles[4] = first, second
	w.PlayerPoisons[0][1] = palflow.WorldPoison{ID: 8, Script: roundHook}
	party, err := combatParty(w)
	if err != nil {
		t.Fatal(err)
	}
	game, tables, err := combatTables(source, w)
	if err != nil {
		t.Fatal(err)
	}
	random := &combatRandom{state: 2} // First enemy target draw selects party index zero.
	cb := &combatCallbacks{source: source, world: w, random: random, options: palflow.Options{MaxSteps: 100, MaxCallDepth: 32, MaxTextBytes: palflow.DefaultMaxTextBytes}}
	e, err := palcombat.New(palcombat.Config{Game: game, Tables: tables, Party: party, Inventory: map[uint16]uint16{}, Random: random, Callbacks: cb})
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Submit([]palcombat.Action{{Actor: 2, Kind: palcombat.Defend}, {Actor: 4, Kind: palcombat.Defend}})
	if err != nil || !r.Committed || r.Outcome != palcombat.Ongoing || cb.world.Roles[2][9] != 0 || cb.world.Roles[4][9] != 100 || cb.world.BattleEnemyMoving {
		t.Fatalf("enemy cast did not stay scoped to the action: %+v %v", r, err)
	}
	r, err = e.Submit([]palcombat.Action{{Actor: 4, Kind: palcombat.Magic, ObjectID: magicObject, Target: palcombat.Target{Side: palcombat.Enemy}}})
	if err != nil || !r.Committed || r.Outcome != palcombat.Victory || cb.world.Battle.Enemies[0].HP != 0 || cb.world.Roles[4][10] != 19 || cb.world.BattleEnemyMoving {
		t.Fatalf("player cast did not take the non-enemy branch: %+v %v", r, err)
	}
	type jump struct{ pc, next, event uint16 }
	var jumps []jump
	for _, trigger := range cb.triggers {
		for _, step := range trigger.Trace {
			if step.Opcode == 0x68 {
				jumps = append(jumps, jump{step.PC, step.NextPC, step.EventID})
			}
		}
	}
	want := []jump{
		{roundHook, roundHook + 1, 1}, // Initial turn-start.
		{roundHook, roundHook + 1, 1}, // Ready.
		{branch, enemyBranch, 2},      // Enemy spell, invoking role ID 2.
		{friendHook, friendHook + 2, 4},
		{roundHook, roundHook + 1, 4}, // Poison.
		{roundHook, roundHook + 1, 1}, // Next turn-start.
		{branch, branch + 1, 1},       // Player spell, invoking fixed enemy slot 1.
		{roundHook, roundHook + 1, 1}, // Post-battle.
	}
	if !slices.Equal(jumps, want) {
		t.Fatalf("opcode 0068 branches = %v, want %v", jumps, want)
	}
}

func TestBattleEntryClearsPrivateEnemyActionFlag(t *testing.T) {
	source, w := numericCombatFixture(t, false)
	w.BattleEnemyMoving = true
	w.Objects[10] = [7]uint16{2: 14}
	source.Game.Scripts[14] = paldata.Instruction{Opcode: 0x68, Operands: [3]uint16{13}}
	source.Game.Scripts[13] = paldata.Instruction{Opcode: 0xdead}
	result, err := source.RunBattle(w, 0, false, nil, palflow.Options{MaxSteps: 100})
	if err != nil || result.World == nil || result.World.BattleEnemyMoving || !w.BattleEnemyMoving {
		t.Fatalf("battle entry did not isolate the action flag: %+v %v", result, err)
	}
}
