package content

import (
	"fmt"
	"slices"
)

const (
	MaxMaps      = 64
	MaxMapWidth  = 32
	MaxMapHeight = 24
	MaxMapEvents = 128
)

// TileMap defines an authored grid of walkable ground (.) and walls (#).
type TileMap struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Rows   []string   `json:"rows"`
	Spawn  MapPoint   `json:"spawn"`
	Events []MapEvent `json:"events,omitempty"`
}

// MapPoint uses zero-based coordinates, with X selecting the column and Y the row.
type MapPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// MapEvent links a walkable tile to a story node.
type MapEvent struct {
	Position MapPoint `json:"position"`
	Node     string   `json:"node"`
}

// Walkable reports whether p selects a ground tile.
func (m TileMap) Walkable(p MapPoint) bool {
	if p.Y < 0 || p.Y >= len(m.Rows) {
		return false
	}
	row := m.Rows[p.Y]
	return p.X >= 0 && p.X < len(row) && row[p.X] == '.'
}

// EventAt returns the event placed at p, if any.
func (m TileMap) EventAt(p MapPoint) (MapEvent, bool) {
	for _, event := range m.Events {
		if event.Position == p {
			return event, true
		}
	}
	return MapEvent{}, false
}

func validateMaps(maps []TileMap, nodes map[string]bool) error {
	if len(maps) > MaxMaps {
		return fmt.Errorf("pack exceeds %d maps", MaxMaps)
	}
	ids := make(map[string]bool, len(maps))
	for _, m := range maps {
		if err := validID(m.ID, "map"); err != nil {
			return err
		}
		if ids[m.ID] {
			return fmt.Errorf("duplicate map ID %q", m.ID)
		}
		ids[m.ID] = true
		if err := m.validate(nodes); err != nil {
			return fmt.Errorf("map %s: %w", m.ID, err)
		}
	}
	return nil
}

func (m TileMap) validate(nodes map[string]bool) error {
	if err := validText(m.Name, "name", 256, true); err != nil {
		return err
	}
	if len(m.Rows) < 1 || len(m.Rows) > MaxMapHeight {
		return fmt.Errorf("height must be 1–%d", MaxMapHeight)
	}
	width := len(m.Rows[0])
	if width < 1 || width > MaxMapWidth {
		return fmt.Errorf("width must be 1–%d", MaxMapWidth)
	}
	for y, row := range m.Rows {
		if len(row) != width {
			return fmt.Errorf("row %d must have width %d", y, width)
		}
		for x := range len(row) {
			if row[x] != '.' && row[x] != '#' {
				return fmt.Errorf("invalid tile at (%d, %d): use '.' or '#'", x, y)
			}
		}
	}
	if !m.Walkable(m.Spawn) {
		return fmt.Errorf("spawn (%d, %d) must be on a walkable tile", m.Spawn.X, m.Spawn.Y)
	}
	if len(m.Events) == 0 {
		return nil
	}
	if len(m.Events) > MaxMapEvents {
		return fmt.Errorf("too many events (max %d)", MaxMapEvents)
	}
	reachable := m.reachableFromSpawn()
	positions := make(map[MapPoint]bool, len(m.Events))
	for _, event := range m.Events {
		p := event.Position
		if !m.Walkable(p) {
			return fmt.Errorf("event at (%d, %d) must be on a walkable tile", p.X, p.Y)
		}
		if positions[p] {
			return fmt.Errorf("duplicate event position (%d, %d)", p.X, p.Y)
		}
		positions[p] = true
		if !nodes[event.Node] {
			return fmt.Errorf("event at (%d, %d) references missing node %q", p.X, p.Y, event.Node)
		}
		if !reachable[p] {
			return fmt.Errorf("event at (%d, %d) is unreachable from spawn", p.X, p.Y)
		}
	}
	return nil
}

func (m TileMap) reachableFromSpawn() map[MapPoint]bool {
	directions := [...]MapPoint{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}}
	reachable := map[MapPoint]bool{m.Spawn: true}
	pending := []MapPoint{m.Spawn}
	for i := 0; i < len(pending); i++ {
		p := pending[i]
		for _, direction := range directions {
			next := MapPoint{X: p.X + direction.X, Y: p.Y + direction.Y}
			if m.Walkable(next) && !reachable[next] {
				reachable[next] = true
				pending = append(pending, next)
			}
		}
	}
	return reachable
}

func cloneMaps(maps []TileMap) []TileMap {
	cloned := slices.Clone(maps)
	for i := range cloned {
		cloned[i].Rows = slices.Clone(cloned[i].Rows)
		cloned[i].Events = slices.Clone(cloned[i].Events)
	}
	return cloned
}
