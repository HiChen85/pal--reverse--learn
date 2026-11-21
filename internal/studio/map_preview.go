package studio

import (
	"errors"
	"fmt"

	"github.com/HiChen85/pal--reverse--learn/internal/content"
)

// MapPreview tracks a position on a map from the applied pack. Creating or
// moving a preview leaves the story session untouched.
type MapPreview struct {
	tileMap  content.TileMap
	position content.MapPoint
}

func (s *Studio) NewMapPreview(mapID string) (*MapPreview, error) {
	for _, tileMap := range s.pack.Maps {
		if tileMap.ID == mapID {
			return &MapPreview{tileMap: tileMap, position: tileMap.Spawn}, nil
		}
	}
	return nil, fmt.Errorf("unknown map %q", mapID)
}

func (p *MapPreview) Position() content.MapPoint { return p.position }

// Move takes one cardinal step. A blocked or invalid move keeps the current
// position and event.
func (p *MapPreview) Move(dx, dy int) error {
	horizontal := dy == 0 && (dx == -1 || dx == 1)
	vertical := dx == 0 && (dy == -1 || dy == 1)
	if !horizontal && !vertical {
		return errors.New("move must be one cardinal step")
	}
	next := content.MapPoint{X: p.position.X + dx, Y: p.position.Y + dy}
	if !p.tileMap.Walkable(next) {
		return fmt.Errorf("tile (%d, %d) is blocked", next.X, next.Y)
	}
	p.position = next
	return nil
}

// EventNode returns the node at the current tile. Call Studio.Start to enter it.
func (p *MapPreview) EventNode() (string, bool) {
	event, ok := p.tileMap.EventAt(p.position)
	return event.Node, ok
}
