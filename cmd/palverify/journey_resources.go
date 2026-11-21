package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
)

var journeyOptionalNames = []string{"PAT.MKF", "RNG.MKF", "FBP.MKF"}

type journeyMediaResources struct {
	palettes *palasset.PaletteCatalog
	rng      *palasset.RNGCatalog
	fbp      *palasset.FBPCatalog
	files    []fileEvidence
}

// Discover paths before opening catalogs, so an invalid optional input still
// cannot be overwritten by a report through an alternate filename or hardlink.
func journeyOptionalPaths(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := map[string]string{}
	for _, entry := range entries {
		name := strings.ToUpper(entry.Name())
		if !slices.Contains(journeyOptionalNames, name) {
			continue
		}
		if paths[name] != "" {
			return nil, fmt.Errorf("journey: ambiguous %s", name)
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("journey: %s must be a regular non-symlink file", name)
		}
		paths[name] = filepath.Join(dir, entry.Name())
	}
	return paths, nil
}

func journeyOpenMedia(paths map[string]string) (journeyMediaResources, error) {
	var result journeyMediaResources
	var err error
	for _, name := range journeyOptionalNames {
		path := paths[name]
		if path == "" {
			continue
		}
		var digest string
		var size int64
		switch name {
		case "PAT.MKF":
			result.palettes, err = palasset.OpenPAT(path)
			if err == nil {
				digest, size = result.palettes.SourceSHA256(), result.palettes.SourceSize()
			}
		case "RNG.MKF":
			result.rng, err = palasset.OpenRNG(path)
			if err == nil {
				digest, size = result.rng.SourceSHA256(), result.rng.SourceSize()
			}
		case "FBP.MKF":
			result.fbp, err = palasset.OpenFBP(path)
			if err == nil {
				digest, size = result.fbp.SourceSHA256(), result.fbp.SourceSize()
			}
		}
		if err != nil {
			return result, fmt.Errorf("journey %s: %w", name, err)
		}
		result.files = append(result.files, fileEvidence{Name: name, SHA256: digest, Size: size})
	}
	return result, nil
}

func journeyCheckOptionalHashes(hashes map[string]string) error {
	for name, digest := range hashes {
		decoded, err := hex.DecodeString(digest)
		if !slices.Contains(journeyOptionalNames, name) || err != nil || len(decoded) != 32 || digest != strings.ToLower(digest) {
			return fmt.Errorf("journey 无效 optional_files_sha256 名称或摘要: %s", name)
		}
	}
	return nil
}

func journeyMatchOptional(hashes map[string]string, files []fileEvidence) error {
	for name, digest := range hashes {
		found := false
		for _, file := range files {
			if file.Name == name && file.SHA256 == digest {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("journey 可选资源缺失或摘要不匹配: %s", name)
		}
	}
	return nil
}
