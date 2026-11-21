package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

const originalFastBattleChoice = "快速验证战斗（重置后生效）"

func (o *originalData) selectedBattleMode() string {
	if o.fastVerify {
		return "fast_verify"
	}
	return "auto_attack"
}

func (a *workbench) originalBattlePolicyView(c *ui.Context) {
	o := a.original
	ui.Checkbox(c.Key("original-fast-battle"), &o.fastVerify, originalFastBattleChoice).Disabled(o.runtime == nil)
	next := o.selectedBattleMode()
	ui.Text(c, fmt.Sprintf("下次重置战斗模式：%s", next)).FontSize(12).TextColor(c.Theme().TextMuted)
	active := ""
	if o.session != nil {
		active = o.session.Snapshot().World.BattleMode
		ui.Text(c, fmt.Sprintf("当前会话战斗模式：%s", active)).FontSize(12)
		if active != next {
			ui.Text(c, "选择已改变；当前会话沿用原模式，重置后生效。").FontSize(12).TextColor(c.Theme().Warning)
		}
	}
	if o.fastVerify || active == "fast_verify" {
		ui.Text(c, "快速验证将敌人非零初始生命值降为1，仍执行实际战斗、脚本与奖励；不保证取胜，也不用于原版平衡验收。").FontSize(12).TextColor(c.Theme().Warning)
	}
}
