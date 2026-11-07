package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func loadCampaign(path string) (campaignManifest, string, []campaignLoadedSegment, []string, error) {
	var manifest campaignManifest
	data, manifestInfo, err := campaignReadRegular(path, journeyRouteLimit)
	if err != nil {
		return manifest, "", nil, nil, err
	}
	if !utf8.Valid(data) {
		return manifest, "", nil, nil, errors.New("campaign manifest must be UTF-8")
	}
	if err := journeyJSONTokens(data); err != nil {
		return manifest, "", nil, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, "", nil, nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return manifest, "", nil, nil, err
	}
	if err := journeyKeys(fields, "version", "segments"); err != nil {
		return manifest, "", nil, nil, err
	}
	if manifest.Version != 1 || len(manifest.Segments) < 1 || len(manifest.Segments) > campaignMaxSegments {
		return manifest, "", nil, nil, fmt.Errorf("campaign manifest needs version=1 and 1..%d segments", campaignMaxSegments)
	}
	digest := journeyBytesDigest(data)
	segments := make([]campaignLoadedSegment, 0, len(manifest.Segments))
	protected := []string{path}
	identities := []os.FileInfo{manifestInfo}
	digests := map[string]bool{}
	totalBytes, totalActions := 0, 0
	for i, name := range manifest.Segments {
		if strings.TrimSpace(name) == "" || len(name) > 4096 || strings.ContainsRune(name, '\x00') {
			return manifest, "", nil, nil, fmt.Errorf("campaign segment %d has invalid path", i+1)
		}
		full := name
		if !filepath.IsAbs(full) {
			full = filepath.Join(filepath.Dir(path), full)
		}
		full = filepath.Clean(full)
		data, info, err := campaignReadRegular(full, journeyRouteLimit)
		if err != nil {
			return manifest, "", nil, nil, fmt.Errorf("campaign segment %d: %w", i+1, err)
		}
		for _, other := range identities {
			if os.SameFile(info, other) {
				return manifest, "", nil, nil, fmt.Errorf("campaign segment %d repeats an input file or alias", i+1)
			}
		}
		totalBytes += len(data)
		if totalBytes > campaignMaxBytes {
			return manifest, "", nil, nil, errors.New("campaign segment inputs exceed the cumulative 64MiB limit")
		}
		spec, routeDigest, err := decodeJourneyRoute(data, i == 0)
		if err != nil {
			return manifest, "", nil, nil, fmt.Errorf("campaign segment %d: %w", i+1, err)
		}
		if digests[routeDigest] {
			return manifest, "", nil, nil, fmt.Errorf("campaign segment %d repeats an identical route document", i+1)
		}
		totalActions += len(spec.Actions)
		if totalActions > campaignMaxActions {
			return manifest, "", nil, nil, fmt.Errorf("campaign actions exceed the cumulative %d limit", campaignMaxActions)
		}
		if i > 0 && (!maps.Equal(spec.FilesSHA256, segments[0].spec.FilesSHA256) || !maps.Equal(spec.OptionalFilesSHA256, segments[0].spec.OptionalFilesSHA256)) {
			return manifest, "", nil, nil, fmt.Errorf("campaign segment %d changes the pinned resource hashes", i+1)
		}
		segments = append(segments, campaignLoadedSegment{path: full, digest: routeDigest, spec: spec})
		protected = append(protected, full)
		identities = append(identities, info)
		digests[routeDigest] = true
	}
	return manifest, digest, segments, protected, nil
}

func campaignReadRegular(path string, limit int64) ([]byte, os.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("campaign input must be a regular non-symlink file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Size() > limit {
		return nil, nil, fmt.Errorf("campaign input changed identity or exceeds %d bytes: %s", limit, path)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > limit {
		return nil, nil, fmt.Errorf("campaign input exceeds %d bytes: %s", limit, path)
	}
	return data, info, nil
}
