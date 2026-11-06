package palgame

import (
	"fmt"
	"maps"
	"slices"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

const maxAutomaticCombatRounds = 1000

func (s Source) RunBattleWithContext(world *palflow.WorldState, team uint16, boss bool, inputs []palflow.Input, options palflow.Options, context palflow.CombatContext) (palflow.CombatExecution, error) {
	context.IdleFrames = maps.Clone(context.IdleFrames)
	s.battleContext = &context
	return s.RunBattle(world, team, boss, inputs, options)
}

// RunBattle executes numeric rounds using an explicitly selected attack policy.
// Even committed rounds live only in this invocation's private proposal; any
// later failure returns no World and cannot update a live Session checkpoint.
func (s Source) RunBattle(world *palflow.WorldState, team uint16, boss bool, inputs []palflow.Input, options palflow.Options) (palflow.CombatExecution, error) {
	result := palflow.CombatExecution{Result: -1, RawResult: 1000}
	if world == nil || world.BattleMode != "auto_attack" {
		return result, fmt.Errorf("numeric battle requires the explicit auto_attack policy")
	}
	if world.InBattle {
		return result, &palcombat.UnsupportedError{Feature: "nested battle"}
	}
	if options.MaxSteps == 0 {
		options.MaxSteps = 10000
	}
	if options.MaxCallDepth == 0 {
		options.MaxCallDepth = 32
	}
	if options.MaxTextBytes == 0 {
		options.MaxTextBytes = palflow.DefaultMaxTextBytes
	}
	if _, err := palflow.New(s, options); err != nil {
		return result, err
	}
	w := palflow.CloneWorld(world)
	context := palflow.CombatContext{ScriptSuccess: true}
	if s.battleContext != nil {
		context = *s.battleContext
		context.IdleFrames = maps.Clone(context.IdleFrames)
	}
	for _, role := range w.PartyRoles {
		raw := w.Roles[role]
		if raw[9] == 0 {
			raw[9] = 1
			w.PlayerStatus[role][4] = 0
			w.Roles[role] = raw
		}
	}
	initializer, err := palflow.New(s, options)
	if err != nil {
		return result, err
	}
	initial, _, err := initializer.InitializeEquipment(w)
	if err != nil {
		return result, err
	}
	hasEquipment := false
	for _, raw := range w.Roles {
		for _, item := range raw[11:17] {
			hasEquipment = hasEquipment || item != 0
		}
	}
	w = initial.World
	if hasEquipment {
		context.LastEventID, context.ScriptSuccess = initial.LastEventID, initial.ScriptSuccess
	}
	w.InBattle, w.BattleBoss, w.BattleResult, w.BattleResultSet = true, boss, 1000, false
	g, tables, err := combatTables(s, w)
	if err != nil {
		return result, err
	}
	party, err := combatParty(w)
	if err != nil {
		return result, err
	}
	inventory, err := combatInventory(w)
	if err != nil {
		return result, err
	}
	random := &combatRandom{state: w.BattleRandomState}
	callbacks := &combatCallbacks{source: s, world: w, random: random, options: options, inputs: slices.Clone(inputs), context: context, work: initial.PlayerSubsteps}
	engine, err := palcombat.New(palcombat.Config{Tables: tables, Game: g, Party: party, TeamID: team, Battlefield: w.Battlefield, Boss: boss, HidingTime: 0, Cash: w.Cash, Inventory: inventory, Random: random, Callbacks: callbacks})
	if err != nil {
		return result, err
	}
	if err := snapshotToCombatWorld(callbacks.world, engine.Snapshot()); err != nil {
		return result, err
	}

	for round := 0; round < maxAutomaticCombatRounds; round++ {
		if callbacks.work >= options.MaxSteps {
			err = fmt.Errorf("combat exhausted the shared modeled instruction budget")
			break
		}
		before := engine.Snapshot()
		actions := automaticCombatAttacks(before)
		callbacks.work++ // one bounded numeric round, plus all callback VM work
		report, submitErr := engine.Submit(actions)
		result.Reports = append(result.Reports, report)
		if submitErr != nil {
			err = submitErr
			break
		}
		after := engine.Snapshot()
		if after.Outcome == palcombat.Ongoing {
			continue
		}
		switch after.Outcome {
		case palcombat.Terminated:
			result.Result, result.RawResult = palflow.BattleWin, 0
		case palcombat.Victory:
			result.Result, result.RawResult = palflow.BattleWin, 3
		case palcombat.Defeat:
			result.Result, result.RawResult = palflow.BattleLost, 1
		case palcombat.Escaped:
			result.Result, result.RawResult = palflow.BattleFled, 0xffff
		default:
			return result, &palcombat.UnsupportedError{Feature: "unrecognized battle outcome " + string(after.Outcome)}
		}
		callbacks.world.InBattle = false
		for role := range 6 {
			for status, value := range callbacks.world.PlayerStatus[role] {
				if value <= 999 {
					callbacks.world.PlayerStatus[role][status] = 0
				}
			}
			for attr := range 75 {
				callbacks.world.EquipmentEffects[6][attr][role] = 0
			}
		}
		for i := range callbacks.world.InventorySlots {
			callbacks.world.InventorySlots[i].InUse = 0
		}

		callbacks.world.BattleResult, callbacks.world.BattleResultSet = result.RawResult, false
		result.World = palflow.CloneWorld(callbacks.world)
		result.Triggers, result.InputsConsumed, result.Work = callbacks.triggers, callbacks.inputIndex, callbacks.work
		ctx := callbacks.context
		ctx.IdleFrames = maps.Clone(ctx.IdleFrames)
		result.Context = &ctx
		return result, nil
	}
	if err == nil {
		err = fmt.Errorf("combat reached the automatic policy limit of %d rounds", maxAutomaticCombatRounds)
	}
	result.Triggers, result.InputsConsumed, result.Work = callbacks.triggers, callbacks.inputIndex, callbacks.work
	return result, err
}

func automaticCombatAttacks(snapshot palcombat.Snapshot) []palcombat.Action {
	target := 0
	for i, enemy := range snapshot.Enemies {
		if enemy.HP > 0 {
			target = i
			break
		}
	}
	actions := make([]palcombat.Action, 0, len(snapshot.Party))
	for _, player := range snapshot.Party {
		if player.Role.HP > 0 && player.Status[palcombat.Sleep] == 0 && player.Status[palcombat.Paralyzed] == 0 {
			actions = append(actions, palcombat.Action{Actor: player.Role.ID, Kind: palcombat.Attack, Target: palcombat.Target{Side: palcombat.Enemy, Index: target}})
		}
	}
	return actions
}

// combatRandom is a tool replay generator. Its values satisfy the numeric
// engine's explicit intervals; this does not reproduce the original PAL RNG or
// claim its distribution. The state belongs only to the proposed private world.
type combatRandom struct{ state uint64 }

func (r *combatRandom) Intn(n int) (int, error) {
	if n < 1 {
		return 0, fmt.Errorf("combat replay random bound must be positive")
	}
	r.state += 0x9e3779b97f4a7c15
	x := r.state
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	x ^= x >> 31
	return int(x % uint64(n)), nil
}

// Inclusive script requests use the round's recording random wrapper, so draws
// made by the VM remain in the same combat Report as initiative and damage.
type combatScriptRandom struct{ random palcombat.RandomSource }

func (r combatScriptRandom) RandomLong(minimum, maximum int) (int, error) {
	if minimum > maximum || minimum < -32768 || maximum > 65535 {
		return 0, fmt.Errorf("unsupported combat script random interval [%d,%d]", minimum, maximum)
	}
	if minimum == maximum {
		return minimum, nil
	}
	v, err := r.random.Intn(maximum - minimum + 1)
	return minimum + v, err
}

func combatRole(id uint16, raw [paldata.RoleWords]uint16) paldata.Role {
	r := paldata.Role{ID: id, Raw: raw, Avatar: raw[0], BattleSprite: raw[1], Sprite: raw[2], NameIndex: raw[3], AttackAll: raw[4], Level: raw[6], MaxHP: raw[7], MaxMP: raw[8], HP: raw[9], MP: raw[10], Attack: raw[17], Magic: raw[18], Defense: raw[19], Dexterity: raw[20], FleeRate: raw[21], PoisonResistance: raw[22], CoveredBy: raw[31], WalkFrames: raw[64], CooperativeMagic: raw[65], DeathSound: raw[68], AttackSound: raw[69], WeaponSound: raw[70], CriticalSound: raw[71], MagicSound: raw[72], CoverSound: raw[73], DyingSound: raw[74]}
	copy(r.Equipment[:], raw[11:17])
	copy(r.ElementalResistance[:], raw[23:28])
	copy(r.Magics[:], raw[32:64])
	return r
}

func combatRoleRaw(r paldata.Role) [paldata.RoleWords]uint16 {
	raw := r.Raw
	raw[0], raw[1], raw[2], raw[3], raw[4] = r.Avatar, r.BattleSprite, r.Sprite, r.NameIndex, r.AttackAll
	raw[6], raw[7], raw[8], raw[9], raw[10] = r.Level, r.MaxHP, r.MaxMP, r.HP, r.MP
	copy(raw[11:17], r.Equipment[:])
	raw[17], raw[18], raw[19], raw[20], raw[21], raw[22] = r.Attack, r.Magic, r.Defense, r.Dexterity, r.FleeRate, r.PoisonResistance
	copy(raw[23:28], r.ElementalResistance[:])
	raw[31] = r.CoveredBy
	copy(raw[32:64], r.Magics[:])
	raw[64], raw[65] = r.WalkFrames, r.CooperativeMagic
	raw[68], raw[69], raw[70], raw[71], raw[72], raw[73], raw[74] = r.DeathSound, r.AttackSound, r.WeaponSound, r.CriticalSound, r.MagicSound, r.CoverSound, r.DyingSound
	return raw
}

func combatExperience(e palflow.Experience) paldata.Experience {
	return paldata.Experience{Exp: e.Exp, Reserved: e.Reserved, Level: e.Level, Count: e.Count}
}

func worldExperience(e paldata.Experience) palflow.Experience {
	return palflow.Experience{Exp: e.Exp, Reserved: e.Reserved, Level: e.Level, Count: e.Count}
}

func combatParty(w *palflow.WorldState) ([]palcombat.Player, error) {
	if len(w.PartyRoles) < 1 || len(w.PartyRoles) > 3 {
		return nil, fmt.Errorf("classic combat requires 1..3 party members")
	}
	if w.PlayerStatusMode != "classic" {
		return nil, fmt.Errorf("combat requires explicit classic player status semantics")
	}
	party := make([]palcombat.Player, 0, len(w.PartyRoles))
	for member, id := range w.PartyRoles {
		raw, ok := w.Roles[id]
		if !ok || id >= paldata.RoleCount {
			return nil, fmt.Errorf("combat role %d attributes are absent or invalid", id)
		}
		p := palcombat.Player{Role: combatRole(id, raw), Status: w.PlayerStatus[id], Experience: uint32(w.Experience[id][0].Exp), EquipmentEffects: &palcombat.Effects{}}
		for slot := range p.EquipmentEffects {
			for attribute := range p.EquipmentEffects[slot] {
				p.EquipmentEffects[slot][attribute] = w.EquipmentEffects[slot][attribute][id]
			}
		}
		tracks := w.Experience[id]
		p.HiddenExperience = &palcombat.HiddenExperience{Health: combatExperience(tracks[1]), Magic: combatExperience(tracks[2]), Attack: combatExperience(tracks[3]), MagicPower: combatExperience(tracks[4]), Defense: combatExperience(tracks[5]), Dexterity: combatExperience(tracks[6]), Flee: combatExperience(tracks[7])}
		for slot := range p.Poisons {
			poison := w.PlayerPoisons[slot][member]
			p.Poisons[slot] = palcombat.Poison{ID: poison.ID, Script: poison.Script}
		}
		party = append(party, p)
	}
	return party, nil
}

func combatInventory(w *palflow.WorldState) (map[uint16]uint16, error) {
	if w.Inventory == nil && w.InventorySlots == nil {
		return nil, fmt.Errorf("combat requires explicit inventory counts or ordered slots")
	}
	counts := map[uint16]uint16{}
	if w.InventorySlots != nil {
		if len(w.InventorySlots) > 256 {
			return nil, fmt.Errorf("combat inventory has more than 256 slots")
		}
		for i, slot := range w.InventorySlots {
			if slot.Amount > 99 || slot.InUse > 99 {
				return nil, fmt.Errorf("combat inventory slot %d amount or reservation exceeds 99", i)
			}
			if slot.Item == 0 {
				break
			}
			if _, exists := counts[slot.Item]; !exists {
				counts[slot.Item] = slot.Amount
			}
		}
	}
	if w.Inventory != nil {
		if len(w.Inventory) > 256 {
			return nil, fmt.Errorf("combat inventory has more than 256 entries")
		}
		if w.InventorySlots != nil && len(counts) != len(w.Inventory) {
			return nil, fmt.Errorf("combat inventory count map conflicts with ordered slots")
		}
		for id, amount := range w.Inventory {
			if id == 0 || amount < 0 || amount > 99 {
				return nil, fmt.Errorf("combat inventory item %d has invalid amount %d", id, amount)
			}
			if w.InventorySlots != nil {
				if n, exists := counts[id]; !exists || int(n) != amount {
					return nil, fmt.Errorf("combat inventory item %d differs from ordered slots", id)
				}
			}
			counts[id] = uint16(amount)
		}
	}
	return counts, nil
}

func writeCombatInventory(w *palflow.WorldState, counts map[uint16]uint16) error {
	if len(counts) > 256 {
		return fmt.Errorf("combat inventory exceeds 256 entries")
	}
	for id, amount := range counts {
		if id == 0 || amount > 99 {
			return fmt.Errorf("combat inventory item %d has invalid amount %d", id, amount)
		}
	}
	if w.InventorySlots != nil {
		slots := make([]palflow.InventorySlot, 256)
		copy(slots, w.InventorySlots)
		remaining := maps.Clone(counts)
		end := 0
		for end < len(slots) && slots[end].Item != 0 {
			id := slots[end].Item
			if amount, ok := remaining[id]; ok {
				slots[end].Amount = amount
				delete(remaining, id)
			} else if _, exists := counts[id]; !exists {
				slots[end].Amount = 0
			}
			end++
		}
		ids := make([]uint16, 0, len(remaining))
		for id := range remaining {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		if len(ids) > len(slots)-end {
			return fmt.Errorf("combat inventory cannot retain ordered slots and append %d new items", len(ids))
		}
		for _, id := range ids {
			slots[end] = palflow.InventorySlot{Item: id, Amount: remaining[id]}
			end++
		}
		w.InventorySlots = slots
		w.Inventory = map[uint16]int{}
		for _, slot := range slots[:end] {
			if _, exists := w.Inventory[slot.Item]; !exists {
				w.Inventory[slot.Item] = int(slot.Amount)
			}
		}
	} else {
		w.Inventory = make(map[uint16]int, len(counts))
		for id, amount := range counts {
			w.Inventory[id] = int(amount)
		}
	}
	return nil
}

// These copies honor mutations already made by earlier story instructions.
// Runtime imports and the caller's Game remain immutable throughout a fight.
func combatTables(s Source, w *palflow.WorldState) (*paldata.Game, *paldata.RuntimeTables, error) {
	if s.Game == nil || s.Runtime == nil {
		return nil, nil, fmt.Errorf("combat requires imported game and runtime DATA tables")
	}
	g := *s.Game
	g.Objects = slices.Clone(s.Game.Objects)
	for id, object := range w.Objects {
		if int(id) >= len(g.Objects) {
			return nil, nil, fmt.Errorf("combat mutable object %d is outside imported table", id)
		}
		g.Objects[id] = object
	}
	r := *s.Runtime
	r.Magics = slices.Clone(s.Runtime.Magics)
	for id, damage := range w.MagicBaseDamage {
		if int(id) >= len(r.Magics) {
			return nil, nil, fmt.Errorf("combat mutable magic %d is outside imported table", id)
		}
		r.Magics[id].BaseDamage, r.Magics[id].Raw[13] = damage, damage
	}
	return &g, &r, nil
}

type combatCallbacks struct {
	context    palflow.CombatContext
	source     Source
	world      *palflow.WorldState
	random     *combatRandom
	options    palflow.Options
	inputs     []palflow.Input
	inputIndex int
	work       int
	textBytes  int
	triggers   []palflow.Report
}

type combatTransaction struct {
	context    palflow.CombatContext
	owner      *combatCallbacks
	world      *palflow.WorldState
	inputIndex int
	textBytes  int
	closed     bool
}

func (c *combatCallbacks) Begin(snapshot palcombat.Snapshot) (palcombat.ScriptTransaction, error) {
	ctx := c.context
	ctx.IdleFrames = maps.Clone(ctx.IdleFrames)
	tx := &combatTransaction{context: ctx, owner: c, world: palflow.CloneWorld(c.world), inputIndex: c.inputIndex, textBytes: c.textBytes}
	if err := snapshotToCombatWorld(tx.world, snapshot); err != nil {
		return nil, err
	}
	return tx, nil
}

func (tx *combatTransaction) Run(request palcombat.ScriptRequest, snapshot *palcombat.Snapshot, random palcombat.RandomSource) (palcombat.ScriptResult, error) {
	result := palcombat.ScriptResult{}
	if tx.closed || snapshot == nil {
		return result, fmt.Errorf("combat script transaction is closed or has no snapshot")
	}
	if err := snapshotToCombatWorld(tx.world, *snapshot); err != nil {
		return result, err
	}
	remaining := tx.owner.options.MaxSteps - tx.owner.work
	if remaining < 1 {
		return result, fmt.Errorf("combat script %04x exhausted the shared instruction budget", request.Entry)
	}
	options := tx.owner.options
	options.MaxSteps = remaining
	options.MaxTextBytes -= tx.textBytes
	if options.MaxTextBytes < 1 {
		return result, fmt.Errorf("combat scripts exhausted the shared dialogue byte budget")
	}
	source := tx.owner.source
	source.Random = combatScriptRandom{random: random}
	runner, err := palflow.New(source, options)
	if err != nil {
		return result, err
	}
	before := palflow.CloneWorld(tx.world)
	report, err := runner.Trace(palflow.Request{World: tx.world, Entry: request.Entry, EventID: request.Invoker, LastEventID: tx.context.LastEventID, IdleFrames: maps.Clone(tx.context.IdleFrames), Inputs: tx.owner.inputs[tx.inputIndex:]})
	tx.owner.work += report.State.ExecutedSteps + report.State.AutoSubsteps + report.State.PlayerSubsteps + report.State.WorldTicks + report.State.CombatWork
	evidence := report
	evidence.State.World = nil
	tx.owner.triggers = append(tx.owner.triggers, evidence)
	appendCombatScriptTrace(&result.Trace, request.Entry, report.Trace, report.State.ScriptSuccess)
	if err != nil {
		return result, fmt.Errorf("combat %s object %04x script %04x: %w", request.Hook, request.ObjectID, request.Entry, err)
	}
	if !report.Completed || report.State.World == nil {
		return result, fmt.Errorf("combat callback did not complete with a world snapshot")
	}
	after := report.State.World
	for id, damage := range after.MagicBaseDamage {
		if old, ok := before.MagicBaseDamage[id]; !ok || old != damage {
			if result.MagicBaseDamage == nil {
				result.MagicBaseDamage = map[uint16]uint16{}
			}
			result.MagicBaseDamage[id] = damage
		}
	}
	for id, object := range after.Objects {
		if old, ok := before.Objects[id]; !ok || old != object {
			if result.ObjectChanges == nil {
				result.ObjectChanges = map[uint16][7]uint16{}
			}
			result.ObjectChanges[id] = object
		}
	}
	if after.BattleResultSet {
		var outcome palcombat.Outcome
		switch after.BattleResult {
		case 0:
			outcome = palcombat.Terminated
		case 1:
			outcome = palcombat.Defeat
		case 3:
			outcome = palcombat.Victory
		case 0xffff:
			outcome = palcombat.Escaped
		default:
			return result, &palcombat.UnsupportedError{Feature: fmt.Sprintf("script assigns raw battle result %04x", after.BattleResult), ObjectID: request.ObjectID, Entry: request.Entry}
		}
		result.ForcedOutcome = &outcome
		after.BattleResultSet = false
	}
	if err := combatWorldToSnapshot(after, snapshot); err != nil {
		return result, err
	}
	tx.world = palflow.CloneWorld(after)
	tx.inputIndex += report.InputsConsumed
	tx.textBytes += report.TextBytes
	tx.context = palflow.CombatContext{LastEventID: report.State.LastEventID, ScriptSuccess: report.State.ScriptSuccess, IdleFrames: maps.Clone(report.State.IdleFrames)}
	result.NextEntry, result.Success = report.State.ReturnedEntry, report.State.ScriptSuccess
	return result, nil
}

func appendCombatScriptTrace(out *[]palcombat.ScriptTrace, entry uint16, trace []palflow.TraceStep, success bool) {
	for _, step := range trace {
		*out = append(*out, palcombat.ScriptTrace{Entry: entry, PC: step.PC, Opcode: step.Opcode, Operands: step.Operands, Success: success})
		appendCombatScriptTrace(out, entry, step.Children, success)
	}
}

func (tx *combatTransaction) Commit(snapshot palcombat.Snapshot) error {
	if tx.closed {
		return fmt.Errorf("combat script transaction is already closed")
	}
	w := palflow.CloneWorld(tx.world)
	if err := snapshotToCombatWorld(w, snapshot); err != nil {
		return err
	}
	w.BattleRandomState = tx.owner.random.state
	tx.owner.world, tx.owner.inputIndex, tx.owner.textBytes = w, tx.inputIndex, tx.textBytes
	tx.owner.context = tx.context
	tx.owner.context.IdleFrames = maps.Clone(tx.context.IdleFrames)
	tx.closed = true
	return nil
}

func (tx *combatTransaction) Rollback() { tx.closed = true }

func snapshotToCombatWorld(w *palflow.WorldState, snapshot palcombat.Snapshot) error {
	if w == nil {
		return fmt.Errorf("combat synchronization requires a private world")
	}
	if w.Roles == nil {
		return fmt.Errorf("combat synchronization requires role attributes")
	}
	copy := palcombat.CloneSnapshot(snapshot)
	w.Battle = &copy
	w.BattleHidingTime = snapshot.HidingTime
	w.Cash = snapshot.Cash
	if err := writeCombatInventory(w, snapshot.Inventory); err != nil {
		return err
	}
	for member, p := range snapshot.Party {
		id := p.Role.ID
		if id >= paldata.RoleCount {
			return fmt.Errorf("combat snapshot role %d is invalid", id)
		}
		w.Roles[id] = combatRoleRaw(p.Role)
		w.PlayerStatus[id] = p.Status
		for slot, poison := range p.Poisons {
			w.PlayerPoisons[slot][member] = palflow.WorldPoison{ID: poison.ID, Script: poison.Script}
		}
		w.Experience[id][0].Exp = uint16(p.Experience)
		w.Experience[id][0].Level = p.Role.Level
		if h := p.HiddenExperience; h != nil {
			for i, track := range []paldata.Experience{h.Health, h.Magic, h.Attack, h.MagicPower, h.Defense, h.Dexterity, h.Flee} {
				w.Experience[id][i+1] = worldExperience(track)
			}
		}
		if p.EquipmentEffects != nil {
			for slot := range p.EquipmentEffects {
				for attribute := range p.EquipmentEffects[slot] {
					w.EquipmentEffects[slot][attribute][id] = p.EquipmentEffects[slot][attribute]
				}
			}
		}
	}
	for id, object := range snapshot.ObjectChanges {
		w.Objects[id] = object
	}
	for id, damage := range snapshot.MagicBaseDamage {
		w.MagicBaseDamage[id] = damage
	}
	for key, entry := range snapshot.ScriptEntries {
		id, word := uint16(key/8), key%8
		object, ok := w.Objects[id]
		if !ok || word >= 7 {
			return fmt.Errorf("combat continuation object %d word %d is absent or invalid", id, word)
		}
		object[word] = entry
		w.Objects[id] = object
	}
	return nil
}

// Only fields exposed to script mutation are copied. Fighter slices, identity,
// round counters, continuations and rewards remain owned by the numeric engine.
func combatWorldToSnapshot(w *palflow.WorldState, snapshot *palcombat.Snapshot) error {
	if w == nil || w.Battle == nil || !w.InBattle {
		return &palcombat.UnsupportedError{Feature: "script removes active battle state"}
	}
	if len(w.PartyRoles) != len(snapshot.Party) || len(w.Battle.Party) != len(snapshot.Party) || len(w.Battle.Enemies) != len(snapshot.Enemies) {
		return &palcombat.UnsupportedError{Feature: "script changes combat topology"}
	}
	for i := range snapshot.Party {
		p := &snapshot.Party[i]
		id := p.Role.ID
		raw, ok := w.Roles[id]
		if !ok || w.PartyRoles[i] != id || w.Battle.Party[i].Role.ID != id {
			return &palcombat.UnsupportedError{Feature: "script changes party identity"}
		}
		p.Role, p.Status = combatRole(id, raw), w.PlayerStatus[id]
		for slot := range p.Poisons {
			poison := w.PlayerPoisons[slot][i]
			p.Poisons[slot] = palcombat.Poison{ID: poison.ID, Script: poison.Script}
		}
		p.Experience = uint32(w.Experience[id][0].Exp)
		tracks := w.Experience[id]
		p.HiddenExperience = &palcombat.HiddenExperience{Health: combatExperience(tracks[1]), Magic: combatExperience(tracks[2]), Attack: combatExperience(tracks[3]), MagicPower: combatExperience(tracks[4]), Defense: combatExperience(tracks[5]), Dexterity: combatExperience(tracks[6]), Flee: combatExperience(tracks[7])}
		for slot := range p.EquipmentEffects {
			for attribute := range p.EquipmentEffects[slot] {
				p.EquipmentEffects[slot][attribute] = w.EquipmentEffects[slot][attribute][id]
			}
		}
	}
	for i := range snapshot.Enemies {
		x, changed := &snapshot.Enemies[i], w.Battle.Enemies[i]
		if x.ObjectID != changed.ObjectID || x.Slot != changed.Slot {
			return &palcombat.UnsupportedError{Feature: "script changes enemy identity", ObjectID: changed.ObjectID}
		}
		x.Enemy, x.HP, x.Status, x.Poisons = changed.Enemy, changed.HP, changed.Status, changed.Poisons
	}
	counts, err := combatInventory(w)
	if err != nil {
		return err
	}
	snapshot.Cash, snapshot.Inventory, snapshot.HidingTime = w.Cash, counts, w.BattleHidingTime
	return nil
}
