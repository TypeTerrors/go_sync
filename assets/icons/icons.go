// Package icons embeds original, generated GoSync tray assets. Color and shape
// distinguish health; monochrome templates adapt to macOS light/dark menu bars.
//
//go:generate go run generate.go
package icons

import "embed"

//go:embed *.png
var files embed.FS

func read(name string) []byte { data, _ := files.ReadFile(name); return data }
func Color(state string) []byte {
	data := read(state + ".png")
	if len(data) == 0 {
		return read("unavailable.png")
	}
	return data
}
func Template(state string) []byte {
	data := read(state + "-template.png")
	if len(data) == 0 {
		return read("unavailable-template.png")
	}
	return data
}
