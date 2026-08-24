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

const (
	cwapiSingleInstanceID = "007f7623-c85d-481d-be63-7e6887667f4c"
	cwapiWindowWidth      = 375
	cwapiWindowHeight     = 690
)

func main() {
	app := NewApp()
	if err := wails.Run(applicationOptions(app)); err != nil {
		fmt.Println("CWapi startup failed:", err.Error())
	}
}

func applicationOptions(app *App) *options.App {
	return &options.App{
		Title:             "CWapi",
		Width:             cwapiWindowWidth,
		Height:            cwapiWindowHeight,
		MinWidth:          cwapiWindowWidth,
		MinHeight:         cwapiWindowHeight,
		MaxWidth:          cwapiWindowWidth,
		MaxHeight:         cwapiWindowHeight,
		DisableResize:     true,
		Frameless:         true,
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 8, G: 10, B: 32, A: 255},
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
