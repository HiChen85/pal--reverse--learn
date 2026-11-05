// Package studio coordinates editing and the playable story preview. All pack
// changes are validated before they replace the current workspace.
package studio

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/HiChen85/pal--reverse--learn/internal/battle"
	"github.com/HiChen85/pal--reverse--learn/internal/content"
	"github.com/HiChen85/pal--reverse--learn/internal/story"
)

const ProgressVersion = 1
const MaxProgressBytes = 4 << 20
const MaxSteps = 10000

type Studio struct {
	pack    content.Pack
	session *story.Session
	start   string
	steps   []step
}
type step struct {
	Kind    string          `json:"kind"`
	Choice  *int            `json:"choice,omitempty"`
	Command *battle.Command `json:"command,omitempty"`
}
type progress struct {
	Version    int    `json:"version"`
	PackDigest string `json:"pack_sha256"`
	Start      string `json:"start"`
	Steps      []step `json:"steps"`
}

func New(p content.Pack) (*Studio, error) {
	session, err := story.New(p)
	if err != nil {
		return nil, err
	}
	return &Studio{pack: content.Clone(p), session: session}, nil
}
func (s *Studio) Pack() content.Pack { return content.Clone(s.pack) }
func (s *Studio) ReplacePack(p content.Pack) error {
	next, err := New(p)
	if err != nil {
		return err
	}
	*s = *next
	return nil
}
func (s *Studio) LoadPack(path string) error {
	p, err := content.Load(path)
	if err != nil {
		return err
	}
	return s.ReplacePack(p)
}
func (s *Studio) SavePack(path string) error { return content.Save(path, s.pack) }
func (s *Studio) MergePack(path string) error {
	p, err := content.LoadExtension(path, s.pack)
	if err != nil {
		return err
	}
	merged, err := content.Merge(s.pack, p)
	if err != nil {
		return err
	}
	return s.ReplacePack(merged)
}
func (s *Studio) Current() story.View { return s.session.View() }
func (s *Studio) Start(nodeID string) error {
	if nodeID == "" {
		nodeID = s.pack.Start
	}
	if err := s.session.Start(nodeID); err != nil {
		return err
	}
	s.start = nodeID
	s.steps = nil
	return nil
}
func (s *Studio) apply(op step) error {
	if len(s.steps) >= MaxSteps {
		return errors.New("preview reached 10000 actions; start a new preview")
	}
	var err error
	switch op.Kind {
	case "continue":
		err = s.session.Continue()
	case "choose":
		if op.Choice == nil {
			return errors.New("choice is required")
		}
		err = s.session.Choose(*op.Choice)
	case "act":
		if op.Command == nil {
			return errors.New("battle command is required")
		}
		err = s.session.Act(*op.Command)
	case "retry":
		err = s.session.RetryBattle()
	default:
		return fmt.Errorf("unsupported progress action %q", op.Kind)
	}
	if err == nil {
		s.steps = append(s.steps, op)
	}
	return err
}
func (s *Studio) Continue() error        { return s.apply(step{Kind: "continue"}) }
func (s *Studio) Choose(index int) error { return s.apply(step{Kind: "choose", Choice: &index}) }
func (s *Studio) Act(command battle.Command) error {
	return s.apply(step{Kind: "act", Command: &command})
}
func (s *Studio) RetryBattle() error { return s.apply(step{Kind: "retry"}) }
func digest(p content.Pack) string {
	data, _ := json.Marshal(p)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SaveProgress stores bounded deterministic commands rather than trusting a
// writable HP/inventory snapshot. Loading replays every command in a fresh
// session and replaces the active preview only when the entire replay succeeds.
func (s *Studio) SaveProgress(path string) error {
	if !s.Current().Started {
		return errors.New("start the preview before saving progress")
	}
	data, err := json.MarshalIndent(progress{ProgressVersion, digest(s.pack), s.start, s.steps}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > MaxProgressBytes {
		return errors.New("progress is too large")
	}
	return writeAtomic(path, data)
}
func (s *Studio) LoadProgress(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxProgressBytes+1))
	if err != nil {
		return err
	}
	if len(data) > MaxProgressBytes {
		return errors.New("progress exceeds 4 MiB")
	}
	if !utf8.Valid(data) {
		return errors.New("progress must be UTF-8")
	}
	var p progress
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("decode progress: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return errors.New("progress must contain exactly one JSON document")
	}
	if p.Version != ProgressVersion {
		return fmt.Errorf("unsupported progress version %d", p.Version)
	}
	if p.PackDigest != digest(s.pack) {
		return errors.New("progress belongs to a different or edited pack")
	}
	if len(p.Steps) > MaxSteps {
		return errors.New("too many progress actions")
	}
	if p.Start == "" {
		return errors.New("progress start is required")
	}
	next, err := New(s.pack)
	if err != nil {
		return err
	}
	if err := next.Start(p.Start); err != nil {
		return err
	}
	for i, op := range p.Steps {
		switch op.Kind {
		case "continue", "retry":
			if op.Choice != nil || op.Command != nil {
				return fmt.Errorf("action %d has unexpected fields", i)
			}
		case "choose":
			if op.Choice == nil || op.Command != nil {
				return fmt.Errorf("invalid choice action %d", i)
			}
		case "act":
			if op.Command == nil || op.Choice != nil {
				return fmt.Errorf("invalid battle action %d", i)
			}
		default:
			return fmt.Errorf("unknown progress action %q", op.Kind)
		}
		if err := next.apply(op); err != nil {
			return fmt.Errorf("replay action %d: %w", i, err)
		}
	}
	*s = *next
	return nil
}
func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".progress-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
