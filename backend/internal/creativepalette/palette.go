// Package creativepalette owns versioned image palettes and their matching rules.
package creativepalette

import (
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
)

// Matching thresholds are consumed by creativelibrary.appearanceCondition;
// regression assertions run through SearchAssets against PostgreSQL.
const (
	Algorithm        = "oklab-k5-v1"
	NeutralChroma    = 0.04
	NeutralLightness = 0.12
	ColorLightness   = 0.2
	HueDistance      = 25.0
	MinWeight        = 0.1
)

type Color struct {
	Hex    string  `json:"hex"`
	Weight float64 `json:"weight"`
	L      float64 `json:"l"`
	C      float64 `json:"c"`
	H      float64 `json:"h"`
}
type Result struct {
	State  string  `json:"state"`
	Reason string  `json:"reason"`
	Colors []Color `json:"colors"`
}
type lab struct{ l, a, b float64 }
type sample struct {
	value  lab
	weight float64
}

var ErrColor = errors.New("color must be #rrggbb")

func ParseHex(s string) (Color, error) {
	if len(s) != 7 || s[0] != '#' {
		return Color{}, ErrColor
	}
	raw, err := hex.DecodeString(s[1:])
	if err != nil {
		return Color{}, ErrColor
	}
	return fromLab(toLab(float64(raw[0])/255, float64(raw[1])/255, float64(raw[2])/255), 1), nil
}
func Failed(reason string) Result { return Result{State: "failed", Reason: reason, Colors: []Color{}} }

// Extract samples an oriented image at no more than 64 pixels on the long edge.
// Fixed raster order and five constant initial centers (no RNG) make k-means
// deterministic. Transparent pixels carry no color; partial alpha weights the
// visible pigment, never an arbitrary black/white matte. Empty clusters vanish.
func Extract(img image.Image) Result {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return Failed("invalid_dimensions")
	}
	scale := math.Min(1, 64/float64(max(w, h)))
	sw, sh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	points := make([]sample, 0, sw*sh)
	total := 0.0
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+(2*x+1)*w/(2*sw), b.Min.Y+(2*y+1)*h/(2*sh)))
			rgba, ok := c.(color.NRGBA)
			if !ok || rgba.A == 0 {
				continue
			}
			weight := float64(rgba.A) / 255
			points = append(points, sample{value: toLab(float64(rgba.R)/255, float64(rgba.G)/255, float64(rgba.B)/255), weight: weight})
			total += weight
		}
	}
	if total == 0 {
		return Failed("no_visible_pixels")
	}
	// Fixed light/dark and RGB anchors cover luminance and hue without a
	// random seed or dependence on the first visible pixel's location.
	centers := []lab{toLab(0, 0, 0), toLab(1, 1, 1), toLab(1, 0, 0), toLab(0, 1, 0), toLab(0, 0, 1)}
	for range 30 {
		sums := make([]lab, len(centers))
		weights := make([]float64, len(centers))
		for _, p := range points {
			i, _ := nearest(p.value, centers)
			weights[i] += p.weight
			sums[i].l += p.value.l * p.weight
			sums[i].a += p.value.a * p.weight
			sums[i].b += p.value.b * p.weight
		}
		movement := 0.0
		for i, c := range centers {
			if weights[i] == 0 {
				// Revive an empty cluster at the largest remaining quantization error.
				// Ties keep raster order; uniform images naturally still return one color.
				farthest, d := c, 0.0
				for _, p := range points {
					_, n := nearest(p.value, centers)
					if n > d {
						farthest, d = p.value, n
					}
				}
				if d > 1e-12 {
					centers[i] = farthest
					movement += distance(c, farthest)
				}
				continue
			}
			n := lab{sums[i].l / weights[i], sums[i].a / weights[i], sums[i].b / weights[i]}
			movement += distance(c, n)
			centers[i] = n
		}
		if movement < 1e-12 {
			break
		}
	}
	weights := make([]float64, len(centers))
	for _, p := range points {
		i, _ := nearest(p.value, centers)
		weights[i] += p.weight
	}
	out := Result{State: "ready", Colors: []Color{}}
	for i, c := range centers {
		if weights[i] > 0 {
			out.Colors = append(out.Colors, fromLab(c, weights[i]/total))
		}
	}
	sort.Slice(out.Colors, func(i, j int) bool {
		if out.Colors[i].Weight == out.Colors[j].Weight {
			return out.Colors[i].Hex < out.Colors[j].Hex
		}
		return out.Colors[i].Weight > out.Colors[j].Weight
	})
	return out
}
func distance(a, b lab) float64 {
	return (a.l-b.l)*(a.l-b.l) + (a.a-b.a)*(a.a-b.a) + (a.b-b.b)*(a.b-b.b)
}
func nearest(p lab, centers []lab) (int, float64) {
	i, d := 0, distance(p, centers[0])
	for j := 1; j < len(centers); j++ {
		if n := distance(p, centers[j]); n < d {
			i, d = j, n
		}
	}
	return i, d
}
func linear(c float64) float64 {
	if c <= .04045 {
		return c / 12.92
	}
	return math.Pow((c+.055)/1.055, 2.4)
}

// Matrices: Björn Ottosson's public-domain reference implementation (2021).
// https://bottosson.github.io/posts/oklab/#converting-from-linear-srgb-to-oklab
func toLab(r, g, b float64) lab {
	r, g, b = linear(r), linear(g), linear(b)
	l := math.Cbrt(.4122214708*r + .5363325363*g + .0514459929*b)
	m := math.Cbrt(.2119034982*r + .6806995451*g + .1073969566*b)
	s := math.Cbrt(.0883024619*r + .2817188376*g + .6299787005*b)
	return lab{.2104542553*l + .7936177850*m - .0040720468*s, 1.9779984951*l - 2.4285922050*m + .4505937099*s, .0259040371*l + .7827717662*m - .8086757660*s}
}
func fromLab(c lab, weight float64) Color {
	l, m, s := c.l+.3963377774*c.a+.2158037573*c.b, c.l-.1055613458*c.a-.0638541728*c.b, c.l-.0894841775*c.a-1.2914855480*c.b
	l, m, s = l*l*l, m*m*m, s*s*s
	channel := func(v float64) int {
		if v <= .0031308 {
			v *= 12.92
		} else {
			v = 1.055*math.Pow(v, 1/2.4) - .055
		}
		return int(math.Round(math.Max(0, math.Min(1, v)) * 255))
	}
	h := math.Atan2(c.b, c.a) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return Color{Hex: fmt.Sprintf("#%02x%02x%02x", channel(4.0767416621*l-3.3077115913*m+.2309699292*s), channel(-1.2684380046*l+2.6097574011*m-.3413193965*s), channel(-.0041960863*l-.7034186147*m+1.707614701*s)), Weight: weight, L: c.l, C: math.Hypot(c.a, c.b), H: h}
}
