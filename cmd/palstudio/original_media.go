package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
)

// Missing optional archives leave their providers absent. An operation that
// actually needs one then fails explicitly in palgame.Source.
func loadOriginalMedia(dir string) (*palasset.RNGCatalog, *palasset.FBPCatalog, []paldata.FileInfo, error) {
	var rng *palasset.RNGCatalog
	var fbp *palasset.FBPCatalog
	var files []paldata.FileInfo
	var failures []error
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, wanted := range []string{"RNG.MKF", "FBP.MKF"} {
		present := false
		for _, entry := range entries {
			if strings.EqualFold(entry.Name(), wanted) {
				present = true
				break
			}
		}
		if !present {
			continue
		}
		name, err := originalResourceName(dir, wanted)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		var digest string
		var size int64
		if wanted == "RNG.MKF" {
			rng, err = palasset.OpenRNG(filepath.Join(dir, name))
			if err == nil {
				digest, size = rng.SourceSHA256(), rng.SourceSize()
			}
		} else {
			fbp, err = palasset.OpenFBP(filepath.Join(dir, name))
			if err == nil {
				digest, size = fbp.SourceSHA256(), fbp.SourceSize()
			}
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		files = append(files, paldata.FileInfo{Name: name, SHA256: digest, Size: size})
	}
	return rng, fbp, files, errors.Join(failures...)
}

// Protection deliberately does not depend on successful parsing. A malformed
// media file, symlink or ambiguous case variant is still imported source data.
func (o *originalData) protectedOriginalResourcePaths() ([]string, error) {
	entries, err := os.ReadDir(o.loadedDirectory)
	if err != nil {
		return nil, err
	}
	wanted := []string{"SSS.MKF", "DATA.MKF", "M.MSG", "WORD.DAT", "MGO.MKF", "MAP.MKF", "GOP.MKF", "PAT.MKF", "RNG.MKF", "FBP.MKF"}
	var paths []string
	for _, entry := range entries {
		if slices.Contains(wanted, strings.ToUpper(entry.Name())) {
			paths = append(paths, filepath.Join(o.loadedDirectory, entry.Name()))
		}
	}
	return paths, nil
}
