package main

import (
	"embed"
	"fmt"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

const (
	cwapiSingleInstanceID      = "007f7623-c85d-481d-be63-7e6887667f4c"
	cwapiProbeSingleInstanceID = "90e9bd2a-3834-4ba4-95e8-9f4683543610"
	cwapiWindowWidth           = 430
	cwapiWindowHeight          = 625
)

func main() {
	app := NewApp()
	if err := wails.Run(applicationOptions(app)); err != nil {
		fmt.Println("CWapi startup failed:", err.Error())
	}
}

func currentSingleInstanceID() string {
	if strings.TrimSpace(os.Getenv("CWAPI_GUI_PROBE_CONFIG")) != "" {
		return cwapiProbeSingleInstanceID
	}
	return cwapiSingleInstanceID
}

func applicationOptions(app *App) *options.App {
	return &options.App{
		Title: "CWapi", Width: cwapiWindowWidth, Height: cwapiWindowHeight,
		MinWidth: cwapiWindowWidth, MinHeight: cwapiWindowHeight,
		MaxWidth: cwapiWindowWidth, MaxHeight: cwapiWindowHeight,
		DisableResize: true, Frameless: true, HideWindowOnClose: true,
		AssetServer: &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 8, G: 10, B: 20, A: 255},
		OnStartup: app.startup, OnShutdown: app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: currentSingleInstanceID(), OnSecondInstanceLaunch: app.onSecondInstanceLaunch},
		Bind: []interface{}{app},
	}
}
