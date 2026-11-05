// Command paltool provides a display-independent companion for the studio.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/battle"
	"github.com/HiChen85/pal--reverse--learn/internal/content"
	"github.com/HiChen85/pal--reverse--learn/internal/pal"
	"github.com/HiChen85/pal--reverse--learn/internal/studio"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}
func usage(w io.Writer) {
	fmt.Fprintln(w, `paltool export-demo <pack.json>
paltool validate <pack.json>
paltool merge <base.json> <extension.json> <output.json>
paltool mkf <file.mkf>
paltool demo
paltool play [pack.json]

play 命令: next | choose <从1起的选项编号> | attack [敌方编号] | skill [敌方编号]
          defend | item <物品ID> [队员编号] | retry | save <文件> | load <文件> | quit
所有战斗结算均在 Go 中执行。demo 自动运行原创示例，不读取原版素材。
Windows 98 原版资源审计和触发脚本记录：go run ./cmd/palverify help`)
}
func run(args []string, input io.Reader, out io.Writer) error {
	if len(args) == 0 {
		usage(out)
		return nil
	}
	switch args[0] {
	case "export-demo":
		if len(args) != 2 {
			return errors.New("用法: export-demo <pack.json>")
		}
		return content.Save(args[1], content.Demo())
	case "validate":
		if len(args) != 2 {
			return errors.New("用法: validate <pack.json>")
		}
		p, err := content.Load(args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "有效: %s; %d 人物 / %d 物品 / %d 节点\n", p.Title, len(p.Characters), len(p.Items), len(p.Nodes))
		return nil
	case "merge":
		if len(args) != 4 {
			return errors.New("用法: merge <base.json> <extension.json> <output.json>")
		}
		p, err := content.Load(args[1])
		if err != nil {
			return err
		}
		s, err := studio.New(p)
		if err != nil {
			return err
		}
		if err = s.MergePack(args[2]); err != nil {
			return err
		}
		return s.SavePack(args[3])
	case "mkf":
		if len(args) != 2 {
			return errors.New("用法: mkf <file.mkf>")
		}
		return inventoryMKF(args[1], out)
	case "demo":
		if len(args) != 1 {
			return errors.New("用法: demo")
		}
		return demo(out)
	case "play":
		if len(args) > 2 {
			return errors.New("用法: play [pack.json]")
		}
		p := content.Demo()
		var err error
		if len(args) == 2 {
			p, err = content.Load(args[1])
			if err != nil {
				return err
			}
		}
		s, err := studio.New(p)
		if err != nil {
			return err
		}
		if err = s.Start(""); err != nil {
			return err
		}
		return play(s, input, out)
	case "help", "-h", "--help":
		usage(out)
		return nil
	default:
		return fmt.Errorf("未知命令 %q (用 help 查看)", args[0])
	}
}
func inventoryMKF(path string, out io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	a, err := pal.Open(f, info.Size())
	if err != nil {
		return err
	}
	sizes := make([]int64, a.Count())
	for i := range sizes {
		sizes[i], err = a.ChunkSize(i)
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(out).Encode(struct {
		Chunks int     `json:"chunks"`
		Sizes  []int64 `json:"sizes"`
	}{a.Count(), sizes})
}
func demo(out io.Writer) error {
	s, err := studio.New(content.Demo())
	if err != nil {
		return err
	}
	if err = s.Start(""); err != nil {
		return err
	}
	for step := 0; step < 200; step++ {
		v := s.Current()
		fmt.Fprintf(out, "%s: %s\n", v.Node.ID, v.Node.Text)
		if v.Battle != nil && v.Battle.Outcome == battle.Ongoing {
			target := firstLiving(v.Battle.Enemies)
			if target == "" {
				return errors.New("无有效敌方目标")
			}
			if err = s.Act(battle.Command{ActorID: v.Battle.Turn, Kind: battle.Attack, TargetID: target}); err != nil {
				return err
			}
			continue
		}
		if v.Battle != nil && v.Battle.Outcome == battle.Defeat {
			return errors.New("演示战斗失败")
		}
		if v.Ended {
			fmt.Fprintln(out, "演示完成")
			return nil
		}
		if len(v.Choices) > 0 {
			err = s.Choose(0)
		} else {
			err = s.Continue()
		}
		if err != nil {
			return err
		}
	}
	return errors.New("演示超过200步")
}
func firstLiving(units []battle.Unit) string {
	for _, u := range units {
		if u.HP > 0 {
			return u.ID
		}
	}
	return ""
}
func show(s *studio.Studio, out io.Writer) {
	v := s.Current()
	fmt.Fprintf(out, "\n[%s] %s\n", v.Node.ID, v.Node.Text)
	for i, ch := range v.Choices {
		fmt.Fprintf(out, "  %d. %s\n", i+1, ch.Text)
	}
	if v.Battle != nil {
		b := v.Battle
		fmt.Fprintf(out, "战斗: %s, 回合 %d, 行动者 %s\n", b.Outcome, b.Round, b.Turn)
		for i, u := range b.Party {
			fmt.Fprintf(out, "队员%d %s HP %d/%d MP %d/%d\n", i+1, u.Name, u.HP, u.MaxHP, u.MP, u.MaxMP)
		}
		for i, u := range b.Enemies {
			fmt.Fprintf(out, "敌方%d %s HP %d/%d\n", i+1, u.Name, u.HP, u.MaxHP)
		}
		if len(b.Log) > 0 {
			fmt.Fprintln(out, b.Log[len(b.Log)-1].Text)
		}
	}
	if v.Ended {
		fmt.Fprintln(out, "本章结束。可保存进度，或 quit 退出。")
	}
}
func play(s *studio.Studio, input io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(input)
	show(s, out)
	for {
		fmt.Fprint(out, "> ")
		if !scan.Scan() {
			return scan.Err()
		}
		fields := strings.Fields(scan.Text())
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "quit" {
			return nil
		}
		if fields[0] == "help" {
			usage(out)
			continue
		}
		err := execute(s, fields)
		if err != nil {
			fmt.Fprintln(out, "错误:", err)
		}
		show(s, out)
	}
}
func execute(s *studio.Studio, f []string) error {
	switch f[0] {
	case "next":
		if len(f) != 1 {
			return errors.New("next 不接受参数")
		}
		return s.Continue()
	case "choose":
		if len(f) != 2 {
			return errors.New("choose <编号>")
		}
		n, err := strconv.Atoi(f[1])
		if err != nil {
			return err
		}
		return s.Choose(n - 1)
	case "save", "load":
		if len(f) != 2 {
			return errors.New("save/load <文件>")
		}
		if f[0] == "save" {
			return s.SaveProgress(f[1])
		}
		return s.LoadProgress(f[1])
	case "retry":
		return s.RetryBattle()
	case "attack", "skill", "defend", "item":
		v := s.Current()
		if v.Battle == nil {
			return errors.New("当前没有战斗")
		}
		b := v.Battle
		cmd := battle.Command{ActorID: b.Turn}
		switch f[0] {
		case "attack", "skill":
			if len(f) > 2 {
				return errors.New("attack/skill [敌方编号]")
			}
			cmd.Kind = battle.Attack
			if f[0] == "skill" {
				cmd.Kind = battle.Skill
			}
			cmd.TargetID = firstLiving(b.Enemies)
			if len(f) == 2 {
				n, err := strconv.Atoi(f[1])
				if err != nil || n < 1 || n > len(b.Enemies) {
					return errors.New("无效敌方编号")
				}
				cmd.TargetID = b.Enemies[n-1].ID
			}
		case "defend":
			if len(f) != 1 {
				return errors.New("defend 不接受参数")
			}
			cmd.Kind = battle.Defend
		case "item":
			if len(f) < 2 || len(f) > 3 {
				return errors.New("item <物品ID> [队员编号]")
			}
			cmd.Kind = battle.UseItem
			cmd.ItemID = f[1]
			cmd.TargetID = b.Turn
			if len(f) == 3 {
				n, err := strconv.Atoi(f[2])
				if err != nil || n < 1 || n > len(b.Party) {
					return errors.New("无效队员编号")
				}
				cmd.TargetID = b.Party[n-1].ID
			}
		}
		return s.Act(cmd)
	default:
		return fmt.Errorf("未知命令 %q", f[0])
	}
}
