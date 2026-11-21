package palgame

import (
	"fmt"

	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

type PathOptions struct {
	MaxNodes          int
	AllowedTouchEvent uint16
}

// PlanPath searches the current collision snapshot. It returns direction keys,
// never a position mutation. Callers must execute and verify each Move because
// moving NPCs and scripts may invalidate the planned route. Other touch events
// are avoided so a route does not unknowingly cross an unrelated portal.
func PlanPath(source palflow.ObstacleSource, world *palflow.WorldState, target palflow.WorldPosition, options PathOptions) ([]uint16, error) {
	if source == nil || world == nil {
		return nil, fmt.Errorf("path planning requires loaded collision state")
	}
	limit := options.MaxNodes
	if limit == 0 {
		limit = 65536
	}
	if limit < 1 || limit > 65536 {
		return nil, fmt.Errorf("path search limit must be 1..65536")
	}
	ids, err := palflow.SceneEventIDs(world, "path planning")
	if err != nil {
		return nil, err
	}
	goal := func(p palflow.WorldPosition) bool { return p == target }
	if options.AllowedTouchEvent != 0 {
		e, ok := world.Events[options.AllowedTouchEvent]
		if !ok || e.State <= 0 || e.VanishTime != 0 || e.TriggerMode < 4 || e.X != target.X || e.Y != target.Y {
			return nil, fmt.Errorf("path touch goal must identify an active marker at the requested position")
		}
		member := false
		for _, id := range ids {
			member = member || id == options.AllowedTouchEvent
		}
		if !member {
			return nil, fmt.Errorf("path touch goal belongs to another scene")
		}
		goal = func(p palflow.WorldPosition) bool {
			return controlAbs(int(int16(p.X))-int(e.X))+controlAbs(int(int16(p.Y))-int(e.Y))*2 < (int(e.TriggerMode)-4)*32+16
		}
	}
	start := controlPosition(world)
	if _, err := source.IsObstacle(world, start, 0, false, false); err != nil {
		return nil, err
	}
	if goal(start) {
		return []uint16{}, nil
	}
	if blocked, err := source.IsObstacle(world, target, 0, true, true); err != nil || (options.AllowedTouchEvent == 0 && blocked) {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("path target is blocked")
	}
	type edge struct {
		previous  palflow.WorldPosition
		direction uint16
	}
	parents := map[palflow.WorldPosition]edge{start: {}}
	queue := []palflow.WorldPosition{start}
	for head := 0; head < len(queue); head++ {
		current := queue[head]
		for direction := uint16(0); direction < 4; direction++ {
			dx, dy := controlOffsets(direction)
			point := palflow.WorldPosition{X: current.X + uint16(dx), Y: current.Y + uint16(dy)}
			if _, seen := parents[point]; seen {
				continue
			}
			blocked, err := source.IsObstacle(world, point, 0, true, true)
			if err != nil {
				return nil, err
			}
			if blocked {
				continue
			}
			unsafe := false
			for _, id := range ids {
				if id == options.AllowedTouchEvent {
					continue
				}
				e := world.Events[id]
				if e.State > 0 && e.VanishTime == 0 && e.TriggerMode >= 4 && controlAbs(int(int16(point.X))-int(e.X))+controlAbs(int(int16(point.Y))-int(e.Y))*2 < (int(e.TriggerMode)-4)*32+16 {
					unsafe = true
					break
				}
			}
			if unsafe {
				continue
			}
			if len(parents) >= limit {
				return nil, fmt.Errorf("path search exhausted %d collision nodes", limit)
			}
			parents[point] = edge{current, direction}
			if goal(point) {
				var path []uint16
				for p := point; p != start; {
					e := parents[p]
					path = append(path, e.direction)
					p = e.previous
				}
				for a, b := 0, len(path)-1; a < b; a, b = a+1, b-1 {
					path[a], path[b] = path[b], path[a]
				}
				return path, nil
			}
			queue = append(queue, point)
		}
	}
	return nil, fmt.Errorf("no collision-safe path in the current snapshot")
}
