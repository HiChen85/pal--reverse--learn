package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDemoSaveLoadAndIndependentCopies(t *testing.T) {
	p := Demo()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "chapter.json")
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Ignore nil versus empty optional slices introduced by custom callers.
	before, _ := json.Marshal(p)
	after, _ := json.Marshal(got)
	if string(before) != string(after) {
		t.Fatalf("round trip changed content")
	}
	clone := Clone(p)
	clone.Nodes[2].SetFlags["knows-spring"] = false
	if !p.Nodes[2].SetFlags["knows-spring"] {
		t.Fatal("clone shares flags")
	}
	clone.Nodes[4].Choices[0].RequireFlags["knows-spring"] = false
	if !p.Nodes[4].Choices[0].RequireFlags["knows-spring"] {
		t.Fatal("clone shares choice flags")
	}
	clone.Encounters[0].Enemies[0].Name = "changed"
	if p.Encounters[0].Enemies[0].Name == "changed" {
		t.Fatal("clone shares enemies")
	}
	p.Characters[0].Name = "changed"
	if Demo().Characters[0].Name == "changed" {
		t.Fatal("Demo shares slices")
	}
}

func TestValidateRejectsBrokenAuthoring(t *testing.T) {
	cases := map[string]func(*Pack){
		"schema":              func(p *Pack) { p.SchemaVersion = 2 },
		"missing start":       func(p *Pack) { p.Start = "missing" },
		"missing speaker":     func(p *Pack) { p.Nodes[0].Speaker = "missing" },
		"missing battle":      func(p *Pack) { p.Nodes[3].Encounter = "missing" },
		"missing next":        func(p *Pack) { p.Nodes[0].Next = "missing" },
		"missing choice":      func(p *Pack) { p.Nodes[1].Choices[0].Next = "missing" },
		"duplicate node":      func(p *Pack) { p.Nodes = append(p.Nodes, p.Nodes[0]) },
		"duplicate character": func(p *Pack) { p.Characters = append(p.Characters, p.Characters[0]) },
		"bad stat":            func(p *Pack) { p.Characters[0].MaxHP = 0 },
		"overflow stat":       func(p *Pack) { p.Characters[0].Attack = maxStat + 1 },
		"bad item":            func(p *Pack) { p.Items[0].HealHP = -1 },
		"empty encounter":     func(p *Pack) { p.Encounters[0].Enemies = nil },
		"duplicate enemy":     func(p *Pack) { e := &p.Encounters[0]; e.Enemies = append(e.Enemies, e.Enemies[0]) },
		"choice and next":     func(p *Pack) { p.Nodes[1].Next = "ending" },
		"battle choice":       func(p *Pack) { p.Nodes[3].Next = ""; p.Nodes[3].Choices = []Choice{{Text: "skip", Next: "ending"}} },
		"blank name":          func(p *Pack) { p.Characters[0].Name = " " },
		"bad flag":            func(p *Pack) { p.Nodes[0].SetFlags = map[string]bool{"bad flag": true} },
		"bad UTF8":            func(p *Pack) { p.Nodes[0].Text = string([]byte{255}) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := Demo()
			mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("accepted invalid pack")
			}
		})
	}
}

func TestLoadStrictnessAndLimits(t *testing.T) {
	data, _ := json.Marshal(Demo())
	cases := map[string]string{
		"unknown":      strings.Replace(string(data), `"schema_version":1`, `"schema_version":1,"typo":true`, 1),
		"duplicate":    strings.Replace(string(data), `"schema_version":1`, `"schema_version":2,"schema_version":1`, 1),
		"trailing":     string(data) + `{}`,
		"null":         `null`,
		"malformed":    `{`,
		"invalid UTF8": string([]byte{255}),
		"too large":    strings.Repeat(" ", MaxFileBytes+1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pack.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("accepted invalid document")
			}
		})
	}
}

func TestInvalidSavePreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pack.json")
	p := Demo()
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p.Nodes[0].Next = "missing"
	if err := Save(path, p); err == nil {
		t.Fatal("invalid save succeeded")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("invalid save damaged existing file")
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".pal-pack-*.tmp"))
	if len(matches) > 0 {
		t.Fatal("left temp files")
	}
}

func TestMergeReferencesCollisionAndIsolation(t *testing.T) {
	base := Demo()
	ext := Pack{SchemaVersion: 1, ID: "chapter-two", Title: "第二章", Start: "two", Nodes: []Node{{ID: "two", Speaker: base.Characters[0].ID, Text: "续篇", Next: "ending", SetFlags: map[string]bool{"chapter-two": true}}}}
	merged, err := Merge(base, ext)
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID != base.ID || merged.Start != base.Start || len(merged.Nodes) != len(base.Nodes)+1 {
		t.Fatal("merge changed identity or dropped content")
	}
	merged.Nodes[len(merged.Nodes)-1].SetFlags["chapter-two"] = false
	if !ext.Nodes[0].SetFlags["chapter-two"] {
		t.Fatal("merge shares extension flags")
	}
	merged.Characters[0].Name = "changed"
	if base.Characters[0].Name == "changed" {
		t.Fatal("merge shares base characters")
	}
	before := Clone(base)
	ext.Nodes[0].ID = base.Nodes[0].ID
	ext.Start = ext.Nodes[0].ID
	if _, err := Merge(base, ext); err == nil {
		t.Fatal("accepted collision")
	}
	if !reflect.DeepEqual(Clone(base), before) {
		t.Fatal("failed merge mutated base")
	}
	ext.Nodes[0].ID = "two"
	ext.Start = "missing"
	if _, err := Merge(base, ext); err == nil {
		t.Fatal("accepted missing extension start")
	}
	ext.Start = "two"
	ext.Nodes[0].Next = "unknown"
	if _, err := Merge(base, ext); err == nil {
		t.Fatal("accepted dangling extension reference")
	}
}

func TestExtensionLoadAllowsBaseReferences(t *testing.T) {
	base := Demo()
	ext := Pack{SchemaVersion: 1, ID: "extension", Title: "续篇", Start: "extra", Nodes: []Node{{ID: "extra", Speaker: base.Characters[0].ID, Text: "新章", Next: "ending"}}}
	path := filepath.Join(t.TempDir(), "extension.json")
	if err := SaveExtension(path, ext, base); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("standalone load accepted extension without characters")
	}
	got, err := LoadExtension(path, base)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != ext.ID || got.Start != "extra" || len(got.Nodes) != 1 {
		t.Fatal("extension load returned merged pack")
	}
}

func TestRepositoryExtensionExample(t *testing.T) {
	ext, err := LoadExtension("../../examples/chapter-two.json", Demo())
	if err != nil {
		t.Fatal(err)
	}
	if len(ext.Characters) != 0 || ext.Start != "river-arrival" {
		t.Fatal("example must demonstrate base character references and independent entry")
	}
	merged, err := Merge(Demo(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Start != Demo().Start {
		t.Fatal("example changed base entry")
	}
}

func TestDecodeMatchesFileParsing(t *testing.T) {
	data, err := json.Marshal(Demo())
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(roundTrip) != string(data) {
		t.Fatal("Decode changed valid content")
	}
	cases := map[string][]byte{
		"invalid UTF8":      {255},
		"duplicate keys":    []byte(strings.Replace(string(data), `"schema_version":1`, `"schema_version":2,"schema_version":1`, 1)),
		"unknown field":     []byte(strings.Replace(string(data), `"schema_version":1`, `"schema_version":1,"typo":true`, 1)),
		"trailing document": append(append([]byte(nil), data...), []byte(` {}`)...),
		"invalid graph":     []byte(strings.Replace(string(data), `"start":"arrival"`, `"start":"missing"`, 1)),
		"too large":         []byte(strings.Repeat(" ", MaxFileBytes+1)),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(input); err == nil {
				t.Fatal("Decode accepted invalid input")
			}
			path := filepath.Join(t.TempDir(), "pack.json")
			if err := os.WriteFile(path, input, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("Load accepted invalid input")
			}
		})
	}
}
