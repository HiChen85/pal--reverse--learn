package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash"
	"io"
	"maps"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
)

const (
	campaignMaxSegments = 256
	campaignMaxBytes    = 64 << 20
	campaignMaxActions  = 250000
)

type campaignManifest struct {
	Version  int      `json:"version"`
	Segments []string `json:"segments"`
}

type campaignLoadedSegment struct {
	path   string
	digest string
	spec   journeyRoute
}

type campaignSegmentResult struct {
	Index            int    `json:"index"`
	Route            string `json:"route"`
	RouteSHA256      string `json:"route_sha256"`
	Report           string `json:"report"`
	ReportSHA256     string `json:"report_sha256,omitempty"`
	Status           string `json:"status"`
	InitialSHA256    string `json:"initial_sha256,omitempty"`
	FinalSHA256      string `json:"final_sha256,omitempty"`
	ActionsAttempted int    `json:"actions_attempted"`
	ActionsCommitted int    `json:"actions_committed"`
	Error            string `json:"error,omitempty"`
}

type campaignReport struct {
	Version             int                     `json:"format_version"`
	Kind                string                  `json:"kind"`
	ManifestSHA256      string                  `json:"manifest_sha256"`
	FullGameVerified    bool                    `json:"full_game_verified"`
	Completed           bool                    `json:"completed"`
	Error               string                  `json:"error,omitempty"`
	Scope               string                  `json:"scope"`
	Dataset             datasetSummary          `json:"dataset"`
	Files               []fileEvidence          `json:"files"`
	ExecutionPolicy     journeyPolicy           `json:"execution_policy"`
	OptionalFilesSHA256 map[string]string       `json:"optional_files_sha256,omitempty"`
	InitialSHA256       string                  `json:"initial_sha256"`
	FinalSHA256         string                  `json:"final_sha256"`
	ActionSummarySHA256 string                  `json:"action_summary_sha256"`
	ActionsPlanned      int                     `json:"actions_planned"`
	ActionsAttempted    int                     `json:"actions_attempted"`
	ActionsCommitted    int                     `json:"actions_committed"`
	Segments            []campaignSegmentResult `json:"segments"`
}

func runCampaign(args []string, out, errout io.Writer) error {
	fs := flag.NewFlagSet("campaign", flag.ContinueOnError)
	fs.SetOutput(errout)
	dir := fs.String("data", "", "同目录的原版数据表、地图与媒体资源")
	manifestPath := fs.String("manifest", "", "version=1、按顺序列出segments路径的JSON清单")
	outDir := fs.String("out-dir", "", "报告目录；不覆盖已有输出，其父目录须已存在")
	encoding := fs.String("encoding", "", "big5或gbk；默认采用首段路线")
	layout := fs.String("layout", "", "dos或windows；默认采用首段路线或windows")
	maxSteps := fs.Int("max-steps", 10000, "每个动作的统一执行预算")
	compact := fs.Bool("compact", true, "成功动作只保留摘要；失败动作保留完整可用诊断")
	battleMode := fs.String("battle-mode", "auto_attack", "auto_attack或fast_verify")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *dir == "" || *manifestPath == "" || *outDir == "" || fs.NArg() != 0 {
		return errors.New("campaign需要-data、-manifest、-out-dir；不接受位置参数")
	}
	if *maxSteps < 1 || *maxSteps > palflow.MaxAllowedSteps {
		return errors.New("campaign max-steps须为1..100000")
	}
	if *battleMode != "auto_attack" && *battleMode != "fast_verify" {
		return errors.New("campaign battle-mode须为auto_attack或fast_verify")
	}
	manifest, manifestDigest, segments, protected, err := loadCampaign(*manifestPath)
	if err != nil {
		return err
	}
	selectedEncoding, err := journeySetting("encoding", *encoding, segments[0].spec.Encoding, "")
	if err != nil {
		return err
	}
	selectedLayout, err := journeySetting("layout", *layout, segments[0].spec.Layout, "windows")
	if err != nil {
		return err
	}
	for i, segment := range segments {
		if _, err := journeySetting("encoding", selectedEncoding, segment.spec.Encoding, ""); err != nil {
			return fmt.Errorf("campaign segment %d: %w", i+1, err)
		}
		if _, err := journeySetting("layout", selectedLayout, segment.spec.Layout, ""); err != nil {
			return fmt.Errorf("campaign segment %d: %w", i+1, err)
		}
	}
	paths, err := journeyFiles(*dir)
	if err != nil {
		return err
	}
	optionalPaths, err := journeyOptionalPaths(*dir)
	if err != nil {
		return err
	}
	for _, name := range journeyFileNames {
		protected = append(protected, paths[name])
	}
	for _, name := range journeyOptionalNames {
		if path := optionalPaths[name]; path != "" {
			protected = append(protected, path)
		}
	}
	g, err := paldata.Load(*dir, paldata.Options{Encoding: selectedEncoding, Layout: selectedLayout})
	if err != nil {
		return err
	}
	runtime, err := paldata.LoadRuntime(*dir)
	if err != nil {
		return err
	}
	assets, err := palasset.OpenMGO(paths["MGO.MKF"], 8<<20)
	if err != nil {
		return err
	}
	mapCatalog, err := palasset.OpenMaps(paths["MAP.MKF"], paths["GOP.MKF"])
	if err != nil {
		return err
	}
	media, err := journeyOpenMedia(optionalPaths)
	if err != nil {
		return err
	}
	files := make([]fileEvidence, 0, len(journeyFileNames)+len(media.files))
	for _, file := range g.Files {
		files = append(files, fileEvidence{Name: strings.ToUpper(file.Name), SHA256: file.SHA256, Size: file.Size})
	}
	files = append(files,
		fileEvidence{Name: "MGO.MKF", SHA256: assets.SourceSHA256(), Size: assets.SourceSize()},
		fileEvidence{Name: "MAP.MKF", SHA256: mapCatalog.MAPSourceSHA256(), Size: mapCatalog.MAPSourceSize()},
		fileEvidence{Name: "GOP.MKF", SHA256: mapCatalog.GOPSourceSHA256(), Size: mapCatalog.GOPSourceSize()})
	// All segment bindings are checked before constructing the one active
	// Session. Imported catalogs retain immutable snapshots throughout the run.
	for i, segment := range segments {
		for _, file := range files {
			if segment.spec.FilesSHA256[file.Name] != file.SHA256 {
				return fmt.Errorf("campaign segment %d resource SHA-256 mismatch: %s", i+1, file.Name)
			}
		}
		if err := journeyMatchOptional(segment.spec.OptionalFilesSHA256, media.files); err != nil {
			return fmt.Errorf("campaign segment %d: %w", i+1, err)
		}
	}
	files = append(files, media.files...)
	output, err := prepareCampaignOutput(*outDir, len(segments), protected)
	if err != nil {
		return err
	}
	session, err := palgame.NewSession(g, runtime, palgame.SessionOptions{Assets: assets, Maps: mapCatalog, Palettes: media.palettes, RNG: media.rng, FBP: media.fbp, BattleMode: *battleMode, RedactText: true, MaxSteps: *maxSteps})
	if err != nil {
		return err
	}
	initial := session.Snapshot()
	index := campaignReport{Version: 1, Kind: "continuous_session_campaign", ManifestSHA256: manifestDigest, Scope: "Ordered hash-bound route segments run in one continuous new-game Session without restore, injected worlds or scene resets. Segment reports chain complete Session snapshot hashes, including the cumulative committed-action count. Completion covers only this manifest; original-runtime parity, the complete main story and all side quests remain unverified.", Dataset: summarize(g), Files: files, OptionalFilesSHA256: maps.Clone(segments[0].spec.OptionalFilesSHA256), InitialSHA256: journeyDigest(initial), Segments: make([]campaignSegmentResult, len(segments))}
	index.ExecutionPolicy = journeyPolicy{VM: session.ExecutionOptions(), RandomMode: initial.World.RandomMode, BattleMode: initial.World.BattleMode, InitialRandomState: initial.World.BattleRandomState, BattleSimplifications: palgame.BattlePolicyNotes(initial.World.BattleMode)}
	for i, segment := range segments {
		index.Segments[i] = campaignSegmentResult{Index: i + 1, Route: manifest.Segments[i], RouteSHA256: segment.digest, Report: campaignSegmentName(i), Status: "not_run"}
		index.ActionsPlanned += len(segment.spec.Actions)
	}
	summary := sha256.New()
	var executionErr error
	for i, segment := range segments {
		before := session.Snapshot()
		r, stepErr := executeJourneyDetail(session, segment.spec, segment.digest, index.Dataset, files, *compact)
		r.Kind = "continuous_session_campaign_segment"
		r.Scope = fmt.Sprintf("Campaign segment %d continues the same live Session. initial_sha256 is this segment's actual pre-action Session snapshot, linked to the preceding segment's final snapshot. Initialization describes the original bootstrap and was not rerun. Completion covers this segment only; complete main story, side quests and original-runtime parity are not verified.", i+1)
		if !*compact && i > 0 {
			r.TraceStateEncoding = "The preceding campaign segment's final world and this segment's initial_idle_frames, plus the ordered deltas, reconstruct intermediate snapshots. Initialization is original new-game metadata, not a replacement world. Final includes the complete snapshot."
		}
		entry := &index.Segments[i]
		entry.InitialSHA256, entry.FinalSHA256 = r.InitialSHA256, journeyDigest(r.Final)
		entry.ActionsAttempted, entry.ActionsCommitted = len(r.Actions), r.Final.Actions-before.Actions
		index.ActionsAttempted += entry.ActionsAttempted
		index.ActionsCommitted += entry.ActionsCommitted
		for _, action := range r.Actions {
			campaignHashAction(summary, action)
		}
		data, encodeErr := campaignReportBytes(r)
		if encodeErr != nil {
			stepErr = errors.Join(stepErr, encodeErr)
		} else if err := output.write(entry.Report, data); err != nil {
			stepErr = errors.Join(stepErr, err)
		} else {
			entry.ReportSHA256 = journeyBytesDigest(data)
		}
		entry.Status = "completed"
		if stepErr != nil {
			entry.Status, entry.Error = "failed", stepErr.Error()
			executionErr = fmt.Errorf("campaign segment %d (%s): %w", i+1, manifest.Segments[i], stepErr)
			break
		}
	}
	index.FinalSHA256 = journeyDigest(session.Snapshot())
	index.ActionSummarySHA256 = hex.EncodeToString(summary.Sum(nil))
	index.Completed = executionErr == nil
	if executionErr != nil {
		index.Error = executionErr.Error()
	}
	data, err := campaignReportBytes(index)
	if err != nil {
		return errors.Join(executionErr, err)
	}
	if err := output.write("campaign.json", data); err != nil {
		executionErr = errors.Join(executionErr, err)
		index.Completed, index.Error = false, executionErr.Error()
		data, err = campaignReportBytes(index)
		if err != nil {
			return errors.Join(executionErr, err)
		}
	}
	if _, err := out.Write(data); err != nil {
		return errors.Join(executionErr, err)
	}
	return executionErr
}

func campaignSegmentName(index int) string { return fmt.Sprintf("segment-%04d.json", index+1) }

// Per-action hashes exclude the segment-local index and bulky trace details.
// Thus the same continuous action stream has the same summary across splits.
func campaignHashAction(h hash.Hash, action journeyActionReport) {
	value := struct {
		BeforeSHA256, AfterSHA256 string
		Work                      int
		Messages                  []uint16
		Battles                   []journeyBattleSummary
		Committed, Matched        bool
		Error                     string
	}{action.BeforeSHA256, action.AfterSHA256, action.Work, action.MessageIDs, action.Battles, action.Committed, action.ExpectationsMatched, action.Error}
	data, _ := json.Marshal(value)
	h.Write(data)
	h.Write([]byte{'\n'})
}

func campaignReportBytes(report any) ([]byte, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data)+1 > journeyReportLimit {
		return nil, errors.New("campaign report exceeds 32MiB; not published")
	}
	return append(data, '\n'), nil
}
