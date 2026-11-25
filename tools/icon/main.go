// Draws the app icon: go run ./tools/icon out.png
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

const size = 1024

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: icon out.png")
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const cx, cy = 512.0, 560.0
	const start, sweep = 150.0, 240.0 // degrees clockwise from the X axis
	const value = 17.0 / 24           // needle position (a segment boundary)
	na := (start + sweep*value) * math.Pi / 180
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := float64(x)+.5, float64(y)+.5
			t := py / size
			paint(img, x, y, mix(color.NRGBA{58, 66, 92, 255}, color.NRGBA{14, 17, 30, 255}, t),
				cover(x, y, 100, 100, 924, 924, 185))

			// Gauge: segments from teal to red, unfilled ones are dim.
			r := math.Hypot(px-cx, py-cy)
			deg := math.Mod(math.Atan2(py-cy, px-cx)*180/math.Pi+360, 360)
			p := math.Mod(deg-start+360, 360) / sweep
			if p <= 1 {
				const segs = 24
				f := math.Abs(math.Mod(p*segs, 1) - .5)
				arc := sweep / segs * math.Pi / 180 * r // segment width in pixels
				edge := clamp((.36-f)*arc + .5)
				c := ramp(p)
				if p > value {
					c = color.NRGBA{78, 86, 110, 255}
				}
				paint(img, x, y, c, edge*clamp(46.5-math.Abs(r-285)))
			}

			// Tapered needle and hub.
			along := (px-cx)*math.Cos(na) + (py-cy)*math.Sin(na)
			across := math.Abs(-(px-cx)*math.Sin(na) + (py-cy)*math.Cos(na))
			if along > 0 && along < 215 {
				paint(img, x, y, color.NRGBA{250, 250, 255, 255}, clamp(24-along*.085-across+.5))
			}
			paint(img, x, y, color.NRGBA{250, 250, 255, 255}, clamp(54.5-r))
			paint(img, x, y, color.NRGBA{30, 35, 54, 255}, clamp(24.5-r))
		}
	}
	// Bars below the gauge.
	for i, h := range []float64{44, 78, 58, 100} {
		x0 := 400 + float64(i)*60
		for y := 700; y < 860; y++ {
			for x := 380; x < 660; x++ {
				paint(img, x, y, ramp(float64(i)/4), cover(x, y, x0, 830-h, x0+44, 830, 12))
			}
		}
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

// cover is the antialiased pixel coverage of a rounded rectangle (0–1).
func cover(x, y int, x0, y0, x1, y1, r float64) float64 {
	dx := math.Abs(float64(x)+.5-(x0+x1)/2) - (x1-x0)/2 + r
	dy := math.Abs(float64(y)+.5-(y0+y1)/2) - (y1-y0)/2 + r
	d := math.Hypot(max(dx, 0), max(dy, 0)) + min(max(dx, dy), 0) - r
	return min(1, max(0, .5-d))
}

func clamp(v float64) float64 { return min(1, max(0, v)) }

func mix(a, b color.NRGBA, t float64) color.NRGBA {
	m := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.NRGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), a.A}
}

func ramp(t float64) color.NRGBA {
	stops := []color.NRGBA{{38, 208, 206, 255}, {70, 214, 120, 255}, {255, 200, 60, 255}, {255, 80, 70, 255}}
	p := clamp(t) * 3
	i := min(2, int(p))
	return mix(stops[i], stops[i+1], p-float64(i))
}

func paint(img *image.NRGBA, x, y int, c color.NRGBA, a float64) {
	if a == 0 {
		return
	}
	old := img.NRGBAAt(x, y)
	if old.A == 0 {
		c.A = uint8(255 * a)
		img.SetNRGBA(x, y, c)
		return
	}
	img.SetNRGBA(x, y, mix(old, c, a))
}
