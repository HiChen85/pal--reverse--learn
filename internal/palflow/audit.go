package palflow

import (
	"fmt"
	"sort"
)

func opcodeName(op uint16) string {
	switch op {
	case 0:
		return "stop"
	case 1:
		return "stop_replace_next"
	case 2:
		return "counted_stop_replace"
	case 3:
		return "counted_jump"
	case 4:
		return "call_trigger"
	case 5:
		return "redraw_record_and_optional_standing_gestures"
	case 0x43:
		return "music_config_record_no_playback"
	case 0x45:
		return "battle_music_config"
	case 0x47:
		return "sound_record_no_playback"
	case 0x8e:
		return "restore_screen_record_only"
	case 6:
		return "random_branch_with_declared_input_policy"
	case 7:
		return "numeric_battle_or_explicit_supplied_outcome"
	case 8:
		return "replace_next_entry"
	case 0xa:
		return "confirm_explicit_input"
	case 0x3b:
		return "dialog_center_record_only"
	case 0x3c:
		return "dialog_upper_record_only"
	case 0x3d:
		return "dialog_lower_record_only"
	case 0x3e:
		return "dialog_window_record_only"
	case 0xffff:
		return "message_table_reference"
	default:
		if name := shopOpcodeName(op); name != "" {
			return name
		}
		if name := poisonOpcodeName(op); name != "" {
			return name
		}
		if name := battleOpcodeName(op); name != "" {
			return name
		}
		if name := worldExtraOpcodeName(op); name != "" {
			return name
		}
		if name := movementOpcodeName(op); name != "" {
			return name
		}
		if name := conditionsOpcodeName(op); name != "" {
			return name
		}
		if name := presentationOpcodeName(op); name != "" {
			return name
		}
		if name := partyWalkOpcodeName(op); name != "" {
			return name
		}
		if name := ticksOpcodeName(op); name != "" {
			return name
		}
		if name := playerOpcodeName(op); name != "" {
			return name
		}
		if name := inventoryOpcodeName(op); name != "" {
			return name
		}
		if name := worldOpcodeName(op); name != "" {
			return name
		}
		return "unsupported"
	}
}
func supported(op uint16) bool { return opcodeName(op) != "unsupported" }
func operandRestriction(inst Instruction) string {
	if inst.Opcode == 0x70 {
		return "party walk requires a complete party world snapshot and bounded auto scheduling; table-only audit cannot expand it"
	}
	if inst.Opcode == 9 {
		return "wait requires explicit current-scene world state and bounded auto scheduling; table-only audit cannot safely expand it"
	}
	return ""
}
func counts(entries []Instruction) []OpcodeCount {
	byOp := map[uint16]int{}
	modeled := map[uint16]int{}
	for _, inst := range entries {
		byOp[inst.Opcode]++
		if supported(inst.Opcode) && operandRestriction(inst) == "" {
			modeled[inst.Opcode]++
		}
	}
	out := make([]OpcodeCount, 0, len(byOp))
	for op, n := range byOp {
		constraints := []string(nil)
		if op == 5 {
			constraints = []string{"Nonzero operand 2 requires complete party standing-gesture state."}
		}
		out = append(out, OpcodeCount{ModeledCount: modeled[op], OperandConstraints: constraints, Opcode: op, Hex: fmt.Sprintf("0x%04x", op), Name: opcodeName(op), Count: n, Supported: supported(op)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Opcode < out[j].Opcode })
	return out
}

// Inventory counts table records, including sentinel record zero. Supported
// means modeled by this partial runner, not verified for this game or route.
func Inventory(entries []Instruction) InventoryReport {
	result := InventoryReport{Instructions: len(entries), Opcodes: counts(entries)}
	for _, count := range result.Opcodes {
		if count.Supported {
			result.ModeledInstructions += count.ModeledCount
		}
	}
	return result
}
func Audit(entries []Instruction, start uint16) AuditReport {
	return AuditMany(entries, []uint16{start})
}

// AuditMany visits a union of entry points in one bounded worklist. It reports
// an overapproximation of control edges only for the modeled prefix. Expansion
// stops at unsupported instructions; their outgoing behavior is unknown. Resume
// edges include returned entry points for hypothetical subsequent invocations,
// not instructions necessarily executed during the current invocation.
func AuditMany(entries []Instruction, roots []uint16) AuditReport {
	result := AuditReport{Coverage: "static modeled-prefix control-flow overapproximation; not an executed trace", Limitations: []string{
		"Traversal stops at unsupported instructions; outgoing behavior and the rest of a quest may be unknown.",
		"Both branch outcomes are included without evaluating world conditions, idle counters, confirmations, random values or battle outcomes.",
		"Call-return and subsequent-trigger resume edges are possible edges, not proof that a callee returns or an event is triggered again.",
		"This audit does not resolve equipment rebuild implicit calls or verify scene transitions, automatic scripts, world effects, dialogue rendering, combat, main story or side quests.",
		"Message references are not resolved by this table-only audit; a missing message blocks a runtime trace when reached.",
		"Scene teleport calls require a runtime scene table; traversal stops there rather than guessing their callee or return behavior.",
		"Random skips use the reference integer-division range. A skip with more than 4096 possible targets, or a graph exceeding 65536 edges, stops with an explicit expansion blocker.",
	}}
	rootSet := map[uint16]bool{}
	queue := make([]uint16, 0, len(roots))
	for _, root := range roots {
		if !rootSet[root] {
			rootSet[root] = true
			result.Roots = append(result.Roots, root)
			if root != 0 {
				queue = append(queue, root)
			}
		}
	}
	sort.Slice(result.Roots, func(i, j int) bool { return result.Roots[i] < result.Roots[j] })
	visited := map[uint16]bool{}
	var reached []Instruction
	if len(entries) > 1<<16 {
		result.Blockers = append(result.Blockers, StaticBlocker{Reason: "script table exceeds 16-bit address space"})
	}
	for head := 0; head < len(queue); head++ {
		pc := queue[head]
		if pc == 0 || visited[pc] {
			continue
		}
		visited[pc] = true
		if int(pc) >= len(entries) {
			result.Blockers = append(result.Blockers, StaticBlocker{PC: pc, Reason: "script address is outside the supplied table"})
			continue
		}
		result.Reachable = append(result.Reachable, pc)
		inst := entries[pc]
		reached = append(reached, inst)
		if !supported(inst.Opcode) || operandRestriction(inst) != "" {
			reason := "opcode is not modeled; static traversal stops here"
			if restriction := operandRestriction(inst); restriction != "" {
				reason = restriction
			}
			if inst.Opcode == 9 {
				reason = "wait invokes game updates and automatic scripts; traversal cannot treat it as a no-op"
			}
			result.Blockers = append(result.Blockers, StaticBlocker{PC: pc, Opcode: inst.Opcode, Reason: reason})
			continue
		}
		edgeLimitReached := false
		add := func(to uint16, kind string) {
			if len(result.Edges) >= 1<<16 {
				if !edgeLimitReached {
					result.Blockers = append(result.Blockers, StaticBlocker{PC: pc, Opcode: inst.Opcode, Reason: "static edge expansion limit of 65536 reached; outgoing behavior is not fully expanded"})
					edgeLimitReached = true
				}
				return
			}
			result.Edges = append(result.Edges, Edge{From: pc, To: to, Kind: kind})
			if to != 0 && !visited[to] {
				queue = append(queue, to)
			}
		}
		next := func(kind string) {
			if pc == 0xffff {
				result.Blockers = append(result.Blockers, StaticBlocker{PC: pc, Opcode: inst.Opcode, Reason: "fallthrough would overflow 16-bit address space"})
				return
			}
			add(pc+1, kind)
		}
		a := inst.Operands
		switch inst.Opcode {
		case 0:
		case 1:
			next("subsequent_trigger_resume")
		case 2:
			add(a[0], "subsequent_trigger_resume")
			if a[1] != 0 {
				next("idle_threshold_fallthrough")
			}
		case 3:
			add(a[0], "jump")
			if a[1] != 0 {
				next("idle_threshold_fallthrough")
			}
		case 4:
			add(a[0], "call")
			next("possible_call_return")
		case 6:
			add(a[1], "possible_random_jump")
			next("possible_random_fallthrough")
		case 7:
			if a[1] != 0 {
				add(a[1], "external_battle_lost")
			}
			if a[2] != 0 {
				add(a[2], "external_battle_fled")
			}
			next("external_battle_other_result")
		case 0x1e:
			if int16(a[0]) < 0 {
				add(a[1], "possible_cash_insufficient")
			}
			next("possible_cash_sufficient")
		case 0x58:
			add(a[2], "possible_inventory_insufficient")
			next("possible_inventory_sufficient")
		case 0x20:
			if a[2] != 0 {
				add(a[2], "possible_inventory_and_equipment_insufficient")
			}
			next("possible_item_removal")
		case 0x38:
			result.Blockers = append(result.Blockers, StaticBlocker{PC: pc, Opcode: inst.Opcode, Reason: "scene teleport callee depends on runtime scene state; static traversal stops without assuming a callee or return"})
		case 0x5d:
			add(a[1], "possible_poison_kind_absent")
			next("possible_poison_kind_present")
		case 0x61:
			add(a[0], "possible_ordinary_poison_absent")
			next("possible_ordinary_poison_present")
		case 0x74:
			add(a[0], "possible_party_member_below_maximum_hp")
			next("possible_party_hp_full")
		case 0x79:
			add(a[1], "possible_party_name_present")
			next("possible_party_name_absent")
		case 0x81, 0x83, 0x84:
			add(a[2], "possible_spatial_condition_failed")
			next("possible_spatial_condition_succeeded")
		case 0x86:
			add(a[2], "possible_equipped_item_count_below_threshold")
			next("possible_equipped_item_count_sufficient")
		case 0x94:
			add(a[2], "possible_event_state_equal")
			next("possible_event_state_different")
		case 0x95:
			add(a[1], "possible_current_scene_equal")
			next("possible_current_scene_different")
		case 0xa2:
			maximum := referenceRandomSkipMaximum(a[0])
			if maximum+1 > 4096 || maximum+1 > (1<<16)-len(result.Edges) {
				result.Blockers = append(result.Blockers, StaticBlocker{PC: pc, Opcode: inst.Opcode, Reason: fmt.Sprintf("random skip has %d possible reference results; bounded static edge expansion stops here", maximum+1)})
				break
			}
			if int(pc)+maximum >= 0xffff {
				result.Blockers = append(result.Blockers, StaticBlocker{PC: pc, Opcode: inst.Opcode, Reason: "some reference random skip results overflow the modeled 16-bit program counter"})
			}
			for skip := 0; skip <= maximum && int(pc)+skip < 0xffff; skip++ {
				add(pc+uint16(skip)+1, "possible_reference_random_skip")
			}
		case 0xa:
			add(a[0], "confirm_no")
			next("confirm_yes")
		default:
			next("fallthrough")
		}
	}
	sort.Slice(result.Reachable, func(i, j int) bool { return result.Reachable[i] < result.Reachable[j] })
	sort.Slice(result.Blockers, func(i, j int) bool {
		if result.Blockers[i].PC == result.Blockers[j].PC {
			return result.Blockers[i].Reason < result.Blockers[j].Reason
		}
		return result.Blockers[i].PC < result.Blockers[j].PC
	})
	result.Opcodes = counts(reached)
	return result
}
