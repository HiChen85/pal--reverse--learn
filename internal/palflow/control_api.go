package palflow

import (
	"fmt"
	"maps"
)

func UpdatePartyGestures(source Source, world *WorldState, walking bool) error {
	return updatePartyGestures(source, world, walking)
}

func SceneEventIDs(world *WorldState, context string) ([]uint16, error) {
	return sceneEventIDs(world, context)
}

// RunAutoScript executes one event's auto invocation without adding a game
// frame or applying trigger/touch processing. Controllers charge those steps.
// The entire invocation is detached, including nested triggers and input use.
func (r *Runner) RunAutoScript(state *State, eventID uint16, inputs []Input) ([]AutoStep, error) {
	if r == nil || state == nil || state.World == nil {
		return nil, fmt.Errorf("auto invocation requires a runner and world state")
	}
	event, ok := state.World.Events[eventID]
	if !ok || eventID == 0 {
		return nil, fmt.Errorf("auto event %d missing", eventID)
	}
	if event.AutoScript == 0 {
		return nil, nil
	}
	working := *state
	if working.Random == nil {
		if provider, ok := r.source.(ScriptRandomProvider); ok {
			working.Random = provider.ScriptRandom()
		}
	}
	working.World = CloneWorld(state.World)
	working.Frames = append([]Frame(nil), state.Frames...)
	working.IdleFrames = maps.Clone(state.IdleFrames)
	if working.IdleFrames == nil {
		working.IdleFrames = map[uint16]uint16{}
	}
	dummy := len(working.Frames) == 0
	if dummy {
		working.Frames = []Frame{{PC: 1, EventID: eventID}}
	}
	scheduler := autoScheduler{runner: r, state: &working, inputs: inputs}
	if err := scheduler.syncRemaining(eventID, event.AutoScript); err != nil {
		return nil, err
	}
	if err := scheduler.run(eventID, 1); err != nil {
		return nil, err
	}
	if dummy {
		working.Frames = nil
	}
	*state = working
	return scheduler.steps, nil
}
