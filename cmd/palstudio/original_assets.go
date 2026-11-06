package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/paldata"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

const originalSpriteDecodeLimit = 8 << 20

// loadOriginalAssets selects only MGO.MKF, case-insensitively and without aliases
// or ambiguous duplicate names. OpenMGO owns the bounded immutable byte snapshot.
func loadOriginalAssets(dir string) (*palasset.SpriteCatalog, *paldata.FileInfo, error) {
	name, err := originalResourceName(dir, "MGO.MKF")
	if err != nil {
		return nil, nil, err
	}
	catalog, err := palasset.OpenMGO(filepath.Join(dir, name), originalSpriteDecodeLimit)
	if err != nil {
		return nil, nil, err
	}
	identity := &paldata.FileInfo{Name: name, SHA256: catalog.SourceSHA256(), Size: catalog.SourceSize()}
	return catalog, identity, nil
}

func originalResourceName(dir, wanted string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	name := ""
	for _, entry := range entries {
		if !strings.EqualFold(entry.Name(), wanted) {
			continue
		}
		if name != "" {
			return "", fmt.Errorf("%s 名称不唯一：%q 和 %q", wanted, name, entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%s 必须是普通文件，不接受符号链接或目录", wanted)
		}
		name = entry.Name()
	}
	if name == "" {
		return "", fmt.Errorf("未找到 %s；需要此资源的操作将停止", wanted)
	}
	return name, nil
}

func loadOriginalMaps(dir string) (*palasset.MapCatalog, []paldata.FileInfo, error) {
	mapName, err := originalResourceName(dir, "MAP.MKF")
	if err != nil {
		return nil, nil, err
	}
	gopName, err := originalResourceName(dir, "GOP.MKF")
	if err != nil {
		return nil, nil, err
	}
	catalog, err := palasset.OpenMaps(filepath.Join(dir, mapName), filepath.Join(dir, gopName))
	if err != nil {
		return nil, nil, err
	}
	return catalog, []paldata.FileInfo{{Name: mapName, SHA256: catalog.MAPSourceSHA256(), Size: catalog.MAPSourceSize()}, {Name: gopName, SHA256: catalog.GOPSourceSHA256(), Size: catalog.GOPSourceSize()}}, nil
}

func loadOriginalPalette(dir string) (*palasset.PaletteCatalog, *paldata.FileInfo, error) {
	name, err := originalResourceName(dir, "PAT.MKF")
	if err != nil {
		return nil, nil, err
	}
	catalog, err := palasset.OpenPAT(filepath.Join(dir, name))
	if err != nil {
		return nil, nil, err
	}
	return catalog, &paldata.FileInfo{Name: name, SHA256: catalog.SourceSHA256(), Size: catalog.SourceSize()}, nil
}

func originalDiagnosticOptions() palflow.Options {
	return palflow.Options{MaxSteps: 1000, MaxCallDepth: 32, MaxTextBytes: 256 << 10}
}

// These are the default session limits. The work budget can be changed before
// reset; evidence always records the actual session invocation's options.
func originalSessionOptions() palflow.Options {
	return palflow.Options{MaxSteps: 10000, MaxCallDepth: 64, MaxTextBytes: 1 << 20}
}

type originalSceneDisplay struct {
	Number   int
	Original paldata.Scene
	Current  palflow.WorldScene
}

func (o *originalData) sceneDisplay() (originalSceneDisplay, error) {
	index, err := o.activeSceneIndex()
	if err != nil {
		return originalSceneDisplay{}, err
	}
	if index < 0 || index >= len(o.game.Scenes) {
		return originalSceneDisplay{}, errors.New("活动场景超出导入表")
	}
	original := o.game.Scenes[index]
	current := palflow.WorldScene{Map: original.Map, OnEnter: o.enterPC[index], OnTeleport: original.OnTeleport, EventStart: original.EventStart}
	if o.useSession {
		var ok bool
		current, ok = o.session.Snapshot().World.Scenes[uint16(index+1)]
		if !ok {
			return originalSceneDisplay{}, errors.New("逻辑场景记录缺失")
		}
	}
	return originalSceneDisplay{Number: index + 1, Original: original, Current: current}, nil
}
func (d originalSceneDisplay) text() string {
	return fmt.Sprintf("活动场景 %d · 地图 原始 %d / 当前 %d · OnEnter 原始 %04x / 当前 %04x · OnTeleport 原始 %04x / 当前 %04x", d.Number, d.Original.Map, d.Current.Map, d.Original.OnEnter, d.Current.OnEnter, d.Original.OnTeleport, d.Current.OnTeleport)
}

type originalEventDisplay struct {
	ID       int
	Original paldata.Event
	Current  palflow.WorldEvent
}

func (o *originalData) eventDisplay(id int) (originalEventDisplay, error) {
	if o.game == nil || id < 1 || id > len(o.game.Events) {
		return originalEventDisplay{}, errors.New("事件 ID 超出导入表")
	}
	original := o.game.Events[id-1]
	current := palflow.WorldEvent{Trigger: o.triggerPC[id-1], AutoScript: original.AutoScript, State: original.State, X: original.X, Y: original.Y}
	if o.useSession {
		var ok bool
		current, ok = o.session.Snapshot().World.Events[uint16(id)]
		if !ok {
			return originalEventDisplay{}, errors.New("逻辑事件记录缺失")
		}
	}
	return originalEventDisplay{ID: id, Original: original, Current: current}, nil
}
func (d originalEventDisplay) text() string {
	return fmt.Sprintf("事件 %d · 坐标 原始 (%d,%d) / 当前 (%d,%d) · 状态 原始 %d / 当前 %d · Trigger 原始 %04x / 当前 %04x · Auto 原始 %04x / 当前 %04x（未执行）", d.ID, d.Original.X, d.Original.Y, d.Current.X, d.Current.Y, d.Original.State, d.Current.State, d.Original.Trigger, d.Current.Trigger, d.Original.AutoScript, d.Current.AutoScript)
}
