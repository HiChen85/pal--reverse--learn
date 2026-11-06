package palflow

import (
	"fmt"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

// BattleDataSource supplies the unmodified DATA.MKF baseline used by 0x64.
// The active enemy record is mutable, so its HP cannot serve as that baseline.
type BattleDataSource interface {
	RuntimeTables() *paldata.RuntimeTables
}

func battleOpcodeName(op uint16) string {
	switch op {
	case 0x21:
		return "damage one or all enemies using WORD arithmetic"
	case 0x2e:
		return "set enemy status after explicit sorcery resistance draw"
	case 0x33:
		return "collect enemy value or jump when zero"
	case 0x34:
		return "exchange collected value for an item using explicit random input"
	case 0x39:
		return "drain enemy HP into the moving player's HP"
	case 0x3a:
		return "escape battle or jump when fighting a boss"
	case 0x5b:
		return "damage enemy by bounded half HP plus one"
	case 0x5c:
		return "set negative battle hiding duration"
	case 0x60:
		return "set enemy HP to zero"
	case 0x64:
		return "jump when enemy HP exceeds its imported baseline percentage"
	case 0x67:
		return "set enemy magic and its usage rate"
	case 0x68:
		return "jump during enemy movement"
	case 0x69:
		return "request enemy escape and terminate the battle"
	case 0x6a:
		return "steal enemy cash or an item using explicit random input"
	case 0x6b:
		return "set signed enemy blow displacement"
	case 0x89:
		return "set raw battle result"
	case 0x91:
		return "jump if invoking enemy is not first of its kind"
	case 0x92:
		return "request party magic casting animation"
	}
	return ""
}

// evaluateBattleOps executes only numeric transitions. Its working state,
// random consumption and evidence are published together after all checks.
// Visual delays are presentation requests and never run a GameUpdate.
func (r *Runner) evaluateBattleOps(state *State, step *TraceStep, inst Instruction, inputs []Input) (bool, error) {
	name := battleOpcodeName(inst.Opcode)
	if name == "" {
		return false, nil
	}
	if state == nil || step == nil || len(state.Frames) == 0 || state.World == nil {
		return true, fmt.Errorf("battle instruction requires state, frame, trace and explicit world")
	}
	working := *state
	working.World = CloneWorld(state.World)
	working.Frames = append([]Frame(nil), state.Frames...)
	evidence := *step
	evidence.RandomInputs = append([]InputUse(nil), step.RandomInputs...)
	evidence.Effect = name
	index := len(working.Frames) - 1
	frame := working.Frames[index]
	w, a := working.World, inst.Operands
	var jump *uint16
	branch := func(target uint16) { jump = &target }
	enemy := func() (*palcombat.EnemyState, error) { return battleEnemy(w, frame.EventID) }
	random := func(low, high int) (int, error) {
		return battleRandom(&working, &evidence, inputs, low, high)
	}
	switch inst.Opcode {
	case 0x21:
		if a[0] != 0 {
			if err := battleSnapshot(w); err != nil {
				return true, err
			}
			for i := range w.Battle.Enemies {
				x := &w.Battle.Enemies[i]
				if x.ObjectID != 0 {
					setBattleEnemyHP(x, uint16(x.HP)-a[1])
				}
			}
		} else {
			x, err := enemy()
			if err != nil {
				return true, err
			}
			setBattleEnemyHP(x, uint16(x.HP)-a[1])
		}
	case 0x2e:
		if a[0] >= palcombat.StatusCount {
			return true, fmt.Errorf("enemy status %d is outside classic status indexes 0..8", a[0])
		}
		x, err := enemy()
		if err != nil {
			return true, err
		}
		object, ok := w.Objects[x.ObjectID]
		if !ok {
			return true, fmt.Errorf("enemy object %d is absent from world snapshot", x.ObjectID)
		}
		v, err := random(0, 9)
		if err != nil {
			return true, err
		}
		if v > int(object[1]) {
			x.Status[a[0]] = a[1]
		} else {
			branch(a[2])
		}
	case 0x33:
		x, err := enemy()
		if err != nil {
			return true, err
		}
		if x.Enemy.CollectValue != 0 {
			w.CollectValue += x.Enemy.CollectValue
		} else {
			branch(a[0])
		}
	case 0x34:
		if w.CollectValue == 0 {
			branch(a[0])
			break
		}
		tables, err := r.battleTables()
		if err != nil {
			return true, err
		}
		if len(tables.Stores) == 0 {
			return true, fmt.Errorf("collection exchange requires imported store zero")
		}
		v, err := random(1, int(w.CollectValue))
		if err != nil {
			return true, err
		}
		v = min(v, 9)
		item := tables.Stores[0][v-1]
		if err := battleAddItem(w, item); err != nil {
			return true, err
		}
		w.CollectValue -= uint16(v)
		evidence.Screen = &ScreenRecord{Kind: "collection_item_dialog"}
		evidence.Effect += fmt.Sprintf("; collected cost %d; item %d requested", v, item)
	case 0x39:
		x, err := enemy()
		if err != nil {
			return true, err
		}
		if int(w.BattleMovingPlayer) >= len(w.PartyRoles) {
			return true, fmt.Errorf("moving player party index %d is outside supplied party", w.BattleMovingPlayer)
		}
		role := w.PartyRoles[w.BattleMovingPlayer]
		raw, err := playerRole(w, role)
		if err != nil {
			return true, err
		}
		setBattleEnemyHP(x, uint16(x.HP)-a[0])
		raw[9] += a[0]
		raw[9] = min(raw[9], raw[7])
		w.Roles[role] = raw
	case 0x3a:
		if w.BattleBoss {
			branch(a[0])
		} else {
			w.BattleResult, w.BattleResultSet = 0xffff, true
			evidence.Screen = &ScreenRecord{Kind: "battle_player_escape"}
		}
	case 0x5b:
		x, err := enemy()
		if err != nil {
			return true, err
		}
		damage := min(uint16(x.HP)/2+1, a[0])
		setBattleEnemyHP(x, uint16(x.HP)-damage)
	case 0x5c:
		w.BattleHidingTime = -int(a[0])
	case 0x60:
		x, err := enemy()
		if err != nil {
			return true, err
		}
		setBattleEnemyHP(x, 0)
	case 0x64:
		x, err := enemy()
		if err != nil {
			return true, err
		}
		object, ok := w.Objects[x.ObjectID]
		if !ok {
			return true, fmt.Errorf("enemy object %d is absent from world snapshot", x.ObjectID)
		}
		tables, err := r.battleTables()
		if err != nil {
			return true, err
		}
		if int(object[0]) >= len(tables.Enemies) {
			return true, fmt.Errorf("enemy baseline index %d is outside imported table", object[0])
		}
		baseline := tables.Enemies[object[0]].HP
		// Both operands are promoted to signed INT in the source. Refuse
		// undefined signed overflow rather than silently replacing it.
		product := uint32(baseline) * uint32(a[0])
		if product > 1<<31-1 {
			return true, fmt.Errorf("enemy baseline percentage product overflows source signed int")
		}
		if uint32(x.HP)*100 > product {
			branch(a[1])
		}
	case 0x67:
		x, err := enemy()
		if err != nil {
			return true, err
		}
		x.Enemy.Magic, x.Enemy.MagicRate = a[0], a[1]
		if x.Enemy.MagicRate == 0 {
			x.Enemy.MagicRate = 10
		}
		x.Enemy.Raw[15], x.Enemy.Raw[16] = x.Enemy.Magic, x.Enemy.MagicRate
	case 0x68:
		if w.BattleEnemyMoving {
			branch(a[0])
		}
	case 0x69:
		w.BattleResult, w.BattleResultSet = 0, true
		evidence.Screen = &ScreenRecord{Kind: "battle_enemy_escape"}
		evidence.Audio = &AudioRecord{Kind: "sound", ID: 45, PlaybackRequested: true}
	case 0x6a:
		x, err := enemy()
		if err != nil {
			return true, err
		}
		evidence.Screen = &ScreenRecord{Kind: "battle_steal"}
		if x.Enemy.StealCount == 0 {
			break
		}
		v, err := random(0, 10)
		if err != nil {
			return true, err
		}
		if v > int(a[0]) && a[0] != 0 {
			break
		}
		if x.Enemy.StealItem == 0 {
			divisor, err := random(2, 3)
			if err != nil {
				return true, err
			}
			amount := x.Enemy.StealCount / uint16(divisor)
			x.Enemy.StealCount -= amount
			w.Cash += uint32(amount)
			evidence.Effect += fmt.Sprintf("; stole %d cash", amount)
		} else {
			if err := battleAddItem(w, x.Enemy.StealItem); err != nil {
				return true, err
			}
			x.Enemy.StealCount--
			evidence.Effect += fmt.Sprintf("; item %d requested", x.Enemy.StealItem)
		}
		x.Enemy.Raw[20] = x.Enemy.StealCount
	case 0x6b:
		w.BattleBlow = int16(a[0])
	case 0x89:
		w.BattleResult, w.BattleResultSet = a[0], true
	case 0x91:
		if !w.InBattle {
			break
		}
		x, err := enemy()
		if err != nil {
			return true, err
		}
		for _, other := range w.Battle.Enemies {
			if other.Slot < x.Slot && other.ObjectID == x.ObjectID {
				branch(a[0])
				break
			}
		}
	case 0x92:
		if w.InBattle {
			if a[0] != 0 && int(a[0]-1) >= len(w.PartyRoles) {
				return true, fmt.Errorf("magic casting party index %d is outside supplied party", a[0]-1)
			}
			evidence.Screen = &ScreenRecord{Kind: "battle_party_pre_magic"}
		}
	}
	var nextPC uint16
	if jump != nil {
		nextPC = *jump
	} else {
		var err error
		nextPC, err = fallthroughPC(frame.PC)
		if err != nil {
			return true, err
		}
	}
	working.Frames[index].PC = nextPC
	*state, *step = working, evidence
	return true, nil
}

func battleSnapshot(w *WorldState) error {
	if w.Battle == nil {
		return fmt.Errorf("enemy instruction requires an explicit battle snapshot")
	}
	seen := [paldata.EnemyTeamSize]bool{}
	for _, x := range w.Battle.Enemies {
		if x.Slot < 0 || x.Slot >= paldata.EnemyTeamSize || seen[x.Slot] {
			return fmt.Errorf("battle enemy slot %d is invalid or duplicated", x.Slot)
		}
		seen[x.Slot] = true
		if x.HP < 0 || x.HP > 65535 {
			return fmt.Errorf("enemy HP %d is outside the source WORD range", x.HP)
		}
	}
	return nil
}

func battleEnemy(w *WorldState, slot uint16) (*palcombat.EnemyState, error) {
	if err := battleSnapshot(w); err != nil {
		return nil, err
	}
	for i := range w.Battle.Enemies {
		if w.Battle.Enemies[i].Slot == int(slot) {
			return &w.Battle.Enemies[i], nil
		}
	}
	return nil, fmt.Errorf("enemy slot %d is absent from battle snapshot", slot)
}

func setBattleEnemyHP(x *palcombat.EnemyState, hp uint16) {
	x.HP, x.Enemy.HP, x.Enemy.Raw[11] = int(hp), hp, hp
}

func (r *Runner) battleTables() (*paldata.RuntimeTables, error) {
	if r != nil {
		if source, ok := r.source.(BattleDataSource); ok {
			if tables := source.RuntimeTables(); tables != nil {
				return tables, nil
			}
		}
	}
	return nil, fmt.Errorf("battle instruction requires imported runtime tables")
}

func battleAddItem(w *WorldState, item uint16) error {
	if item == 0 {
		return nil
	}
	slots, _, err := checkedInventory(w, true)
	if err != nil {
		return err
	}
	changeInventorySlots(slots, item, 1)
	w.InventorySlots, w.Inventory = slots, inventoryCounts(slots)
	return nil
}

func battleRandom(s *State, step *TraceStep, inputs []Input, low, high int) (int, error) {
	value, use, err := consumeRandom(s, inputs, low, high, "battle script")
	if err != nil {
		return 0, err
	}
	if use != nil {
		if step.Input == nil {
			step.Input = use
		}
		step.RandomInputs = append(step.RandomInputs, *use)
	}
	return value, nil
}
