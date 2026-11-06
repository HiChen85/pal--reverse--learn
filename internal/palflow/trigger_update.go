package palflow

import "fmt"

type WorldUpdateRecord struct {
	Tick         uint16      `json:"tick"`
	BeforeFrame  uint32      `json:"before_frame"`
	AfterFrame   uint32      `json:"after_frame"`
	TriggerSteps []TraceStep `json:"trigger_steps,omitempty"`
	AutoSteps    []AutoStep  `json:"auto_steps,omitempty"`
}

// updateTriggeredTick is the trigger-enabled PAL_GameUpdate order used by
// opcode 9. The caller owns a detached state and has reserved every wait tick
// plus the enclosing instruction. Nested triggers share its input, depth,
// dialogue and work budgets; a scene change returns before autos/frame counters.
func (r *Runner) updateTriggeredTick(s *State, scheduler *autoScheduler, inputs []Input, tick uint16) ([]TraceStep, error) {
	var calls []TraceStep
	invoke := func(entry, event uint16) error {
		trace, err := r.runTriggerInline(s, entry, event, inputs, scheduler.reserved)
		if err != nil {
			return err
		}
		calls = append(calls, trace...)
		return scheduler.syncRemaining(event, entry)
	}
	if s.World.SceneChanged {
		if s.World.SceneLoadPending {
			return nil, fmt.Errorf("trigger-enabled update requires pending scene resources to be loaded")
		}
		scene := s.World.Scene
		record, ok := s.World.Scenes[scene]
		if !ok {
			return nil, fmt.Errorf("scene OnEnter missing")
		}
		s.World.SceneChanged = false
		if err := invoke(record.OnEnter, 0xffff); err != nil {
			return nil, err
		}
		record = s.World.Scenes[scene]
		record.OnEnter = s.LastInlineReturn
		s.World.Scenes[scene] = record
		if s.World.SceneChanged {
			return calls, nil
		}
	}
	ids, err := sceneEventIDs(s.World, "trigger-enabled update")
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		w := s.World
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
				e.State = int16(-int(e.State))
				e.Frame = 0
				w.Events[id] = e
			}
			continue
		}
		if e.State <= 0 || e.TriggerMode < 4 {
			continue
		}
		dx, dy := w.ViewportX+w.PartyOffsetX-int(e.X), w.ViewportY+w.PartyOffsetY-int(e.Y)
		if absAutoOffset(dx)+absAutoOffset(dy)*2 >= (int(e.TriggerMode)-4)*32+16 {
			continue
		}
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
			if err := updatePartyGestures(r.source, w, false); err != nil {
				return nil, err
			}
		}
		if err := invoke(e.Trigger, id); err != nil {
			return nil, err
		}
		e = s.World.Events[id]
		e.Trigger = s.LastInlineReturn
		s.World.Events[id] = e
		if s.World.SceneChanged {
			return calls, nil
		}
	}
	for _, id := range ids {
		w := s.World
		e := w.Events[id]
		if e.State > 0 && e.VanishTime == 0 && e.AutoScript != 0 {
			if err := scheduler.syncRemaining(id, e.AutoScript); err != nil {
				return nil, err
			}
			if err := scheduler.run(id, tick); err != nil {
				return nil, err
			}
			w = s.World
			if w.SceneChanged {
				return calls, nil
			}
			e = w.Events[id]
		}
		if e.State < 2 || e.Sprite == 0 || absAutoOffset(int(e.X)-w.ViewportX-w.PartyOffsetX)+absAutoOffset(int(e.Y)-w.ViewportY-w.PartyOffsetY)*2 > 12 {
			continue
		}
		collision, ok := r.source.(ObstacleSource)
		if !ok {
			return nil, fmt.Errorf("NPC displacement requires loaded collision resources")
		}
		for n := uint16(1); n <= 4; n++ {
			direction := (e.Direction + n) % 4
			position := WorldPosition{X: uint16(w.ViewportX + w.PartyOffsetX), Y: uint16(w.ViewportY + w.PartyOffsetY)}
			dx, dy := 16, 8
			if direction == 0 || direction == 1 {
				dx = -dx
			}
			if direction == 1 || direction == 2 {
				dy = -dy
			}
			position.X += uint16(dx)
			position.Y += uint16(dy)
			blocked, err := collision.IsObstacle(w, position, 0, true, true)
			if err != nil {
				return nil, err
			}
			if blocked {
				continue
			}
			if err := scheduler.spend(1, id, e.AutoScript); err != nil {
				return nil, err
			}
			w.ViewportX = int(int16(int(int16(position.X)) - w.PartyOffsetX))
			w.ViewportY = int(int16(int(int16(position.Y)) - w.PartyOffsetY))
			w.PartyX, w.PartyY = int(int16(position.X)), int(int16(position.Y))
			break
		}
	}
	advanceWorldCounters(s.World, 1)
	return calls, nil
}
