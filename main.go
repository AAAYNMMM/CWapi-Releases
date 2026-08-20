package main

import (
	"embed"
	"fmt"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

const cwapiSingleInstanceID = "007f7623-c85d-481d-be63-7e6887667f4c"

func main() {
	app, err := NewApp()
	if err != nil {
		fmt.Println("CWapi startup failed:", err.Error())
		return
	}
	if err := wails.Run(applicationOptions(app)); err != nil {
		fmt.Println("CWapi startup failed:", err.Error())
	}
}

func applicationOptions(app *App) *options.App {
	return &options.App{
		Title:             "CWapi",
		Width:             1280,
		Height:            820,
		MinWidth:          960,
		MinHeight:         640,
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 20, G: 24, B: 31, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               cwapiSingleInstanceID,
			OnSecondInstanceLaunch: app.onSecondInstanceLaunch,
		},
		Bind: []interface{}{
			app,
		},
	}
}
