package palflow

import "fmt"

// SpriteResource is decode evidence, not rendered pixels or a guarantee that
// every RLE frame is safe to display. IDs refer to MGO's direct chunk indexes.
type SpriteResource struct {
	ID            uint16 `json:"id"`
	ArchiveSHA256 string `json:"archive_sha256"`
	SHA256        string `json:"sha256"`
	DecodedSize   int    `json:"decoded_size"`
	FrameCount    uint16 `json:"frame_count"`
	UnsafeFrames  int    `json:"unsafe_frames"`
}

type SpriteResourceSource interface {
	SpriteResource(id uint16) (SpriteResource, error)
}

// PAL_LoadResources(kLoadPlayerSprite) reloads the whole current party and
// followers, not just the role changed by 0x65. Scene loads are separate work;
// a pending scene load must never be cleared by pretending only sprites loaded.
func (r *Runner) evaluateResources(state *State, step *TraceStep, inst Instruction) (bool, error) {
	if inst.Opcode != 0x65 || inst.Operands[2] == 0 {
		return false, nil
	}
	if state == nil || len(state.Frames) == 0 || step == nil {
		return true, fmt.Errorf("sprite reload requires state, frame and trace step")
	}
	if state.World == nil {
		return true, fmt.Errorf("sprite reload requires an explicit world snapshot")
	}
	if !state.World.InBattle && state.World.SceneLoadPending {
		return true, fmt.Errorf("sprite reload also has a pending scene/map load, which is not modeled")
	}
	working := *state
	working.Frames = append([]Frame(nil), state.Frames...)
	workingStep := *step
	plain := inst
	plain.Operands[2] = 0
	if _, err := evaluateWorld(&working, &workingStep, plain); err != nil {
		return true, err
	}
	if working.World.InBattle {
		workingStep.Effect = "set player sprite ID; battle context suppresses resource loading"
		*state = working
		*step = workingStep
		return true, nil
	}
	w := working.World
	if err := r.loadPartySprites(w); err != nil {
		return true, err
	}
	workingStep.Resources = append([]SpriteResource(nil), w.PlayerResources...)
	workingStep.Effect = "set role sprite ID and decode every current party/follower MGO sprite; retain metadata only, no rendering"
	*state = working
	*step = workingStep
	return true, nil
}

func (r *Runner) loadPartySprites(w *WorldState) error {
	if w.SceneLoadPending {
		return fmt.Errorf("player reload also has a pending scene/map resource load")
	}
	source, ok := r.source.(SpriteResourceSource)
	if !ok {
		return fmt.Errorf("player reload requires a local MGO resource source")
	}
	if len(w.PartyRoles) < 1 || len(w.PartyRoles)+len(w.FollowerSprites) > 5 {
		return fmt.Errorf("sprite reload requires a party/follower snapshot with 1..5 members")
	}
	ids := make([]uint16, 0, 5)
	for _, role := range w.PartyRoles {
		raw, err := playerRole(w, role)
		if err != nil {
			return err
		}
		id := raw[2]
		if mirror, ok := w.PlayerSprites[role]; ok && mirror != id {
			return fmt.Errorf("role %d sprite mirror differs from canonical attribute", role)
		}
		ids = append(ids, id)
	}
	ids = append(ids, w.FollowerSprites...)
	loaded := make([]SpriteResource, 0, len(ids))
	for _, id := range ids {
		info, err := source.SpriteResource(id)
		if err != nil {
			return err
		}
		if info.ID != id || info.DecodedSize < 2 || info.FrameCount == 0 {
			return fmt.Errorf("sprite %d has no usable decoded frame header", id)
		}
		loaded = append(loaded, info)
	}
	w.PlayerResources = loaded
	return nil
}
