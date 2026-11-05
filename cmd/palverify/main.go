// palverify inventories a requested Windows-layout PAL dataset and records
// explicitly bounded trigger-script traces. It never certifies a full playthrough.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

type fileEvidence struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type datasetSummary struct {
	Profile           string         `json:"requested_profile"`
	ReleaseIdentified bool           `json:"release_identified"`
	Encoding          string         `json:"encoding"`
	Files             []fileEvidence `json:"files"`
	Scripts           int            `json:"scripts"`
	Scenes            int            `json:"scene_table_records"`
	Events            int            `json:"events"`
	Objects           int            `json:"objects"`
	Messages          int            `json:"messages"`
	Words             int            `json:"words"`
	Warnings          []string       `json:"warnings,omitempty"`
}
type envelope struct {
	Version                 int                      `json:"format_version"`
	Kind                    string                   `json:"kind"`
	FullGameVerified        bool                     `json:"full_game_verified"`
	Dataset                 datasetSummary           `json:"dataset"`
	Inventory               *palflow.InventoryReport `json:"inventory,omitempty"`
	Audit                   *palflow.AuditReport     `json:"audit,omitempty"`
	Trace                   *palflow.Report          `json:"trace,omitempty"`
	Scenario                string                   `json:"scenario,omitempty"`
	ExpectedMessagesMatched *bool                    `json:"expected_messages_matched,omitempty"`
	Limitations             []string                 `json:"limitations"`
}
type route struct {
	Version          int               `json:"version"`
	Name             string            `json:"name"`
	FilesSHA256      map[string]string `json:"files_sha256"`
	Request          palflow.Request   `json:"request"`
	ExpectedMessages *[]uint16         `json:"expected_messages"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}
func help(w io.Writer) {
	fmt.Fprintln(w, `palverify inspect -data <目录> -encoding <big5|gbk> [-out reports/import.json]
palverify audit   -data <目录> -encoding <big5|gbk> [-entry <脚本索引>] [-out reports/audit.json]
palverify trace   -data <目录> -encoding <big5|gbk> -route <路线.json> [-include-text] [-out reports/trace.json]

目标是 Windows 98 柔情篇的资源布局；版本身份需后续用实际文件确认。
inspect 读取表与 SHA-256；audit 仅审计已建模指令前缀；trace 仅执行指定触发脚本。
任何结果均不等于主线或全部支线已验证。遇到未实现指令时停止并返回非零。
对话正文默认以消息ID和摘要代替；-include-text 仅用于本地核对，请勿提交商业台词。`)
}
func run(args []string, out, errout io.Writer) error {
	if len(args) == 0 {
		help(out)
		return nil
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		help(out)
		return nil
	}
	mode := args[0]
	if mode != "inspect" && mode != "audit" && mode != "trace" {
		return fmt.Errorf("未知命令 %q", mode)
	}
	fs := flag.NewFlagSet(mode, flag.ContinueOnError)
	fs.SetOutput(errout)
	dir := fs.String("data", "", "原版数据目录")
	encoding := fs.String("encoding", "", "明确选择 big5 或 gbk")
	output := fs.String("out", "", "本地 JSON 报告路径；默认 stdout")
	entry := fs.Int("entry", -1, "仅审计一个入口；默认场景与事件触发入口")
	routePath := fs.String("route", "", "带文件摘要与预期消息序列的路线")
	includeText := fs.Bool("include-text", false, "在本地 trace 中保留原版台词")
	maxSteps := fs.Int("max-steps", 10000, "单次触发脚本最大指令数")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("不接受额外位置参数，请用 -data 指定目录")
	}
	if *dir == "" {
		return errors.New("必须指定 -data，且目录需包含 SSS.MKF、DATA.MKF、M.MSG、WORD.DAT")
	}
	if mode != "audit" && *entry != -1 {
		return errors.New("entry 仅用于 audit；trace 入口须写入路线文件")
	}
	if *entry < -1 || *entry > 65535 {
		return errors.New("entry 须为 0..65535")
	}
	if *maxSteps < 1 || *maxSteps > 100000 {
		return errors.New("max-steps 须为 1..100000")
	}
	if mode != "trace" && (*routePath != "" || *includeText) {
		return errors.New("route/include-text 仅用于 trace")
	}
	if *output != "" && strings.ToLower(filepath.Ext(*output)) != ".json" {
		return errors.New("报告只允许写入 .json 文件，避免覆盖原版资源")
	}
	if *routePath != "" && *output != "" {
		a, _ := filepath.Abs(*routePath)
		b, _ := filepath.Abs(*output)
		if a == b {
			return errors.New("报告不能覆盖路线文件")
		}
	}
	g, err := paldata.Load(*dir, paldata.Options{Encoding: *encoding})
	if err != nil {
		return err
	}
	result := envelope{Version: 1, Kind: mode, Dataset: summarize(g), Limitations: []string{"requested_windows_layout_not_release_identification", "no_full_game_route_verified", "no_world_or_auto_script_simulation", "object_item_magic_roots_not_classified"}}
	instructions := make([]palflow.Instruction, len(g.Scripts))
	for i, v := range g.Scripts {
		instructions[i] = palflow.Instruction{Opcode: v.Opcode, Operands: v.Operands}
	}
	var executionErr error
	switch mode {
	case "inspect":
	case "audit":
		inv := palflow.Inventory(instructions)
		result.Inventory = &inv
		var roots []uint16
		if *entry >= 0 {
			roots = []uint16{uint16(*entry)}
		} else {
			for _, scene := range g.Scenes {
				roots = append(roots, scene.OnEnter, scene.OnTeleport)
			}
			for _, event := range g.Events {
				roots = append(roots, event.Trigger)
			}
			result.Limitations = append(result.Limitations, "auto_script_entrypoints_excluded_different_interpreter_semantics")
		}
		audit := palflow.AuditMany(instructions, roots)
		result.Audit = &audit
	case "trace":
		if *routePath == "" {
			return errors.New("trace 必须指定 -route")
		}
		spec, err := loadRoute(*routePath)
		if err != nil {
			return err
		}
		if err = checkRoute(spec, g); err != nil {
			return err
		}
		result.Scenario = spec.Name
		req := spec.Request
		if req.IdleFrames == nil {
			req.IdleFrames = map[uint16]uint16{}
		}
		// Missing per-event counters come from the imported initial state. A route
		// can override them explicitly for a known save-state checkpoint.
		for i, event := range g.Events {
			id := uint16(i + 1)
			if _, ok := req.IdleFrames[id]; !ok {
				req.IdleFrames[id] = event.ScriptIdleFrame
			}
		}
		engine, err := palflow.New(gameSource{g, *includeText}, palflow.Options{MaxSteps: *maxSteps, MaxCallDepth: 64})
		if err != nil {
			return err
		}
		report, traceErr := engine.Trace(req)
		result.Trace = &report
		matched := slices.Equal(report.DialogueIDs, *spec.ExpectedMessages)
		result.ExpectedMessagesMatched = &matched
		switch {
		case traceErr != nil:
			executionErr = traceErr
		case report.Status != "script_stopped":
			executionErr = errors.New("脚本未正常停止")
		case len(report.UnusedInputs) > 0:
			executionErr = errors.New("路线有未消费输入，不能视为精确回放")
		case !matched:
			executionErr = errors.New("实际消息ID序列与路线预期不一致")
		}
	}
	if err := writeReport(*output, result, out); err != nil {
		return err
	}
	return executionErr
}
func summarize(g *paldata.Game) datasetSummary {
	r := datasetSummary{Profile: g.Profile, Encoding: g.Encoding, Scripts: len(g.Scripts), Scenes: len(g.Scenes), Events: len(g.Events), Objects: len(g.Objects), Messages: len(g.Messages), Words: len(g.Words)}
	r.Warnings = append([]string(nil), g.Warnings...)
	for _, f := range g.Files {
		r.Files = append(r.Files, fileEvidence{Name: strings.ToUpper(f.Name), SHA256: f.SHA256, Size: f.Size})
	}
	return r
}

type gameSource struct {
	game        *paldata.Game
	includeText bool
}

func (s gameSource) Instruction(pc uint16) (palflow.Instruction, error) {
	if int(pc) >= len(s.game.Scripts) {
		return palflow.Instruction{}, fmt.Errorf("脚本索引 %d 越界", pc)
	}
	v := s.game.Scripts[pc]
	return palflow.Instruction{Opcode: v.Opcode, Operands: v.Operands}, nil
}
func (s gameSource) Message(id uint16) (string, error) {
	if int(id) >= len(s.game.Messages) {
		return "", fmt.Errorf("消息索引 %d 越界", id)
	}
	if s.includeText {
		return s.game.Messages[id], nil
	}
	digest := sha256.Sum256(s.game.RawMessages[id])
	return fmt.Sprintf("[message:%d sha256:%x]", id, digest), nil
}
func loadRoute(path string) (route, error) {
	f, err := os.Open(path)
	if err != nil {
		return route{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return route{}, err
	}
	if len(data) > 1<<20 {
		return route{}, errors.New("路线文件超过1MiB")
	}
	if !utf8.Valid(data) {
		return route{}, errors.New("路线文件必须为 UTF-8")
	}
	var r route
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&r); err != nil {
		return r, err
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return r, errors.New("路线须为单个JSON文档")
	}
	return r, nil
}
func checkRoute(r route, g *paldata.Game) error {
	if r.Version != 1 || strings.TrimSpace(r.Name) == "" {
		return errors.New("路线需要 version=1 和非空 name")
	}
	if r.ExpectedMessages == nil {
		return errors.New("路线必须明确指定 expected_messages 数组")
	}
	if len(r.FilesSHA256) != len(g.Files) {
		return errors.New("路线必须固定全部四个原版文件的 SHA-256")
	}
	for _, f := range g.Files {
		if r.FilesSHA256[strings.ToUpper(f.Name)] != f.SHA256 {
			return fmt.Errorf("路线文件摘要不匹配或缺失: %s", strings.ToUpper(f.Name))
		}
	}
	if r.Request.Entry == 0 || int(r.Request.Entry) >= len(g.Scripts) {
		return errors.New("路线需要有效的非零脚本入口")
	}
	event := r.Request.EventID
	if event == 65535 {
		event = r.Request.LastEventID
	}
	if int(event) > len(g.Events) || int(r.Request.LastEventID) > len(g.Events) {
		return errors.New("路线事件上下文超出事件表")
	}
	for id := range r.Request.IdleFrames {
		if id == 0 || int(id) > len(g.Events) {
			return fmt.Errorf("空闲计数事件ID无效: %d", id)
		}
	}
	return nil
}
func writeReport(path string, value any, out io.Writer) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "" {
		_, err = out.Write(data)
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pal-report-*.tmp")
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
	return os.Rename(tmp, path)
}
