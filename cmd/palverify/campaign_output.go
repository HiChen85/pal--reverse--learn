package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type campaignOutput struct {
	dir       string
	identity  os.FileInfo
	protected []string
}

func prepareCampaignOutput(dir string, count int, protected []string) (campaignOutput, error) {
	var output campaignOutput
	abs, err := filepath.Abs(dir)
	if err != nil {
		return output, err
	}
	abs = filepath.Clean(abs)
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return output, err
	}
	if parent != filepath.Dir(abs) {
		return output, errors.New("campaign output directory must not use a symlink alias")
	}
	info, err := os.Lstat(abs)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return output, err
	}
	if err == nil && !info.IsDir() {
		return output, errors.New("campaign output must be a real directory, not a symlink or file")
	}
	// Check every generated output before creating even a new directory. The
	// parent's canonical spelling makes these paths stable while it is absent.
	for i := -1; i < count; i++ {
		name := "campaign.json"
		if i >= 0 {
			name = campaignSegmentName(i)
		}
		target := filepath.Join(abs, name)
		if _, err := os.Lstat(target); err == nil {
			return output, fmt.Errorf("campaign output already exists; choose another directory: %s", target)
		} else if !errors.Is(err, os.ErrNotExist) {
			return output, err
		}
		for _, input := range protected {
			source, err := filepath.Abs(input)
			if err != nil {
				return output, err
			}
			if filepath.Clean(source) == target {
				return output, fmt.Errorf("campaign output cannot overwrite an input: %s", input)
			}
		}
	}
	if info == nil {
		if err := os.Mkdir(abs, 0700); err != nil {
			return output, err
		}
		info, err = os.Lstat(abs)
		if err != nil {
			return output, err
		}
	}
	return campaignOutput{dir: abs, identity: info, protected: append([]string(nil), protected...)}, nil
}

func (o campaignOutput) write(name string, data []byte) error {
	if filepath.Base(name) != name || filepath.Ext(name) != ".json" {
		return errors.New("campaign output name must be a local JSON filename")
	}
	current, err := os.Lstat(o.dir)
	if err != nil {
		return err
	}
	if !current.IsDir() || !os.SameFile(current, o.identity) {
		return errors.New("campaign output directory changed identity")
	}
	path := filepath.Join(o.dir, name)
	if err := journeyOutputPath(path, o.protected); err != nil {
		return err
	}
	f, err := os.CreateTemp(o.dir, ".pal-campaign-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Publishing with Link is atomic and fails if any destination has appeared
	// since preflight. Rename would overwrite a concurrently created input.
	if err := os.Link(tmp, path); err != nil {
		return fmt.Errorf("campaign could not publish new output %s: %w", name, err)
	}
	return nil
}
