package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
)

const (
	journeyRouteLimit  = 1 << 20
	journeyReportLimit = 32 << 20
	journeyMaxActions  = 10000
	// Reserve room for the final imported world and report metadata. The action
	// budget is checked before retaining each potentially large trace.
	journeyActionBudget = journeyReportLimit - (8 << 20) - (1 << 20)
)

var journeyFileNames = []string{"SSS.MKF", "DATA.MKF", "M.MSG", "WORD.DAT", "MGO.MKF", "MAP.MKF", "GOP.MKF"}

type journeyRoute struct {
	Version     int               `json:"version"`
	Name        string            `json:"name"`
	Encoding    string            `json:"encoding,omitempty"`
	Layout      string            `json:"layout,omitempty"`
	FilesSHA256 map[string]string `json:"files_sha256"`
	Actions     []journeyAction   `json:"actions"`
}

type journeyAction struct {
	Type             string          `json:"type"`
	Event            *uint16         `json:"event,omitempty"`
	Direction        *uint16         `json:"direction,omitempty"`
	Item             *uint16         `json:"item,omitempty"`
	TargetRole       *uint16         `json:"target_role,omitempty"`
	Inputs           []palflow.Input `json:"inputs"`
	ExpectedMessages *[]uint16       `json:"expected_messages,omitempty"`
	ExpectedScene    *uint16         `json:"expected_scene,omitempty"`
	// Inventory assertions cover the listed item IDs; zero asserts absence.
	ExpectedInventory map[uint16]int `json:"expected_inventory,omitempty"`
}

type journeyChange struct {
	Path   string          `json:"path"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

type journeyInvocation struct {
	*palgame.Invocation
	WorldSHA256      string `json:"world_sha256"`
	IdleFramesSHA256 string `json:"idle_frames_sha256"`
}

type journeyActionReport struct {
	Battles             []journeyBattleSummary `json:"battles,omitempty"`
	MessageIDs          []uint16               `json:"message_ids"`
	Work                int                    `json:"work"`
	Index               int                    `json:"index"`
	Action              journeyAction          `json:"action"`
	Committed           bool                   `json:"committed"`
	ExpectationsMatched bool                   `json:"expectations_matched"`
	Error               string                 `json:"error,omitempty"`
	BeforeSHA256        string                 `json:"before_sha256"`
	AfterSHA256         string                 `json:"after_sha256"`
	Delta               []journeyChange        `json:"delta"`
	Trace               *palflow.Report        `json:"trace,omitempty"`
	Control             *palgame.ControlReport `json:"control,omitempty"`
	Invocation          *journeyInvocation     `json:"invocation,omitempty"`
	EvidenceOmitted     string                 `json:"evidence_omitted,omitempty"`
}

type journeyBattleSummary struct {
	Mode      string `json:"mode"`
	EnemyTeam uint16 `json:"enemy_team"`
	RawResult uint16 `json:"raw_result"`
	Rounds    int    `json:"rounds"`
	Work      int    `json:"work"`
}

type journeyReport struct {
	ExecutionPolicy    journeyPolicy           `json:"execution_policy"`
	TraceDetail        string                  `json:"trace_detail"`
	Version            int                     `json:"format_version"`
	Kind               string                  `json:"kind"`
	Name               string                  `json:"name"`
	RouteSHA256        string                  `json:"route_sha256"`
	FullGameVerified   bool                    `json:"full_game_verified"`
	Completed          bool                    `json:"completed"`
	Error              string                  `json:"error,omitempty"`
	Scope              string                  `json:"scope"`
	Dataset            datasetSummary          `json:"dataset"`
	Files              []fileEvidence          `json:"files"`
	Initialization     palgame.Initialization  `json:"initialization"`
	InitialSHA256      string                  `json:"initial_sha256"`
	InitialIdleFrames  map[uint16]uint16       `json:"initial_idle_frames"`
	TraceStateEncoding string                  `json:"trace_state_encoding"`
	Actions            []journeyActionReport   `json:"actions"`
	Final              palgame.SessionSnapshot `json:"final"`
}

type journeyPolicy struct {
	VM                 palflow.Options `json:"vm"`
	RandomMode         string          `json:"random_mode"`
	BattleMode         string          `json:"battle_mode"`
	InitialRandomState uint64          `json:"initial_random_state"`
}

func runJourney(args []string, out, errout io.Writer) error {
	fs := flag.NewFlagSet("journey", flag.ContinueOnError)
	fs.SetOutput(errout)
	dir := fs.String("data", "", "同目录的四张数据表与 MGO/MAP/GOP.MKF")
	routePath := fs.String("route", "", "连续动作 JSON 路线")
	encoding := fs.String("encoding", "", "big5 或 gbk；也可由路线明确指定")
	layout := fs.String("layout", "", "windows 或 dos；默认 windows，也可由路线指定")
	dest := fs.String("out", "", "JSON 报告路径；默认 stdout")
	initPath := fs.String("init-route", "", "生成固定七文件摘要及单个 enter 动作的 JSON 路线")
	maxSteps := fs.Int("max-steps", 10000, "每个动作的统一执行预算")
	compact := fs.Bool("compact", false, "成功动作只保留输入、消息ID、工作量和前后摘要；失败保留完整轨迹")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *dir == "" || fs.NArg() != 0 {
		return errors.New("journey 需要 -data；不接受位置参数")
	}
	if *maxSteps < 1 || *maxSteps > palflow.MaxAllowedSteps {
		return errors.New("max-steps 须为1..100000")
	}
	if (*routePath == "") == (*initPath == "") {
		return errors.New("journey 需要且只接受 -route 或 -init-route 之一")
	}
	if *initPath != "" && *dest != "" {
		return errors.New("-init-route 不接受 -out；路线写入 init-route 指定路径")
	}
	var spec journeyRoute
	var routeDigest string
	if *routePath != "" {
		var err error
		spec, routeDigest, err = loadJourneyRoute(*routePath)
		if err != nil {
			return err
		}
	}
	selectedEncoding, err := journeySetting("encoding", *encoding, spec.Encoding, "")
	if err != nil {
		return err
	}
	selectedLayout, err := journeySetting("layout", *layout, spec.Layout, "windows")
	if err != nil {
		return err
	}
	paths, err := journeyFiles(*dir)
	if err != nil {
		return err
	}
	protected := make([]string, 0, 8)
	for _, name := range journeyFileNames {
		protected = append(protected, paths[name])
	}
	if *routePath != "" {
		protected = append(protected, *routePath)
	}
	output := *dest
	if *initPath != "" {
		output = *initPath
	}
	if err := journeyOutputPath(output, protected); err != nil {
		return err
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
	maps, err := palasset.OpenMaps(paths["MAP.MKF"], paths["GOP.MKF"])
	if err != nil {
		return err
	}
	files := make([]fileEvidence, 0, 7)
	for _, f := range g.Files {
		files = append(files, fileEvidence{Name: strings.ToUpper(f.Name), SHA256: f.SHA256, Size: f.Size})
	}
	files = append(files,
		fileEvidence{Name: "MGO.MKF", SHA256: assets.SourceSHA256(), Size: assets.SourceSize()},
		fileEvidence{Name: "MAP.MKF", SHA256: maps.MAPSourceSHA256(), Size: maps.MAPSourceSize()},
		fileEvidence{Name: "GOP.MKF", SHA256: maps.GOPSourceSHA256(), Size: maps.GOPSourceSize()})
	if *initPath != "" {
		// Check the same bootstrap invariants as execution before producing a
		// starter route. Merely producing this file does not execute OnEnter.
		if _, err := palgame.NewSession(g, runtime, palgame.SessionOptions{Assets: assets, Maps: maps, RedactText: true, MaxSteps: *maxSteps}); err != nil {
			return err
		}
		spec = journeyRoute{Version: 1, Name: "new-game-journey", Encoding: selectedEncoding, Layout: selectedLayout, FilesSHA256: map[string]string{}, Actions: []journeyAction{{Type: "enter", Inputs: []palflow.Input{}}}}
		for _, f := range files {
			spec.FilesSHA256[f.Name] = f.SHA256
		}
		return writeReport(*initPath, spec, out)
	}
	for _, f := range files {
		if spec.FilesSHA256[f.Name] != f.SHA256 {
			return fmt.Errorf("journey 路线文件摘要不匹配: %s", f.Name)
		}
	}
	session, err := palgame.NewSession(g, runtime, palgame.SessionOptions{Assets: assets, Maps: maps, RedactText: true, MaxSteps: *maxSteps})
	if err != nil {
		return err
	}
	report, executionErr := executeJourneyDetail(session, spec, routeDigest, summarize(g), files, *compact)
	if err := journeyWriteReport(*dest, report, out); err != nil {
		return err
	}
	return executionErr
}

func journeySetting(name, cli, route, fallback string) (string, error) {
	if cli != "" && route != "" && cli != route {
		return "", fmt.Errorf("journey %s 与路线不一致", name)
	}
	if cli != "" {
		return cli, nil
	}
	if route != "" {
		return route, nil
	}
	return fallback, nil
}

func executeJourney(session *palgame.Session, spec journeyRoute, routeDigest string, dataset datasetSummary, files []fileEvidence) (journeyReport, error) {
	return executeJourneyDetail(session, spec, routeDigest, dataset, files, false)
}

func executeJourneyDetail(session *palgame.Session, spec journeyRoute, routeDigest string, dataset datasetSummary, files []fileEvidence, compact bool) (journeyReport, error) {
	initial := session.Initialization()
	initial.World = nil
	before := session.Snapshot()
	beforeJSON, _ := json.Marshal(before)
	beforeDigest := journeyBytesDigest(beforeJSON)
	report := journeyReport{Version: 1, Kind: "continuous_session_journey", Name: spec.Name, RouteSHA256: routeDigest, Dataset: dataset, Files: files, Initialization: initial, InitialSHA256: journeyDigest(before), InitialIdleFrames: before.IdleFrames, TraceStateEncoding: "Nested world and idle_frames are omitted. Initial imported world + initial_idle_frames + ordered action deltas reconstruct session snapshots; final includes the complete snapshot. Invocation digests identify prepared world and idle_frames.", Actions: []journeyActionReport{}, Scope: "One continuous new-game Session with imported local MGO/MAP/GOP resources. enter runs required scene OnEnter; move/interact use modeled frame order, collision and search; use_item executes the selected item's original script. Explicit trigger selects an event ID and does not establish spatial reachability. Battles use the Go numeric auto-attack policy and seeded model RNG. Original-runtime parity, a complete main story and all side quests are not verified."}
	report.TraceDetail = "full"
	report.ExecutionPolicy = journeyPolicy{VM: session.ExecutionOptions(), RandomMode: before.World.RandomMode, BattleMode: before.World.BattleMode, InitialRandomState: before.World.BattleRandomState}
	if compact {
		report.TraceDetail = "compact"
		report.TraceStateEncoding = "Successful actions retain input, actual message IDs, work and before/after SHA-256. Full per-step trace and deltas are omitted by request; intermediate worlds require replay of the hash-bound route. Failed actions retain full available evidence. Final includes complete snapshot."
	}
	used := 0
	var executionErr error
	for index, action := range spec.Actions {
		entry := journeyActionReport{Index: index + 1, Action: action, BeforeSHA256: beforeDigest, Delta: []journeyChange{}}
		var messages []uint16
		var actionErr error
		switch action.Type {
		case "enter", "trigger", "use_item":
			var trace palflow.Report
			if action.Type == "enter" {
				trace, actionErr = session.Enter(action.Inputs)
			} else if action.Type == "trigger" {
				trace, actionErr = session.Trigger(*action.Event, action.Inputs)
			} else {
				trace, actionErr = session.UseItem(*action.Item, *action.TargetRole, action.Inputs)
			}
			messages = trace.DialogueIDs
			trace.State.World = nil
			trace.State.IdleFrames = nil
			entry.Trace = &trace
			if invocation := session.LastInvocation(); invocation != nil {
				worldDigest := journeyDigest(invocation.Request.World)
				idleDigest := journeyDigest(invocation.Request.IdleFrames)
				invocation.Request.World = nil
				invocation.Request.IdleFrames = nil
				entry.Invocation = &journeyInvocation{Invocation: invocation, WorldSHA256: worldDigest, IdleFramesSHA256: idleDigest}
			}
		case "move", "interact":
			var control palgame.ControlReport
			if action.Type == "move" {
				control, actionErr = session.Move(*action.Direction, action.Inputs)
			} else {
				control, actionErr = session.Interact(action.Inputs)
			}
			messages = journeyControlMessages(control)
			for i := range control.Triggers {
				control.Triggers[i].State.World = nil
				control.Triggers[i].State.IdleFrames = nil
			}
			control.State.World = nil
			control.State.IdleFrames = nil
			entry.Control = &control
		}
		after := session.Snapshot()
		entry.Battles = journeyBattles(entry.Trace, entry.Control)
		afterJSON, _ := json.Marshal(after)
		entry.Committed = actionErr == nil
		entry.AfterSHA256 = journeyBytesDigest(afterJSON)
		entry.MessageIDs = append([]uint16{}, messages...)
		if entry.Trace != nil {
			s := entry.Trace.State
			entry.Work = s.ExecutedSteps + s.PlayerSubsteps + s.WorldTicks + s.AutoSubsteps + s.CombatWork
		}
		if entry.Control != nil {
			entry.Work = entry.Control.Work
		}
		if actionErr == nil {
			actionErr = journeyExpect(action, messages, after)
		}
		entry.ExpectationsMatched = actionErr == nil
		if actionErr != nil {
			entry.Error = actionErr.Error()
		}
		if compact && actionErr == nil {
			entry.Trace, entry.Control, entry.Invocation = nil, nil, nil
			entry.Delta = nil
		} else {
			entry.Delta = journeyJSONDiff(beforeJSON, afterJSON)
		}
		encoded, _ := json.MarshalIndent(entry, "    ", "  ")
		if len(encoded)+used > journeyActionBudget {
			// The action may have committed before its trace size became known.
			// Preserve that fact and final world, and explicitly mark missing
			// evidence instead of misrepresenting execution as rolled back.
			entry.Trace, entry.Control, entry.Invocation = nil, nil, nil
			entry.Delta = nil
			entry.EvidenceOmitted = "action trace/delta exceed the cumulative 32 MiB report budget"
			actionErr = errors.New(entry.EvidenceOmitted)
			entry.Error = actionErr.Error()
			entry.ExpectationsMatched = false
		}
		encoded, _ = json.MarshalIndent(entry, "    ", "  ")
		used += len(encoded) + 2
		report.Actions = append(report.Actions, entry)
		beforeJSON, beforeDigest = afterJSON, entry.AfterSHA256
		if actionErr != nil {
			executionErr = fmt.Errorf("journey action %d (%s): %w", index+1, action.Type, actionErr)
			break
		}
	}
	report.Final = session.Snapshot()
	report.Completed = executionErr == nil
	if executionErr != nil {
		report.Error = executionErr.Error()
	}
	return report, executionErr
}

func journeyControlMessages(control palgame.ControlReport) []uint16 {
	var messages []uint16
	search := -1
	if control.SearchEventID != 0 && len(control.Triggers) > 0 {
		search = len(control.Triggers) - 1
	}
	// PAL_GameUpdate runs touch triggers, then autos; PAL_Search follows party
	// control. A search report is therefore the final trigger, after the autos.
	for i, trigger := range control.Triggers {
		if i != search {
			messages = append(messages, trigger.DialogueIDs...)
		}
	}
	var trace func(palflow.TraceStep)
	var auto func(palflow.AutoStep)
	trace = func(step palflow.TraceStep) {
		for _, tick := range step.WorldUpdates {
			for _, child := range tick.TriggerSteps {
				trace(child)
			}
			for _, child := range tick.AutoSteps {
				auto(child)
			}
		}
		if step.Combat != nil {
			for _, trigger := range step.Combat.Triggers {
				messages = append(messages, trigger.DialogueIDs...)
			}
		}
		if step.Message != nil {
			messages = append(messages, step.Message.ID)
		}
		for _, child := range step.Children {
			trace(child)
		}
		for _, child := range step.AutoSteps {
			auto(child)
		}
		if step.PartyWalk != nil {
			for _, movement := range step.PartyWalk.Steps {
				for _, child := range movement.AutoSteps {
					auto(child)
				}
			}
		}
	}
	auto = func(step palflow.AutoStep) {
		for _, child := range step.TriggerSteps {
			trace(child)
		}
		if step.Instruction != nil {
			trace(*step.Instruction)
		}
	}
	for _, step := range control.AutoSteps {
		auto(step)
	}
	if search >= 0 {
		messages = append(messages, control.Triggers[search].DialogueIDs...)
	}
	return messages
}

func journeyExpect(action journeyAction, messages []uint16, state palgame.SessionSnapshot) error {
	if action.ExpectedMessages != nil && !slices.Equal(*action.ExpectedMessages, messages) {
		return errors.New("消息ID序列与 expected_messages 不一致；动作已执行并提交")
	}
	if action.ExpectedScene != nil && state.World.Scene != *action.ExpectedScene {
		return fmt.Errorf("场景 %d 与 expected_scene %d 不一致；动作已执行并提交", state.World.Scene, *action.ExpectedScene)
	}
	for id, count := range action.ExpectedInventory {
		if state.World.Inventory[id] != count {
			return fmt.Errorf("物品 %d 数量 %d 与 expected_inventory %d 不一致；动作已执行并提交", id, state.World.Inventory[id], count)
		}
	}
	return nil
}

func journeyDigest(value any) string {
	data, _ := json.Marshal(value)
	return journeyBytesDigest(data)
}

func journeyBytesDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Deltas recurse through object fields/maps and equal-length arrays. This avoids
// copying an imported event table or all 256 slots for a one-item change.
func journeyDiff(before, after any) []journeyChange {
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	return journeyJSONDiff(a, b)
}

func journeyJSONDiff(a, b []byte) []journeyChange {
	changes := []journeyChange{}
	var compare func(string, json.RawMessage, json.RawMessage)
	compare = func(path string, a, b json.RawMessage) {
		if bytes.Equal(a, b) {
			return
		}
		var left, right map[string]json.RawMessage
		if len(a) > 0 && len(b) > 0 && a[0] == '{' && b[0] == '{' && json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil {
			keys := make([]string, 0, len(left)+len(right))
			for key := range left {
				keys = append(keys, key)
			}
			for key := range right {
				if _, ok := left[key]; !ok {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			for _, key := range keys {
				compare(path+"/"+strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1"), left[key], right[key])
			}
			return
		}
		var leftArray, rightArray []json.RawMessage
		if len(a) > 0 && len(b) > 0 && a[0] == '[' && b[0] == '[' && json.Unmarshal(a, &leftArray) == nil && json.Unmarshal(b, &rightArray) == nil && len(leftArray) == len(rightArray) {
			for i := range leftArray {
				compare(path+"/"+strconv.Itoa(i), leftArray[i], rightArray[i])
			}
			return
		}
		if len(a) == 0 {
			a = json.RawMessage("null")
		}
		if len(b) == 0 {
			b = json.RawMessage("null")
		}
		changes = append(changes, journeyChange{Path: path, Before: a, After: b})
	}
	compare("", a, b)
	return changes
}

func journeyWriteReport(path string, report journeyReport, out io.Writer) error {
	// Account for final-world and initialization size as well as retained
	// actions. No oversized or partial JSON document is ever published.
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > journeyReportLimit {
		return errors.New("journey 最终报告超过32MiB，未写入输出")
	}
	return writeReport(path, report, out)
}

func journeyFiles(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := make(map[string]string, len(journeyFileNames))
	for _, entry := range entries {
		for _, wanted := range journeyFileNames {
			if !strings.EqualFold(entry.Name(), wanted) {
				continue
			}
			if paths[wanted] != "" {
				return nil, fmt.Errorf("journey: ambiguous %s", wanted)
			}
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("journey: %s must be a regular non-symlink file", wanted)
			}
			paths[wanted] = filepath.Join(dir, entry.Name())
		}
	}
	for _, wanted := range journeyFileNames {
		if paths[wanted] == "" {
			return nil, fmt.Errorf("journey: missing %s", wanted)
		}
	}
	return paths, nil
}

func journeyOutputPath(path string, protected []string) error {
	if path == "" {
		return nil
	}
	if strings.ToLower(filepath.Ext(path)) != ".json" {
		return errors.New("journey 输出仅允许 .json 文件")
	}
	canonical := func(path string) (string, error) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			return real, nil
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
		if err != nil {
			return "", err
		}
		return filepath.Join(parent, filepath.Base(abs)), nil
	}
	target, err := canonical(path)
	if err != nil {
		return err
	}
	targetInfo, _ := os.Stat(path)
	for _, input := range protected {
		source, err := canonical(input)
		if err != nil {
			return err
		}
		info, err := os.Stat(input)
		if err != nil {
			return err
		}
		if source == target || (targetInfo != nil && os.SameFile(info, targetInfo)) {
			return fmt.Errorf("journey 输出不能覆盖路线或输入文件（含别名）: %s", input)
		}
	}
	return nil
}

func loadJourneyRoute(path string) (journeyRoute, string, error) {
	var spec journeyRoute
	f, err := os.Open(path)
	if err != nil {
		return spec, "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, journeyRouteLimit+1))
	if err != nil {
		return spec, "", err
	}
	if len(data) > journeyRouteLimit {
		return spec, "", errors.New("journey 路线超过1MiB")
	}
	if !utf8.Valid(data) {
		return spec, "", errors.New("journey 路线必须为UTF-8")
	}
	if err := journeyJSONTokens(data); err != nil {
		return spec, "", err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&spec); err != nil {
		return spec, "", err
	}
	if err := checkJourneyRoute(spec, data); err != nil {
		return spec, "", err
	}
	sum := sha256.Sum256(data)
	return spec, hex.EncodeToString(sum[:]), nil
}

// encoding/json accepts duplicate keys and null for scalar fields. The route
// format deliberately accepts neither, at any nesting level.
func journeyJSONTokens(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return errors.New("journey JSON nesting exceeds 32")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		if token == nil {
			return errors.New("journey JSON 不接受 null")
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return err
					}
					name, ok := key.(string)
					if !ok {
						return errors.New("journey JSON object key must be a string")
					}
					if seen[name] {
						return fmt.Errorf("journey JSON duplicate key %q", name)
					}
					seen[name] = true
					if err := value(depth + 1); err != nil {
						return err
					}
				}
			case '[':
				for d.More() {
					if err := value(depth + 1); err != nil {
						return err
					}
				}
			default:
				return errors.New("journey JSON unexpected closing delimiter")
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("journey 路线须为单个 JSON 文档")
	}
	return nil
}

func checkJourneyRoute(spec journeyRoute, data []byte) error {
	if spec.Version != 1 || strings.TrimSpace(spec.Name) == "" || len(spec.Name) > 256 {
		return errors.New("journey 路线需要 version=1 及1..256字节非空 name")
	}
	if spec.Encoding != "" && spec.Encoding != "big5" && spec.Encoding != "gbk" {
		return errors.New("journey encoding 须为 big5 或 gbk")
	}
	if spec.Layout != "" && spec.Layout != "windows" && spec.Layout != "dos" {
		return errors.New("journey layout 须为 windows 或 dos")
	}
	if len(spec.FilesSHA256) != len(journeyFileNames) {
		return errors.New("journey 必须固定全部七个原版文件 SHA-256")
	}
	for _, name := range journeyFileNames {
		digest := spec.FilesSHA256[name]
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != 32 || digest != strings.ToLower(digest) {
			return fmt.Errorf("journey 缺失或无效 SHA-256: %s", name)
		}
	}
	if len(spec.Actions) < 1 || len(spec.Actions) > journeyMaxActions {
		return errors.New("journey actions 须有1..10000项")
	}
	if spec.Actions[0].Type != "enter" {
		return errors.New("journey 必须从新游戏 enter 动作开始")
	}
	// Require exact spelling as well: Go's struct decoder otherwise accepts
	// case variants such as Version alongside version as the same field.
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	if err := journeyKeys(root, "version", "name", "encoding", "layout", "files_sha256", "actions"); err != nil {
		return err
	}
	var rawActions []map[string]json.RawMessage
	if err := json.Unmarshal(root["actions"], &rawActions); err != nil {
		return err
	}
	for i, action := range spec.Actions {
		if err := journeyKeys(rawActions[i], "type", "event", "direction", "item", "target_role", "inputs", "expected_messages", "expected_scene", "expected_inventory"); err != nil {
			return err
		}
		if action.Inputs == nil {
			return fmt.Errorf("journey action %d 必须明确指定 inputs 数组", i+1)
		}
		if action.Type != "use_item" && (action.Item != nil || action.TargetRole != nil) {
			return fmt.Errorf("journey action %d 仅 use_item 接受 item/target_role", i+1)
		}
		switch action.Type {
		case "use_item":
			if action.Item == nil || *action.Item == 0 || action.TargetRole == nil || action.Event != nil || action.Direction != nil {
				return fmt.Errorf("journey action %d 需要 item/target_role 且不接受 event/direction", i+1)
			}
		case "enter", "interact":
			if action.Event != nil || action.Direction != nil {
				return fmt.Errorf("journey action %d 不接受 event/direction", i+1)
			}
		case "trigger":
			if action.Event == nil || *action.Event == 0 || *action.Event == 0xffff || action.Direction != nil {
				return fmt.Errorf("journey action %d 需要有效 event 且不接受 direction", i+1)
			}
		case "move":
			if action.Direction == nil || *action.Direction > 3 || action.Event != nil {
				return fmt.Errorf("journey action %d 需要 direction=0..3 且不接受 event", i+1)
			}
		default:
			return fmt.Errorf("journey action %d 未知 type %q", i+1, action.Type)
		}
		if action.ExpectedScene != nil && (*action.ExpectedScene == 0 || *action.ExpectedScene > 299) {
			return fmt.Errorf("journey action %d expected_scene 须为1..299", i+1)
		}
		var inputs []map[string]json.RawMessage
		if err := json.Unmarshal(rawActions[i]["inputs"], &inputs); err != nil {
			return err
		}
		for j, input := range action.Inputs {
			if err := journeyKeys(inputs[j], "kind", "value"); err != nil {
				return err
			}
			if _, ok := inputs[j]["value"]; !ok {
				return errors.New("journey input 需要 value")
			}
			switch input.Kind {
			case palflow.InputShop:
				if input.Value < 0 || input.Value > 65535 {
					return errors.New("journey shop 输入须为0..65535；0关闭，其余是物品ID")
				}
			case palflow.InputRandom:
				if input.Value < 0 || input.Value > 2147483647 {
					return errors.New("journey random 输入须为0..2147483647；具体范围由指令校验")
				}
			case palflow.InputKey:
				if input.Value != 1 {
					return errors.New("journey key 输入须为1")
				}
			case palflow.InputConfirm:
				if input.Value < 0 || input.Value > 1 {
					return errors.New("journey confirm 输入须为0或1")
				}
			case palflow.InputBattle:
				if input.Value < 0 || input.Value > 2 {
					return errors.New("journey battle 输入须为0..2")
				}
			default:
				return fmt.Errorf("journey 未知 input kind %q", input.Kind)
			}
		}
		if raw, ok := rawActions[i]["expected_inventory"]; ok {
			var items map[string]int
			if err := json.Unmarshal(raw, &items); err != nil {
				return err
			}
			for key, count := range items {
				id, err := strconv.ParseUint(key, 10, 16)
				if err != nil || id == 0 || strconv.FormatUint(id, 10) != key || count < 0 || count > 99 {
					return errors.New("journey expected_inventory 需要规范的非零物品ID和0..99数量")
				}
			}
		}
	}
	return nil
}

func journeyKeys(object map[string]json.RawMessage, allowed ...string) error {
	for key := range object {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("journey JSON unknown field %q", key)
		}
	}
	return nil
}
