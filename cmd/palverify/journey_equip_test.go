package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/pal"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

const journeyEquipItem = 43

func journeyEquipmentFixture(t *testing.T) (string, journeyRoute, uint16) {
	t.Helper()
	dir, spec := journeyFixture(t)
	path := filepath.Join(dir, "SSS.MKF")
	data := mustReadCampaignInput(t, path)
	archive, err := pal.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	chunks := make([][]byte, archive.Count())
	for i := range chunks {
		chunks[i], err = archive.ReadChunk(i)
		if err != nil {
			t.Fatal(err)
		}
	}
	enter := uint16(len(chunks[4]) / 8)
	equip := enter + 2
	binary.LittleEndian.PutUint16(chunks[1][2:], enter)
	chunks[2] = make([]byte, 14*(journeyEquipItem+1))
	object := chunks[2][14*journeyEquipItem:]
	binary.LittleEndian.PutUint16(object[6:], equip)
	binary.LittleEndian.PutUint16(object[12:], 2|1<<6)
	for _, code := range [][4]uint16{{0x1f, journeyEquipItem, 1}, {}, {0x18, 0xc, journeyEquipItem}, {0x1a, 17, 29}, {1}, {}} {
		for _, word := range code {
			chunks[4] = binary.LittleEndian.AppendUint16(chunks[4], word)
		}
	}
	updated := mkf(chunks...)
	if err := os.WriteFile(path, updated, 0600); err != nil {
		t.Fatal(err)
	}
	spec.FilesSHA256["SSS.MKF"] = fmt.Sprintf("%x", sha256.Sum256(updated))
	return dir, spec, equip
}

func TestJourneyAndCampaignEquipItemShareContinuousSession(t *testing.T) {
	dir, first, start := journeyEquipmentFixture(t)
	action := journeyAction{Type: "equip_item", Item: journeyUint(journeyEquipItem), TargetRole: journeyUint(0), Inputs: []palflow.Input{}, ExpectedInventory: map[uint16]int{journeyEquipItem: 0}}
	second, whole := first, first
	second.Actions = []journeyAction{action}
	whole.Actions = append(whole.Actions, action)
	var output bytes.Buffer
	if err := runJourney([]string{"-data", dir, "-route", journeyRouteFile(t, whole)}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var journey journeyReport
	if err := json.Unmarshal(output.Bytes(), &journey); err != nil {
		t.Fatal(err)
	}
	entry, w := journey.Actions[1], journey.Final.World
	if !journey.Completed || !entry.Committed || !entry.ExpectationsMatched || w.Roles[0][12] != journeyEquipItem || w.Objects[journeyEquipItem][3] != start+3 || w.EquipmentEffects[1][17][0] != 29 {
		t.Fatal("equip_item did not commit expected equipment and returned entry")
	}
	if entry.Invocation == nil || entry.Invocation.Request.Entry != start || entry.Invocation.Request.EventID != 0 || entry.BeforeSHA256 != journey.Actions[0].AfterSHA256 {
		t.Fatal("equip_item lost its prepared invocation or prior action state")
	}
	manifest := campaignManifestFile(t, journeyRouteFile(t, first), journeyRouteFile(t, second))
	dest := filepath.Join(t.TempDir(), "equipment-reports")
	output.Reset()
	if err := run([]string{"campaign", "-data", dir, "-manifest", manifest, "-out-dir", dest}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var campaign campaignReport
	if err := json.Unmarshal(output.Bytes(), &campaign); err != nil {
		t.Fatal(err)
	}
	if !campaign.Completed || campaign.ActionsCommitted != 2 || campaign.Segments[1].InitialSHA256 != entry.BeforeSHA256 || campaign.FinalSHA256 != entry.AfterSHA256 {
		t.Fatal("campaign continuation did not execute equip_item on the same session")
	}
}

func TestJourneyEquipItemRejectsInvalidActionFields(t *testing.T) {
	_, spec := journeyFixture(t)
	for name, change := range map[string]func(*journeyAction){
		"missing item": func(a *journeyAction) { a.Item = nil },
		"zero item":    func(a *journeyAction) { a.Item = journeyUint(0) },
		"missing role": func(a *journeyAction) { a.TargetRole = nil },
		"all roles":    func(a *journeyAction) { a.TargetRole = journeyUint(65535) },
		"role range":   func(a *journeyAction) { a.TargetRole = journeyUint(6) },
		"direction":    func(a *journeyAction) { a.Direction = journeyUint(0) },
		"event":        func(a *journeyAction) { a.Event = journeyUint(1) },
		"no inputs":    func(a *journeyAction) { a.Inputs = nil },
	} {
		t.Run(name, func(t *testing.T) {
			action := journeyAction{Type: "equip_item", Item: journeyUint(journeyEquipItem), TargetRole: journeyUint(0), Inputs: []palflow.Input{}}
			change(&action)
			route := spec
			route.Actions = append(route.Actions, action)
			if _, _, err := loadJourneyRoute(journeyRouteFile(t, route)); err == nil {
				t.Fatal("invalid equip_item action was accepted")
			}
		})
	}
}
