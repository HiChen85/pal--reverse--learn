package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HiChen85/pal--reverse--learn/internal/content"
	"github.com/egoist/mygo/ui"
)

func TestNativeMapEditorAndWalkPreview(t *testing.T) {
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 960, 1100)
	click := func(labels ...string) {
		t.Helper()
		for _, label := range labels {
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
		}
	}
	replaceText := func(label, text string) {
		t.Helper()
		click(label)
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type(text)
	}
	click("地图", "新增地图")
	replaceText("地图名称输入", "雾岸")
	click("格子 (1, 0)", "墙壁", "格子 (0, 1)", "设为起点", "格子 (1, 1)", "剧情节点标记选择", "well", "设置标记")
	if len(a.draft.Maps) != 1 {
		t.Fatal("new map was not added")
	}
	m := a.draft.Maps[0]
	wall, spawn, marker := content.MapPoint{X: 1}, content.MapPoint{Y: 1}, content.MapPoint{X: 1, Y: 1}
	event, marked := m.EventAt(marker)
	if !a.dirty || m.Name != "雾岸" || m.Spawn != spawn || m.Walkable(wall) || !marked || event.Node != "well" {
		t.Fatalf("unexpected map draft: %+v", m)
	}
	click("地图行走试玩")
	if a.maps.preview != nil {
		t.Fatal("walking accepted a draft map")
	}
	click("校验并应用")
	if a.failed || a.dirty {
		t.Fatal("map application failed", a.message)
	}
	if err := a.service.Start("mountain"); err != nil {
		t.Fatal(err)
	}
	storyBefore := a.service.Current()
	click("地图行走试玩", "向西")
	if a.maps.preview == nil || a.maps.preview.Position() != spawn || !tt.HasText("已到达地图边缘") {
		t.Fatal("map edge did not block movement", a.message)
	}
	click("向东")
	if a.maps.preview.Position() != marker || !tt.HasText("剧情标记 · well") {
		t.Fatal("walking failed to reach the marker", a.message)
	}
	click("向北")
	if a.maps.preview.Position() != marker || !tt.HasText("墙壁挡住了去路") {
		t.Fatal("wall did not block movement", a.message)
	}
	if !reflect.DeepEqual(a.service.Current(), storyBefore) {
		t.Fatal("map movement altered the story session")
	}
	click("进入标记剧情")
	if a.tab != 3 || a.service.Current().Node.ID != "well" || !tt.HasText(a.service.Current().Node.Text) {
		t.Fatal("marker failed to open story preview", a.message)
	}

	click("地图", "返回地图编辑")
	replaceText("地图名称输入", " ")
	storyBefore = a.service.Current()
	click("校验并应用")
	if !a.failed || !a.dirty || !reflect.DeepEqual(a.service.Current(), storyBefore) {
		t.Fatal("failed map application altered the story session", a.message)
	}
	click("撤销草稿", "地图行走试玩", "完整 JSON")
	replaceText("完整内容 JSON", "{}")
	click("地图")
	if !a.jsonDirty || tt.HasText("向东") || tt.HasText("新增地图") {
		t.Fatal("pending JSON edits did not suspend map editing and movement")
	}
	click("撤销草稿")
	if a.maps.preview != nil || !reflect.DeepEqual(a.service.Current(), storyBefore) {
		t.Fatal("undo failed to reset walking preview while retaining the active story")
	}
	a.packPath = filepath.Join(t.TempDir(), "map.json")
	click("保存内容包", "删除地图", "撤销草稿", "打开")
	if a.failed || !reflect.DeepEqual(a.draft.Maps, []content.TileMap{m}) {
		t.Fatal("map was not restored after save, delete, undo and open", a.message)
	}
}

func TestNativeMapWidthAtMinimumWindow(t *testing.T) {
	a, err := newWorkbench(content.Demo())
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 960, 700)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	replaceWidth := func(text string) {
		t.Helper()
		tt.Scroll(115, 580, 0, 500)
		r, ok := tt.Find("地图列数")
		if !ok || r.W == 0 {
			t.Fatal("missing map width input")
		}
		tt.ClickAt(r.X+20, r.Y+r.H/2)
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type(text)
		click("调整尺寸")
		tt.Scroll(115, 580, 0, -500)
	}
	click("地图")
	click("新增地图")
	replaceWidth("32")
	m := &a.draft.Maps[0]
	if a.failed || len(m.Rows[0]) != content.MaxMapWidth {
		t.Fatal("map width was not adjusted to 32", a.message)
	}
	preview, ok := tt.Find("地图行走试玩")
	if !ok || preview.W == 0 {
		t.Fatal("map preview action is not visible at minimum window size")
	}
	click("格子 (31, 0)")
	if a.maps.cell != (content.MapPoint{X: 31}) {
		t.Fatal("rightmost grid cell is not clickable at minimum width")
	}
	click("设为起点")
	replaceWidth("31")
	if !a.failed || len(m.Rows[0]) != content.MaxMapWidth || m.Spawn.X != 31 {
		t.Fatal("shrinking a map would remove the spawn", a.message)
	}
}
