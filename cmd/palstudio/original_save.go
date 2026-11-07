package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
	"github.com/egoist/mygo/ui"
)

// Session files are local modern JSON checkpoints. Explicit UI load is the only
// operation here that installs a restored session; saving never resets one.
func (o *originalData) runSessionFile(kind string) (err error) {
	if o.session == nil || o.runtime == nil || o.changedSource() {
		return errors.New("请先读取来源并建立会话，来源选项改变后须重新读取")
	}
	before := o.session.Snapshot()
	record := originalInvocation{Kind: kind, Mode: "session_file", ActionIntent: originalActionIntent{Kind: kind, Path: o.savePath, SceneNumber: int(before.World.Scene)}, SceneNumber: int(before.World.Scene), SessionBefore: &before}
	defer func() {
		after := o.session.Snapshot()
		record.SessionAfter = &after
		if err != nil {
			record.Error = err.Error()
			o.saveResult = "会话文件操作失败：" + err.Error()
		} else if kind == "load_session" {
			record.SessionCommitted = true
			o.saveResult = fmt.Sprintf("已恢复本地 JSON 会话：场景 %d · 已提交 %d 次动作 · 待进入场景 %t；存档不作为路线可达证据。", after.World.Scene, after.Actions, after.NeedsSceneEnter)
		} else {
			o.saveResult = "已原子保存本地 JSON 会话；这是工作台格式，不是原版 RPG 存档。"
		}
		o.history = append(o.history, record)
		if len(o.history) > 16 {
			o.history = o.history[len(o.history)-16:]
		}
	}()
	if kind != "save_session" && kind != "load_session" {
		return errors.New("未知会话文件操作")
	}
	path, err := o.validateSessionPath()
	if err != nil {
		return err
	}
	if kind == "save_session" {
		data, err := o.session.MarshalSave()
		if err != nil {
			return err
		}
		return writeOriginalSessionAtomic(path, data, func() error { _, err := o.validateSessionPath(); return err })
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !os.SameFile(info, current) || unsafeSessionLinks(info) {
		return errors.New("会话读取路径必须是单链接普通文件")
	}
	if info.Size() < 1 || info.Size() > palgame.MaxSaveBytes {
		return errors.New("会话 JSON 文件大小超出限制")
	}
	data, err := io.ReadAll(io.LimitReader(f, palgame.MaxSaveBytes+1))
	if err != nil {
		return err
	}
	if len(data) > palgame.MaxSaveBytes {
		return errors.New("会话 JSON 文件超过大小限制")
	}
	next, err := palgame.RestoreSession(o.game, o.runtime, palgame.SessionOptions{Assets: o.assets, Maps: o.maps, Palettes: o.palettes, RNG: o.rng, FBP: o.fbp}, data)
	if err != nil {
		return err
	}
	// Every fallible read and validation has finished before the active pointer
	// changes. Rendering errors are visible in the viewport and do not reset it.
	o.session = next
	o.fastVerify = next.Snapshot().World.BattleMode == "fast_verify"
	o.useSession = true
	o.scene = fmt.Sprint(next.Snapshot().World.Scene)
	o.event = ""
	o.inputsJSON = "[]"
	o.maxSteps = float64(next.ExecutionOptions().MaxSteps)
	checkpoint := next.InitialCheckpoint()
	o.checkpoint = originalCheckpoint{Scene: checkpoint.Scene, AssumeSceneEntered: checkpoint.AssumeSceneEntered}
	o.assumeEntered = checkpoint.AssumeSceneEntered
	o.updateViewport()
	return nil
}

func unsafeSessionLinks(info os.FileInfo) bool {
	// Unix exposes link counts on FileInfo.Sys. SameFile checks below protect
	// imported resources on platforms whose FileInfo has no link-count field.
	stat := reflect.ValueOf(info.Sys())
	if stat.IsValid() && stat.Kind() == reflect.Pointer {
		stat = stat.Elem()
	}
	if stat.IsValid() && stat.Kind() == reflect.Struct {
		links := stat.FieldByName("Nlink")
		if links.IsValid() && links.CanUint() {
			return links.Uint() != 1
		}
	}
	return false
}

func (o *originalData) validateSessionPath() (string, error) {
	if o.savePath == "" || strings.ToLower(filepath.Ext(o.savePath)) != ".json" {
		return "", errors.New("会话文件必须使用 .json 扩展名")
	}
	path, err := filepath.Abs(o.savePath)
	if err != nil {
		return "", err
	}
	path = filepath.Clean(path)
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	if parent != filepath.Dir(path) {
		return "", errors.New("会话文件目录不接受符号链接别名")
	}
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err == nil && (!info.Mode().IsRegular() || unsafeSessionLinks(info)) {
		return "", errors.New("会话文件不接受符号链接、硬链接或特殊文件")
	}
	protected := []string{o.exportPath}
	files := append([]paldata.FileInfo(nil), o.game.Files...)
	if o.mgoIdentity != nil {
		files = append(files, *o.mgoIdentity)
	}
	files = append(files, o.mapIdentities...)
	if o.paletteIdentity != nil {
		files = append(files, *o.paletteIdentity)
	}
	files = append(files, o.mediaIdentities...)
	for _, file := range files {
		protected = append(protected, filepath.Join(o.loadedDirectory, file.Name))
	}
	resources, err := o.protectedOriginalResourcePaths()
	if err != nil {
		return "", err
	}
	protected = append(protected, resources...)
	for _, input := range protected {
		if input == "" {
			continue
		}
		absolute, err := filepath.Abs(input)
		if err != nil {
			return "", err
		}
		if path == absolute {
			return "", errors.New("会话路径不能覆盖资源或脚本报告")
		}
		if info != nil {
			other, err := os.Stat(absolute)
			if err == nil && os.SameFile(info, other) {
				return "", errors.New("会话路径是资源或脚本报告的别名")
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
	}
	return path, nil
}

func writeOriginalSessionAtomic(path string, data []byte, recheck func() error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pal-session-*.json")
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
	if err = recheck(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (a *workbench) originalSaveView(c *ui.Context) {
	o := a.original
	ui.Row(c).Gap(8).Children(func() {
		ui.TextInput(c.Key("original-session-path"), &o.savePath).Label("本地会话 JSON 路径").Grow(1)
		if ui.Button(c.Key("original-session-save"), "保存会话 JSON").Disabled(o.session == nil || o.changedSource()).Clicked() {
			a.report(o.runSessionFile("save_session"), "已保存本地会话 JSON")
		}
		if ui.Button(c.Key("original-session-load"), "读取会话 JSON").Disabled(o.runtime == nil || o.changedSource()).Clicked() {
			a.report(o.runSessionFile("load_session"), "已恢复本地会话 JSON；存档不作为路线可达证据")
		}
	})
	if o.saveResult != "" {
		ui.Text(c, o.saveResult).FontSize(12).TextColor(c.Theme().TextMuted)
	}
}
