package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The committed fixture contains numeric inputs and hashes, never game assets.
func TestLocalCampaignOpeningThroughSuzhou(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR to the hash-matched local sample")
	}
	dest := filepath.Join(t.TempDir(), "campaign")
	var output bytes.Buffer
	if err := runCampaign([]string{"-data", dir, "-manifest", "../../testdata/routes/sample-opening-suzhou-campaign.json", "-out-dir", dest, "-battle-mode", "fast_verify", "-max-steps", "100000"}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var index campaignReport
	if err := json.Unmarshal(output.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if !index.Completed || index.FullGameVerified || index.ActionsCommitted != 5688 || index.ActionsAttempted != 5688 || len(index.Segments) != 4 || index.ExecutionPolicy.BattleMode != "fast_verify" {
		t.Fatal("Suzhou campaign totals mismatch")
	}
	messages, battles, terminated := 0, 0, 0
	previous := index.InitialSHA256
	for i, segment := range index.Segments {
		if segment.Status != "completed" || segment.InitialSHA256 != previous {
			t.Fatal("campaign broke continuity", i)
		}
		previous = segment.FinalSHA256
		r := campaignReadReport(t, filepath.Join(dest, segment.Report))
		for _, action := range r.Actions {
			if !action.Committed || !action.ExpectationsMatched || action.Action.Type == "trigger" || action.Action.ExpectedMessages == nil {
				t.Fatal("route skipped a physical action or dialogue assertion", i, action.Index)
			}
			for _, input := range action.Action.Inputs {
				if input.Kind == "battle" {
					t.Fatal("route supplied a battle outcome")
				}
			}
			messages += len(action.MessageIDs)
			for _, battle := range action.Battles {
				if battle.Mode != "fast_verify" {
					t.Fatal("battle used an undeclared policy", battle)
				}
				battles++
				if battle.RawResult == 0 {
					terminated++
				} else if battle.RawResult != 3 {
					t.Fatal("unexpected story battle outcome", battle)
				}
			}
		}
		role := r.Final.World.Roles[0]
		if i == 2 && slices.Contains(role[32:64], uint16(345)) {
			t.Fatal("pre-lesson temple approach already knows magic 345")
		}
		if i == 3 {
			w := r.Final.World
			if !slices.Contains(role[32:64], uint16(345)) || w.Scene != 21 || w.PartyX != 448 || w.PartyY != 1296 || r.Final.NeedsSceneEnter || !slices.Equal(w.PartyRoles, []uint16{0, 1}) {
				t.Fatal("Suzhou arrival or party mismatch")
			}
		}
	}
	if previous != index.FinalSHA256 || messages != 1383 || battles != 16 || terminated != 2 {
		t.Fatal("campaign evidence mismatch", messages, battles, terminated)
	}
	t.Logf("actions=%d messages=%d numeric_battles=%d", index.ActionsCommitted, messages, battles)
}
