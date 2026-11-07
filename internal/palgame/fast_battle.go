package palgame

import (
	"slices"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

const fastVerifyDescription = "Verification-only battle tables cap each nonzero enemy template HP at 1, including summons and morph templates. All other numeric attributes, scripts, initiative and reward rules remain active. This changes combat coverage and does not guarantee victory or original balance. Imported tables and permanent party base attributes are not modified by this policy."

// BattlePolicyNotes reports verification-only numeric changes. Each call
// returns independent storage so report consumers cannot alter later evidence.
func BattlePolicyNotes(mode string) []string {
	if mode == "fast_verify" {
		return []string{fastVerifyDescription}
	}
	return nil
}

// fastVerifyTables changes only a private per-battle copy. Zero HP remains zero
// so malformed templates still fail validation instead of becoming usable.
func fastVerifyTables(tables *paldata.RuntimeTables) *paldata.RuntimeTables {
	out := *tables
	out.Enemies = slices.Clone(tables.Enemies)
	for i := range out.Enemies {
		if out.Enemies[i].HP != 0 {
			out.Enemies[i].HP = 1
			out.Enemies[i].Raw[11] = 1
		}
	}
	return &out
}
