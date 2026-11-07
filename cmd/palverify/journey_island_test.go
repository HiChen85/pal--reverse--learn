package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestLocalJourneySixStatuesBothBattlePolicies(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR to the hash-matched local sample")
	}
	for _, mode := range []string{"auto_attack", "fast_verify"} {
		t.Run(mode, func(t *testing.T) {
			var output bytes.Buffer
			if err := runJourney([]string{"-data", dir, "-compact", "-battle-mode", mode, "-route", "../../testdata/routes/sample-opening-six-statues.json"}, &output, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			var report journeyReport
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			w := report.Final.World
			if !report.Completed || report.FullGameVerified || len(report.Actions) != 1722 || w.Scene != 17 || w.PartyX != 1488 || w.PartyY != 280 || w.Inventory[279] != 0 || w.Events[263].State != 1 || report.ExecutionPolicy.BattleMode != mode || len(report.OptionalFilesSHA256) != 3 {
				t.Fatal("continuous six-statue route mismatch")
			}
			for id := uint16(238); id <= 243; id++ {
				if w.Events[id].State != 0 {
					t.Fatal("statue still active", id)
				}
			}
			messages, battles, rounds := 0, 0, 0
			for _, action := range report.Actions {
				if !action.Committed || !action.ExpectationsMatched || action.Action.Type == "trigger" || action.Action.ExpectedMessages == nil {
					t.Fatal("route bypassed an action or dialogue assertion", action.Index)
				}
				for _, input := range action.Action.Inputs {
					if input.Kind == "battle" {
						t.Fatal("route supplied a battle outcome")
					}
				}
				messages += len(action.MessageIDs)
				for _, battle := range action.Battles {
					wantMode := "numeric"
					if mode == "fast_verify" {
						wantMode = mode
					}
					if battle.Mode != wantMode || battle.RawResult != 3 {
						t.Fatal("battle did not execute its declared numeric policy", battle)
					}
					battles++
					rounds += battle.Rounds
				}
			}
			wantRounds := 16
			if mode == "fast_verify" {
				wantRounds = 8
				if len(report.ExecutionPolicy.BattleSimplifications) != 1 {
					t.Fatal("compact report lost simplification scope")
				}
			}
			if messages != 374 || battles != 4 || rounds != wantRounds {
				t.Fatal("route dialogue/battle coverage mismatch", messages, battles, rounds)
			}
			t.Logf("actions=%d messages=%d battles=%d rounds=%d", len(report.Actions), messages, battles, rounds)
		})
	}
}

func TestLocalJourneyIslandWedding(t *testing.T) {
	dir := os.Getenv("PAL_GAME_DATA_DIR")
	if dir == "" {
		t.Skip("set PAL_GAME_DATA_DIR to the hash-matched local sample")
	}
	var output bytes.Buffer
	if err := runJourney([]string{"-data", dir, "-compact", "-battle-mode", "fast_verify", "-max-steps", "100000", "-route", "../../testdata/routes/sample-opening-island-wedding.json"}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var report journeyReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	w := report.Final.World
	if !report.Completed || report.FullGameVerified || len(report.Actions) != 2263 || w.Scene != 20 || w.PartyX != 528 || w.PartyY != 1512 || w.Inventory[273] != 1 || w.Inventory[279] != 0 || w.Events[349].State != 0 || report.ExecutionPolicy.VM.MaxSteps != 100000 || report.ExecutionPolicy.BattleMode != "fast_verify" || len(report.Actions[2262].MessageIDs) != 111 {
		t.Fatal("continuous palace/wedding route mismatch")
	}
	messages := 0
	for _, action := range report.Actions {
		if !action.Committed || !action.ExpectationsMatched || action.Action.Type == "trigger" || action.Action.ExpectedMessages == nil {
			t.Fatal("wedding route bypassed a physical action or dialogue assertion", action.Index)
		}
		messages += len(action.MessageIDs)
	}
	if messages != 622 {
		t.Fatal("wedding dialogue count mismatch", messages)
	}
	t.Logf("actions=%d messages=%d last_dialogue=%d", len(report.Actions), messages, len(report.Actions[2262].MessageIDs))
}
