package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLocalCampaignAttackAllThroughSnakeCave(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR to the hash-matched local sample")
	}
	dest := filepath.Join(t.TempDir(), "campaign")
	var output bytes.Buffer
	if err := runCampaign([]string{"-data", dir, "-manifest", "../../testdata/routes/sample-opening-snake-cave-campaign.json", "-out-dir", dest, "-battle-mode", "fast_verify", "-max-steps", "100000"}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var index campaignReport
	if err := json.Unmarshal(output.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if !index.Completed || index.FullGameVerified || index.ActionsCommitted != 7304 || len(index.Segments) != 7 {
		t.Fatal("snake-cave campaign mismatch")
	}
	messages, battles, sweeps, terminated := 0, 0, 0, 0
	previous := index.InitialSHA256
	for _, segment := range index.Segments {
		if segment.InitialSHA256 != previous || segment.Status != "completed" {
			t.Fatal("campaign broke continuity")
		}
		previous = segment.FinalSHA256
		r := campaignReadReport(t, filepath.Join(dest, segment.Report))
		for _, action := range r.Actions {
			if !action.Committed || !action.ExpectationsMatched || action.Action.Type == "trigger" {
				t.Fatal("snake route bypassed or failed a physical action")
			}
			for _, input := range action.Action.Inputs {
				if input.Kind == "battle" {
					t.Fatal("snake route supplied a battle outcome")
				}
			}
			messages += len(action.MessageIDs)
			for _, battle := range action.Battles {
				if battle.Mode != "fast_verify" {
					t.Fatal("undeclared battle policy")
				}
				battles++
				sweeps += battle.AllTargetSweeps
				if battle.RawResult == 0 {
					terminated++
				} else if battle.RawResult != 3 {
					t.Fatal("unexpected battle outcome", battle)
				}
			}
		}
	}
	r := campaignReadReport(t, filepath.Join(dest, index.Segments[6].Report))
	w := r.Final.World
	if messages != 2011 || battles != 25 || sweeps != 6 || terminated != 3 || previous != index.FinalSHA256 || w.Scene != 46 || w.PartyX != 784 || w.PartyY != 536 || !slices.Equal(w.PartyRoles, []uint16{0, 2}) || r.Final.NeedsSceneEnter {
		t.Fatal("snake-cave evidence mismatch", messages, battles, sweeps, terminated)
	}
	t.Logf("actions=7304 messages=%d battles=%d actual_all_sweeps=%d", messages, battles, sweeps)
}
