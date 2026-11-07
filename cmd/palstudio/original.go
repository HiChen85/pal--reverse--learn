package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
	"github.com/egoist/mygo/ui"
)

// originalData is separate from editable content: importing the original tables
// never changes a project, writes game resources, or invents a quest result.
type originalData struct {
	directory, encoding, layout                   string
	loadedDirectory, loadedEncoding, loadedLayout string
	game                                          *paldata.Game
	runtime                                       *paldata.RuntimeTables
	assets                                        *palasset.SpriteCatalog
	maps                                          *palasset.MapCatalog
	palettes                                      *palasset.PaletteCatalog
	rng                                           *palasset.RNGCatalog
	fbp                                           *palasset.FBPCatalog
	mediaIdentities                               []paldata.FileInfo
	mediaError                                    string
	mapIdentities                                 []paldata.FileInfo
	paletteIdentity                               *paldata.FileInfo
	mapError, paletteError, viewportError         string
	viewport                                      *ui.Bitmap
	mgoIdentity                                   *paldata.FileInfo
	assetError                                    string
	runtimeError                                  string
	session                                       *palgame.Session
	useSession, assumeEntered                     bool
	fastVerify                                    bool
	checkpoint                                    originalCheckpoint
	scene, event                                  string
	item, itemRole                                string
	inputsJSON, exportPath                        string
	savePath, saveResult                          string
	lastEvent                                     float64
	maxSteps                                      float64
	enterPC                                       map[int]uint16
	triggerPC                                     map[int]uint16
	history                                       []originalInvocation
}
type originalCheckpoint struct {
	Scene              uint16 `json:"scene"`
	AssumeSceneEntered bool   `json:"assume_scene_entered"`
}
type originalActionIntent struct {
	Path        string          `json:"path,omitempty"`
	Kind        string          `json:"kind"`
	SceneNumber int             `json:"scene_number"`
	EventID     uint16          `json:"event_id"`
	Inputs      []palflow.Input `json:"inputs"`
	Direction   *uint16         `json:"direction,omitempty"`
	Item        *uint16         `json:"item,omitempty"`
	TargetRole  *uint16         `json:"target_role,omitempty"`
}
type originalInvocation struct {
	Invoked          bool                     `json:"invoked"`
	ActionIntent     originalActionIntent     `json:"action_intent"`
	Options          *palflow.Options         `json:"options,omitempty"`
	Mode             string                   `json:"mode"`
	Error            string                   `json:"error,omitempty"`
	Checkpoint       *originalCheckpoint      `json:"checkpoint,omitempty"`
	SessionBefore    *palgame.SessionSnapshot `json:"session_before,omitempty"`
	SessionAfter     *palgame.SessionSnapshot `json:"session_after,omitempty"`
	SessionCommitted bool                     `json:"session_committed"`
	Kind             string                   `json:"kind"`
	SceneNumber      int                      `json:"scene_number"`
	EventID          uint16                   `json:"event_id"`
	Request          *palflow.Request         `json:"request,omitempty"`
	Report           palflow.Report           `json:"report"`
	Control          *palgame.ControlReport   `json:"control,omitempty"`
}
type originalEvidence struct {
	MGOIdentity       *paldata.FileInfo  `json:"mgo_identity,omitempty"`
	MapIdentities     []paldata.FileInfo `json:"map_identities,omitempty"`
	PaletteIdentity   *paldata.FileInfo  `json:"palette_identity,omitempty"`
	MediaIdentities   []paldata.FileInfo `json:"media_identities,omitempty"`
	SpriteDecodeLimit int                `json:"sprite_decode_limit"`
	SchemaVersion     int                `json:"schema_version"`
	Scope             string             `json:"scope"`
	Encoding          string             `json:"encoding"`
	Layout            string             `json:"layout"`
	Files             []paldata.FileInfo `json:"files"`
	Invocation        originalInvocation `json:"invocation"`
}

func newOriginalData() *originalData {
	return &originalData{directory: filepath.Join("assets", "original"), encoding: "big5", layout: "dos", inputsJSON: "[]", exportPath: "original.paltrace.json", savePath: "original.palsave.json", maxSteps: 10000}
}
func (o *originalData) load() error {
	g, err := paldata.Load(o.directory, paldata.Options{Encoding: o.encoding, Layout: o.layout})
	if err != nil {
		return err
	}
	o.game = g
	o.loadedDirectory = o.directory
	o.loadedEncoding = o.encoding
	o.loadedLayout = o.layout
	o.scene = "1"
	o.event = ""
	o.enterPC = map[int]uint16{}
	o.triggerPC = map[int]uint16{}
	o.history = nil
	o.saveResult = ""
	o.lastEvent = 0
	o.inputsJSON = "[]"
	for i, s := range g.Scenes {
		o.enterPC[i] = s.OnEnter
	}
	for i, e := range g.Events {
		o.triggerPC[i] = e.Trigger
	}
	o.assets, o.mgoIdentity, err = loadOriginalAssets(o.directory)
	o.assetError = ""
	if err != nil {
		o.assetError = err.Error()
	}
	o.maps, o.mapIdentities, err = loadOriginalMaps(o.directory)
	o.mapError = ""
	if err != nil {
		o.mapError = err.Error()
	}
	o.palettes, o.paletteIdentity, err = loadOriginalPalette(o.directory)
	o.paletteError = ""
	if err != nil {
		o.paletteError = err.Error()
	}
	o.rng, o.fbp, o.mediaIdentities, err = loadOriginalMedia(o.directory)
	o.mediaError = ""
	if err != nil {
		o.mediaError = err.Error()
	}
	o.viewport, o.viewportError = nil, ""
	o.runtime = nil
	o.session = nil
	o.runtimeError = ""
	o.useSession = false
	o.assumeEntered = false
	rt, runtimeErr := paldata.LoadRuntime(o.directory)
	if runtimeErr == nil {
		runtimeErr = rt.ValidateReferences(g)
	}
	if runtimeErr == nil {
		o.runtime = rt
		runtimeErr = o.resetSession()
	}
	if runtimeErr != nil {
		o.runtimeError = runtimeErr.Error()
		o.runtime = nil
		o.session = nil
	}
	return nil
}
func (o *originalData) changedSource() bool {
	return o.game != nil && (o.directory != o.loadedDirectory || o.encoding != o.loadedEncoding || o.layout != o.loadedLayout)
}
func (o *originalData) sceneIndex() (int, error) {
	if o.game == nil {
		return 0, errors.New("请先读取原版数据目录")
	}
	n, err := strconv.Atoi(o.scene)
	if err != nil || n < 1 || n > len(o.game.Scenes) {
		return 0, errors.New("请选择有效的场景编号")
	}
	return n - 1, nil
}
func (o *originalData) resetSession() error {
	if o.runtime == nil {
		return errors.New("运行表未成功导入，逻辑会话不可用")
	}
	if o.changedSource() {
		return errors.New("来源选项已改变，请重新读取后重置")
	}
	index, err := o.sceneIndex()
	if err != nil {
		return err
	}
	if o.maxSteps < 1 || o.maxSteps > palflow.MaxAllowedSteps || o.maxSteps != float64(int(o.maxSteps)) {
		return errors.New("会话工作量上限必须是允许范围内的整数")
	}
	next, err := palgame.NewSession(o.game, o.runtime, palgame.SessionOptions{Assets: o.assets, Maps: o.maps, Palettes: o.palettes, RNG: o.rng, FBP: o.fbp, BattleMode: o.selectedBattleMode(), Scene: uint16(index + 1), AssumeSceneEntered: o.assumeEntered, MaxSteps: int(o.maxSteps)})
	if err != nil {
		return err
	}
	o.session = next
	o.checkpoint = originalCheckpoint{Scene: uint16(index + 1), AssumeSceneEntered: o.assumeEntered}
	o.history = nil
	o.event = ""
	o.updateViewport()
	return nil
}
func (o *originalData) activeSceneIndex() (int, error) {
	if o.useSession {
		if o.session == nil {
			return 0, errors.New("请先建立逻辑会话")
		}
		return int(o.session.Snapshot().World.Scene) - 1, nil
	}
	return o.sceneIndex()
}
func (o *originalData) eventRange() (int, int, error) {
	index, err := o.activeSceneIndex()
	if err != nil {
		return 0, 0, err
	}
	if o.useSession {
		world := o.session.Snapshot().World
		current, ok := world.Scenes[uint16(index+1)]
		next, nextOK := world.Scenes[uint16(index+2)]
		if !ok || !nextOK {
			return 0, 0, errors.New("当前逻辑场景没有后继 EventStart，不能推断事件范围")
		}
		start, end := int(current.EventStart), int(next.EventStart)
		if start > end || end > len(o.game.Events) {
			return 0, 0, errors.New("逻辑场景事件边界超出事件表")
		}
		return start, end, nil
	}
	if index+1 >= len(o.game.Scenes) {
		return 0, 0, errors.New("末条场景记录没有后继 EventStart，不能推断事件范围")
	}
	start, end := int(o.game.Scenes[index].EventStart), int(o.game.Scenes[index+1].EventStart)
	if start > end || end > len(o.game.Events) {
		return 0, 0, errors.New("场景事件边界超出已导入事件表")
	}
	return start, end, nil
}

func decodeOriginalInputs(data string) ([]palflow.Input, error) {
	if len(data) > 64<<10 {
		return nil, errors.New("输入 JSON 超过 64 KiB")
	}
	dec := json.NewDecoder(bytes.NewBufferString(data))
	token, err := dec.Token()
	if err != nil || token != json.Delim('[') {
		return nil, errors.New("显式输入必须是 JSON 数组")
	}
	result := []palflow.Input{}
	for dec.More() {
		if len(result) >= 4096 {
			return nil, errors.New("最多允许 4096 个显式输入")
		}
		token, err = dec.Token()
		if err != nil || token != json.Delim('{') {
			return nil, errors.New("每个显式输入必须是 JSON 对象")
		}
		var in palflow.Input
		seen := map[string]bool{}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return nil, errors.New("显式输入含有重复字段")
			}
			seen[key] = true
			switch key {
			case "kind":
				if err := dec.Decode(&in.Kind); err != nil {
					return nil, err
				}
			case "value":
				var value *int
				if err := dec.Decode(&value); err != nil {
					return nil, err
				}
				if value == nil {
					return nil, errors.New("显式 value 不能为 null")
				}
				in.Value = *value
			default:
				return nil, fmt.Errorf("显式输入字段未知: %q", key)
			}
		}
		if token, err = dec.Token(); err != nil || token != json.Delim('}') {
			return nil, errors.New("显式输入对象未完整结束")
		}
		if !seen["kind"] || !seen["value"] {
			return nil, errors.New("显式输入必须同时包含 kind 与 value")
		}
		i := len(result)
		switch in.Kind {
		case palflow.InputShop:
			if in.Value < 0 || in.Value > 65535 {
				return nil, fmt.Errorf("输入 %d 的 shop 必须为0..65535；0关闭，其余为物品ID", i)
			}
		case palflow.InputRandom:
			if in.Value < 0 || in.Value > 2147483647 {
				return nil, fmt.Errorf("输入 %d 的 random 必须为 0..2147483647；具体指令另行检查其范围", i)
			}
		case palflow.InputKey:
			if in.Value != 1 {
				return nil, fmt.Errorf("输入 %d 的 key 必须为 1，表示显式按键确认", i)
			}
		case palflow.InputConfirm:
			if in.Value != 0 && in.Value != 1 {
				return nil, fmt.Errorf("输入 %d 的 confirm 必须为 0 或 1", i)
			}
		case palflow.InputBattle:
			if in.Value < 0 || in.Value > 2 {
				return nil, fmt.Errorf("输入 %d 的 battle 必须为 0..2", i)
			}
		default:
			return nil, fmt.Errorf("输入 %d 的 kind 未知: %q", i, in.Kind)
		}
		result = append(result, in)
	}
	if token, err = dec.Token(); err != nil || token != json.Delim(']') {
		return nil, errors.New("显式输入数组未完整结束")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("输入必须是单个 JSON 数组")
	}
	return result, nil
}

func (o *originalData) run(kind string) error {
	index, err := o.activeSceneIndex()
	if err != nil {
		return err
	}
	if o.changedSource() {
		return errors.New("来源选项已改变，请重新读取后运行")
	}
	inputs, err := decodeOriginalInputs(o.inputsJSON)
	if err != nil {
		return err
	}
	if o.useSession {
		return o.runSession(kind, inputs)
	}
	request := palflow.Request{Inputs: inputs, LastEventID: uint16(o.lastEvent), IdleFrames: map[uint16]uint16{}}
	for i, e := range o.game.Events {
		request.IdleFrames[uint16(i+1)] = e.ScriptIdleFrame
	}
	eventIndex := -1
	switch kind {
	case "on_enter":
		request.Entry = o.enterPC[index]
		request.EventID = 0xffff
	case "event_trigger":
		start, end, err := o.eventRange()
		if err != nil {
			return err
		}
		id, err := strconv.Atoi(o.event)
		if err != nil || id < start+1 || id > end {
			return errors.New("请选择当前场景范围内的事件")
		}
		eventIndex = id - 1
		request.EventID = uint16(id)
		request.Entry = o.triggerPC[eventIndex]
	default:
		return errors.New("未知原版脚本调用类型")
	}
	if o.runtime != nil {
		request.World, err = palgame.InitialSnapshot(o.game, o.runtime, uint16(index+1))
		if err != nil {
			return err
		}
	}
	options := originalDiagnosticOptions()
	runner, err := palflow.New(palgame.Source{Game: o.game, Runtime: o.runtime, Assets: o.assets, Maps: o.maps, Palettes: o.palettes, RNG: o.rng, FBP: o.fbp}, options)
	if err != nil {
		return err
	}
	report, runErr := runner.Trace(request)
	o.history = append(o.history, originalInvocation{Invoked: true, ActionIntent: originalActionIntent{Kind: kind, SceneNumber: index + 1, EventID: request.EventID, Inputs: append([]palflow.Input(nil), inputs...)}, Options: &options, Mode: "diagnostic", Kind: kind, SceneNumber: index + 1, EventID: request.EventID, Request: &request, Report: report})
	if len(o.history) > 16 {
		o.history = o.history[len(o.history)-16:]
	}
	if len(report.UnusedInputs) > 0 {
		runErr = errors.Join(runErr, fmt.Errorf("显式输入剩余 %d 个未消费；入口地址未提交", len(report.UnusedInputs)))
	}
	if runErr != nil {
		o.history[len(o.history)-1].Error = runErr.Error()
		return runErr
	}
	if report.Completed {
		if eventIndex < 0 {
			o.enterPC[index] = report.State.ReturnedEntry
		} else {
			o.triggerPC[eventIndex] = report.State.ReturnedEntry
		}
	}
	return runErr
}
func (o *originalData) runSession(kind string, inputs []palflow.Input) error {
	if o.session == nil {
		return errors.New("请先建立逻辑会话")
	}
	before := o.session.Snapshot()
	scene := before.World.Scene
	eventID := uint16(0xffff)
	var report palflow.Report
	var runErr error
	switch kind {
	case "on_enter":
		report, runErr = o.session.Enter(inputs)
	case "event_trigger":
		id, err := strconv.Atoi(o.event)
		if err != nil || id < 1 || id > 65535 {
			return errors.New("请选择有效事件 ID")
		}
		eventID = uint16(id)
		report, runErr = o.session.Trigger(uint16(id), inputs)
	default:
		return errors.New("未知原版脚本调用类型")
	}
	after := o.session.Snapshot()
	checkpoint := o.checkpoint
	record := originalInvocation{ActionIntent: originalActionIntent{Kind: kind, SceneNumber: int(scene), EventID: eventID, Inputs: append([]palflow.Input(nil), inputs...)}, Mode: "session", Kind: kind, SceneNumber: int(scene), EventID: eventID, Report: report, Checkpoint: &checkpoint, SessionBefore: &before, SessionAfter: &after, SessionCommitted: runErr == nil && after.Actions > before.Actions}
	if actual := o.session.LastInvocation(); actual != nil {
		record.Invoked = true
		record.Request, record.Options = &actual.Request, &actual.Options
	}
	if runErr != nil {
		record.Error = runErr.Error()
	}
	o.history = append(o.history, record)
	if len(o.history) > 16 {
		o.history = o.history[len(o.history)-16:]
	}
	o.updateViewport()
	return runErr
}
func (o *originalData) export() error {
	if o.game == nil || len(o.history) == 0 {
		return errors.New("请先执行一次脚本记录")
	}
	if err := o.validateExportPath(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(originalEvidence{MGOIdentity: o.mgoIdentity, MapIdentities: o.mapIdentities, PaletteIdentity: o.paletteIdentity, MediaIdentities: o.mediaIdentities, SpriteDecodeLimit: originalSpriteDecodeLimit, SchemaVersion: 2, Scope: "bounded script or spatial control action; no full-game verification", Encoding: o.loadedEncoding, Layout: o.loadedLayout, Files: o.game.Files, Invocation: o.history[len(o.history)-1]}, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(o.exportPath), ".paltrace-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(append(data, '\n')); err != nil {
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
	return os.Rename(tmp, o.exportPath)
}

// Exports can only replace JSON reports, never imported game files or aliases.
func (o *originalData) validateExportPath() error {
	if strings.ToLower(filepath.Ext(o.exportPath)) != ".json" {
		return errors.New("脚本记录只能导出为 .json 或 .paltrace.json 文件")
	}
	output, err := filepath.Abs(o.exportPath)
	if err != nil {
		return err
	}
	outputInfo, err := os.Stat(output)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	sourceFiles := append([]paldata.FileInfo(nil), o.game.Files...)
	if o.mgoIdentity != nil {
		sourceFiles = append(sourceFiles, *o.mgoIdentity)
	}
	sourceFiles = append(sourceFiles, o.mapIdentities...)
	if o.paletteIdentity != nil {
		sourceFiles = append(sourceFiles, *o.paletteIdentity)
	}
	sourceFiles = append(sourceFiles, o.mediaIdentities...)
	protectedPaths, err := o.protectedOriginalResourcePaths()
	if err != nil {
		return err
	}
	for _, path := range protectedPaths {
		source, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if source == output {
			return errors.New("不能覆盖原版资源文件")
		}
		if outputInfo != nil {
			info, err := os.Stat(source)
			if err == nil && os.SameFile(outputInfo, info) {
				return errors.New("导出路径是原版资源文件的别名，不能覆盖")
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	for _, file := range sourceFiles {
		source, err := filepath.Abs(filepath.Join(o.loadedDirectory, file.Name))
		if err != nil {
			return err
		}
		if output == source {
			return errors.New("不能覆盖已导入的游戏数据文件")
		}
		if outputInfo != nil {
			info, err := os.Stat(source)
			if err == nil && os.SameFile(outputInfo, info) {
				return errors.New("导出路径是原版数据文件的别名，不能覆盖")
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}
func (a *workbench) originalView(c *ui.Context) {
	if a.original == nil {
		a.original = newOriginalData()
	}
	o := a.original
	t := c.Theme()
	ui.Text(c, "原版数据 · 场景与脚本").Bold().FontSize(20)
	ui.Text(c, "读取 SSS.MKF、DATA.MKF、M.MSG、WORD.DAT。编码与对象布局由你明确选择；这里只执行已支持的触发脚本子集，遇到未知指令立即停止，不等于通关。").TextColor(t.TextMuted)
	ui.Row(c).Gap(8).Children(func() {
		ui.TextInput(c.Key("original-directory"), &o.directory).Label("原版数据目录").Grow(1)
		ui.Select(c.Key("original-encoding"), &o.encoding, []string{"big5", "gbk"}).Label("原版文本编码").Width(100)
		ui.Select(c.Key("original-layout"), &o.layout, []string{"dos", "windows"}).Label("原版对象布局").Width(120)
		if ui.PrimaryButton(c, "读取原版数据").Clicked() {
			a.report(o.load(), "原版四张数据表已读取；未执行原程序")
		}
	})
	if o.game == nil {
		return
	}
	ui.Text(c, fmt.Sprintf("已读取: %s · %s / %s · 场景 %d，事件 %d，脚本 %d，对话 %d，词条 %d，对象 %d", o.loadedDirectory, o.loadedEncoding, o.loadedLayout, len(o.game.Scenes), len(o.game.Events), len(o.game.Scripts), len(o.game.Messages), len(o.game.Words), len(o.game.RawObjects))).FontSize(12)
	if o.changedSource() {
		ui.Text(c, "目录、编码或布局已改变；当前数据显示上次导入内容，请重新读取。").TextColor(t.Warning)
	}
	if o.assets != nil {
		ui.Text(c, fmt.Sprintf("MGO 目录可用：%s · 精灵 %d · %d 字节 · SHA256 %s · 单精灵解压上限 %d 字节", o.mgoIdentity.Name, o.assets.Count(), o.mgoIdentity.Size, o.mgoIdentity.SHA256, originalSpriteDecodeLimit)).FontSize(12).TextColor(t.TextMuted)
	} else {
		ui.Text(c, "MGO 元数据目录不可用："+o.assetError).FontSize(12).TextColor(t.Warning)
	}
	if o.maps != nil {
		for _, file := range o.mapIdentities {
			ui.Text(c, fmt.Sprintf("%s · %d 字节 · SHA256 %s", file.Name, file.Size, file.SHA256)).FontSize(12).TextColor(t.TextMuted)
		}
	} else {
		ui.Text(c, "地图目录不可用："+o.mapError).FontSize(12).TextColor(t.Warning)
	}
	if o.palettes == nil {
		ui.Text(c, "调色板不可用："+o.paletteError).FontSize(12).TextColor(t.Warning)
	} else {
		ui.Text(c, fmt.Sprintf("%s · %d 字节 · SHA256 %s", o.paletteIdentity.Name, o.paletteIdentity.Size, o.paletteIdentity.SHA256)).FontSize(12).TextColor(t.TextMuted)
	}
	for _, file := range o.mediaIdentities {
		ui.Text(c, fmt.Sprintf("%s · %d 字节 · SHA256 %s", file.Name, file.Size, file.SHA256)).FontSize(12).TextColor(t.TextMuted)
	}
	if o.mediaError != "" {
		ui.Text(c, "媒体目录读取失败："+o.mediaError).FontSize(12).TextColor(t.Warning)
	}
	if o.runtimeError != "" {
		ui.Text(c, "运行表不可用，保留单次诊断模式："+o.runtimeError).FontSize(12).TextColor(t.Warning)
	}
	ui.Row(c).Gap(10).Children(func() {
		ui.Checkbox(c.Key("original-session-mode"), &o.useSession, "逻辑会话").Disabled(o.runtime == nil || o.session == nil)
		ui.Checkbox(c.Key("original-assume-entered"), &o.assumeEntered, "声明场景已进入（诊断检查点）").Disabled(o.runtime == nil)
		ui.NumberInput(c.Key("original-step-budget"), &o.maxSteps, 1, palflow.MaxAllowedSteps, 1).Label("会话工作量上限（重置后生效）").Width(160)
		if ui.Button(c, "重置到选定场景").Disabled(o.runtime == nil || o.changedSource()).Clicked() {
			a.report(o.resetSession(), "已重置逻辑检查点；没有验证原游戏可达性")
		}
	})
	a.originalBattlePolicyView(c)
	a.originalSaveView(c)
	ui.Text(c, "会话只提交完整、输入精确的动作。地图操作执行碰撞、调查和自动更新；事件 ID 触发属于诊断调用。改变场景选择后须显式重置，不会传送当前会话。").FontSize(12).TextColor(t.TextMuted)
	if o.useSession && o.session != nil {
		snapshot := o.session.Snapshot()
		ui.Text(c, fmt.Sprintf("当前逻辑场景 %d · 已提交 %d 次调用 · 待执行 OnEnter: %t · 初始检查点 %d / 声明已进入: %t", snapshot.World.Scene, snapshot.Actions, snapshot.NeedsSceneEnter, o.checkpoint.Scene, o.checkpoint.AssumeSceneEntered)).FontSize(12)
		inventory := []int{}
		for id, count := range snapshot.World.Inventory {
			if count > 0 {
				inventory = append(inventory, int(id))
			}
		}
		sort.Ints(inventory)
		labels := []string{}
		for i, id := range inventory {
			if i >= 12 {
				labels = append(labels, "…")
				break
			}
			labels = append(labels, fmt.Sprintf("%d×%d", id, snapshot.World.Inventory[uint16(id)]))
		}
		ui.Text(c, "会话背包（物品 ID×数量）："+strings.Join(labels, "，")).FontSize(12)
	}
	a.originalPlayView(c)
	ids := make([]string, len(o.game.Scenes))
	for i := range ids {
		ids[i] = strconv.Itoa(i + 1)
	}
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, "检查点场景编号")
		ui.Select(c.Key("original-scene"), &o.scene, ids).Label("原版场景编号").Width(130)
		if display, err := o.sceneDisplay(); err == nil {
			ui.Text(c, display.text()).Grow(1)
		}
	})
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, "诊断上次事件上下文")
		ui.NumberInput(c.Key("original-context"), &o.lastEvent, 0, 65534, 1).Label("原版上次事件上下文").Disabled(o.useSession).Width(130)
		if ui.Button(c, "执行场景 OnEnter").Disabled(o.changedSource()).Clicked() {
			a.report(o.run("on_enter"), "单次 OnEnter 记录已停止；不等于完成场景")
		}
	})
	start, end, rangeErr := o.eventRange()
	if rangeErr != nil {
		ui.Text(c, rangeErr.Error()).TextColor(t.Warning)
	} else {
		eventIDs := make([]string, 0, end-start)
		for i := start; i < end; i++ {
			eventIDs = append(eventIDs, strconv.Itoa(i+1))
		}
		o.event = validSelection(o.event, eventIDs)
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, fmt.Sprintf("当前场景事件 [%d, %d) · 事件 ID = 表索引 + 1", start, end)).Grow(1)
			ui.Select(c.Key("original-event"), &o.event, eventIDs).Label("原版事件编号").Width(110)
			if ui.Button(c, "执行事件触发脚本").Disabled(len(eventIDs) == 0 || o.changedSource()).Clicked() {
				a.report(o.run("event_trigger"), "单次事件脚本记录已停止；不等于完成任务")
			}
		})
		if id, err := strconv.Atoi(o.event); err == nil && id >= start+1 && id <= end {
			if display, err := o.eventDisplay(id); err == nil {
				ui.Text(c, display.text()).FontSize(12)
			}
		}
	}
	if !o.useSession {
		ui.Text(c, "单次诊断每次从导入快照开始；运行表可用时附加初始世界快照，否则世界指令会阻塞。它不会恢复上次调用的世界状态。").FontSize(12).TextColor(t.TextMuted)
	}
	ui.Text(c, "显式输入 JSON（按消费顺序）：confirm 0/1；key 1；shop 0关闭、其他值为选购物品ID；random 0..2147483647（具体指令检查范围）。会话战斗执行Go数值自动攻击；独立诊断的battle 0/1/2为外部给定胜/败/逃。缺少输入会保留阻塞原因。").FontSize(12).TextColor(t.TextMuted)
	ui.Text(c, `JSON 示例：[{"kind":"confirm","value":1}]；不确定的分支请保留 [] 以查看缺少输入的阻塞点。`).FontSize(12).TextColor(t.TextMuted)
	ui.TextArea(c.Key("original-inputs"), &o.inputsJSON).Label("原版显式输入 JSON").Font("monospace").Height(85).FillWidth()
	if len(o.game.Warnings) > 0 {
		ui.Text(c, fmt.Sprintf("导入器警告 %d 条；前一条: %s", len(o.game.Warnings), o.game.Warnings[0])).FontSize(12).TextColor(t.Warning)
	}
	if len(o.history) == 0 {
		return
	}
	record := o.history[len(o.history)-1]
	report := record.Report
	ui.Divider(c)
	if record.Error != "" {
		ui.Text(c, record.Error).TextColor(t.Warning)
	}
	if record.Mode == "session" || record.Mode == "control" {
		ui.Text(c, fmt.Sprintf("逻辑会话状态已提交: %t", record.SessionCommitted)).FontSize(12)
	}
	if record.Mode == "session_file" {
		ui.Text(c, fmt.Sprintf("会话文件操作 %s · 路径 %s · 已替换活动会话: %t", record.Kind, record.ActionIntent.Path, record.SessionCommitted)).FontSize(12)
	} else if record.Control != nil {
		a.originalControlReportView(c, record)
	} else {
		entry := "未调用"
		if record.Request != nil {
			entry = fmt.Sprintf("%04x", record.Request.Entry)
		}
		ui.Text(c, fmt.Sprintf("单次脚本记录 %d · 场景 %d · 入口 %s · 主指令 %d / 玩家子步骤 %d / 世界 tick %d · 消费 %d 个输入", len(o.history), record.SceneNumber, entry, report.State.ExecutedSteps, report.State.PlayerSubsteps, report.State.WorldTicks, report.InputsConsumed)).Bold()
		unused := len(report.UnusedInputs)
		if !record.Invoked {
			unused = len(record.ActionIntent.Inputs)
			ui.Text(c, "运行器未调用；显式输入没有消费。").TextColor(t.TextMuted)
		}
		ui.Text(c, fmt.Sprintf("未消费显式输入: %d 个", unused)).FontSize(12).TextColor(t.TextMuted)
		if report.Blocker != nil {
			b := report.Blocker
			ui.Text(c, fmt.Sprintf("阻塞 PC %04x · opcode %04x · %s", b.PC, b.Opcode, b.Reason)).TextColor(t.Warning)
		} else if record.Error != "" && len(report.Trace) == 0 {
			ui.Text(c, "本次调用被拒绝；没有指令提交。").TextColor(t.TextMuted)
		} else {
			ui.Text(c, "本次脚本调用已停止；不代表主线或支线完成。").TextColor(t.TextMuted)
		}
		if report.BattleOutcomesStubbed {
			ui.Text(c, "本记录使用了外部给定的战斗结果；未运行战斗程序。").TextColor(t.Warning)
		}
		for i, step := range report.Trace {
			if step.Message == nil {
				continue
			}
			ui.Column(c.Key(fmt.Sprintf("original-message-%d", i))).Gap(5).Padding(10).FillWidth().Background(t.Surface).Children(func() {
				ui.Text(c, fmt.Sprintf("PC %04x · 对话索引 %d · 原始解码文本（控制标记未渲染）", step.PC, step.Message.ID)).FontSize(12).TextColor(t.TextMuted)
				ui.Text(c, step.Message.Text)
			})
		}
		if len(report.DialogueIDs) == 0 {
			ui.Text(c, "已执行的指令前缀没有对话输出；不会跳过阻塞点搜索后续对白。").TextColor(t.TextMuted)
		}
	}
	ui.Row(c).Gap(8).Children(func() {
		ui.TextInput(c.Key("original-export"), &o.exportPath).Label("原版记录导出路径").Grow(1)
		if ui.Button(c, "导出本次记录").Clicked() {
			a.report(o.export(), "已导出请求、显式输入、栈帧、阻塞证据与来源哈希")
		}
	})
}
