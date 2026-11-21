package palgame

import (
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

func TestSaveRetainsNonEventRoleAndEnemySlotContext(t *testing.T) {
	s := controlFixture(t, []paldata.Event{{State: 1, TriggerMode: 1}})
	bindSaveFixture(t, s)
	for context := uint16(0); context < paldata.RoleCount; context++ {
		s.current.LastEventID = context
		if got := saveRoundTrip(t, s).Snapshot().LastEventID; got != context {
			t.Fatal("shared role/battle context was lost", context, got)
		}
	}
	s.current.LastEventID = paldata.RoleCount
	if _, err := s.MarshalSave(); err == nil {
		t.Fatal("absent event beyond the role/battle namespaces was accepted")
	}
}
