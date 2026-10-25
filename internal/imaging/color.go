package imaging

import (
	"fmt"
	"image"
	_ "image/gif"  // register decoders for image.Decode
	_ "image/jpeg" //
	_ "image/png"  //
	"io"
	"math"
	"os"

	_ "golang.org/x/image/webp" // register webp decoder
)

// DominantColorFile decodes an image file (jpeg/png/gif/webp) and returns a
// soft dominant colour as "#rrggbb". See DominantColor.
func DominantColorFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return DominantColor(f)
}

// DominantColor returns a pleasant background colour for an image.
//
// Algorithm (cheap, no clustering): sample at most ~4k pixels, build a coarse
// 4x4x4 RGB histogram, pick the most populated bucket while slightly
// favouring saturated buckets, and average the pixels in it. Very dark and
// very bright pixels are down-weighted so the result is usable as a tint.
func DominantColor(r io.Reader) (string, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return "", err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return "", fmt.Errorf("empty image")
	}
	step := int(math.Max(1, math.Sqrt(float64(w*h)/4096)))

	type bucket struct {
		r, g, b, weight float64
	}
	var hist [64]bucket
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			cr, cg, cb, ca := img.At(x, y).RGBA()
			if ca < 0x8000 {
				continue
			}
			r8, g8, b8 := float64(cr>>8), float64(cg>>8), float64(cb>>8)
			mx := math.Max(r8, math.Max(g8, b8))
			mn := math.Min(r8, math.Min(g8, b8))
			light := (mx + mn) / 2 / 255
			sat := 0.0
			if mx > 0 {
				sat = (mx - mn) / mx
			}
			wgt := 1.0 + sat*1.5
			if light < 0.12 || light > 0.92 {
				wgt *= 0.25
			}
			i := (int(r8)>>6)<<4 | (int(g8)>>6)<<2 | int(b8)>>6
			hist[i].r += r8 * wgt
			hist[i].g += g8 * wgt
			hist[i].b += b8 * wgt
			hist[i].weight += wgt
		}
	}
	best := -1
	for i := range hist {
		if hist[i].weight > 0 && (best < 0 || hist[i].weight > hist[best].weight) {
			best = i
		}
	}
	if best < 0 {
		return "#a8a29e", nil
	}
	bk := hist[best]
	return fmt.Sprintf("#%02x%02x%02x",
		clamp8(bk.r/bk.weight), clamp8(bk.g/bk.weight), clamp8(bk.b/bk.weight)), nil
}

func clamp8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}
