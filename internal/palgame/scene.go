package palgame

import (
	"fmt"

	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

// LoadResources mirrors the two independent resource flags. It does not enter
// the scene or advance time. Everything is prepared on a detached snapshot, so
// a failed MAP, GOP or MGO decode cannot partly replace a live session's scene.
func (s Source) LoadResources(world *palflow.WorldState, scene, players bool) (*palflow.WorldState, error) {
	if world == nil {
		return nil, fmt.Errorf("resource load requires a world snapshot")
	}
	w := palflow.CloneWorld(world)
	if scene {
		if s.Maps == nil {
			return nil, fmt.Errorf("scene %d needs local MAP.MKF and GOP.MKF catalogs", w.Scene)
		}
		current, ok := w.Scenes[w.Scene]
		if !ok || w.Scene == 0 {
			return nil, fmt.Errorf("scene %d is absent from scene table", w.Scene)
		}
		next, ok := w.Scenes[w.Scene+1]
		if !ok || next.EventStart < current.EventStart {
			return nil, fmt.Errorf("scene %d has no valid following event boundary", w.Scene)
		}
		m, err := s.Maps.Map(current.Map)
		if err != nil {
			return nil, err
		}
		info := m.Info()
		resources := &palflow.SceneResourceState{SceneID: w.Scene, MapID: current.Map, MAPArchiveSHA256: s.Maps.MAPSourceSHA256(), GOPArchiveSHA256: s.Maps.GOPSourceSHA256(), MapSHA256: info.SHA256, GOPSHA256: info.GOPSHA256, EventSprites: map[uint16]palflow.SpriteResource{}}
		for n := int(current.EventStart) + 1; n <= int(next.EventStart); n++ {
			id := uint16(n)
			event, ok := w.Events[id]
			if !ok {
				return nil, fmt.Errorf("scene %d event %d missing", w.Scene, id)
			}
			if event.Sprite == 0 {
				continue
			}
			sprite, err := s.SpriteResource(event.Sprite)
			if err != nil {
				return nil, fmt.Errorf("scene %d event %d: %w", w.Scene, id, err)
			}
			if sprite.DecodedSize < 2 {
				return nil, fmt.Errorf("event %d sprite has no frame header", id)
			}
			event.SpriteFramesAuto = sprite.FrameCount
			w.Events[id] = event
			resources.EventSprites[id] = sprite
		}
		if w.SceneChanged {
			w.ScreenWave, w.WaveProgression = 0, 0
		}
		w.PartyOffsetX, w.PartyOffsetY = 160, 112
		w.SceneResources = resources
		w.SceneLoadPending = false
	}
	if players {
		if len(w.PartyRoles) == 0 || len(w.PartyRoles)+len(w.FollowerSprites) > 5 {
			return nil, fmt.Errorf("player resource load requires 1..5 party/follower slots")
		}
		ids := make([]uint16, 0, 5)
		for _, role := range w.PartyRoles {
			raw, ok := w.Roles[role]
			if role >= 6 || !ok {
				return nil, fmt.Errorf("party role %d missing", role)
			}
			if mirror, ok := w.PlayerSprites[role]; ok && mirror != raw[2] {
				return nil, fmt.Errorf("role %d sprite cache differs from canonical attributes", role)
			}
			ids = append(ids, raw[2])
		}
		ids = append(ids, w.FollowerSprites...)
		loaded := make([]palflow.SpriteResource, 0, len(ids))
		for _, id := range ids {
			info, err := s.SpriteResource(id)
			if err != nil {
				return nil, err
			}
			if info.FrameCount == 0 || info.DecodedSize < 2 {
				return nil, fmt.Errorf("party sprite %d has no usable frame header", id)
			}
			loaded = append(loaded, info)
		}
		w.PlayerResources = loaded
	}
	return w, nil
}

func (s Source) IsObstacle(w *palflow.WorldState, position palflow.WorldPosition, self uint16, checkEvents, checkRange bool) (bool, error) {
	if w == nil || s.Maps == nil || w.SceneResources == nil || w.SceneResources.SceneID != w.Scene || w.SceneLoadPending {
		return false, fmt.Errorf("collision needs the current loaded scene and MAP/GOP catalogs")
	}
	if checkRange {
		blocked, err := palflow.ObstacleOutOfRange(w, position)
		if err != nil || blocked {
			return blocked, err
		}
	}
	m, err := s.Maps.Map(w.SceneResources.MapID)
	if err != nil {
		return false, err
	}
	if w.SceneResources.MAPArchiveSHA256 != s.Maps.MAPSourceSHA256() || w.SceneResources.GOPArchiveSHA256 != s.Maps.GOPSourceSHA256() || w.SceneResources.MapSHA256 != m.DecodedSHA256() {
		return false, fmt.Errorf("loaded scene belongs to different map resources")
	}
	x, y, h := palflow.ObstacleTile(position)
	if m.IsBlocked(x, y, h) {
		return true, nil
	}
	if checkEvents {
		return palflow.CheckEventObstacle(w, position, self)
	}
	return false, nil
}
