// Package story runs the editable chapter graph. Its small Go state machine is
// independent of the original PAL opcode VM, which is not implemented here.
package story

import (
	"errors"
	"fmt"
	"maps"

	"github.com/HiChen85/pal--reverse--learn/internal/battle"
	"github.com/HiChen85/pal--reverse--learn/internal/content"
)

// View is an isolated snapshot. Choices contains only currently visible choices;
// Choose takes an index into this slice. Missing flags have the value false.
type View struct {
	Started bool
	Ended   bool
	Node    content.Node
	Choices []content.Choice
	Flags   map[string]bool
	Battle  *battle.State
}

type Session struct {
	pack           content.Pack
	nodes          map[string]content.Node
	current        string
	flags          map[string]bool
	inventory      map[string]int
	combat         *battle.Battle
	started, ended bool
}

func New(p content.Pack) (*Session, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	p = content.Clone(p)
	s := &Session{pack: p, nodes: make(map[string]content.Node, len(p.Nodes))}
	for _, n := range p.Nodes {
		s.nodes[n.ID] = n
	}
	return s, nil
}

// Start starts a fresh preview at a chapter node. The first three authored
// characters (or all when fewer) form the party. Every item starts with three
// units, and every encounter
// begins at full HP/MP. These are explicit preview rules, not original PAL rules.
func (s *Session) Start(nodeID string) error {
	if nodeID == "" {
		nodeID = s.pack.Start
	}
	if _, ok := s.nodes[nodeID]; !ok {
		return fmt.Errorf("unknown start node %q", nodeID)
	}
	next := &Session{pack: s.pack, nodes: s.nodes, flags: map[string]bool{}, inventory: map[string]int{}, started: true}
	for _, it := range s.pack.Items {
		next.inventory[it.ID] = 3
	}
	if err := next.enter(nodeID, next.inventory); err != nil {
		return err
	}
	*s = *next
	return nil
}

func cloneNode(n content.Node) content.Node {
	n.SetFlags = maps.Clone(n.SetFlags)
	n.Choices = append([]content.Choice(nil), n.Choices...)
	for i := range n.Choices {
		n.Choices[i].RequireFlags = maps.Clone(n.Choices[i].RequireFlags)
	}
	return n
}
func (s *Session) visible() []content.Choice {
	if !s.started || s.ended || s.combat != nil {
		return nil
	}
	var out []content.Choice
	for _, ch := range s.nodes[s.current].Choices {
		match := true
		for key, want := range ch.RequireFlags {
			if s.flags[key] != want {
				match = false
				break
			}
		}
		if match {
			ch.RequireFlags = maps.Clone(ch.RequireFlags)
			out = append(out, ch)
		}
	}
	return out
}
func (s *Session) View() View {
	v := View{Started: s.started, Ended: s.ended, Flags: maps.Clone(s.flags)}
	if !s.started {
		return v
	}
	v.Node = cloneNode(s.nodes[s.current])
	v.Choices = s.visible()
	if s.combat != nil {
		state := s.combat.Snapshot()
		v.Battle = &state
	}
	return v
}

// enter prepares a target encounter before committing any changes.
func (s *Session) enter(nodeID string, inventory map[string]int) error {
	if nodeID == "" {
		s.ended = true
		s.combat = nil
		s.inventory = maps.Clone(inventory)
		return nil
	}
	node, ok := s.nodes[nodeID]
	if !ok {
		return fmt.Errorf("unknown node %q", nodeID)
	}
	var combat *battle.Battle
	if node.Encounter != "" {
		var err error
		partyIDs := make([]string, 0, 3)
		for _, c := range s.pack.Characters[:min(3, len(s.pack.Characters))] {
			partyIDs = append(partyIDs, c.ID)
		}
		combat, err = battle.New(s.pack, partyIDs, node.Encounter, inventory)
		if err != nil {
			return fmt.Errorf("start encounter: %w", err)
		}
	}
	flags := maps.Clone(s.flags)
	for key, value := range node.SetFlags {
		flags[key] = value
	}
	s.current = nodeID
	s.flags = flags
	s.combat = combat
	s.inventory = maps.Clone(inventory)
	s.ended = node.Encounter == "" && node.Next == "" && len(node.Choices) == 0
	return nil
}

func (s *Session) Continue() error {
	if !s.started {
		return errors.New("story has not started")
	}
	if s.ended {
		return errors.New("chapter has ended")
	}
	node := s.nodes[s.current]
	if len(node.Choices) > 0 {
		return errors.New("choose a visible story option")
	}
	inventory := s.inventory
	if s.combat != nil {
		state := s.combat.Snapshot()
		if state.Outcome != battle.Victory {
			return errors.New("win the encounter before continuing")
		}
		inventory = state.Inventory
	}
	return s.enter(node.Next, inventory)
}
func (s *Session) Choose(index int) error {
	if !s.started || s.ended {
		return errors.New("no active story choice")
	}
	if s.combat != nil {
		return errors.New("cannot choose during an encounter")
	}
	choices := s.visible()
	if index < 0 || index >= len(choices) {
		return errors.New("choice index is not visible")
	}
	return s.enter(choices[index].Next, s.inventory)
}
func (s *Session) Act(command battle.Command) error {
	if !s.started || s.ended || s.combat == nil {
		return errors.New("no active encounter")
	}
	return s.combat.Act(command)
}
func (s *Session) RetryBattle() error {
	if s.combat == nil || s.combat.Snapshot().Outcome != battle.Defeat {
		return errors.New("only a lost encounter can be retried")
	}
	// inventory is the snapshot at encounter entry; a retry restores it.
	return s.enter(s.current, s.inventory)
}
