package palflow

import (
	"reflect"
	"strings"
	"testing"
)

func TestAuditManyUnionsRootsAndStopsAtUnsupported(t *testing.T) {
	scripts := make([]Instruction, 10)
	scripts[1] = ins(6, 50, 4)
	scripts[2] = ins(4, 6)
	scripts[3] = ins(0x7fff)
	scripts[4] = ins(0xffff, 0)
	scripts[5] = ins(0)
	scripts[6] = ins(9)
	scripts[8] = ins(0xa, 5)
	scripts[9] = ins(0)
	report := AuditMany(scripts, []uint16{1, 8, 1, 0})
	if !reflect.DeepEqual(report.Roots, []uint16{0, 1, 8}) || !reflect.DeepEqual(report.Reachable, []uint16{1, 2, 3, 4, 5, 6, 8, 9}) {
		t.Fatalf("bad reachable union: %+v", report)
	}
	if len(report.Blockers) != 2 || report.Blockers[0].PC != 3 || report.Blockers[1].PC != 6 {
		t.Fatal("unsupported blockers missing")
	}
	for _, edge := range report.Edges {
		if edge.From == 3 || edge.From == 6 {
			t.Fatal("unsupported instruction was blindly traversed")
		}
	}
	if !strings.Contains(report.Coverage, "static") || len(report.Limitations) == 0 {
		t.Fatal("static audit presented as runtime verification")
	}
	inventory := Inventory(scripts)
	if inventory.Instructions != 10 || inventory.ModeledInstructions != 8 {
		t.Fatalf("inventory wrong: %+v", inventory)
	}
}
func TestAuditStopAndResumeEdgesDifferFromRuntime(t *testing.T) {
	scripts := []Instruction{ins(0), ins(1), ins(2, 4), ins(0xffff, 0), ins(0)}
	audit := Audit(scripts, 1)
	if !reflect.DeepEqual(audit.Reachable, []uint16{1, 2, 4}) {
		t.Fatalf("bad resume graph: %v", audit.Reachable)
	}
	if audit.Edges[0].Kind != "subsequent_trigger_resume" || audit.Edges[1].To != 4 {
		t.Fatal("returned-entry semantics hidden")
	}
	trace := run(t, tables{scripts: scripts}, Request{Entry: 1})
	if !reflect.DeepEqual(pcList(trace), []uint16{1}) {
		t.Fatal("runtime must not follow future-trigger resume edge")
	}
}
func TestAuditBoundsAndCounterBranchOverapproximation(t *testing.T) {
	scripts := []Instruction{ins(0), ins(3, 1, 2), ins(0)}
	report := AuditMany(scripts, []uint16{1, 60000})
	if len(report.Blockers) != 1 || report.Blockers[0].PC != 60000 || len(report.Edges) != 2 {
		t.Fatal("bounds or counter branch missing")
	}
	full := make([]Instruction, 1<<16)
	full[65535] = ins(8)
	report = Audit(full, 65535)
	if len(report.Blockers) != 1 || len(report.Edges) != 0 || len(report.Reachable) != 1 || report.Blockers[0].PC != 65535 {
		t.Fatal("audit wrapped 16-bit PC")
	}
}

func TestConditionalRedrawSupportInInventoryAndAudit(t *testing.T) {
	scripts := []Instruction{ins(0), ins(5), ins(5, 0, 0, 1), ins(0x43, 9, 3), ins(0)}
	inventory := Inventory(scripts)
	var redraw OpcodeCount
	for _, count := range inventory.Opcodes {
		if count.Opcode == 5 {
			redraw = count
		}
	}
	if !redraw.Supported || redraw.Count != 2 || redraw.ModeledCount != 1 || len(redraw.OperandConstraints) == 0 || inventory.ModeledInstructions != 4 {
		t.Fatalf("operand-constrained support hidden: %+v", inventory)
	}
	audit := Audit(scripts, 1)
	if !reflect.DeepEqual(audit.Reachable, []uint16{1, 2}) || len(audit.Blockers) != 1 || audit.Blockers[0].PC != 2 || !strings.Contains(audit.Blockers[0].Reason, "PAL_UpdatePartyGestures") {
		t.Fatalf("static audit passed gesture update: %+v", audit)
	}
}
