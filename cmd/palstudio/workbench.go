package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/battle"
	"github.com/HiChen85/pal--reverse--learn/internal/content"
	"github.com/HiChen85/pal--reverse--learn/internal/studio"
	"github.com/egoist/mygo/ui"
)

type workbench struct {
	original                               *originalData
	service                                *studio.Studio
	draft                                  content.Pack
	tab                                    int
	packPath, extensionPath, progressPath  string
	message                                string
	failed, dirty, jsonDirty               bool
	nodeID, characterID, itemID, previewID string
	jsonDraft                              string
	enemyTarget, allyTarget, battleItem    string
}

func newWorkbench(p content.Pack) (*workbench, error) {
	s, err := studio.New(p)
	if err != nil {
		return nil, err
	}
	a := &workbench{service: s, packPath: "chapter.json", extensionPath: "extension.json", progressPath: "progress.sav.json"}
	a.reloadDraft()
	a.report(s.Start(""), "已载入原创示例，可以直接试玩。")
	return a, nil
}
func (a *workbench) report(err error, success string) {
	a.failed = err != nil
	if err != nil {
		a.message = err.Error()
	} else {
		a.message = success
	}
}
func (a *workbench) reloadDraft() {
	a.draft = a.service.Pack()
	a.dirty = false
	if len(a.draft.Nodes) > 0 {
		a.nodeID = a.draft.Nodes[0].ID
		a.previewID = a.draft.Start
	}
	if len(a.draft.Characters) > 0 {
		a.characterID = a.draft.Characters[0].ID
	}
	if len(a.draft.Items) > 0 {
		a.itemID = a.draft.Items[0].ID
	}
	a.refreshJSON()
}
func (a *workbench) refreshJSON() {
	b, _ := json.MarshalIndent(a.draft, "", "  ")
	a.jsonDraft = string(b)
	a.jsonDirty = false
}
func (a *workbench) apply() error {
	if a.jsonDirty {
		return errors.New("JSON 编辑区有未载入的修改，请先点击从 JSON 载入草稿")
	}
	if err := a.service.ReplacePack(a.draft); err != nil {
		return err
	}
	a.dirty = false
	a.refreshJSON()
	return nil
}
func (a *workbench) applyJSON() error {
	p, err := content.Decode([]byte(a.jsonDraft))
	if err != nil {
		return err
	}

	a.draft = p
	a.jsonDirty = false
	a.dirty = true
	if len(p.Nodes) > 0 {
		a.nodeID = p.Nodes[0].ID
	}
	if len(p.Characters) > 0 {
		a.characterID = p.Characters[0].ID
	}
	if len(p.Items) > 0 {
		a.itemID = p.Items[0].ID
	}
	return nil
}
func (a *workbench) savePack() error {
	if a.jsonDirty {
		return errors.New("JSON 编辑区有未载入的修改，请先点击从 JSON 载入草稿")
	}
	if a.dirty {
		if err := a.apply(); err != nil {
			return err
		}
	}
	return a.service.SavePack(a.packPath)
}
func (a *workbench) start() error {
	if a.dirty {
		return errors.New("请先校验并应用草稿，再开始试玩")
	}
	return a.service.Start(a.previewID)
}
func (a *workbench) field(c *ui.Context, label string, value *string) {
	ui.Row(c).Gap(12).FillWidth().Children(func() {
		ui.Text(c, label).Width(110).TextColor(c.Theme().TextMuted)
		if ui.TextInput(c.Key(label), value).Label(label + "输入").Grow(1).Changed() {
			a.dirty = true
		}
	})
}
func (a *workbench) number(c *ui.Context, label string, value *int, min int) {
	ui.Row(c).Gap(12).FillWidth().Children(func() {
		ui.Text(c, label).Width(110).TextColor(c.Theme().TextMuted)
		f := float64(*value)
		if ui.NumberInput(c.Key(label), &f, float64(min), 1000000, 1).Label(label + "数值").Width(190).Changed() {
			*value = int(f)
			a.dirty = true
		}
	})
}
func (a *workbench) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		ui.Row(c).Padding(18, 22).Gap(18).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, "PAL1 Go Studio").Bold().FontSize(25)
				ui.Text(c, "故事工坊 · 内容编辑与纯 Go 战斗").TextColor(t.TextMuted)
			})
			ui.Text(c, "MyGo 0.7.4 · Go 1.27.3").FontSize(12).TextColor(t.TextMuted)
		})
		ui.Column(c).Padding(12, 22).Gap(8).Background(t.Surface).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, "内容包").Width(70)
				ui.TextInput(c.Key("pack-path"), &a.packPath).Label("内容包路径").Grow(1)
				if ui.Button(c, "打开").Disabled(a.dirty).Clicked() {
					err := a.service.LoadPack(a.packPath)
					if err == nil {
						a.reloadDraft()
					}
					a.report(err, "已打开内容包")
				}
				if ui.PrimaryButton(c, "保存内容包").Clicked() {
					a.report(a.savePack(), "内容包已保存")
				}
				if ui.Button(c, "校验并应用").Clicked() {
					a.report(a.apply(), "校验成功，试玩已重置")
				}
				if ui.Button(c, "撤销草稿").Disabled(!a.dirty).Clicked() {
					a.reloadDraft()
					a.report(nil, "已撤销未应用草稿")
				}
			})
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, "扩展包").Width(70)
				ui.TextInput(c.Key("extension-path"), &a.extensionPath).Label("扩展包路径").Grow(1)
				if ui.Button(c, "追加章节").Disabled(a.dirty).Clicked() {
					err := a.service.MergePack(a.extensionPath)
					if err == nil {
						a.reloadDraft()
					}
					a.report(err, "扩展已合并；在试玩中选择章节入口")
				}
				state := "已应用"
				if a.dirty {
					state = "草稿有修改 · 应用后重置试玩"
				}
				ui.Text(c, state).FontSize(12).TextColor(t.TextMuted)
			})
		})
		ui.Tabs(c, &a.tab, "剧情", "人物", "物品", "试玩", "完整 JSON", "原版数据").Padding(12, 22)
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(8, 22, 22).Gap(12).FillWidth().Children(func() {
				switch a.tab {
				case 0:
					a.storyEditor(c)
				case 1:
					a.characterEditor(c)
				case 2:
					a.itemEditor(c)
				case 3:
					a.preview(c)
				case 4:
					a.jsonEditor(c)
				case 5:
					a.originalView(c)
				}
			})
		})
		color := t.TextMuted
		if a.failed {
			color = t.Danger
		}
		ui.Text(c, a.message).TextColor(color).Padding(10, 22).FillWidth().Background(t.Surface)
	})
}
func (a *workbench) storyEditor(c *ui.Context) {
	a.field(c, "作品标题", &a.draft.Title)
	a.field(c, "默认入口", &a.draft.Start)
	ids := make([]string, len(a.draft.Nodes))
	for i, n := range a.draft.Nodes {
		ids[i] = n.ID
	}
	ui.Row(c).Gap(10).Children(func() {
		ui.Text(c, "剧情节点").Width(110)
		ui.Select(c, &a.nodeID, ids).Grow(1)
		if ui.Button(c, "新增节点").Clicked() {
			id := uniqueID("scene", ids)
			a.draft.Nodes = append(a.draft.Nodes, content.Node{ID: id, Text: "在这里书写新的故事。"})
			a.nodeID = id
			a.dirty = true
		}
	})
	for i := range a.draft.Nodes {
		n := &a.draft.Nodes[i]
		if n.ID != a.nodeID {
			continue
		}
		ui.Text(c, "ID: "+n.ID).FontSize(12).TextColor(c.Theme().TextMuted)
		a.field(c, "说话人物 ID", &n.Speaker)
		ui.Text(c, "对白 / 叙述")
		if ui.TextArea(c.Key("node-text"), &n.Text).Label("对白内容").Height(150).FillWidth().Changed() {
			a.dirty = true
		}
		a.field(c, "战斗遭遇 ID", &n.Encounter)
		a.field(c, "后续节点", &n.Next)
		ui.Text(c, "分支选项：设置分支后请清空后续节点；条件标记可在完整 JSON 中编辑。").FontSize(12).TextColor(c.Theme().TextMuted)
		for j := range n.Choices {
			ch := &n.Choices[j]
			ui.Column(c.Key(fmt.Sprintf("choice-%d", j))).Gap(6).Padding(10).Background(c.Theme().Surface).Children(func() { a.field(c, "选项文字", &ch.Text); a.field(c, "跳转节点", &ch.Next) })
		}
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "新增分支选项").Disabled(n.Encounter != "").Clicked() {
				n.Choices = append(n.Choices, content.Choice{Text: "新的选择", Next: n.ID})
				n.Next = ""
				a.dirty = true
			}
			if ui.Button(c, "删除末尾选项").Disabled(len(n.Choices) == 0).Clicked() {
				n.Choices = n.Choices[:len(n.Choices)-1]
				a.dirty = true
			}
		})
	}
}
func (a *workbench) characterEditor(c *ui.Context) {
	ids := make([]string, len(a.draft.Characters))
	for i, ch := range a.draft.Characters {
		ids[i] = ch.ID
	}
	ui.Row(c).Gap(10).Children(func() {
		ui.Text(c, "人物").Width(110)
		ui.Select(c, &a.characterID, ids).Grow(1)
		if ui.Button(c, "新增人物").Clicked() {
			id := uniqueID("character", ids)
			a.draft.Characters = append(a.draft.Characters, content.Character{ID: id, Name: "新人物", MaxHP: 100, MaxMP: 20, Attack: 15, Defense: 5, Speed: 10})
			a.characterID = id
			a.dirty = true
		}
	})
	ui.Text(c, "前三名人物组成队伍，每场战斗生命/灵力恢复，背包跨战斗保留；败北重试还原本场初始背包。").TextColor(c.Theme().TextMuted).FontSize(12)
	for i := range a.draft.Characters {
		ch := &a.draft.Characters[i]
		if ch.ID != a.characterID {
			continue
		}
		ui.Text(c, "ID: "+ch.ID)
		a.field(c, "人物姓名", &ch.Name)
		a.number(c, "生命上限", &ch.MaxHP, 1)
		a.number(c, "灵力上限", &ch.MaxMP, 0)
		a.number(c, "攻击", &ch.Attack, 0)
		a.number(c, "防御", &ch.Defense, 0)
		a.number(c, "速度", &ch.Speed, 0)
	}
}
func (a *workbench) itemEditor(c *ui.Context) {
	ids := make([]string, len(a.draft.Items))
	for i, it := range a.draft.Items {
		ids[i] = it.ID
	}
	ui.Row(c).Gap(10).Children(func() {
		ui.Text(c, "物品").Width(110)
		ui.Select(c, &a.itemID, ids).Grow(1)
		if ui.Button(c, "新增物品").Clicked() {
			id := uniqueID("item", ids)
			a.draft.Items = append(a.draft.Items, content.Item{ID: id, Name: "新药草", HealHP: 10})
			a.itemID = id
			a.dirty = true
		}
	})
	for i := range a.draft.Items {
		it := &a.draft.Items[i]
		if it.ID != a.itemID {
			continue
		}
		ui.Text(c, "ID: "+it.ID)
		a.field(c, "物品名称", &it.Name)
		a.field(c, "物品描述", &it.Description)
		a.number(c, "回复生命", &it.HealHP, 0)
		a.number(c, "回复灵力", &it.RestoreMP, 0)
	}
}
func (a *workbench) jsonEditor(c *ui.Context) {
	ui.Text(c, "编辑完整内容包：章节、条件标记、战斗遭遇及敌人。同步按钮会用表单替换 JSON 草稿；载入按钮会用 JSON 替换表单草稿。载入后仍需校验并应用。").TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, "将表单同步到 JSON").Clicked() {
			a.refreshJSON()
			a.report(nil, "已将表单内容写入 JSON 编辑区")
		}
		if ui.Button(c, "从 JSON 载入草稿").Clicked() {
			a.report(a.applyJSON(), "JSON 已载入草稿，请校验并应用")
		}
	})
	if ui.TextArea(c.Key("pack-json"), &a.jsonDraft).Label("完整内容 JSON").Font("monospace").Height(420).FillWidth().Changed() {
		a.dirty = true
		a.jsonDirty = true
	}
}
func uniqueID(prefix string, ids []string) string {
	for n := 1; ; n++ {
		id := fmt.Sprintf("%s-%d", prefix, n)
		exists := false
		for _, v := range ids {
			if v == id {
				exists = true
				break
			}
		}
		if !exists {
			return id
		}
	}
}

func (a *workbench) preview(c *ui.Context) {
	p := a.service.Pack()
	ids := make([]string, len(p.Nodes))
	for i, n := range p.Nodes {
		ids[i] = n.ID
	}
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, "章节入口").Width(80)
		ui.Select(c.Key("preview-node"), &a.previewID, ids).Grow(1)
		if ui.PrimaryButton(c, "从入口开始").Disabled(a.dirty).Clicked() {
			a.report(a.start(), "试玩已开始")
		}
	})
	ui.Row(c).Gap(8).Children(func() {
		ui.TextInput(c.Key("progress-path"), &a.progressPath).Label("进度存档路径").Grow(1)
		if ui.Button(c, "保存进度").Clicked() {
			a.report(a.service.SaveProgress(a.progressPath), "进度已保存")
		}
		if ui.Button(c, "读取进度").Disabled(a.dirty).Clicked() {
			a.report(a.service.LoadProgress(a.progressPath), "进度已恢复")
		}
	})
	if a.dirty {
		ui.Text(c, "有未应用草稿；当前试玩仍使用上次应用的内容。").TextColor(c.Theme().Warning)
	}
	v := a.service.Current()
	if !v.Started {
		ui.Text(c, "选择章节入口开始试玩。")
		return
	}
	ui.Column(c).Gap(12).Padding(20).FillWidth().Radius(10).Background(c.Theme().Surface).Children(func() {
		speaker := "旁白"
		for _, ch := range p.Characters {
			if ch.ID == v.Node.Speaker {
				speaker = ch.Name
			}
		}
		ui.Text(c, speaker).Bold().FontSize(18)
		ui.Text(c, v.Node.Text).FontSize(17)
		if v.Ended {
			ui.Text(c, "本章完 · 可选择其他入口继续探索").TextColor(c.Theme().Accent)
			return
		}
		if v.Battle != nil {
			a.battleView(c, *v.Battle)
			return
		}
		for i, ch := range v.Choices {
			if ui.Button(c.Key(fmt.Sprintf("preview-choice-%d", i)), ch.Text).FillWidth().Clicked() {
				a.report(a.service.Choose(i), "剧情已推进")
			}
		}
		if len(v.Choices) == 0 && len(v.Node.Choices) > 0 {
			ui.Text(c, "当前标记不满足任何分支条件，请选择其他章节入口，或在编辑器中调整条件。").TextColor(c.Theme().Warning)
		} else if len(v.Choices) == 0 {
			if ui.PrimaryButton(c, "继续剧情").Clicked() {
				a.report(a.service.Continue(), "剧情已推进")
			}
		}
	})
}
func (a *workbench) battleView(c *ui.Context, s battle.State) {
	outcome := map[battle.Outcome]string{battle.Ongoing: "进行中", battle.Victory: "胜利", battle.Defeat: "失败"}[s.Outcome]
	ui.Text(c, fmt.Sprintf("战斗 · 第 %d 回合 · %s", s.Round, outcome)).Bold()
	for _, u := range append(append([]battle.Unit(nil), s.Party...), s.Enemies...) {
		marker := ""
		if u.ID == s.Turn {
			marker = " · 当前行动"
		}
		ui.Text(c, fmt.Sprintf("%s  HP %d/%d   MP %d/%d%s", u.Name, u.HP, u.MaxHP, u.MP, u.MaxMP, marker))
		ui.Progress(c, float64(u.HP)/float64(u.MaxHP)).FillWidth()
	}
	if s.Outcome == battle.Victory {
		if ui.PrimaryButton(c, "战斗胜利，继续剧情").Clicked() {
			a.report(a.service.Continue(), "剧情已推进")
		}
	} else if s.Outcome == battle.Defeat {
		ui.Text(c, "队伍落败，可重试本场战斗。")
		if ui.Button(c, "重试战斗").Clicked() {
			a.report(a.service.RetryBattle(), "战斗已重置")
		}
	} else {
		enemies := []string{}
		enemyNames := []string{}
		for _, u := range s.Enemies {
			if u.HP > 0 {
				enemies = append(enemies, u.ID)
				enemyNames = append(enemyNames, u.Name)
			}
		}
		allies := []string{}
		allyNames := []string{}
		for _, u := range s.Party {
			if u.HP > 0 {
				allies = append(allies, u.ID)
				allyNames = append(allyNames, u.Name)
			}
		}
		a.enemyTarget = validSelection(a.enemyTarget, enemies)
		a.allyTarget = validSelection(a.allyTarget, allies)
		items := []string{}
		itemNames := []string{}
		for _, it := range a.service.Pack().Items {
			if s.Inventory[it.ID] > 0 {
				items = append(items, it.ID)
				itemNames = append(itemNames, fmt.Sprintf("%s ×%d", it.Name, s.Inventory[it.ID]))
			}
		}
		a.battleItem = validSelection(a.battleItem, items)
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, "敌方目标")
			selectNamed(c.Key("enemy-target"), &a.enemyTarget, enemies, enemyNames).Label("敌方目标选择").Grow(1)
			if ui.Button(c, "攻击").Clicked() {
				a.report(a.service.Act(battle.Command{ActorID: s.Turn, Kind: battle.Attack, TargetID: a.enemyTarget}), "已攻击")
			}
			if ui.Button(c, "技能 · 3 灵力").Clicked() {
				a.report(a.service.Act(battle.Command{ActorID: s.Turn, Kind: battle.Skill, TargetID: a.enemyTarget}), "已施放技能")
			}
			if ui.Button(c, "防御").Clicked() {
				a.report(a.service.Act(battle.Command{ActorID: s.Turn, Kind: battle.Defend}), "进入防御")
			}
		})
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, "物品 / 友方")
			selectNamed(c.Key("battle-item"), &a.battleItem, items, itemNames).Label("战斗物品选择").Grow(1)
			selectNamed(c.Key("ally-target"), &a.allyTarget, allies, allyNames).Label("友方目标选择").Grow(1)
			if ui.Button(c, "使用物品").Disabled(len(items) == 0).Clicked() {
				a.report(a.service.Act(battle.Command{ActorID: s.Turn, Kind: battle.UseItem, TargetID: a.allyTarget, ItemID: a.battleItem}), "已使用物品")
			}
		})
	}
	logs := s.Log
	if len(logs) > 10 {
		logs = logs[len(logs)-10:]
	}
	lines := make([]string, len(logs))
	for i, e := range logs {
		lines[i] = e.Text
	}
	ui.Text(c, strings.Join(lines, "\n")).FontSize(12).TextColor(c.Theme().TextMuted)
}
func validSelection(current string, options []string) string {
	for _, s := range options {
		if s == current {
			return current
		}
	}
	if len(options) > 0 {
		return options[0]
	}
	return ""
}

// selectNamed keeps stable IDs in state while showing authored names to players.
func selectNamed(c *ui.Context, selected *string, ids, names []string) ui.Element {
	labels := append([]string(nil), names...)
	counts := map[string]int{}
	for _, name := range names {
		counts[name]++
	}
	for i, name := range names {
		if counts[name] > 1 {
			labels[i] = fmt.Sprintf("%s · %d", name, i+1)
		}
	}
	label := ""
	for i, id := range ids {
		if id == *selected {
			label = labels[i]
		}
	}
	el := ui.Select(c, &label, labels)
	for i, candidate := range labels {
		if candidate == label {
			*selected = ids[i]
			break
		}
	}
	return el
}
