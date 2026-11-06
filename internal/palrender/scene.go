// Package palrender draws imported PAL scene snapshots using standard Go images.
// Coordinates and format semantics were researched from SDLPAL map.c/scene.c;
// this implementation does not embed game pixels or reference-engine code.
package palrender

import (
	"fmt"
	"image"
	"image/draw"
	"sort"

	"github.com/HiChen85/pal--reverse--learn/internal/palasset"
	"github.com/HiChen85/pal--reverse--learn/internal/palflow"
)

const Width, Height = 320, 200
const maxDecodedFrameBytes = 8 << 20

// Limitations describes presentation that is deliberately outside this static
// viewport: rendering does not advance effects, dialogue, sound or world time.
const Limitations = "静态场景预览：底图、上层、角色、事件及高度遮挡；不绘制对话框、淡入、色调或波纹效果，不推进时间。"

type Source struct {
	Maps     *palasset.MapCatalog
	Sprites  *palasset.SpriteCatalog
	Palettes *palasset.PaletteCatalog
}

type bitmap struct {
	img        *image.RGBA
	x, foot, z int
}

// Render returns a fresh 320x200 image. Missing, stale or corrupt resources
// return an error and no image, so callers cannot present a fake replacement.
func Render(source Source, world *palflow.WorldState) (*image.RGBA, error) {
	if world == nil || world.SceneResources == nil || world.SceneLoadPending || world.SceneResources.SceneID != world.Scene {
		return nil, fmt.Errorf("viewport requires the current loaded scene resources")
	}
	if source.Maps == nil || source.Palettes == nil {
		return nil, fmt.Errorf("viewport requires local MAP.MKF, GOP.MKF and PAT.MKF catalogs")
	}
	if world.InBattle {
		return nil, fmt.Errorf("overworld viewport cannot render an active battle")
	}
	for _, n := range []int{world.ViewportX, world.ViewportY, world.PartyOffsetX, world.PartyOffsetY} {
		if n < -32768 || n > 32767 {
			return nil, fmt.Errorf("viewport coordinate is outside signed 16-bit range")
		}
	}
	resources := world.SceneResources
	if _, ok := world.Scenes[world.Scene]; !ok {
		return nil, fmt.Errorf("viewport scene is absent")
	}
	m, err := source.Maps.Map(resources.MapID)
	if err != nil {
		return nil, err
	}
	info := m.Info()
	if resources.MAPArchiveSHA256 != source.Maps.MAPSourceSHA256() || resources.GOPArchiveSHA256 != source.Maps.GOPSourceSHA256() || resources.MapSHA256 != info.SHA256 || resources.GOPSHA256 != info.GOPSHA256 {
		return nil, fmt.Errorf("viewport map resource identity differs from loaded catalogs")
	}
	palette, err := source.Palettes.Palette(world.Palette, world.NightPalette)
	if err != nil {
		return nil, err
	}
	cache := map[string]*image.RGBA{}
	decodedBytes := 0
	decode := func(key string, read func() ([]byte, error)) (*image.RGBA, error) {
		if img := cache[key]; img != nil {
			return img, nil
		}
		data, err := read()
		if err != nil {
			return nil, err
		}
		indexed, err := palasset.DecodeRLE(data, Width*Height)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		cost := indexed.Width * indexed.Height * 4
		if cost > maxDecodedFrameBytes-decodedBytes {
			return nil, fmt.Errorf("viewport exceeds decoded frame cache bound %d", maxDecodedFrameBytes)
		}
		img, err := indexed.RGBA(palette)
		if err != nil {
			return nil, err
		}
		cache[key], decodedBytes = img, decodedBytes+cost
		return img, nil
	}
	tile := func(x, y, h, layer int) (*image.RGBA, error) {
		// Upper-layer zero is the documented absent bitmap, not a decode error.
		if layer == 1 && m.Tile(x, y, h)>>16&0x10ff == 0 {
			return nil, nil
		}
		frame, ok := m.TileFrame(x, y, h, layer)
		if !ok {
			return nil, fmt.Errorf("viewport GOP tile (%d,%d,%d) layer %d has no safe frame", x, y, h, layer)
		}
		return decode(fmt.Sprintf("GOP/%d", frame.Index), func() ([]byte, error) { return m.Frame(frame.Index) })
	}
	out := image.NewRGBA(image.Rect(0, 0, Width, Height))
	draw.Draw(out, out.Bounds(), image.NewUniform(palette[0]), image.Point{}, draw.Src)
	blit := func(img *image.RGBA, x, y int) {
		if img != nil {
			draw.Draw(out, img.Bounds().Add(image.Pt(x, y)), img, image.Point{}, draw.Over)
		}
	}
	// Each map row has two staggered half rows. Out-of-map bottom tiles use
	// the format's first bitmap; upper tiles outside the map are absent.
	for layer := 0; layer < 2; layer++ {
		for y := world.ViewportY/16 - 1; y < (world.ViewportY+Height)/16+2; y++ {
			for h := 0; h < 2; h++ {
				for x := world.ViewportX/32 - 1; x < (world.ViewportX+Width)/32+2; x++ {
					tx, ty, th := x, y, h
					if x < 0 || x >= palasset.MapColumns || y < 0 || y >= palasset.MapRows {
						if layer == 1 {
							continue
						}
						tx, ty, th = 0, 0, 0
					}
					img, err := tile(tx, ty, th, layer)
					if err != nil {
						return nil, err
					}
					blit(img, x*32+h*16-16-world.ViewportX, y*16+h*8-8-world.ViewportY)
				}
			}
		}
	}
	items := []bitmap{}
	sprite := func(resource palflow.SpriteResource, frame int) (*image.RGBA, error) {
		if source.Sprites == nil || resource.ArchiveSHA256 != source.Sprites.SourceSHA256() {
			return nil, fmt.Errorf("viewport sprite %d requires its loaded MGO catalog", resource.ID)
		}
		info, err := source.Sprites.Sprite(resource.ID)
		if err != nil {
			return nil, err
		}
		if resource.SHA256 != info.SHA256 || resource.FrameCount != info.FrameCount {
			return nil, fmt.Errorf("viewport sprite %d identity differs from loaded resource", resource.ID)
		}
		return decode(fmt.Sprintf("MGO/%d/%d", resource.ID, frame), func() ([]byte, error) { return source.Sprites.Frame(resource.ID, frame) })
	}
	count := len(world.PartyRoles) + len(world.FollowerSprites)
	if count < 1 || count > 5 || len(world.PlayerResources) != count || len(world.PartyFrames) < count || len(world.PartyPositions) < count {
		return nil, fmt.Errorf("viewport requires loaded party/follower resources, frames and screen positions")
	}
	for i := 0; i < count; i++ {
		img, err := sprite(world.PlayerResources[i], int(world.PartyFrames[i]))
		if err != nil {
			return nil, fmt.Errorf("party slot %d: %w", i, err)
		}
		x, y := int(int16(world.PartyPositions[i].X)), int(int16(world.PartyPositions[i].Y))
		items = append(items, bitmap{img, x - img.Bounds().Dx()/2, y + int(world.Layer) + 10, int(world.Layer) + 6})
	}
	ids, err := palflow.SceneEventIDs(world, "viewport")
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		e := world.Events[id]
		if e.Sprite == 0 || e.State <= 0 || e.VanishTime > 0 {
			continue
		}
		resource, ok := resources.EventSprites[id]
		if !ok || resource.ID != e.Sprite {
			return nil, fmt.Errorf("viewport event %d has no matching loaded sprite", id)
		}
		frame := int(e.Frame)
		if e.SpriteFrames == 3 {
			if frame == 2 {
				frame = 0
			} else if frame == 3 {
				frame = 2
			}
		}
		frame += int(e.Direction) * int(e.SpriteFrames)
		img, err := sprite(resource, frame)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", id, err)
		}
		x, y := int(int16(e.X))-world.ViewportX, int(int16(e.Y))-world.ViewportY
		items = append(items, bitmap{img, x - img.Bounds().Dx()/2, y + int(e.Layer)*8 + 9, int(e.Layer)*8 + 2})
	}
	// Height-bearing tiles may cover an actor. Build covering candidates from
	// its footprint and bitmap rectangle, then sort them together by foot Y.
	actors := append([]bitmap(nil), items...)
	for _, actor := range actors {
		sx := world.ViewportX + actor.x - actor.z/2
		sy := world.ViewportY + actor.foot - actor.z
		half := 0
		if sx%32 != 0 {
			half = 1
		}
		for y := (sy - actor.img.Bounds().Dy() - 15) / 16; y <= sy/16; y++ {
			for x := (sx - actor.img.Bounds().Dx()/2) / 32; x <= (sx+actor.img.Bounds().Dx()/2)/32; x++ {
				candidates := [][3]int{{x + 1, y, half}, {x + half, y + half, 1 - half}}
				if x == (sx-actor.img.Bounds().Dx()/2)/32 {
					candidates = append([][3]int{{x, y, half}, {x - 1, y, half}, {x - 1 + half, y + half, 1 - half}}, candidates...)
				}
				for _, c := range candidates {
					tx, ty, h := c[0], c[1], c[2]
					if tx < 0 || tx >= palasset.MapColumns || ty < 0 || ty >= palasset.MapRows {
						continue
					}
					for layer := 0; layer < 2; layer++ {
						height := int(m.TileHeight(tx, ty, h, layer))
						if height == 0 || (ty+height)*16+h*8 < sy {
							continue
						}
						img, err := tile(tx, ty, h, layer)
						if err != nil {
							return nil, err
						}
						if img != nil {
							items = append(items, bitmap{img, tx*32 + h*16 - 16 - world.ViewportX, ty*16 + h*8 + 7 + layer + height*8 - world.ViewportY, height*8 + layer})
						}
					}
				}
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].foot < items[j].foot })
	for _, item := range items {
		blit(item.img, item.x, item.foot-item.img.Bounds().Dy()-item.z)
	}
	return out, nil
}
