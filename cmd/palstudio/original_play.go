package main

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"

	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
	"github.com/HiChen85/pal--reverse--learn/internal/palgame"
	"github.com/HiChen85/pal--reverse--learn/internal/palrender"
	"github.com/egoist/mygo/ui"
)

func (o *originalData) updateViewport() {
	o.viewport = nil
	if o.session == nil {
		o.viewportError = "请先建立原版会话"
		return
	}
	img, err := palrender.Render(palrender.Source{Maps: o.maps, Sprites: o.assets, Palettes: o.palettes}, o.session.Snapshot().World)
	if err != nil {
		o.viewportError = err.Error()
		return
	}
	o.viewportError = ""
	o.viewport = ui.NewBitmap(img).Pixelated()
}

// runControl never selects an event ID. The engine chooses touch/search
// targets from the current position, direction and scene, on a transaction.
func (o *originalData) runControl(kind string, direction uint16) error {
	if o.changedSource() {
		return errors.New("来源选项已改变，请重新读取后运行")
	}
	if !o.useSession || o.session == nil {
		return errors.New("空间操作需要启用逻辑会话")
	}
	inputs, err := decodeOriginalInputs(o.inputsJSON)
	if err != nil {
		return err
	}
	before := o.session.Snapshot()
	intent := originalActionIntent{Kind: kind, SceneNumber: int(before.World.Scene), Inputs: append([]palflow.Input(nil), inputs...)}
	var report palgame.ControlReport
	switch kind {
	case "move":
		intent.Direction = &direction
		report, err = o.session.Move(direction, inputs)
	case "interact":
		report, err = o.session.Interact(inputs)
	default:
		return errors.New("未知空间操作")
	}
	after := o.session.Snapshot()
	checkpoint := o.checkpoint
	record := originalInvocation{ActionIntent: intent, Mode: "control", Kind: kind, SceneNumber: int(before.World.Scene), Checkpoint: &checkpoint, SessionBefore: &before, SessionAfter: &after, SessionCommitted: err == nil && after.Actions > before.Actions, Control: &report}
	if err != nil {
		record.Error = err.Error()
	}
	o.history = append(o.history, record)
	if len(o.history) > 16 {
		o.history = o.history[len(o.history)-16:]
	}
	o.updateViewport()
	return err
}

func (a *workbench) originalPlayView(c *ui.Context) {
	o := a.original
	if !o.useSession || o.session == nil {
		return
	}
	t := c.Theme()
	ui.Text(c, "地图视口 · 空间操作").Bold()
	ui.Text(c, palrender.Limitations).FontSize(12).TextColor(t.TextMuted)
	if o.viewport != nil {
		ui.Image(c.Key("original-viewport"), o.viewport).Size(640, 400).Fit(ui.Contain)
	} else {
		ui.Text(c, "视口不可用："+o.viewportError).FontSize(12).TextColor(t.Warning)
	}
	s := o.session.Snapshot()
	if s.World.NeedToFadeIn || s.World.ScreenTint != "" || s.World.ScreenWave != 0 || s.World.WaveProgression != 0 {
		ui.Text(c, fmt.Sprintf("资源预览未应用当前画面切换：待淡入 %t · 色调 %q · 波纹 %d / %d", s.World.NeedToFadeIn, s.World.ScreenTint, s.World.ScreenWave, s.World.WaveProgression)).FontSize(12).TextColor(t.Warning)
	}
	ui.Text(c, fmt.Sprintf("地图位置 (%d,%d) · 朝向 %d · 视口 (%d,%d) · 世界帧 %d", s.World.ViewportX+s.World.PartyOffsetX, s.World.ViewportY+s.World.PartyOffsetY, s.World.Direction, s.World.ViewportX, s.World.ViewportY, s.World.FrameNum)).FontSize(12)
	ui.Row(c).Gap(8).Children(func() {
		for _, button := range []struct {
			label     string
			direction uint16
		}{{"向南 ↙", 0}, {"向西 ↖", 1}, {"向北 ↗", 2}, {"向东 ↘", 3}} {
			if ui.Button(c.Key(fmt.Sprintf("original-move-%d", button.direction)), button.label).Disabled(o.changedSource()).Clicked() {
				a.report(o.runControl("move", button.direction), "空间移动帧已提交")
			}
		}
		if ui.PrimaryButton(c.Key("original-interact"), "调查").Disabled(o.changedSource()).Clicked() {
			a.report(o.runControl("interact", 0xffff), "调查帧已提交；目标由当前位置和朝向选出")
		}
	})
	ui.Text(c, "方向键每次推进一个世界帧并移动16/8像素；调查按当前位置和朝向搜索。缺少地图、待进入场景或脚本阻塞会回滚整帧；下方事件 ID 调用只用于诊断。").FontSize(12).TextColor(t.TextMuted)
	items := []string{}
	for id, count := range s.World.Inventory {
		if count > 0 && s.World.Objects[id][6]&1 != 0 {
			items = append(items, strconv.Itoa(int(id)))
		}
	}
	for _, role := range s.World.PartyRoles {
		raw := s.World.Roles[role]
		for _, id := range raw[11:17] {
			if id != 0 && s.World.Objects[id][6]&1 != 0 {
				items = append(items, strconv.Itoa(int(id)))
			}
		}
	}
	sort.Strings(items)
	items = slices.Compact(items)
	roles := []string{"65535"}
	for _, id := range s.World.PartyRoles {
		roles = append(roles, strconv.Itoa(int(id)))
	}
	ui.Row(c).Gap(8).Children(func() {
		ui.Select(c.Key("original-use-item"), &o.item, items).Label("使用物品 ID").Width(130)
		ui.Select(c.Key("original-item-role"), &o.itemRole, roles).Label("人物 ID（全体为65535）").Width(200)
		if ui.Button(c.Key("original-item-submit"), "使用物品").Disabled(o.changedSource() || len(items) == 0).Clicked() {
			a.report(o.runUseItem(), "物品使用动作已提交")
		}
	})
}

func (o *originalData) runUseItem() error {
	if o.changedSource() || o.session == nil {
		return errors.New("请先读取来源并建立会话")
	}
	item, err := strconv.ParseUint(o.item, 10, 16)
	if err != nil || item == 0 {
		return errors.New("请选择物品 ID")
	}
	role, err := strconv.ParseUint(o.itemRole, 10, 16)
	if err != nil {
		return errors.New("请选择人物 ID；全体物品选择65535")
	}
	inputs, err := decodeOriginalInputs(o.inputsJSON)
	if err != nil {
		return err
	}
	before := o.session.Snapshot()
	report, runErr := o.session.UseItem(uint16(item), uint16(role), inputs)
	after := o.session.Snapshot()
	i, target, checkpoint := uint16(item), uint16(role), o.checkpoint
	record := originalInvocation{ActionIntent: originalActionIntent{Kind: "use_item", Item: &i, TargetRole: &target, Inputs: inputs, SceneNumber: int(before.World.Scene)}, Kind: "use_item", Mode: "session", SceneNumber: int(before.World.Scene), Report: report, Checkpoint: &checkpoint, SessionBefore: &before, SessionAfter: &after, SessionCommitted: runErr == nil && after.Actions > before.Actions}
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

func (a *workbench) originalControlReportView(c *ui.Context, record originalInvocation) {
	r := record.Control
	ui.Text(c, fmt.Sprintf("空间动作 %s · 完成 %t · 移动 %t · 碰撞阻挡 %t · 调查事件 %d · 触发事件 %v · 消费输入 %d / 剩余 %d · 工作量 %d", record.Kind, r.Completed, r.Moved, r.Blocked, r.SearchEventID, r.TriggeredEvents, r.InputsConsumed, len(r.UnusedInputs), r.Work)).FontSize(12)
	for index, trigger := range r.Triggers {
		if trigger.Blocker != nil {
			ui.Text(c, fmt.Sprintf("空间脚本 %d 阻塞 PC %04x · opcode %04x · %s", index, trigger.Blocker.PC, trigger.Blocker.Opcode, trigger.Blocker.Reason)).TextColor(c.Theme().Warning)
		}
		for i, step := range trigger.Trace {
			if step.Message != nil {
				ui.Text(c.Key(fmt.Sprintf("control-message-%d-%d", index, i)), step.Message.Text)
			}
		}
	}
}
