// palstudio is a native MyGo editor and previewer. All game logic lives in Go.
package main

import (
	"log"

	"github.com/HiChen85/pal--reverse--learn/internal/content"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	app, err := newWorkbench(content.Demo())
	if err != nil {
		log.Fatal(err)
	}
	mygo.App.WhenReady(func() {
		mygo.Theme.SetSource(mygo.ThemeDark)
		mygo.NewWindow(mygo.WindowOptions{Title: "PAL1 Go Studio · 故事工坊", Width: 1200, Height: 860, MinWidth: 960, MinHeight: 700, Content: ui.View(app.view)})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
