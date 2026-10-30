package gui

import "github.com/egoist/mygo/ui"

// Keep toolbar icons in one stroke style, parsed once and colored by the theme.
var (
	consoleIcon  = toolbarIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="m7 8 4 4-4 4m6 0h4"/>`)
	settingsIcon = toolbarIcon(`<path d="M21.88 10.44 L21.88 13.56 L19.79 13.80 L18.78 16.24 L20.09 17.88 L17.88 20.09 L16.24 18.78 L13.80 19.79 L13.56 21.88 L10.44 21.88 L10.20 19.79 L7.76 18.78 L6.12 20.09 L3.91 17.88 L5.22 16.24 L4.21 13.80 L2.12 13.56 L2.12 10.44 L4.21 10.20 L5.22 7.76 L3.91 6.12 L6.12 3.91 L7.76 5.22 L10.20 4.21 L10.44 2.12 L13.56 2.12 L13.80 4.21 L16.24 5.22 L17.88 3.91 L20.09 6.12 L18.78 7.76 L19.79 10.20Z"/><circle cx="12" cy="12" r="3"/>`)
	deployIcon   = toolbarIcon(`<path d="M12 16V3m-5 5 5-5 5 5M4 15v5a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-5"/>`)
	buildIcon    = toolbarIcon(`<path d="m12 3 9 5v9l-9 5-9-5V8Zm-9 5 9 5 9-5m-9 5v9M7.5 5.5l9 5"/>`)
	folderIcon   = toolbarIcon(`<path d="M3 7V5a1 1 0 0 1 1-1h5l2 3h9a1 1 0 0 1 1 1v2M3 7v12a1 1 0 0 0 1 1h15l3-10H6L3 20"/>`)
	reloadIcon   = toolbarIcon(`<path d="M20 7v5h-5M4 17v-5h5m11 0a8 8 0 0 0-14-5m-2 5a8 8 0 0 0 14 5"/>`)
)

func toolbarIcon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

// iconButton gives an empty button an icon, visible text and an accessible name.
func iconButton(c *ui.Context, button ui.Element, icon *ui.SVG, label string) ui.Element {
	return button.Label(label).Children(func() {
		ui.Icon(c, icon).Size(16, 16)
		ui.Text(c, label).SingleLine()
	})
}
