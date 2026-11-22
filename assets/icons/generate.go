//go:build ignore

// Original geometric assets, reproducible using only the Go standard library.
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func polygon(x, y float64, p ...[2]float64) bool {
	in := false
	j := len(p) - 1
	for i := range p {
		if (p[i][1] > y) != (p[j][1] > y) && x < (p[j][0]-p[i][0])*(y-p[i][1])/(p[j][1]-p[i][1])+p[i][0] {
			in = !in
		}
		j = i
	}
	return in
}
func makeIcon(name string, size int, template bool) {
	im := image.NewNRGBA(image.Rect(0, 0, size, size))
	colors := map[string]color.NRGBA{"idle": {45, 185, 130, 255}, "syncing": {40, 150, 240, 255}, "starting": {140, 110, 230, 255}, "stopped": {135, 145, 155, 255}, "warning": {235, 165, 35, 255}, "error": {235, 65, 65, 255}, "unavailable": {150, 150, 150, 255}, "gosync": {40, 155, 210, 255}}
	ink := colors[name]
	if template {
		ink = color.NRGBA{0, 0, 0, 255}
	}
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			x, y := float64(px)*64/float64(size), float64(py)*64/float64(size)
			dx, dy := x-28, y-28
			r := math.Hypot(dx, dy)
			angle := math.Atan2(dy, dx)
			on := r >= 16 && r <= 22 && ((angle > -.9 && angle < 1.9) || (angle > 2.25 || angle < -1.25))
			on = on || polygon(x, y, [2]float64{45, 8}, [2]float64{52, 25}, [2]float64{34, 21}) || polygon(x, y, [2]float64{11, 48}, [2]float64{4, 31}, [2]float64{22, 35})
			if name != "gosync" && x > 34 && y > 34 {
				on = false
				u, v := x-48, y-48
				switch name {
				case "idle":
					on = u*u+v*v < 81
				case "stopped":
					on = math.Abs(u) < 8 && math.Abs(v) < 8
				case "starting":
					on = math.Abs(v) < 10 && math.Abs(u) < math.Abs(v)
				case "syncing":
					on = polygon(u, v, [2]float64{-8, -9}, [2]float64{10, 0}, [2]float64{-8, 9})
				case "warning":
					on = polygon(u, v, [2]float64{0, -11}, [2]float64{11, 9}, [2]float64{-11, 9}) && !(math.Abs(u) < 1.6 && v > -5 && v < 4)
				case "error":
					on = math.Abs(u) < 9 && math.Abs(v) < 9 && (math.Abs(u-v) < 3 || math.Abs(u+v) < 3)
				default:
					on = u*u+v*v < 100 && u*u+v*v > 49
				}
			}
			if on {
				im.SetNRGBA(px, py, ink)
			}
		}
	}
	suffix := ""
	if template {
		suffix = "-template"
	}
	f, err := os.Create(name + suffix + ".png")
	if err != nil {
		panic(err)
	}
	if err = png.Encode(f, im); err != nil {
		panic(err)
	}
	f.Close()
}
func main() {
	for _, name := range []string{"idle", "syncing", "starting", "stopped", "warning", "error", "unavailable"} {
		makeIcon(name, 64, false)
		makeIcon(name, 64, true)
	}
	makeIcon("gosync", 512, false)
}
