package palgame

import (
	"reflect"
	"strings"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

func TestControlScenePresentationsIncludeTouchAndSearchDraws(t *testing.T) {
	for _, kind := range []string{"move", "touch", "search"} {
		t.Run(kind, func(t *testing.T) {
			var events []paldata.Event
			wantDraws := 1
			if kind == "touch" {
				events = []paldata.Event{{X: 320, Y: 160, State: 1, TriggerMode: 4, SpriteFrames: 3}}
				wantDraws++
			} else if kind == "search" {
				events = []paldata.Event{{X: 336, Y: 168, State: 1, TriggerMode: 1, SpriteFrames: 3}}
				wantDraws++
			}
			s := controlFixture(t, events)
			s.palettes = cameraPAT(t, make([]byte, palasset.PaletteBytes))
			s.current.World.Palette, s.current.World.Direction = 0, 3
			s.current.World.ScreenWave, s.current.World.WaveProgression, s.current.World.ScreenShakeTime = 10, 2, 100
			s.current.World.NeedToFadeIn = true
			var r ControlReport
			var err error
			if kind == "move" {
				r, err = s.Move(3, nil)
			} else {
				r, err = s.Interact(nil)
			}
			w := s.current.World
			if err != nil || !r.Completed || r.ScreenUpdates != wantDraws+1 || w.ScreenWave != uint16(10+2*wantDraws) || w.WavePhase != uint8(wantDraws) || w.ScreenShakeTime != uint16(99-wantDraws) || w.NeedToFadeIn || w.FrameNum != 41 {
				t.Fatalf("%s presentation calls: %+v world=%+v err=%v", kind, r, w, err)
			}
			if kind == "touch" && (!r.SearchSuppressed || len(r.TriggeredEvents) != 1) {
				t.Fatal("touch did not run before outer scene draw")
			}
			if kind == "search" && r.SearchEventID != 1 {
				t.Fatal("search did not perform its second redraw")
			}
		})
	}
}

func TestControlLatePendingFadeFailureRollsBackEarlierTouchAndMovement(t *testing.T) {
	s := controlFixture(t, []paldata.Event{{X: 320, Y: 160, State: 1, TriggerMode: 4, SpriteFrames: 3, Trigger: 1}})
	s.game.Scripts[1] = paldata.Instruction{Opcode: 0x50}
	s.current.World.ScreenWave, s.current.World.WaveProgression, s.current.World.ScreenShakeTime = 10, 2, 100
	before := s.Snapshot()
	r, err := s.Move(3, nil)
	if err == nil || !strings.Contains(err.Error(), "palette") || r.Completed || len(r.Triggers) != 1 || !r.Triggers[0].Completed || !r.Moved || !reflect.DeepEqual(s.Snapshot(), before) {
		t.Fatalf("late implicit fade committed the partial frame: %+v %v", r, err)
	}
}
