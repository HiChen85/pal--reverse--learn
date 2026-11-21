package palflow

import (
	"reflect"
	"strings"
	"testing"
)

func TestAuditConditionAndEquipmentBranchesReachBothSourcePaths(t *testing.T) {
	for _, tc := range []struct {
		name string
		inst Instruction
	}{
		{"inventory and equipment removal", ins(0x20, 10, 3, 8)},
		{"poison kind", ins(0x5d, 10, 8)},
		{"ordinary poison", ins(0x61, 8)},
		{"party HP", ins(0x74, 8)},
		{"party name", ins(0x79, 10, 8)},
		{"party facing", ins(0x81, 10, 0, 8)},
		{"event zone", ins(0x83, 10, 0, 8)},
		{"item placement", ins(0x84, 10, 0, 8)},
		{"equipped item threshold", ins(0x86, 10, 1, 8)},
		{"event state", ins(0x94, 10, 1, 8)},
		{"current scene", ins(0x95, 10, 8)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := make([]Instruction, 9)
			entries[1], entries[8] = tc.inst, ins(0x7fff)
			report := Audit(entries, 1)
			if !reflect.DeepEqual(report.Reachable, []uint16{1, 2, 8}) || len(report.Edges) != 2 || len(report.Blockers) != 1 || report.Blockers[0].PC != 8 {
				t.Fatalf("audit missed a source branch or its blocker: %+v", report)
			}
			for _, edge := range report.Edges {
				if edge.From != 1 || edge.Kind == "fallthrough" {
					t.Fatalf("condition path was represented as unconditional fallthrough: %+v", edge)
				}
			}
		})
	}
	entries := []Instruction{{}, ins(0x20, 10, 1, 0), ins(0)}
	report := Audit(entries, 1)
	if len(report.Edges) != 1 || report.Edges[0].To != 2 || len(report.Blockers) != 0 {
		t.Fatalf("unconditional item removal invented an insufficient-item jump: %+v", report)
	}
}

func TestAuditTeleportStopsAtDynamicSceneCallee(t *testing.T) {
	entries := []Instruction{{}, ins(0x38, 3), ins(0x7fff), ins(0x7fff)}
	report := Audit(entries, 1)
	if !reflect.DeepEqual(report.Reachable, []uint16{1}) || len(report.Edges) != 0 || len(report.Blockers) != 1 || report.Blockers[0].PC != 1 || !strings.Contains(report.Blockers[0].Reason, "runtime scene") {
		t.Fatalf("teleport audit guessed a dynamic callee or return: %+v", report)
	}
}

func TestAuditRandomSkipIncludesActualDivisionTail(t *testing.T) {
	for _, bound := range []uint16{0, 1, 2, 7} {
		entries := make([]Instruction, 12)
		entries[1] = ins(0xa2, bound)
		maximum := int(bound)
		if bound <= 1 {
			maximum = 0
		}
		entries[2+maximum] = ins(0x7fff)
		report := Audit(entries, 1)
		if len(report.Edges) != maximum+1 || len(report.Blockers) != 1 || report.Blockers[0].PC != uint16(2+maximum) {
			t.Fatalf("bound %d omitted reference tail or deterministic path: %+v", bound, report)
		}
		for skip, edge := range report.Edges {
			if edge.From != 1 || edge.To != uint16(2+skip) || edge.Kind != "possible_reference_random_skip" {
				t.Fatalf("bound %d random target %d: %+v", bound, skip, edge)
			}
		}
	}
}

func TestAuditRandomExpansionAndPCOverflowProduceExplicitLimits(t *testing.T) {
	for _, bound := range []uint16{4096, 46409, 65535} {
		report := Audit([]Instruction{{}, ins(0xa2, bound), ins(0x7fff)}, 1)
		if !reflect.DeepEqual(report.Reachable, []uint16{1}) || len(report.Edges) != 0 || len(report.Blockers) != 1 || !strings.Contains(report.Blockers[0].Reason, "bounded static edge expansion") {
			t.Fatalf("large random expansion proceeded without an explicit stop: %+v", report)
		}
	}
	entries := make([]Instruction, 1<<16)
	entries[65534] = ins(0xa2, 2)
	report := Audit(entries, 65534)
	if len(report.Edges) != 1 || report.Edges[0].To != 65535 || len(report.Blockers) != 1 || !strings.Contains(report.Blockers[0].Reason, "overflow") {
		t.Fatalf("random target range wrapped the modeled PC: %+v", report)
	}
	entries[65535] = ins(0xa2, 1)
	report = Audit(entries, 65535)
	if len(report.Edges) != 0 || len(report.Blockers) != 1 || !strings.Contains(report.Blockers[0].Reason, "overflow") {
		t.Fatalf("deterministic skip wrapped the modeled PC: %+v", report)
	}
	for i := 1; i < len(entries); i++ {
		entries[i] = ins(0xa2, 2)
	}
	report = Audit(entries, 1)
	if len(report.Edges) > 1<<16 || len(report.Blockers) == 0 {
		t.Fatalf("random graph was unbounded: edges=%d blockers=%d", len(report.Edges), len(report.Blockers))
	}
	found := false
	for _, blocker := range report.Blockers {
		found = found || strings.Contains(blocker.Reason, "bounded static edge expansion")
	}
	if !found {
		t.Fatal("total random edge budget stopped without explaining the coverage boundary")
	}
}
