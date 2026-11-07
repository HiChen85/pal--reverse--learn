package palflow

import (
	"reflect"
	"testing"
)

func TestEquipmentCallbacksEndRNGDialogIncludingZeroEntry(t *testing.T) {
	for _, entry := range []int{-1, 0, 1} {
		s := playerState(playerWorld(), 0)
		s.DialogPlayingRNG = true
		if entry >= 0 {
			raw := s.World.Roles[5] // Off-party equipment also invokes the trigger.
			raw[11] = 10
			s.World.Roles[5] = raw
			s.World.Objects[10] = [7]uint16{3: uint16(entry)}
		}
		if _, err := applyPlayerForTest(t, &s, ins(0x75, 1)); err != nil {
			t.Fatal(err)
		}
		if s.DialogPlayingRNG != (entry < 0) {
			t.Fatalf("OnEquip entry %d: RNG dialog flag=%v", entry, s.DialogPlayingRNG)
		}
	}

	// The first callback ends its dialog, but a later failed equipment script
	// must roll back the entire party instruction including that transient flag.
	s := playerState(playerWorld(), 0)
	s.DialogPlayingRNG = true
	raw := s.World.Roles[0]
	raw[11], raw[12] = 10, 11
	s.World.Roles[0] = raw
	s.World.Objects[10], s.World.Objects[11] = [7]uint16{}, [7]uint16{3: 2}
	before := detachPartyState(s)
	if _, err := applyPlayerForTest(t, &s, ins(0x75, 1)); err == nil || !reflect.DeepEqual(s, before) {
		t.Fatal("failed equipment rebuild committed EndDialog", err)
	}
}

type dialogCombatSource struct {
	autoSource
	dialogEnded bool
	invalid     bool
}

func (s dialogCombatSource) RunBattle(w *WorldState, _ uint16, _ bool, _ []Input, _ Options) (CombatExecution, error) {
	work := 1
	if s.invalid {
		work = 0
	}
	return CombatExecution{World: CloneWorld(w), Result: BattleWin, RawResult: 3, Work: work, DialogEnded: s.dialogEnded}, nil
}

func TestCombatClearsRNGDialogOnlyWithCommittedEndDialogEvidence(t *testing.T) {
	for _, ended := range []bool{false, true} {
		for _, invalid := range []bool{false, true} {
			s := playerState(playerWorld(), 0)
			s.DialogPlayingRNG = true
			s.CurEquipPart = 3
			s.World.BattleMode = "auto_attack"
			r, _ := New(dialogCombatSource{dialogEnded: ended, invalid: invalid}, Options{})
			before := detachPartyState(s)
			step := TraceStep{}
			_, err := r.evaluateCombat(&s, &step, ins(7), nil)
			if invalid {
				if err == nil || !reflect.DeepEqual(s, before) {
					t.Fatal("rejected battle proposal committed EndDialog", err)
				}
			} else if err != nil || s.DialogPlayingRNG != !ended || step.Combat == nil || step.Combat.DialogEnded != ended {
				t.Fatal("battle ignored or invented EndDialog evidence", ended, err)
			} else if ended && s.CurEquipPart != -1 || !ended && s.CurEquipPart != 3 {
				t.Fatal("battle ignored or invented trigger equipment reset", ended, s.CurEquipPart)
			}
		}
	}
}
