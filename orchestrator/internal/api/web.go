package api

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed ui/index.html ui/assets/*
var embeddedUI embed.FS

var uiAssets http.Handler

func init() {
	assets, err := fs.Sub(embeddedUI, "ui/assets")
	if err != nil {
		panic(err)
	}
	uiAssets = http.FileServer(http.FS(assets))
}

func (h *Handlers) AppShell(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	index, err := embeddedUI.ReadFile("ui/index.html")
	if err != nil {
		http.Error(w, "frontend unavailable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(index)
}

func UIAssets() http.Handler {
	return uiAssets
}
