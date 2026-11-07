package palgame

import (
	"errors"
	"fmt"
	"maps"

	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

// ControlReport describes one PAL_StartFrame action. Script evidence remains
// separate for each spatially selected trigger; InputsConsumed counts the
// single ordered input stream shared by all triggers and auto scripts.
type ControlReport struct {
	Kind             string                  `json:"kind"`
	Direction        uint16                  `json:"direction"`
	Completed        bool                    `json:"completed"`
	Moved            bool                    `json:"moved"`
	Blocked          bool                    `json:"blocked"`
	Before           palflow.WorldPosition   `json:"before"`
	After            palflow.WorldPosition   `json:"after"`
	BeforeFrame      uint32                  `json:"before_frame"`
	AfterFrame       uint32                  `json:"after_frame"`
	SearchSuppressed bool                    `json:"search_suppressed"`
	SearchPositions  []palflow.WorldPosition `json:"search_positions,omitempty"`
	SearchEventID    uint16                  `json:"search_event_id,omitempty"`
	TriggeredEvents  []uint16                `json:"triggered_events,omitempty"`
	Triggers         []palflow.Report        `json:"triggers,omitempty"`
	AutoSteps        []palflow.AutoStep      `json:"auto_steps,omitempty"`
	InputsConsumed   int                     `json:"inputs_consumed"`
	UnusedInputs     []palflow.Input         `json:"unused_inputs,omitempty"`
	Work             int                     `json:"work"`
	ScreenUpdates    int                     `json:"screen_updates"`
	State            SessionSnapshot         `json:"state"`
}

// Move executes the reference frame order: trigger-enabled world update, then
// a collision-checked 16/8 pixel party step (south, west, north, east = 0..3).
// A blocked step still turns the party and restores its standing gestures.
func (s *Session) Move(direction uint16, inputs []palflow.Input) (ControlReport, error) {
	if direction > 3 {
		return ControlReport{}, errors.New("movement direction must be in 0..3")
	}
	return s.control("move", direction, inputs)
}

// Interact executes a frame without a direction key, then PAL_Search. Search
// follows the original thirteen positions and scene event order, never an ID
// supplied by the caller. Touch triggers clear the search key for that frame.
func (s *Session) Interact(inputs []palflow.Input) (ControlReport, error) {
	return s.control("interact", 0xffff, inputs)
}

type controlAction struct {
	s         *Session
	source    Source
	inputs    []palflow.Input
	consumed  int
	work      int
	textBytes int
	report    *ControlReport
}

func (s *Session) control(kind string, direction uint16, inputs []palflow.Input) (report ControlReport, err error) {
	report.Kind, report.Direction = kind, direction
	if s == nil || s.game == nil || s.current.World == nil {
		return report, errors.New("controls require an active session and world snapshot")
	}
	if s.current.NeedsSceneEnter || s.current.World.SceneChanged {
		return report, errors.New("scene OnEnter must complete before controls")
	}
	w := s.current.World
	if w.InBattle || w.SceneLoadPending || w.SceneResources == nil || w.SceneResources.SceneID != w.Scene {
		return report, errors.New("controls require an entered overworld scene with loaded scene/map resources")
	}
	if s.maxSteps < 1 || s.maxSteps > palflow.MaxAllowedSteps {
		return report, errors.New("controls require a bounded positive execution budget")
	}
	for _, coordinate := range []int{w.ViewportX, w.ViewportY, w.PartyOffsetX, w.PartyOffsetY} {
		if coordinate < -32768 || coordinate > 32767 {
			return report, errors.New("controls require signed 16-bit viewport and party offsets")
		}
	}
	if w.Direction > 3 {
		return report, errors.New("party direction must be in 0..3")
	}
	if _, err := palflow.SceneEventIDs(w, "controls"); err != nil {
		return report, err
	}
	source := s.source()
	// A manual search may never move. Validate its loaded map identity too;
	// synthetic resource metadata must not let it bypass a missing catalog.
	if _, err := source.IsObstacle(w, controlPosition(w), 0, false, false); err != nil {
		return report, fmt.Errorf("control scene resources: %w", err)
	}
	// All nested scripts operate on this detached session. A late unsupported
	// instruction, bad input, collision-resource error or unused input rolls back
	// the complete frame, including earlier successful triggers and timers.
	shadow := *s
	shadow.current = s.Snapshot()
	a := controlAction{s: &shadow, source: source, inputs: append([]palflow.Input(nil), inputs...), report: &report}
	report.Before, report.BeforeFrame = controlPosition(w), w.FrameNum
	defer func() {
		report.InputsConsumed, report.Work = a.consumed, a.work
		report.UnusedInputs = append([]palflow.Input(nil), a.inputs[a.consumed:]...)
		report.State = shadow.Snapshot()
		report.After, report.AfterFrame = controlPosition(shadow.current.World), shadow.current.World.FrameNum
	}()
	if err := a.spend(1); err != nil {
		return report, err
	}
	touched, err := a.gameUpdate()
	if err != nil {
		return report, err
	}
	w = shadow.current.World
	if !w.SceneChanged {
		if w.SceneLoadPending || w.InBattle {
			return report, errors.New("world update left a pending resource load or battle before party control")
		}
		if err := a.updateParty(direction); err != nil {
			return report, err
		}
		if err := a.present(); err != nil {
			return report, err
		}
		if kind == "interact" {
			report.SearchSuppressed = touched
			if !touched {
				if err := a.search(); err != nil {
					return report, err
				}
			}
		}
	}
	if a.consumed != len(a.inputs) {
		return report, errors.New("unused inputs; control action was not committed")
	}
	syncControlPosition(shadow.current.World)
	shadow.current.NeedsSceneEnter = shadow.current.World.SceneChanged
	shadow.current.Actions = s.current.Actions + 1
	s.current = shadow.current
	report.Completed = true
	return report, nil
}

func (a *controlAction) spend(n int) error {
	if n < 0 || n > a.s.maxSteps-a.work {
		return errors.New("control action exceeds the combined execution budget")
	}
	a.work += n
	return nil
}

// PAL_StartFrame, touch animation and manual search each have a separate
// MakeScene + full-screen update call site. Requests consume wave/shake/fade
// state even though this logical control model does not render pixels.
func (a *controlAction) present() error {
	updates, err := palflow.ApplyScenePresentation(a.s.current.World, a.source)
	if err != nil {
		return fmt.Errorf("control scene redraw: %w", err)
	}
	a.report.ScreenUpdates += updates
	return nil
}

func controlPosition(w *palflow.WorldState) palflow.WorldPosition {
	return palflow.WorldPosition{X: uint16(w.ViewportX + w.PartyOffsetX), Y: uint16(w.ViewportY + w.PartyOffsetY)}
}

func syncControlPosition(w *palflow.WorldState) {
	p := controlPosition(w)
	w.PartyX, w.PartyY = int(int16(p.X)), int(int16(p.Y))
}

func controlOffsets(direction uint16) (int, int) {
	dx, dy := 16, 8
	if direction == 0 || direction == 1 {
		dx = -dx
	}
	if direction == 1 || direction == 2 {
		dy = -dy
	}
	return dx, dy
}

func controlAbs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (a *controlAction) updateParty(direction uint16) error {
	w := a.s.current.World
	walking := false
	if direction <= 3 {
		dx, dy := controlOffsets(direction)
		x, y := w.ViewportX+w.PartyOffsetX, w.ViewportY+w.PartyOffsetY
		w.Direction = direction
		blocked, err := a.source.IsObstacle(w, palflow.WorldPosition{X: uint16(x + dx), Y: uint16(y + dy)}, 0, true, true)
		if err != nil {
			return fmt.Errorf("party movement collision: %w", err)
		}
		a.report.Blocked = blocked
		if !blocked {
			if err := a.spend(1); err != nil {
				return err
			}
			if len(w.Trail) < 5 {
				return errors.New("party movement requires five trail entries")
			}
			copy(w.Trail[1:5], w.Trail[:4])
			w.Trail[0] = palflow.WorldTrail{X: uint16(x), Y: uint16(y), Direction: direction}
			w.ViewportX, w.ViewportY = int(int16(w.ViewportX+dx)), int(int16(w.ViewportY+dy))
			walking, a.report.Moved = true, true
		}
	}
	if err := palflow.UpdatePartyGestures(a.source, w, walking); err != nil {
		return fmt.Errorf("party control gestures: %w", err)
	}
	syncControlPosition(w)
	return nil
}

// gameUpdate follows play.c PAL_GameUpdate(TRUE), including the independent
// trigger and auto passes, offscreen restoration and NPC collision displacement.
func (a *controlAction) gameUpdate() (touched bool, err error) {
	if err := a.spend(1); err != nil {
		return false, err
	}
	ids, err := palflow.SceneEventIDs(a.s.current.World, "control update")
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		w := a.s.current.World
		e := w.Events[id]
		if e.VanishTime != 0 {
			if e.VanishTime < 0 {
				e.VanishTime++
			} else {
				e.VanishTime--
			}
			w.Events[id] = e
			continue
		}
		if e.State < 0 {
			if int(e.X) < w.ViewportX || int(e.X) > w.ViewportX+320 || int(e.Y) < w.ViewportY || int(e.Y) > w.ViewportY+320 {
				e.State, e.Frame = int16(-int(e.State)), 0
				w.Events[id] = e
			}
		} else if e.State > 0 && e.TriggerMode >= 4 {
			dx, dy := w.ViewportX+w.PartyOffsetX-int(e.X), w.ViewportY+w.PartyOffsetY-int(e.Y)
			if controlAbs(dx)+controlAbs(dy)*2 < (int(e.TriggerMode)-4)*32+16 {
				if e.SpriteFrames != 0 {
					e.Frame = 0
					if dx > 0 {
						e.Direction = 2
						if dy > 0 {
							e.Direction = 3
						}
					} else {
						e.Direction = 1
						if dy > 0 {
							e.Direction = 0
						}
					}
					w.Events[id] = e
					if err := palflow.UpdatePartyGestures(a.source, w, false); err != nil {
						return touched, err
					}
					if err := a.present(); err != nil {
						return touched, err
					}
				}
				if err := a.trigger(id); err != nil {
					return touched, err
				}
				touched = true
				if a.s.current.World.SceneChanged {
					return touched, nil
				}
			}
		}
	}
	for _, id := range ids {
		w := a.s.current.World
		e := w.Events[id]
		if e.State > 0 && e.VanishTime == 0 && e.AutoScript != 0 {
			if err := a.auto(id); err != nil {
				return touched, err
			}
			w = a.s.current.World
			if w.SceneChanged {
				return touched, nil
			}
			e = w.Events[id]
		}
		if e.State >= 2 && e.Sprite != 0 && controlAbs(int(e.X)-w.ViewportX-w.PartyOffsetX)+controlAbs(int(e.Y)-w.ViewportY-w.PartyOffsetY)*2 <= 12 {
			direction := (e.Direction + 1) % 4
			for attempt := 0; attempt < 4; attempt++ {
				dx, dy := controlOffsets(direction)
				pos := palflow.WorldPosition{X: uint16(w.ViewportX + w.PartyOffsetX + dx), Y: uint16(w.ViewportY + w.PartyOffsetY + dy)}
				blocked, err := a.source.IsObstacle(w, pos, 0, true, true)
				if err != nil {
					return touched, fmt.Errorf("NPC displacement collision: %w", err)
				}
				if !blocked {
					if err := a.spend(1); err != nil {
						return touched, err
					}
					w.ViewportX, w.ViewportY = int(int16(int(int16(pos.X))-w.PartyOffsetX)), int(int16(int(int16(pos.Y))-w.PartyOffsetY))
					syncControlPosition(w)
					break
				}
				direction = (direction + 1) % 4
			}
		}
	}
	w := a.s.current.World
	w.ChaseCycles--
	if w.ChaseCycles == 0 {
		w.ChaseRange = 1
	}
	w.FrameNum++
	return touched, nil
}

func (a *controlAction) trigger(id uint16) error {
	w := a.s.current.World
	event, ok := w.Events[id]
	if !ok {
		return fmt.Errorf("control trigger event %d is absent", id)
	}
	remaining := a.s.maxSteps - a.work
	if remaining == 0 && event.Trigger != 0 {
		return errors.New("control trigger exceeds the combined execution budget")
	}
	textRemaining := (1 << 20) - a.textBytes
	runner, err := palflow.New(a.source, palflow.Options{MaxSteps: max(1, remaining), MaxCallDepth: 64, MaxTextBytes: max(1, textRemaining)})
	if err != nil {
		return err
	}
	report, err := runner.Trace(palflow.Request{Entry: event.Trigger, EventID: id, LastEventID: a.s.current.LastEventID, IdleFrames: maps.Clone(a.s.current.IdleFrames), World: w, Inputs: a.inputs[a.consumed:]})
	a.report.TriggeredEvents = append(a.report.TriggeredEvents, id)
	a.report.Triggers = append(a.report.Triggers, report)
	if err != nil {
		return err
	}
	if !report.Completed || report.State.World == nil {
		return errors.New("control trigger did not complete with a world snapshot")
	}
	if report.State.TextBytes > textRemaining {
		return errors.New("control action exceeds the cumulative dialogue byte budget")
	}
	a.textBytes += report.State.TextBytes
	if err := a.spend(report.State.ExecutedSteps + report.State.PlayerSubsteps + report.State.WorldTicks + report.State.AutoSubsteps + report.State.CombatWork + report.State.MediaWork); err != nil {
		return err
	}
	if report.InputsConsumed < 0 || report.InputsConsumed > len(a.inputs)-a.consumed {
		return errors.New("control trigger returned an invalid consumed input count")
	}
	a.consumed += report.InputsConsumed
	next := palflow.CloneWorld(report.State.World)
	event, ok = next.Events[id]
	if !ok {
		return fmt.Errorf("control trigger event %d disappeared", id)
	}
	event.Trigger = report.State.ReturnedEntry
	next.Events[id] = event
	a.s.current.World, a.s.current.ScriptSuccess = next, report.State.ScriptSuccess
	a.s.current.LastEventID, a.s.current.IdleFrames = report.State.LastEventID, maps.Clone(report.State.IdleFrames)
	a.s.current.NeedsSceneEnter = next.SceneChanged
	return nil
}

func (a *controlAction) auto(id uint16) error {
	runner, err := palflow.New(a.source, palflow.Options{MaxSteps: a.s.maxSteps, MaxCallDepth: 64, MaxTextBytes: 1 << 20})
	if err != nil {
		return err
	}
	w := a.s.current.World
	state := palflow.State{TextBytes: a.textBytes, CurEquipPart: -1, World: w, ScriptSuccess: a.s.current.ScriptSuccess, LastEventID: a.s.current.LastEventID, IdleFrames: maps.Clone(a.s.current.IdleFrames), InputIndex: a.consumed, ExecutedSteps: a.work, Music: w.Music, MusicKnown: w.MusicKnown, BattleMusic: w.BattleMusic, BattleMusicKnown: w.BattleMusicKnown}
	steps, err := runner.RunAutoScript(&state, id, a.inputs)
	a.report.AutoSteps = append(a.report.AutoSteps, steps...)
	if err != nil {
		return err
	}
	a.work = state.ExecutedSteps + state.PlayerSubsteps + state.WorldTicks + state.AutoSubsteps + state.CombatWork + state.MediaWork
	if a.work > a.s.maxSteps || state.InputIndex < a.consumed || state.InputIndex > len(a.inputs) || state.World == nil {
		return errors.New("auto script returned invalid control execution state")
	}
	a.consumed = state.InputIndex
	a.textBytes = state.TextBytes
	a.s.current.World, a.s.current.ScriptSuccess = state.World, state.ScriptSuccess
	a.s.current.LastEventID, a.s.current.IdleFrames = state.LastEventID, maps.Clone(state.IdleFrames)
	a.s.current.NeedsSceneEnter = state.World.SceneChanged
	return nil
}

func (a *controlAction) search() error {
	w := a.s.current.World
	x, y := w.ViewportX+w.PartyOffsetX, w.ViewportY+w.PartyOffsetY
	dx, dy := controlOffsets(w.Direction)
	positions := make([]palflow.WorldPosition, 13)
	positions[0] = palflow.WorldPosition{X: uint16(x), Y: uint16(y)}
	for i := 0; i < 4; i++ {
		positions[i*3+1] = palflow.WorldPosition{X: uint16(x + dx), Y: uint16(y + dy)}
		positions[i*3+2] = palflow.WorldPosition{X: uint16(x), Y: uint16(y + dy*2)}
		positions[i*3+3] = palflow.WorldPosition{X: uint16(x + dx*2), Y: uint16(y)}
		x, y = x+dx, y+dy
	}
	a.report.SearchPositions = positions
	ids, err := palflow.SceneEventIDs(w, "manual search")
	if err != nil {
		return err
	}
	for i, p := range positions {
		x, y := int(int16(p.X)), int(int16(p.Y))
		half := x%32 != 0
		for _, id := range ids {
			e := w.Events[id]
			if e.State <= 0 || e.TriggerMode >= 4 || int(e.TriggerMode)*6-4 <= i || x/32 != int(e.X)/32 || y/16 != int(e.Y)/16 || half != (e.X%32 != 0) {
				continue
			}
			if int(e.SpriteFrames)*4 > int(e.Frame) {
				e.Frame, e.Direction = 0, (w.Direction+2)%4
				w.Events[id] = e
				if len(w.PartyFrames) < len(w.PartyRoles) {
					return errors.New("manual search requires every party member gesture")
				}
				for member := range w.PartyRoles {
					w.PartyFrames[member] = w.Direction * 3
				}
				if err := a.present(); err != nil {
					return err
				}
			}
			a.report.SearchEventID = id
			return a.trigger(id)
		}
	}
	return nil
}
