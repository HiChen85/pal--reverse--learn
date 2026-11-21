package content

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sampleTileMap(id, node string) TileMap {
	return TileMap{
		ID:    id,
		Name:  "Rain Garden",
		Rows:  []string{".#..", ".###", "...."},
		Spawn: MapPoint{X: 0, Y: 0},
		Events: []MapEvent{
			{Position: MapPoint{X: 3, Y: 2}, Node: node},
		},
	}
}

func TestTileMapStandalonePersistence(t *testing.T) {
	p := Demo()
	m := sampleTileMap("garden", p.Start)
	p.Maps = []TileMap{m}
	path := filepath.Join(t.TempDir(), "chapter.json")
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Maps, p.Maps) {
		t.Fatalf("saved maps changed: got %#v, want %#v", got.Maps, p.Maps)
	}
	for _, point := range []MapPoint{{X: -1}, {Y: -1}, {X: 4}, {Y: 3}, {X: 1}} {
		if m.Walkable(point) {
			t.Errorf("walkable wall or out-of-bounds position: %+v", point)
		}
	}
	if !m.Walkable(m.Spawn) || !m.Walkable(m.Events[0].Position) {
		t.Fatal("ground should be walkable")
	}
	if event, ok := m.EventAt(m.Events[0].Position); !ok || event != m.Events[0] {
		t.Fatalf("event lookup got %+v, %v", event, ok)
	}
	if _, ok := m.EventAt(m.Spawn); ok {
		t.Fatal("event lookup returned a marker at an empty position")
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	unknownField := strings.Replace(string(data), `"position":`, `"typo":0,"position":`, 1)
	if _, err := Decode([]byte(unknownField)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("decode accepted unknown event field or reported a different error: %v", err)
	}
}

func TestTileMapValidation(t *testing.T) {
	cases := []struct {
		name   string
		want   string
		mutate func(*TileMap)
	}{
		{"bad ID", "ID", func(m *TileMap) { m.ID = "bad map" }},
		{"blank name", "name", func(m *TileMap) { m.Name = " " }},
		{"no rows", "height", func(m *TileMap) { m.Rows = nil }},
		{"too tall", "height", func(m *TileMap) { m.Rows = make([]string, MaxMapHeight+1) }},
		{"no columns", "width", func(m *TileMap) { m.Rows = []string{""} }},
		{"too wide", "width", func(m *TileMap) { m.Rows = []string{strings.Repeat(".", MaxMapWidth+1)} }},
		{"ragged", "row 1", func(m *TileMap) { m.Rows[1] = "..." }},
		{"invalid tile", "invalid tile", func(m *TileMap) { m.Rows[1] = ".!##" }},
		{"non-ASCII tile", "invalid tile", func(m *TileMap) { m.Rows[1] = "雨#" }},
		{"spawn outside", "spawn", func(m *TileMap) { m.Spawn.Y = -1 }},
		{"spawn in wall", "spawn", func(m *TileMap) { m.Spawn.X = 1 }},
		{"too many events", "too many events", func(m *TileMap) { m.Events = make([]MapEvent, MaxMapEvents+1) }},
		{"duplicate event", "duplicate event", func(m *TileMap) { m.Events = append(m.Events, m.Events[0]) }},
		{"missing node", "missing node", func(m *TileMap) { m.Events[0].Node = "missing" }},
		{"event outside", "walkable tile", func(m *TileMap) { m.Events[0].Position.Y = 3 }},
		{"event in wall", "walkable tile", func(m *TileMap) { m.Events[0].Position.Y = 1 }},
		{"event on disconnected ground", "unreachable", func(m *TileMap) { m.Events[0].Position.Y = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Demo()
			m := sampleTileMap("garden", p.Start)
			tc.mutate(&m)
			p.Maps = []TileMap{m}
			data, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(data); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("decode error = %v, want %q", err, tc.want)
			}
		})
	}

	p := Demo()
	p.Maps = make([]TileMap, MaxMaps+1)
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("map count validation error = %v", err)
	}
	p.Maps = []TileMap{{
		ID: "diagonal", Name: "Diagonal", Rows: []string{".#", "#."},
		Events: []MapEvent{{Position: MapPoint{X: 1, Y: 1}, Node: p.Start}},
	}}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("diagonally adjacent event validation error = %v", err)
	}
}

func TestTileMapExtensionsAndCopies(t *testing.T) {
	base := Demo()
	base.Maps = []TileMap{sampleTileMap("garden", base.Start)}
	ext := Pack{
		SchemaVersion: SchemaVersion, ID: "extra-maps", Title: "Extra maps",
		Maps: []TileMap{sampleTileMap("terrace", base.Start)},
	}
	path := filepath.Join(t.TempDir(), "extension.json")
	if err := SaveExtension(path, ext, base); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadExtension(path, base)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Maps, ext.Maps) {
		t.Fatalf("saved extension maps changed: got %#v, want %#v", loaded.Maps, ext.Maps)
	}
	merged, err := Merge(base, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Maps) != 2 || merged.Maps[0].ID != "garden" || merged.Maps[1].ID != "terrace" {
		t.Fatalf("merge changed map order or content: %#v", merged.Maps)
	}
	clone := Clone(merged)
	for i := range clone.Maps {
		clone.Maps[i].Name = "changed"
		clone.Maps[i].Rows[0] = "####"
		clone.Maps[i].Events[0].Node = "changed"
	}
	if !reflect.DeepEqual(merged.Maps, []TileMap{base.Maps[0], loaded.Maps[0]}) {
		t.Fatal("clone mutated merged maps")
	}
	for i := range merged.Maps {
		merged.Maps[i].Name = "changed"
		merged.Maps[i].Rows[0] = "####"
		merged.Maps[i].Events[0].Node = "changed"
	}
	if !reflect.DeepEqual(base.Maps[0], sampleTileMap("garden", base.Start)) || !reflect.DeepEqual(loaded.Maps, ext.Maps) {
		t.Fatal("merge result shares maps with an input")
	}
	loaded.Maps[0].ID = base.Maps[0].ID
	if _, err := Merge(base, loaded); err == nil || !strings.Contains(err.Error(), "duplicate map ID") {
		t.Fatalf("map collision merge error = %v", err)
	}
}

func TestPackWithoutMapsOmitsMapField(t *testing.T) {
	p := Demo()
	for _, maps := range [][]TileMap{nil, {}} {
		p.Maps = maps
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"maps":`) {
			t.Fatal("map field must be omitted in the serialized form of a mapless pack")
		}
	}
}
