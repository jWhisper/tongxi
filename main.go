package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := &App{}
	if err := wails.Run(&options.App{
		Title: "同席 · Tongxi", Width: 1180, Height: 800, MinWidth: 760, MinHeight: 620,
		BackgroundColour: &options.RGBA{R: 247, G: 249, B: 251, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup, OnShutdown: app.shutdown, Bind: []interface{}{app},
	}); err != nil {
		log.Fatal(err)
	}
}
