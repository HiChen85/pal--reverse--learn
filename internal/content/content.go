// Package content defines versioned, editable story packs independent of PAL1's
// original binary data. IDs remain stable when a pack is saved or extended.
package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const SchemaVersion = 1
const MaxFileBytes = 8 << 20
const maxRecords = 10000
const maxStat = 1000000

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

type Pack struct {
	SchemaVersion int         `json:"schema_version"`
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	Start         string      `json:"start"`
	Characters    []Character `json:"characters"`
	Items         []Item      `json:"items"`
	Encounters    []Encounter `json:"encounters"`
	Nodes         []Node      `json:"nodes"`
}
type Character struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	MaxHP   int    `json:"max_hp"`
	MaxMP   int    `json:"max_mp"`
	Attack  int    `json:"attack"`
	Defense int    `json:"defense"`
	Speed   int    `json:"speed"`
}
type Item struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	HealHP      int    `json:"heal_hp"`
	RestoreMP   int    `json:"restore_mp"`
}
type Encounter struct {
	ID      string      `json:"id"`
	Enemies []Character `json:"enemies"`
}
type Node struct {
	ID        string          `json:"id"`
	Speaker   string          `json:"speaker,omitempty"`
	Text      string          `json:"text"`
	Encounter string          `json:"encounter,omitempty"`
	Next      string          `json:"next,omitempty"`
	Choices   []Choice        `json:"choices,omitempty"`
	SetFlags  map[string]bool `json:"set_flags,omitempty"`
}
type Choice struct {
	Text         string          `json:"text"`
	Next         string          `json:"next"`
	RequireFlags map[string]bool `json:"require_flags,omitempty"`
}

func validID(id, what string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("%s: ID %q must be 1–128 ASCII letters, digits, _, ., :, or -", what, id)
	}
	return nil
}
func validText(s, what string, max int, required bool) error {
	if !utf8.ValidString(s) || len(s) > max || strings.ContainsRune(s, 0) {
		return fmt.Errorf("%s: invalid text or exceeds %d bytes", what, max)
	}
	if required && strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s: must not be blank", what)
	}
	return nil
}
func validateCharacter(c Character) error {
	if err := validID(c.ID, "character"); err != nil {
		return err
	}
	if err := validText(c.Name, "character "+c.ID+" name", 256, true); err != nil {
		return err
	}
	if c.MaxHP < 1 || c.MaxHP > maxStat || c.MaxMP < 0 || c.MaxMP > maxStat || c.Attack < 0 || c.Attack > maxStat || c.Defense < 0 || c.Defense > maxStat || c.Speed < 0 || c.Speed > maxStat {
		return fmt.Errorf("character %s: HP must be 1–%d and other stats 0–%d", c.ID, maxStat, maxStat)
	}
	return nil
}
func validateFlags(flags map[string]bool) error {
	if len(flags) > 256 {
		return errors.New("too many flags (max 256 per node/choice)")
	}
	for id := range flags {
		if err := validID(id, "flag"); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks numeric bounds, duplicate IDs and every cross-reference. It
// intentionally permits graph cycles and unreachable nodes for authored chapters.
func (p Pack) Validate() error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d (want %d)", p.SchemaVersion, SchemaVersion)
	}
	if err := validID(p.ID, "pack"); err != nil {
		return err
	}
	if err := validText(p.Title, "pack title", 1024, true); err != nil {
		return err
	}
	if len(p.Characters) == 0 || len(p.Nodes) == 0 {
		return errors.New("pack requires at least one character and one node")
	}
	if len(p.Characters)+len(p.Items)+len(p.Encounters)+len(p.Nodes) > maxRecords {
		return fmt.Errorf("pack exceeds %d records", maxRecords)
	}
	chars := map[string]bool{}
	items := map[string]bool{}
	encounters := map[string]bool{}
	nodes := map[string]bool{}
	for _, c := range p.Characters {
		if err := validateCharacter(c); err != nil {
			return err
		}
		if chars[c.ID] {
			return fmt.Errorf("duplicate character ID %q", c.ID)
		}
		chars[c.ID] = true
	}
	for _, it := range p.Items {
		if err := validID(it.ID, "item"); err != nil {
			return err
		}
		if items[it.ID] {
			return fmt.Errorf("duplicate item ID %q", it.ID)
		}
		items[it.ID] = true
		if err := validText(it.Name, "item name", 256, true); err != nil {
			return err
		}
		if err := validText(it.Description, "item description", 8192, false); err != nil {
			return err
		}
		if it.HealHP < 0 || it.HealHP > maxStat || it.RestoreMP < 0 || it.RestoreMP > maxStat {
			return fmt.Errorf("item %s: effects must be 0–%d", it.ID, maxStat)
		}
	}
	for _, e := range p.Encounters {
		if err := validID(e.ID, "encounter"); err != nil {
			return err
		}
		if encounters[e.ID] {
			return fmt.Errorf("duplicate encounter ID %q", e.ID)
		}
		encounters[e.ID] = true
		if len(e.Enemies) == 0 || len(e.Enemies) > 16 {
			return fmt.Errorf("encounter %s: requires 1–16 enemies", e.ID)
		}
		ids := map[string]bool{}
		for _, enemy := range e.Enemies {
			if err := validateCharacter(enemy); err != nil {
				return fmt.Errorf("encounter %s: %w", e.ID, err)
			}
			if ids[enemy.ID] {
				return fmt.Errorf("encounter %s: duplicate enemy ID %q", e.ID, enemy.ID)
			}
			ids[enemy.ID] = true
		}
	}
	for _, n := range p.Nodes {
		if err := validID(n.ID, "node"); err != nil {
			return err
		}
		if nodes[n.ID] {
			return fmt.Errorf("duplicate node ID %q", n.ID)
		}
		nodes[n.ID] = true
	}
	if !nodes[p.Start] {
		return fmt.Errorf("start references missing node %q", p.Start)
	}
	for _, n := range p.Nodes {
		if err := validText(n.Text, "node "+n.ID+" text", 65536, false); err != nil {
			return err
		}
		if n.Speaker != "" && !chars[n.Speaker] {
			return fmt.Errorf("node %s: missing speaker %q", n.ID, n.Speaker)
		}
		if n.Encounter != "" && !encounters[n.Encounter] {
			return fmt.Errorf("node %s: missing encounter %q", n.ID, n.Encounter)
		}
		if n.Next != "" && !nodes[n.Next] {
			return fmt.Errorf("node %s: missing next node %q", n.ID, n.Next)
		}
		if n.Next != "" && len(n.Choices) > 0 {
			return fmt.Errorf("node %s: next and choices are mutually exclusive", n.ID)
		}
		if n.Encounter != "" && len(n.Choices) > 0 {
			return fmt.Errorf("battle node %s: choices are not allowed", n.ID)
		}
		if len(n.Choices) > 64 {
			return fmt.Errorf("node %s: too many choices (max 64)", n.ID)
		}
		if err := validateFlags(n.SetFlags); err != nil {
			return fmt.Errorf("node %s: %w", n.ID, err)
		}
		for _, ch := range n.Choices {
			if err := validText(ch.Text, "choice text", 4096, true); err != nil {
				return err
			}
			if ch.Next != "" && !nodes[ch.Next] {
				return fmt.Errorf("node %s: choice references missing node %q", n.ID, ch.Next)
			}
			if err := validateFlags(ch.RequireFlags); err != nil {
				return fmt.Errorf("node %s choice: %w", n.ID, err)
			}
		}
	}
	return nil
}

// Load reads a bounded, strict JSON document. Unknown fields and trailing JSON
// are errors so misspelled editor fields cannot be silently discarded.
func Load(path string) (Pack, error) {
	data, err := readPackData(path)
	if err != nil {
		return Pack{}, err
	}
	return Decode(data)
}

// Decode applies the same strict parsing and standalone validation as Load to
// editor text. Duplicate keys, unknown fields, invalid UTF-8 and trailing JSON
// are rejected, so file and UI imports cannot interpret the same pack differently.
func Decode(data []byte) (Pack, error) {
	p, err := decodePack(data)
	if err != nil {
		return Pack{}, err
	}
	if err := p.Validate(); err != nil {
		return Pack{}, err
	}
	return p, nil
}

// LoadExtension strictly decodes an extension document and validates it against
// base. The returned pack is the extension itself (including its chapter Start);
// call Merge to obtain combined content. This permits references to base records.
func LoadExtension(path string, base Pack) (Pack, error) {
	ext, err := readPack(path)
	if err != nil {
		return Pack{}, err
	}
	if _, err := Merge(base, ext); err != nil {
		return Pack{}, err
	}
	return ext, nil
}

func readPack(path string) (Pack, error) {
	data, err := readPackData(path)
	if err != nil {
		return Pack{}, err
	}
	return decodePack(data)
}

func readPackData(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
}

func decodePack(data []byte) (Pack, error) {
	if len(data) > MaxFileBytes {
		return Pack{}, fmt.Errorf("pack exceeds %d bytes", MaxFileBytes)
	}
	if !utf8.Valid(data) {
		return Pack{}, errors.New("pack is not valid UTF-8")
	}
	if err := checkJSONKeys(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return Pack{}, fmt.Errorf("decode pack: %w", err)
	}
	var p Pack
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Pack{}, fmt.Errorf("decode pack: %w", err)
	}
	var tail any
	if err := dec.Decode(&tail); err != io.EOF {
		if err == nil {
			err = errors.New("trailing JSON value")
		}
		return Pack{}, fmt.Errorf("decode pack: %w", err)
	}
	return p, nil
}

// checkJSONKeys prevents ambiguous object definitions; encoding/json otherwise
// silently accepts repeated keys and overwrites an earlier authored value.
func checkJSONKeys(dec *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("JSON nesting exceeds 32 levels")
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = true
			if err := checkJSONKeys(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := checkJSONKeys(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	_, err = dec.Token()
	return err
}

// Save validates before writing and atomically renames a temporary file in the
// destination directory. An invalid pack never replaces an existing document.
func Save(path string, p Pack) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return writePack(path, p)
}

// SaveExtension validates cross-references against base and atomically saves only
// the extension records, suitable for loading later with LoadExtension.
func SaveExtension(path string, extension, base Pack) error {
	if _, err := Merge(base, extension); err != nil {
		return err
	}
	return writePack(path, extension)
}

func writePack(path string, p Pack) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > MaxFileBytes {
		return fmt.Errorf("pack exceeds %d bytes", MaxFileBytes)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pal-pack-*.tmp")
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
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

// Clone returns an independent content graph, including flags and choices.
func Clone(p Pack) Pack {
	out := p
	out.Characters = append([]Character(nil), p.Characters...)
	out.Items = append([]Item(nil), p.Items...)
	out.Encounters = make([]Encounter, len(p.Encounters))
	for i, e := range p.Encounters {
		out.Encounters[i] = e
		out.Encounters[i].Enemies = append([]Character(nil), e.Enemies...)
	}
	out.Nodes = make([]Node, len(p.Nodes))
	for i, n := range p.Nodes {
		out.Nodes[i] = n
		out.Nodes[i].SetFlags = cloneFlags(n.SetFlags)
		out.Nodes[i].Choices = make([]Choice, len(n.Choices))
		for j, ch := range n.Choices {
			out.Nodes[i].Choices[j] = ch
			out.Nodes[i].Choices[j].RequireFlags = cloneFlags(ch.RequireFlags)
		}
	}
	return out
}
func cloneFlags(in map[string]bool) map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Merge appends extension records without replacing any base record. The base
// identity and start node remain unchanged. Extension references may target base
// records; the combined graph is validated. Use extension.Start to enter a new
// chapter explicitly after merge. Neither input shares mutable data with output.
func Merge(base, extension Pack) (Pack, error) {
	if err := base.Validate(); err != nil {
		return Pack{}, fmt.Errorf("base: %w", err)
	}
	if extension.SchemaVersion != SchemaVersion {
		return Pack{}, fmt.Errorf("extension: unsupported schema_version %d", extension.SchemaVersion)
	}
	if err := validID(extension.ID, "extension"); err != nil {
		return Pack{}, err
	}
	if extension.ID == base.ID {
		return Pack{}, errors.New("extension must have a different pack ID")
	}
	if err := validText(extension.Title, "extension title", 1024, true); err != nil {
		return Pack{}, err
	}
	out := Clone(base)
	ext := Clone(extension)
	out.Characters = append(out.Characters, ext.Characters...)
	out.Items = append(out.Items, ext.Items...)
	out.Encounters = append(out.Encounters, ext.Encounters...)
	out.Nodes = append(out.Nodes, ext.Nodes...)
	if err := out.Validate(); err != nil {
		return Pack{}, fmt.Errorf("merge: %w", err)
	}
	if extension.Start != "" {
		found := false
		for _, n := range ext.Nodes {
			if n.ID == extension.Start {
				found = true
				break
			}
		}
		if !found {
			return Pack{}, fmt.Errorf("extension start %q must be a node added by extension", extension.Start)
		}
	}
	return out, nil
}
