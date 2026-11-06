package main

import (
	"errors"
	"flag"
	"io"
	"path/filepath"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
)

func runEntry(args []string, out, errout io.Writer) error {
	fs := flag.NewFlagSet("enter", flag.ContinueOnError)
	fs.SetOutput(errout)
	dir := fs.String("data", "", "四张原始数据表目录")
	encoding := fs.String("encoding", "", "big5 或 gbk")
	layout := fs.String("layout", "windows", "windows 或 dos 对象布局")
	scene := fs.Int("scene", 1, "显式场景编号；非1只是声明的检查点")
	dest := fs.String("out", "", "JSON 输出路径，默认 stdout")
	includeText := fs.Bool("include-text", false, "本地报告保留原始解码对白")
	steps := fs.Int("max-steps", 10000, "顶层、装备子调用和帧更新的统一预算")
	mgo := fs.String("mgo", "", "可选的本地 MGO.MKF；用于实际解压玩家精灵")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *dir == "" || fs.NArg() != 0 {
		return errors.New("enter 需要 -data 和 -encoding；不接受位置参数")
	}
	if *scene < 1 || *scene > 299 {
		return errors.New("场景编号须为1..299且有后继事件边界")
	}
	if *steps < 1 || *steps > palflow.MaxAllowedSteps {
		return errors.New("max-steps 须为1..100000")
	}
	if *dest != "" && strings.ToLower(filepath.Ext(*dest)) != ".json" {
		return errors.New("报告仅允许写入 JSON")
	}
	g, err := paldata.Load(*dir, paldata.Options{Encoding: *encoding, Layout: *layout})
	if err != nil {
		return err
	}
	r, err := paldata.LoadRuntime(*dir)
	if err != nil {
		return err
	}
	var assets *palasset.SpriteCatalog
	if *mgo != "" {
		assets, err = palasset.OpenMGO(*mgo, 8<<20)
		if err != nil {
			return err
		}
	}
	s, err := palgame.NewSession(g, r, palgame.SessionOptions{Scene: uint16(*scene), MaxSteps: *steps, RedactText: !*includeText, Assets: assets})
	if err != nil {
		return err
	}
	before := s.Snapshot()
	initial := s.Initialization()
	initial.World = nil // The same bootstrap world is already recorded in Before.
	var mgoIdentity *paldata.FileInfo
	if assets != nil {
		mgoIdentity = &paldata.FileInfo{Name: "MGO.MKF", SHA256: assets.SourceSHA256(), Size: assets.SourceSize()}
	}
	report, runErr := s.Enter(nil)
	evidence := struct {
		Initialization   palgame.Initialization  `json:"initialization"`
		MGO              *paldata.FileInfo       `json:"mgo_identity,omitempty"`
		Options          palflow.Options         `json:"options"`
		Version          int                     `json:"format_version"`
		Kind             string                  `json:"kind"`
		FullGameVerified bool                    `json:"full_game_verified"`
		Dataset          datasetSummary          `json:"dataset"`
		Before           palgame.SessionSnapshot `json:"before"`
		Report           palflow.Report          `json:"report"`
		After            palgame.SessionSnapshot `json:"after"`
		Committed        bool                    `json:"committed"`
		Scope            string                  `json:"scope"`
	}{Initialization: initial, MGO: mgoIdentity, Options: palflow.Options{MaxSteps: *steps, MaxCallDepth: 64, MaxTextBytes: 1 << 20}, Version: 1, Kind: "logical_scene_entry", Dataset: summarize(g), Before: before, Report: report, After: s.Snapshot(), Committed: runErr == nil, Scope: "Imported initial-state/checkpoint model; no spatial reachability, original-runtime comparison, full main-story or side-quest coverage verified."}
	if err = writeReport(*dest, evidence, out); err != nil {
		return err
	}
	return runErr
}
