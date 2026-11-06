package palflow

import (
	"fmt"
	"reflect"
	"testing"
)

type resourceSource struct {
	tables
	fail uint16
}

func resourceWorld() *WorldState {
	w := worldFixture()
	w.Roles = map[uint16][75]uint16{}
	for role, id := range w.PlayerSprites {
		raw := [75]uint16{}
		raw[2] = id
		w.Roles[role] = raw
	}
	return w
}

func (s resourceSource) SpriteResource(id uint16) (SpriteResource, error) {
	if id == s.fail {
		return SpriteResource{}, fmt.Errorf("unreadable sprite %d", id)
	}
	return SpriteResource{ID: id, FrameCount: 3, DecodedSize: 42, SHA256: "synthetic"}, nil
}
func TestSpriteReloadDecodesWholePartyAndFollowers(t *testing.T) {
	w := resourceWorld()
	w.PartyRoles = []uint16{0, 1}
	w.FollowerSprites = []uint16{7}
	before := CloneWorld(w)
	src := resourceSource{tables: tables{scripts: []Instruction{{}, ins(0x65, 0, 2, 0xffff), ins(0)}}}
	r, err := New(src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Trace(Request{Entry: 1, World: w})
	if err != nil {
		t.Fatal(err)
	}
	resources := report.State.World.PlayerResources
	if len(resources) != 3 || resources[0].ID != 2 || resources[1].ID != 13 || resources[2].ID != 7 || len(report.Trace[0].Resources) != 3 {
		t.Fatalf("missing whole-party resource evidence: %+v", resources)
	}
	if !reflect.DeepEqual(w, before) {
		t.Fatal("request world mutated")
	}
	report.State.World.PlayerResources[0].ID = 99
	if report.Trace[0].Resources[0].ID != 2 {
		t.Fatal("resource trace aliases mutable world")
	}
}
func TestSpriteReloadFailureRollsBackAndBattleSuppressesLoad(t *testing.T) {
	for _, mode := range []string{"bad_second_sprite", "pending_scene", "battle"} {
		t.Run(mode, func(t *testing.T) {
			w := resourceWorld()
			w.PartyRoles = []uint16{0, 1}
			w.SceneLoadPending = mode == "pending_scene"
			w.InBattle = mode == "battle"
			before := CloneWorld(w)
			src := resourceSource{tables: tables{scripts: []Instruction{{}, ins(0x65, 0, 2, 1), ins(0)}}, fail: 13}
			r, _ := New(src, Options{})
			report, err := r.Trace(Request{Entry: 1, World: w})
			if mode == "battle" {
				if err != nil || report.State.World.PlayerSprites[0] != 2 || len(report.State.World.PlayerResources) != 0 {
					t.Fatalf("battle reload: %+v %v", report, err)
				}
			} else if err == nil || !reflect.DeepEqual(report.State.World, before) || len(report.Trace) != 0 {
				t.Fatalf("failed load committed prefix: %+v %v", report, err)
			}
			if !reflect.DeepEqual(w, before) {
				t.Fatal("mutated input world")
			}
		})
	}
}
