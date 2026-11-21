package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/content"
	"github.com/HiChen85/pal--reverse--learn/internal/studio"
	"github.com/egoist/mygo/ui"
)

type mapEditorState struct {
	id, eventNode string
	cell          content.MapPoint
	width, height float64
	preview       *studio.MapPreview
}

func (a *workbench) resetMapSelection(id string) {
	a.maps = mapEditorState{eventNode: a.draft.Start}
	if len(a.draft.Maps) == 0 {
		return
	}
	m := &a.draft.Maps[0]
	for i := range a.draft.Maps {
		if a.draft.Maps[i].ID == id {
			m = &a.draft.Maps[i]
			break
		}
	}
	a.maps.id = m.ID
	a.maps.width, a.maps.height = float64(len(m.Rows[0])), float64(len(m.Rows))
	a.selectMapCell(m, m.Spawn)
}

func (a *workbench) selectMapCell(m *content.TileMap, point content.MapPoint) {
	a.maps.cell = point
	if event, ok := m.EventAt(point); ok {
		a.maps.eventNode = event.Node
	}
}

func (a *workbench) mapEditor(c *ui.Context) {
	if a.jsonDirty {
		ui.Text(c, "原创地图").Bold().FontSize(20)
		ui.Text(c, "请先在完整 JSON 中载入草稿，或撤销未应用修改。").TextColor(c.Theme().Warning)
		return
	}
	ids, names := make([]string, 0, len(a.draft.Maps)), make([]string, 0, len(a.draft.Maps))
	for _, m := range a.draft.Maps {
		ids = append(ids, m.ID)
		names = append(names, m.Name+" · "+m.ID)
	}
	ui.Row(c).Gap(10).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Text(c, "原创地图").Bold().FontSize(20)
		if len(ids) > 0 {
			selected := a.maps.id
			selectNamed(c.Key("map-selection"), &selected, ids, names).Label("地图选择").Grow(1)
			if selected != a.maps.id {
				a.resetMapSelection(selected)
			}
			if a.mapPreviewing() {
				if ui.Button(c, "返回地图编辑").Clicked() {
					a.maps.preview = nil
				}
			} else if ui.PrimaryButton(c, "地图行走试玩").Disabled(a.dirty).Clicked() {
				preview, err := a.service.NewMapPreview(a.maps.id)
				if err == nil {
					a.maps.preview = preview
				}
				a.report(err, "从地图起点开始试玩")
			}
		}
		if ui.Button(c, "新增地图").Disabled(len(ids) >= content.MaxMaps).Clicked() {
			id := uniqueID("map", ids)
			rows := make([]string, 6)
			for y := range rows {
				rows[y] = strings.Repeat(".", 8)
			}
			a.draft.Maps = append(a.draft.Maps, content.TileMap{ID: id, Name: "新地图", Rows: rows})
			a.resetMapSelection(id)
			a.dirty = true
		}
		if ui.Button(c, "删除地图").Disabled(len(ids) == 0).Clicked() {
			for i, m := range a.draft.Maps {
				if m.ID == a.maps.id {
					a.draft.Maps = slices.Delete(a.draft.Maps, i, i+1)
					a.resetMapSelection("")
					a.dirty = true
					break
				}
			}
		}
	})
	if len(a.draft.Maps) == 0 {
		ui.Text(c, "新增一张地图后，可布置地面、墙壁、起点和剧情标记。").TextColor(c.Theme().TextMuted)
		return
	}
	for i := range a.draft.Maps {
		m := &a.draft.Maps[i]
		if m.ID == a.maps.id {
			a.selectedMap(c.Key(m.ID), m)
			return
		}
	}
}

func (a *workbench) selectedMap(c *ui.Context, m *content.TileMap) {
	ui.Row(c).Gap(12).FillWidth().AlignItems(ui.Start).Children(func() {
		ui.Column(c).Width(248).Shrink(0).Gap(10).Padding(12).Radius(8).Background(c.Theme().Surface).Children(func() {
			if a.mapPreviewing() {
				a.mapWalkControls(c, m)
			} else {
				a.mapEditControls(c, m)
			}
		})
		a.mapGrid(c, m)
	})
}

func (a *workbench) mapPreviewing() bool {
	return a.maps.preview != nil && !a.dirty && !a.jsonDirty
}

func (a *workbench) mapEditControls(c *ui.Context, m *content.TileMap) {
	t := c.Theme()
	cell := a.maps.cell
	event, marked := m.EventAt(cell)
	floor := m.Walkable(cell)
	terrain := "墙壁"
	if floor {
		terrain = "地面"
	}
	ui.Text(c, fmt.Sprintf("选中 (%d, %d) · %s", cell.X, cell.Y, terrain)).Bold()
	ui.Row(c).Gap(6).Children(func() {
		if ui.Button(c, "地面").Grow(1).Disabled(floor).Clicked() {
			a.setMapTile(m, cell, '.')
		}
		if ui.Button(c, "墙壁").Grow(1).Disabled(!floor || cell == m.Spawn || marked).Clicked() {
			a.setMapTile(m, cell, '#')
		}
	})
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		if ui.Button(c, "设为起点").Grow(1).Disabled(!floor || cell == m.Spawn).Clicked() {
			m.Spawn = cell
			a.dirty = true
		}
		ui.Text(c, fmt.Sprintf("起点 (%d, %d)", m.Spawn.X, m.Spawn.Y)).FontSize(12).TextColor(t.TextMuted)
	})
	nodeIDs := make([]string, 0, len(a.draft.Nodes))
	for _, n := range a.draft.Nodes {
		nodeIDs = append(nodeIDs, n.ID)
	}
	a.maps.eventNode = validSelection(a.maps.eventNode, nodeIDs)
	ui.Select(c.Key("map-event-node"), &a.maps.eventNode, nodeIDs).Label("剧情节点标记选择").FillWidth()
	ui.Row(c).Gap(6).Children(func() {
		disabled := !floor || a.maps.eventNode == "" || marked && a.maps.eventNode == event.Node || !marked && len(m.Events) >= content.MaxMapEvents
		if ui.Button(c, "设置标记").Grow(1).Disabled(disabled).Clicked() {
			a.setMapEvent(m, cell, a.maps.eventNode)
		}
		if ui.Button(c, "移除标记").Grow(1).Disabled(!marked).Clicked() {
			a.setMapEvent(m, cell, "")
		}
	})
	if marked {
		ui.Text(c, "当前标记 · "+event.Node).TextColor(t.TextMuted)
	}
	ui.Text(c, "地图名称").TextColor(t.TextMuted)
	if ui.TextInput(c.Key("map-name"), &m.Name).Label("地图名称输入").FillWidth().Changed() {
		a.dirty = true
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "宽").Width(20)
		ui.NumberInput(c.Key("map-width"), &a.maps.width, 1, content.MaxMapWidth, 1).Label("地图列数").Width(172)
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "高").Width(20)
		ui.NumberInput(c.Key("map-height"), &a.maps.height, 1, content.MaxMapHeight, 1).Label("地图行数").Width(172)
	})
	width, height := int(a.maps.width), int(a.maps.height)
	if ui.Button(c, "调整尺寸").FillWidth().Disabled(width == len(m.Rows[0]) && height == len(m.Rows)).Clicked() {
		a.report(a.resizeMap(m, width, height), "已调整地图草稿尺寸")
	}
}

func (a *workbench) resizeMap(m *content.TileMap, width, height int) error {
	if width < 1 || width > content.MaxMapWidth || height < 1 || height > content.MaxMapHeight {
		return fmt.Errorf("地图尺寸必须为 1–%d 列、1–%d 行", content.MaxMapWidth, content.MaxMapHeight)
	}
	if m.Spawn.X >= width || m.Spawn.Y >= height {
		return errors.New("新尺寸会裁掉起点，请先移动起点")
	}
	for _, event := range m.Events {
		if event.Position.X >= width || event.Position.Y >= height {
			return errors.New("新尺寸会裁掉剧情标记，请先移除标记")
		}
	}
	rows := make([]string, height)
	for y := range rows {
		var keep string
		if y < len(m.Rows) {
			keep = m.Rows[y][:min(width, len(m.Rows[y]))]
		}
		rows[y] = keep + strings.Repeat(".", width-len(keep))
	}
	m.Rows = rows
	if a.maps.cell.X >= width || a.maps.cell.Y >= height {
		a.selectMapCell(m, m.Spawn)
	}
	a.dirty = true
	return nil
}

func (a *workbench) setMapTile(m *content.TileMap, point content.MapPoint, tile byte) {
	row := m.Rows[point.Y]
	m.Rows[point.Y] = row[:point.X] + string(tile) + row[point.X+1:]
	a.dirty = true
}

func (a *workbench) setMapEvent(m *content.TileMap, point content.MapPoint, node string) {
	for i, event := range m.Events {
		if event.Position == point {
			if node == "" {
				m.Events = slices.Delete(m.Events, i, i+1)
			} else {
				m.Events[i].Node = node
			}
			a.dirty = true
			return
		}
	}
	m.Events = append(m.Events, content.MapEvent{Position: point, Node: node})
	a.dirty = true
}

func (a *workbench) mapWalkControls(c *ui.Context, m *content.TileMap) {
	p := a.maps.preview
	ui.Text(c, "行走试玩").Bold()
	pos := p.Position()
	ui.Text(c, fmt.Sprintf("当前位置 (%d, %d)", pos.X, pos.Y)).TextColor(c.Theme().TextMuted)
	move := func(label string, dx, dy int) {
		if ui.Button(c, label).Width(68).Clicked() {
			err := p.Move(dx, dy)
			if err != nil {
				x, y := pos.X+dx, pos.Y+dy
				if x < 0 || x >= len(m.Rows[0]) || y < 0 || y >= len(m.Rows) {
					err = errors.New("已到达地图边缘")
				} else {
					err = errors.New("墙壁挡住了去路")
				}
			}
			a.report(err, "已移动")
		}
	}
	ui.Row(c).Center().FillWidth().Children(func() { move("向北", 0, -1) })
	ui.Row(c).Gap(6).Center().FillWidth().Children(func() {
		move("向西", -1, 0)
		move("向南", 0, 1)
		move("向东", 1, 0)
	})
	if node, ok := p.EventNode(); ok {
		ui.Text(c, "剧情标记 · "+node).TextColor(c.Theme().TextMuted)
		ui.Text(c, "从该节点开始新的剧情试玩。").FontSize(12).TextColor(c.Theme().TextMuted)
		if ui.PrimaryButton(c, "进入标记剧情").FillWidth().Clicked() {
			err := a.service.Start(node)
			if err == nil {
				a.previewID = node
				a.tab = 3
			}
			a.report(err, "已从地图标记开始剧情试玩")
		}
	}
}

func (a *workbench) mapGrid(c *ui.Context, m *content.TileMap) {
	floor, wall := ui.Hex("#536c6a"), ui.Hex("#25364b")
	spawn, marker, player := ui.Hex("#477456"), ui.Hex("#a26c34"), ui.Hex("#2a738c")
	previewing := a.mapPreviewing()
	var pos content.MapPoint
	if previewing {
		pos = a.maps.preview.Position()
	}
	const cellGap = 2
	// The map stays within the 960-DIP minimum window even at maximum width.
	size := float32(min(36, 600/len(m.Rows[0])-cellGap))
	ui.Column(c).Gap(8).Grow(1).Children(func() {
		ui.Row(c).Gap(12).Children(func() {
			for _, item := range []struct {
				color ui.Color
				label string
			}{{floor, "地面"}, {wall, "墙壁"}, {spawn, "起点"}, {marker, "剧情标记"}, {player, "当前位置"}} {
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(11, 11).Radius(2).Background(item.color)
					ui.Text(c, item.label).FontSize(12).TextColor(c.Theme().TextMuted)
				})
			}
		})
		ui.Column(c).AlignSelf(ui.Start).Gap(cellGap).Padding(8).Radius(8).Background(ui.Hex("#13212c")).Children(func() {
			for y, row := range m.Rows {
				ui.Row(c).Gap(cellGap).Children(func() {
					for x := range row {
						point := content.MapPoint{X: x, Y: y}
						color, symbol := floor, ""
						if row[x] == '#' {
							color = wall
						}
						if point == m.Spawn {
							color, symbol = spawn, "起"
						}
						if _, ok := m.EventAt(point); ok {
							color, symbol = marker, "事"
						}
						if previewing && point == pos {
							color, symbol = player, "行"
						}
						var tile ui.Element
						if previewing {
							tile = ui.Box(c)
						} else {
							tile = ui.ButtonBase(c.Key(fmt.Sprintf("tile-%d-%d", x, y)))
							tile.Label(fmt.Sprintf("格子 (%d, %d)", x, y))
							if point == a.maps.cell {
								tile.Border(1, ui.Hex("#f3e4ad"))
							}
							if tile.Clicked() {
								a.selectMapCell(m, point)
							}
						}
						tile.Size(size, size).Shrink(0).Center().Radius(2).Background(color).Children(func() {
							if symbol != "" {
								ui.Text(c, symbol).FontSize(11).TextColor(ui.Hex("#ffffff"))
							}
						})
					}
				})
			}
		})
	})
}
