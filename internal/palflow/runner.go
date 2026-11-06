package palflow

import (
	"errors"
	"fmt"
	"maps"
)

const MaxAllowedSteps = 100000
const MaxAllowedCallDepth = 128
const MaxAllowedTextBytes = 32 << 20
const DefaultMaxTextBytes = 8 << 20

type Runner struct {
	source  Source
	options Options
}

func New(source Source, options Options) (*Runner, error) {
	if source == nil {
		return nil, errors.New("PAL trigger source is required")
	}
	if options.MaxTextBytes == 0 {
		options.MaxTextBytes = DefaultMaxTextBytes
	}
	if options.MaxTextBytes < 1 || options.MaxTextBytes > MaxAllowedTextBytes {
		return nil, fmt.Errorf("max text bytes must be 1..%d", MaxAllowedTextBytes)
	}
	if options.MaxSteps == 0 {
		options.MaxSteps = 10000
	}
	if options.MaxCallDepth == 0 {
		options.MaxCallDepth = 32
	}
	if options.MaxSteps < 1 || options.MaxSteps > MaxAllowedSteps {
		return nil, fmt.Errorf("max steps must be 1..%d", MaxAllowedSteps)
	}
	if options.MaxCallDepth < 1 || options.MaxCallDepth > MaxAllowedCallDepth {
		return nil, fmt.Errorf("max call depth must be 1..%d", MaxAllowedCallDepth)
	}
	return &Runner{source, options}, nil
}

// Trace executes one bounded invocation of the modeled trigger subset. A
// stopped script is not proof of a valid quest route. Only the explicitly
// modeled world/auto subset is run; combat, renderers and custom-message-file
// span remapping are not implemented.
// A failing instruction commits neither state nor input consumption; its evidence
// is returned separately in Blocker, after the successfully committed prefix.
func (r *Runner) Trace(request Request) (Report, error) {
	eventID := request.EventID
	if eventID == 0xffff {
		eventID = request.LastEventID
	}
	state := State{CurEquipPart: -1, ScriptSuccess: true, World: CloneWorld(request.World), Frames: []Frame{{PC: request.Entry, Entry: request.Entry, NextEntry: request.Entry, EventID: eventID}}, IdleFrames: maps.Clone(request.IdleFrames), LastEventID: eventID}
	if state.World != nil {
		state.Music, state.MusicKnown = state.World.Music, state.World.MusicKnown
		state.BattleMusic, state.BattleMusicKnown = state.World.BattleMusic, state.World.BattleMusicKnown
	}
	if provider, ok := r.source.(ScriptRandomProvider); ok {
		state.Random = provider.ScriptRandom()
	}
	if state.IdleFrames == nil {
		state.IdleFrames = map[uint16]uint16{}
	}
	report := Report{Status: "blocked", Coverage: "partial trigger-script model; no main-story or side-quest verification", Limitations: []string{
		"Only the modeled trigger opcode subset is executed; unsupported instructions stop the trace.",
		"World, inventory, equipment, auto scheduling and collision execute only where modeled with explicit resources. Numeric combat requires a combat source and policy; otherwise battle input is an explicitly marked supplied outcome. Dialog/screen/audio requests are recorded without audiovisual playback. This trace alone does not verify a complete route or original-runtime parity.",
		"Message lookup uses direct original-table indexes; custom message-file span remapping is not modeled.",
		"Event contexts and initial idle counters are caller-supplied; event existence and spatial trigger reachability are not verified.",
	}}
	if eventID == 0xffff {
		blocker := &Blocker{Kind: "invalid_context", Reason: "last event context cannot itself be the 0xffff reuse sentinel", PC: request.Entry, EventID: eventID, Depth: 1}
		report.Blocker = blocker
		finalize(&report, state, request.Inputs)
		return report, blocker
	}
	finishZeroFrames(&state)
	for !state.Finished {
		frame := state.Frames[len(state.Frames)-1]
		fail := func(kind, reason string, inst *Instruction) (Report, error) {
			blocker := &Blocker{Kind: kind, Reason: reason, PC: frame.PC, Instruction: inst, EventID: frame.EventID, Depth: len(state.Frames)}
			if inst != nil {
				blocker.Opcode = inst.Opcode
			}
			report.Blocker = blocker
			finalize(&report, state, request.Inputs)
			return report, blocker
		}
		if state.ExecutedSteps+state.PlayerSubsteps+state.WorldTicks+state.AutoSubsteps+state.CombatWork >= r.options.MaxSteps {
			return fail("step_limit", "maximum modeled instruction count reached", nil)
		}
		inst, err := r.source.Instruction(frame.PC)
		if err != nil {
			return fail("source_bounds", err.Error(), nil)
		}
		next := state
		next.Frames = append([]Frame(nil), state.Frames...)
		step := TraceStep{Sequence: state.ExecutedSteps + 1, PC: frame.PC, Opcode: inst.Opcode, Operands: inst.Operands, EventID: frame.EventID, Depth: len(state.Frames)}
		if err := r.evaluateAndCharge(&next, &step, inst, request.Inputs); err != nil {
			kind := "instruction_blocked"
			var textErr textLimitError
			if errors.As(err, &textErr) {
				kind = "text_limit"
			}
			if !supported(inst.Opcode) {
				kind = "unsupported_opcode"
			}
			failed, outer := fail(kind, err.Error(), &inst)
			var nested *Blocker
			if errors.As(err, &nested) {
				failed.Blocker.Cause = nested
			}
			return failed, outer
		}
		finishZeroFrames(&next)
		if len(next.Frames) > 0 {
			step.NextPC = next.Frames[len(next.Frames)-1].PC
		}
		state = next
		report.Trace = append(report.Trace, step)
		collectStepEvidence(&report, step)

	}
	report.Status = "script_stopped"
	report.Completed = true
	finalize(&report, state, request.Inputs)
	return report, nil
}
func finalize(report *Report, state State, inputs []Input) {
	state.Frames = append([]Frame(nil), state.Frames...)
	state.IdleFrames = maps.Clone(state.IdleFrames)
	state.World = CloneWorld(state.World)
	report.TextBytes = state.TextBytes
	report.State = state
	report.InputsConsumed = state.InputIndex
	report.UnusedInputs = append([]Input(nil), inputs[state.InputIndex:]...)
	if len(report.UnusedInputs) > 0 {
		report.Limitations = append(report.Limitations, "Inputs remain unused; this is not an exact input replay match.")
	}
}
func finishZeroFrames(state *State) {
	for len(state.Frames) > 0 && state.Frames[len(state.Frames)-1].PC == 0 {
		stopFrame(state, nil)
	}
}
func stopFrame(state *State, step *TraceStep) {
	index := len(state.Frames) - 1
	state.CurEquipPart = -1
	returned := state.Frames[index].NextEntry
	state.Frames = state.Frames[:index]
	if step != nil {
		step.ReturnedEntry = &returned
	}
	if len(state.Frames) == 0 {
		state.Finished = true
		state.ReturnedEntry = returned
	}
}
func fallthroughPC(pc uint16) (uint16, error) {
	if pc == 0xffff {
		return 0, errors.New("program counter would overflow 16-bit address space")
	}
	return pc + 1, nil
}
func consume(state *State, inputs []Input, kind string) (*InputUse, error) {
	if kind == InputRandom {
		// Probability branches do not otherwise mutate/clone the world. Other
		// random-consuming handlers already own detached working snapshots.
		if state != nil && state.Random == nil && state.World != nil && state.World.RandomMode == "seeded" && state.InputIndex >= 0 && (state.InputIndex >= len(inputs) || inputs[state.InputIndex].Kind != InputRandom) {
			state.World = CloneWorld(state.World)
		}
		_, use, err := consumeRandom(state, inputs, 1, 100, "probability branch")
		return use, err
	}
	if state.InputIndex >= len(inputs) {
		return nil, fmt.Errorf("explicit %s input is required", kind)
	}
	input := inputs[state.InputIndex]
	if input.Kind != kind {
		return nil, fmt.Errorf("input %d: expected %s, received %q", state.InputIndex, kind, input.Kind)
	}
	valid := false
	meaning := ""
	switch kind {
	case InputRandom:
		// The pinned reference's integer-division generator can reach 101
		// at the tail even for a requested inclusive 1..100 range.
		valid = input.Value >= 1 && input.Value <= 1+2147483647/(2147483647/100)
		meaning = "explicit reference RandomLong(1,100) result, including its reachable division-tail value 101"
	case InputConfirm:
		valid = input.Value == 0 || input.Value == 1
		meaning = "explicit confirmation: 0=no, 1=yes"
	case InputKey:
		valid = input.Value == 1
		meaning = "explicit key acknowledgement"
	case InputBattle:
		valid = input.Value >= BattleWin && input.Value <= BattleFled
		meaning = "external/simulated battle outcome: 0=won, 1=lost, 2=fled; combat not executed"
	}
	if !valid {
		return nil, fmt.Errorf("input %d: invalid %s value %d", state.InputIndex, kind, input.Value)
	}
	used := &InputUse{Index: state.InputIndex, Input: input, Meaning: meaning}
	state.InputIndex++
	return used, nil
}
func (r *Runner) evaluate(state *State, step *TraceStep, inst Instruction, inputs []Input) error {
	if handled, err := r.evaluateCombat(state, step, inst, inputs); handled {
		return err
	}
	if handled, err := r.evaluateTicks(state, step, inst, inputs); handled {
		return err
	}
	if handled, err := r.evaluateInterpreted(state, step, inst, inputs); handled {
		return err
	}
	index := len(state.Frames) - 1
	frame := &state.Frames[index]
	a := inst.Operands
	advance := func() error {
		pc, err := fallthroughPC(frame.PC)
		if err != nil {
			return err
		}
		frame.PC = pc
		return nil
	}
	switch inst.Opcode {
	case 0:
		step.Effect = "stop and preserve this invocation's replacement entry"
		stopFrame(state, step)
	case 1:
		pc, err := fallthroughPC(frame.PC)
		if err != nil {
			return err
		}
		frame.NextEntry = pc
		step.Effect = "stop and replace the caller's entry with the following instruction"
		stopFrame(state, step)
	case 2, 3:
		take := true
		if a[1] != 0 {
			if frame.EventID == 0 {
				return errors.New("counted stop/jump requires a nonzero event context")
			}
			idle, ok := state.IdleFrames[frame.EventID]
			if !ok {
				return fmt.Errorf("initial trigger idle counter for event %d must be supplied", frame.EventID)
			}
			idle++ // The upstream field is USHORT: overflow deliberately wraps.
			take = idle < a[1]
			if !take {
				idle = 0
			}
			state.IdleFrames = maps.Clone(state.IdleFrames)
			state.IdleFrames[frame.EventID] = idle
		}
		if take {
			if inst.Opcode == 2 {
				frame.NextEntry = a[0]
				step.Effect = "stop and replace caller entry with operand 0"
				stopFrame(state, step)
			} else {
				frame.PC = a[0]
				step.Effect = "jump to operand 0"
			}
		} else {
			step.Effect = "idle threshold reached; reset event counter and advance"
			return advance()
		}
	case 4:
		if len(state.Frames) >= r.options.MaxCallDepth {
			return errors.New("maximum trigger-call depth reached")
		}
		pc, err := fallthroughPC(frame.PC)
		if err != nil {
			return err
		}
		context := frame.EventID
		if a[1] != 0 {
			context = a[1]
		}
		if context == 0xffff {
			context = state.LastEventID
		}
		frame.PC = pc
		state.LastEventID = context
		state.ScriptSuccess = true
		state.Frames = append(state.Frames, Frame{PC: a[0], Entry: a[0], NextEntry: a[0], EventID: context})
		step.Effect = "call trigger with explicit/inherited event context; callee return entry is ignored"
	case 5:
		if a[2] != 0 {
			world := CloneWorld(state.World)
			if err := updatePartyGestures(r.source, world, false); err != nil {
				return fmt.Errorf("redraw standing gestures: %w", err)
			}
			state.World = world
		}
		if err := advance(); err != nil {
			return err
		}
		delay := uint32(a[1]) * 60
		if a[1] == 0 {
			delay = 60
		}
		step.Screen = &ScreenRecord{Kind: "redraw", ClearDialogRequested: true, OverworldDelayMilliseconds: delay}
		step.Effect = fmt.Sprintf("record dialog-clear/redraw request and conditional overworld delay; standing gestures updated=%t; no rendering or wall-clock wait", a[2] != 0)
	case 0x8e:
		if err := advance(); err != nil {
			return err
		}
		step.Screen = &ScreenRecord{Kind: "restore_screen", ClearDialogRequested: true}
		step.Effect = "record dialog-clear and screen-restore request; no rendering executed"
	case 6:
		input, err := consume(state, inputs, InputRandom)
		if err != nil {
			return err
		}
		step.Input = input
		if input.Input.Value >= int(a[0]) {
			frame.PC = a[1]
			step.Effect = "explicit random result >= operand 0: jump to operand 1"
		} else {
			step.Effect = "explicit random result < operand 0: advance"
			return advance()
		}
	case 7:
		input, err := consume(state, inputs, InputBattle)
		if err != nil {
			return err
		}
		step.Input = input
		step.Effect = "battle is stubbed by an external outcome; no combat executed"
		if input.Input.Value == BattleLost && a[1] != 0 {
			frame.PC = a[1]
		} else if input.Input.Value == BattleFled && a[2] != 0 {
			frame.PC = a[2]
		} else {
			return advance()
		}
	case 8:
		if err := advance(); err != nil {
			return err
		}
		frame.NextEntry = frame.PC
		step.Effect = "replace caller entry with following instruction and continue"
	case 0xa:
		input, err := consume(state, inputs, InputConfirm)
		if err != nil {
			return err
		}
		step.Input = input
		if input.Input.Value == 0 {
			frame.PC = a[0]
			step.Effect = "explicit no confirmation: jump to operand 0"
		} else {
			step.Effect = "explicit yes confirmation: advance"
			return advance()
		}
	case 0x3b, 0x3c, 0x3d, 0x3e:
		if err := advance(); err != nil {
			return err
		}
		dialog := &DialogRecord{Rendered: false}
		switch inst.Opcode {
		case 0x3b:
			dialog.Position = "center"
			dialog.Color = uint8(a[0])
			dialog.RNG = a[2] != 0
		case 0x3c:
			dialog.Position = "upper"
			dialog.Color = uint8(a[1])
			dialog.Portrait = a[0]
			dialog.RNG = a[2] != 0
		case 0x3d:
			dialog.Position = "lower"
			dialog.Color = uint8(a[1])
			dialog.Portrait = a[0]
			dialog.RNG = a[2] != 0
		case 0x3e:
			dialog.Position = "center_window"
			dialog.Color = uint8(a[0])
		}
		step.Dialog = dialog
		step.Effect = "record dialog presentation configuration; no rendering performed"
	case 0xffff:
		text, err := r.source.Message(a[0])
		if err != nil {
			return fmt.Errorf("message %d: %w", a[0], err)
		}
		if err := advance(); err != nil {
			return err
		}
		step.Message = &DisplayedMessage{ID: a[0], Text: text}
		step.Effect = "record direct message-table text without rewriting control characters"
	default:
		if inst.Opcode == 9 {
			return errors.New("wait opcode performs PAL_GameUpdate and auto-script/world effects; it cannot be skipped")
		}
		return fmt.Errorf("opcode 0x%04x is not modeled; world/interpreter effects must not be silently skipped", inst.Opcode)
	}
	return nil
}
