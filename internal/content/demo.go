package content

// Demo returns a fresh original sample chapter. It contains no commercial game
// assets, original PAL dialogue, or assumptions about the canonical ending.
func Demo() Pack {
	return Pack{
		SchemaVersion: SchemaVersion, ID: "after-rain", Title: "雨后余音 · 原创续篇示例", Start: "arrival",
		Characters: []Character{
			{ID: "lin-yue", Name: "林越", MaxHP: 160, MaxMP: 40, Attack: 27, Defense: 10, Speed: 14},
			{ID: "su-qing", Name: "苏青", MaxHP: 130, MaxMP: 60, Attack: 23, Defense: 8, Speed: 18},
		},
		Items: []Item{
			{ID: "herb", Name: "青露草", Description: "山民晒制的药草，回复四十五点生命。", HealHP: 45},
			{ID: "spring", Name: "听泉露", Description: "清泉凝成的小瓶，回复二十点灵力。", RestoreMP: 20},
		},
		Encounters: []Encounter{{ID: "mist-guardian", Enemies: []Character{{ID: "mist", Name: "雾中守影", MaxHP: 100, MaxMP: 0, Attack: 16, Defense: 6, Speed: 9}}}},
		Nodes: []Node{
			{ID: "arrival", Speaker: "lin-yue", Text: "旧日的风波已平。山路尽头，却又响起一串无人拨动的铃声。", Next: "crossroads"},
			{ID: "crossroads", Speaker: "su-qing", Text: "村口的古井结了薄霜。先去问问守井人，还是循着铃声进山？", Choices: []Choice{{Text: "拜访守井人", Next: "well"}, {Text: "循铃入山", Next: "mountain"}}},
			{ID: "well", Speaker: "su-qing", Text: "老人说，铃声每逢雨后便响。他把一枚刻着泉纹的木牌交给我们。", SetFlags: map[string]bool{"knows-spring": true}, Next: "mountain"},
			{ID: "mountain", Speaker: "lin-yue", Text: "山雾里凝出一道守影，挡在残桥中央。", Encounter: "mist-guardian", Next: "bridge"},
			{ID: "bridge", Speaker: "su-qing", Text: "守影散去，桥下的石门显出泉纹。", Choices: []Choice{{Text: "用木牌开启石门", Next: "spring-cave", RequireFlags: map[string]bool{"knows-spring": true}}, {Text: "沿溪寻找另一条路", Next: "ending"}}},
			{ID: "spring-cave", Speaker: "lin-yue", Text: "石门后并非宝库，而是一册记录失踪村民姓名的旧簿。新的旅程，从寻找这些名字开始。", SetFlags: map[string]bool{"opened-cave": true}, Next: "ending"},
			{ID: "ending", Text: "本章结束。你可以在编辑器中添加人物、物品与后续节点，或加载独立扩展包。"},
		},
	}
}
