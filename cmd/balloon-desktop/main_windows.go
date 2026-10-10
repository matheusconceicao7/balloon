// Command balloon-desktop is the double-clickable Windows launcher.
package main

import (
	"os"

	"github.com/dhwanikher/balloon/internal/api"
	"github.com/dhwanikher/balloon/internal/launcher"
	"github.com/dhwanikher/balloon/web"
)

func main() {
	if err := launcher.Run(api.New(web.FS)); err != nil {
		launcher.ShowError(err)
		os.Exit(1)
	}
}
