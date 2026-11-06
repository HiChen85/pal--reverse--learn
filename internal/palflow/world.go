package palflow

import (
	"fmt"
	"maps"
	"slices"

	"github.com/HiChen85/pal--reverse--learn/internal/palcombat"
)

// WorldState is an explicit snapshot for state-only PAL instructions. PartyX and
// PartyY cache the leading member's map pixels; event-relative writes use the
// viewport and party offset, as PAL does, rather than trusting that cache.
// This snapshot does not advance time, load scene resources, or run auto scripts.
type WorldState struct {
	RandomMode         string                `json:"random_mode,omitempty"`
	AutoBattle         bool                  `json:"auto_battle"`
	Battle             *palcombat.Snapshot   `json:"battle,omitempty"`
	BattleMode         string                `json:"battle_mode,omitempty"`
	BattleRandomState  uint64                `json:"battle_random_state"`
	BattleResult       uint16                `json:"battle_result"`
	BattleResultSet    bool                  `json:"battle_result_set"`
	BattleHidingTime   int                   `json:"battle_hiding_time"`
	BattleEnemyMoving  bool                  `json:"battle_enemy_moving"`
	BattleBoss         bool                  `json:"battle_boss"`
	BattleMovingPlayer uint16                `json:"battle_moving_player"`
	BattleEnemyTarget  uint16                `json:"battle_enemy_target"`
	BattleBlow         int16                 `json:"battle_blow"`
	CollectValue       uint16                `json:"collect_value"`
	SceneResources     *SceneResourceState   `json:"scene_resources,omitempty"`
	Experience         [6][8]Experience      `json:"experience"`
	LastUnequippedItem uint16                `json:"last_unequipped_item"`
	MagicBaseDamage    map[uint16]uint16     `json:"magic_base_damage"`
	Palette            uint16                `json:"palette"`
	NightPalette       bool                  `json:"night_palette"`
	ScreenWave         uint16                `json:"screen_wave"`
	WaveProgression    int16                 `json:"wave_progression"`
	NeedToFadeIn       bool                  `json:"need_to_fade_in"`
	ScreenTint         string                `json:"screen_tint,omitempty"`
	Music              uint16                `json:"music"`
	MusicKnown         bool                  `json:"music_known"`
	BattleMusic        uint16                `json:"battle_music"`
	BattleMusicKnown   bool                  `json:"battle_music_known"`
	PartyStepFrame     uint16                `json:"party_step_frame"`
	AutoMessageMode    string                `json:"auto_message_mode,omitempty"`
	InBattle           bool                  `json:"in_battle"`
	SceneLoadPending   bool                  `json:"scene_load_pending"`
	FollowerSprites    []uint16              `json:"follower_sprites"`
	PlayerResources    []SpriteResource      `json:"player_resources"`
	FrameNum           uint32                `json:"frame_num"`
	ChaseCycles        uint16                `json:"chase_cycles"`
	ChaseRange         uint16                `json:"chase_range"`
	Roles              map[uint16][75]uint16 `json:"roles"`
	Objects            map[uint16][7]uint16  `json:"objects"`
	EquipmentEffects   [7][75][6]uint16      `json:"equipment_effects"`
	PlayerStatus       [6][9]uint16          `json:"player_status"`
	PlayerPoisons      [16][5]WorldPoison    `json:"player_poisons"`
	PlayerStatusMode   string                `json:"player_status_mode,omitempty"`
	Scene              uint16                `json:"scene"`
	SceneChanged       bool                  `json:"scene_changed"`
	Layer              int16                 `json:"layer"`
	PartyRoles         []uint16              `json:"party_roles"`
	PartyX             int                   `json:"party_x"`
	PartyY             int                   `json:"party_y"`
	PartyOffsetX       int                   `json:"party_offset_x"`
	PartyOffsetY       int                   `json:"party_offset_y"`
	ViewportX          int                   `json:"viewport_x"`
	ViewportY          int                   `json:"viewport_y"`
	PartyPositions     []WorldPosition       `json:"party_positions"`
	Trail              []WorldTrail          `json:"trail"`
	Direction          uint16                `json:"direction"`
	PartyFrames        []uint16              `json:"party_frames"`
	Cash               uint32                `json:"cash"`
	Battlefield        uint16                `json:"battlefield"`
	Events             map[uint16]WorldEvent `json:"events"`
	Scenes             map[uint16]WorldScene `json:"scenes"`
	Inventory          map[uint16]int        `json:"inventory"`
	InventorySlots     []InventorySlot       `json:"inventory_slots"`
	PlayerSprites      map[uint16]uint16     `json:"player_sprites"`
}

type WorldPoison struct {
	ID     uint16 `json:"id"`
	Script uint16 `json:"script"`
}

type Experience struct{ Exp, Reserved, Level, Count uint16 }
type WorldEvent struct {
	Sprite           uint16 `json:"sprite"`
	SpriteFrames     uint16 `json:"sprite_frames"`
	SpriteFramesAuto uint16 `json:"sprite_frames_auto"`
	AutoIdleFrame    uint16 `json:"auto_idle_frame"`
	Trigger          uint16 `json:"trigger"`
	AutoScript       uint16 `json:"auto_script"`
	State            int16  `json:"state"`
	VanishTime       int16  `json:"vanish_time"`
	Layer            int16  `json:"layer"`
	X                uint16 `json:"x"`
	Y                uint16 `json:"y"`
	Direction        uint16 `json:"direction"`
	Frame            uint16 `json:"frame"`
	TriggerMode      uint16 `json:"trigger_mode"`
}

type WorldScene struct {
	Map        uint16 `json:"map"`
	OnEnter    uint16 `json:"on_enter"`
	OnTeleport uint16 `json:"on_teleport"`
	EventStart uint16 `json:"event_start"`
}

type WorldPosition struct {
	// Party positions retain the raw bits of PAL's signed 16-bit view coordinates.
	X uint16 `json:"x"`
	Y uint16 `json:"y"`
}

type WorldTrail struct {
	X         uint16 `json:"x"`
	Y         uint16 `json:"y"`
	Direction uint16 `json:"direction"`
}

// CloneWorld preserves nil while detaching every mutable collection.
func CloneWorld(world *WorldState) *WorldState {
	if world == nil {
		return nil
	}
	cloned := *world
	if world.Battle != nil {
		battle := palcombat.CloneSnapshot(*world.Battle)
		cloned.Battle = &battle
	}
	if world.SceneResources != nil {
		resource := *world.SceneResources
		resource.EventSprites = maps.Clone(resource.EventSprites)
		cloned.SceneResources = &resource
	}
	cloned.PartyRoles = slices.Clone(world.PartyRoles)
	cloned.FollowerSprites = slices.Clone(world.FollowerSprites)
	cloned.PlayerResources = slices.Clone(world.PlayerResources)
	cloned.PartyFrames = slices.Clone(world.PartyFrames)
	cloned.PartyPositions = slices.Clone(world.PartyPositions)
	cloned.Trail = slices.Clone(world.Trail)
	cloned.InventorySlots = slices.Clone(world.InventorySlots)
	cloned.Events = maps.Clone(world.Events)
	cloned.Scenes = maps.Clone(world.Scenes)
	cloned.Inventory = maps.Clone(world.Inventory)
	cloned.PlayerSprites = maps.Clone(world.PlayerSprites)
	cloned.Roles = maps.Clone(world.Roles)
	cloned.Objects = maps.Clone(world.Objects)
	cloned.MagicBaseDamage = maps.Clone(world.MagicBaseDamage)
	return &cloned
}

// worldOpcodeName lists only transitions implemented below. These transitions
// were checked against SDLPal script.c at d4bf69b64d335cd680555af9df90e7ec4f3006a3.
func worldOpcodeName(op uint16) string {
	switch op {
	case 0x0012:
		return "set event position relative to party"
	case 0x0013:
		return "set event position"
	case 0x0014:
		return "set invoking event gesture"
	case 0x0015:
		return "set party direction and member gesture"
	case 0x0016:
		return "set event direction and gesture"
	case 0x0024:
		return "set event auto script"
	case 0x0025:
		return "set event trigger script"
	case 0x0040:
		return "set event trigger mode"
	case 0x0046:
		return "set party map position and formation"
	case 0x0049:
		return "set event state"
	case 0x004a:
		return "set battlefield"
	case 0x004b:
		return "temporarily vanish invoking event"
	case 0x0059:
		return "request scene change"
	case 0x0065:
		return "set player sprite; reload requires a local MGO source"
	case 0x006c:
		return "move event by signed offset and advance its gesture"
	case 0x006d:
		return "set scene enter and teleport scripts"
	}
	return ""
}

// evaluateWorld commits a complete detached world snapshot and advances the
// current frame only after every operand and context check has succeeded.
func evaluateWorld(state *State, step *TraceStep, inst Instruction) (bool, error) {
	if worldOpcodeName(inst.Opcode) == "" {
		return false, nil
	}
	if state == nil || len(state.Frames) == 0 {
		return true, fmt.Errorf("world instruction requires a current script frame")
	}
	if step == nil {
		return true, fmt.Errorf("world instruction requires a trace step")
	}
	if state.World == nil {
		return true, fmt.Errorf("world instruction requires an explicit world snapshot")
	}
	frameIndex := len(state.Frames) - 1
	frame := state.Frames[frameIndex]
	nextPC, err := fallthroughPC(frame.PC)
	if err != nil {
		return true, err
	}
	world := CloneWorld(state.World)
	a := inst.Operands
	effect := worldOpcodeName(inst.Opcode)
	lookupEvent := func(id uint16) (uint16, WorldEvent, error) {
		if id == 0 || id == 0xffff {
			id = frame.EventID
		}
		if id == 0 || id == 0xffff {
			return 0, WorldEvent{}, fmt.Errorf("event instruction requires a concrete invoking event context")
		}
		// The Dream 2.11 event-ID remapping is deliberately not modeled.
		if id >= 0x9000 {
			return 0, WorldEvent{}, fmt.Errorf("event ID %04x requires unsupported special event addressing", id)
		}
		event, ok := world.Events[id]
		if !ok {
			return 0, WorldEvent{}, fmt.Errorf("event %d is absent from the world snapshot", id)
		}
		return id, event, nil
	}
	lookupScene := func(id uint16) (WorldScene, error) {
		if id == 0 || id > 300 {
			return WorldScene{}, fmt.Errorf("scene ID %d is outside 1..300", id)
		}
		scene, ok := world.Scenes[id]
		if !ok {
			return WorldScene{}, fmt.Errorf("scene %d is absent from the world snapshot", id)
		}
		return scene, nil
	}
	switch inst.Opcode {
	case 0x0012, 0x0013:
		id, event, err := lookupEvent(a[0])
		if err != nil {
			return true, err
		}
		event.X, event.Y = a[1], a[2]
		if inst.Opcode == 0x0012 {
			event.X += uint16(int16(world.ViewportX)) + uint16(int16(world.PartyOffsetX))
			event.Y += uint16(int16(world.ViewportY)) + uint16(int16(world.PartyOffsetY))
		}
		world.Events[id] = event
	case 0x0014, 0x004b:
		id, event, err := lookupEvent(frame.EventID)
		if err != nil {
			return true, err
		}
		if inst.Opcode == 0x0014 {
			event.Frame, event.Direction = a[0], 0 // PAL's south direction.
		} else {
			event.VanishTime = -15
		}
		world.Events[id] = event
	case 0x006c:
		id, event, err := lookupEvent(a[0])
		if err != nil {
			return true, err
		}
		event.X += a[1] // signed addition and assignment have the same uint16 bits.
		event.Y += a[2]
		frames := event.SpriteFrames
		if frames == 3 {
			frames = 4
		}
		if frames == 0 {
			frames = event.SpriteFramesAuto
		}
		if frames != 0 {
			event.Frame++
			event.Frame %= frames
		}
		world.Events[id] = event
	case 0x0015:
		member := int(a[2])
		if member >= 5 || member >= len(world.PartyFrames) {
			return true, fmt.Errorf("party frame index %d is absent or outside 0..4", member)
		}
		world.Direction = a[0]
		world.PartyFrames[member] = a[0]*3 + a[1]
	case 0x0016, 0x0024, 0x0025, 0x0040, 0x0049:
		if a[0] == 0 {
			effect += "; operand 0 zero leaves the event unchanged"
			break
		}
		id, event, err := lookupEvent(a[0])
		if err != nil {
			return true, err
		}
		switch inst.Opcode {
		case 0x0016:
			event.Direction, event.Frame = a[1], a[2]
		case 0x0024:
			event.AutoScript = a[1]
		case 0x0025:
			event.Trigger = a[1]
		case 0x0040:
			event.TriggerMode = a[1]
		case 0x0049:
			event.State = int16(a[1])
		}
		world.Events[id] = event
	case 0x0046:
		if world.PartyOffsetX < -32768 || world.PartyOffsetX > 32767 || world.PartyOffsetY < -32768 || world.PartyOffsetY > 32767 {
			return true, fmt.Errorf("party offsets must be signed 16-bit coordinates")
		}
		x := a[0]*32 + a[2]*16
		y := a[1]*16 + a[2]*8
		world.ViewportX = int(int16(x - uint16(world.PartyOffsetX)))
		world.ViewportY = int(int16(y - uint16(world.PartyOffsetY)))
		xOffset, yOffset := -16, -8
		if world.Direction == 0 || world.Direction == 1 {
			xOffset = 16
		}
		if world.Direction == 1 || world.Direction == 2 {
			yOffset = 8
		}
		world.PartyPositions = make([]WorldPosition, 5)
		world.Trail = make([]WorldTrail, 5)
		for member := range world.PartyPositions {
			px := uint16(world.PartyOffsetX + member*xOffset)
			py := uint16(world.PartyOffsetY + member*yOffset)
			world.PartyPositions[member] = WorldPosition{X: px, Y: py}
			world.Trail[member] = WorldTrail{X: px + uint16(world.ViewportX), Y: py + uint16(world.ViewportY), Direction: world.Direction}
		}
		world.PartyX, world.PartyY = int(int16(x)), int(int16(y))
	case 0x004a:
		world.Battlefield = a[0]
	case 0x0059:
		if a[0] == 0 {
			effect += "; scene zero leaves the scene unchanged"
			break
		}
		if _, err := lookupScene(a[0]); err != nil {
			return true, err
		}
		if world.Scene != a[0] {
			world.Scene, world.SceneChanged, world.Layer = a[0], true, 0
			world.SceneLoadPending = true
		}
	case 0x0065:
		if a[2] != 0 {
			return true, fmt.Errorf("player sprite resource loading requested by operand 2 is not modeled")
		}
		if a[0] >= 6 {
			return true, fmt.Errorf("player role index %d is outside 0..5", a[0])
		}
		if _, ok := world.PlayerSprites[a[0]]; !ok {
			return true, fmt.Errorf("player role %d sprite is absent from the world snapshot", a[0])
		}
		if world.Roles != nil {
			role, ok := world.Roles[a[0]]
			if !ok {
				return true, fmt.Errorf("player role %d attributes are absent from the supplied role snapshot", a[0])
			}
			role[2] = a[1]
			world.Roles[a[0]] = role
		}
		world.PlayerSprites[a[0]] = a[1]
	case 0x006d:
		if a[0] == 0 {
			effect += "; scene zero leaves scene scripts unchanged"
			break
		}
		scene, err := lookupScene(a[0])
		if err != nil {
			return true, err
		}
		if a[1] != 0 || a[2] == 0 {
			scene.OnEnter = a[1]
		}
		if a[2] != 0 || a[1] == 0 {
			scene.OnTeleport = a[2]
		}
		world.Scenes[a[0]] = scene
	}
	frames := append([]Frame(nil), state.Frames...)
	frames[frameIndex].PC = nextPC
	state.World, state.Frames, step.Effect = world, frames, effect
	return true, nil
}
