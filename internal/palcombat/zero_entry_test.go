package palcombat

import (
	"reflect"
	"testing"
)

type zeroEntryCallbacks struct{ calls []ScriptRequest }
type zeroEntryTransaction struct{ owner *zeroEntryCallbacks }

func (c *zeroEntryCallbacks) Begin(Snapshot) (ScriptTransaction, error) {
	return &zeroEntryTransaction{owner: c}, nil
}
func (tx *zeroEntryTransaction) Run(q ScriptRequest, _ *Snapshot, _ RandomSource) (ScriptResult, error) {
	tx.owner.calls = append(tx.owner.calls, q)
	return ScriptResult{Success: true, NextEntry: q.Entry}, nil
}
func (*zeroEntryTransaction) Commit(Snapshot) error { return nil }
func (*zeroEntryTransaction) Rollback()             {}

func TestPreAndPostBattleZeroEntriesVisitImplicitEmptySlots(t *testing.T) {
	c := fixture()
	c.Tables.EnemyTeams[0] = [5]uint16{10, 0, 0, 0xffff, 0xffff}
	c.Tables.Enemies[0].HP = 1
	c.Random = &RecordedRandom{Values: make([]int, 20)}
	callbacks := &zeroEntryCallbacks{}
	c.Callbacks = callbacks
	e := create(t, c)
	if report, err := e.Submit(command(Attack)); err != nil || !report.Committed || report.Outcome != Victory {
		t.Fatalf("battle failed: %+v error=%v", report, err)
	}
	var calls []string
	var slots []uint16
	for _, call := range callbacks.calls {
		if call.Entry != 0 {
			t.Fatal("fixture contains an unexpected nonzero entry")
		}
		calls = append(calls, call.Hook)
		slots = append(slots, call.Invoker)
	}
	if !reflect.DeepEqual(slots, []uint16{0, 1, 2, 0, 1, 2}) || !reflect.DeepEqual(calls, []string{"enemy turn start", "enemy turn start", "enemy turn start", "enemy battle end", "enemy battle end", "enemy battle end"}) {
		t.Fatalf("zero-entry invocation order: slots=%v hooks=%v", slots, calls)
	}
}

func TestLaterTurnStartZeroEntriesSkipClearedAndImplicitEmptySlots(t *testing.T) {
	c := fixture()
	c.Tables.EnemyTeams[0] = [5]uint16{10, 11, 0, 0xffff, 0xffff}
	c.Random = &RecordedRandom{Values: []int{0, 0, 0, 0, 0, 0, 9}}
	callbacks := &zeroEntryCallbacks{}
	c.Callbacks = callbacks
	e := create(t, c)
	e.state.Started = true
	e.state.Enemies[0].Rewarded, e.state.Enemies[0].HP = true, 0
	if _, err := e.Submit(command(Defend)); err != nil {
		t.Fatal(err)
	}
	if len(callbacks.calls) != 2 || callbacks.calls[0].Hook != "enemy ready" || callbacks.calls[1].Hook != "enemy turn start" || callbacks.calls[0].Invoker != 1 || callbacks.calls[1].Invoker != 1 {
		t.Fatalf("later turn used the unfiltered pre-battle pass: %+v", callbacks.calls)
	}
}
