// Package imagediff compares two images pixel by pixel for visual regression checks.
package imagediff

import (
	"bytes"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"os"

	_ "golang.org/x/image/webp"
)

// Result describes how far two images differ.
type Result struct {
	Width, Height int
	SameSize      bool
	DiffPixels    int
	Ratio         float64
}

// Decode reads a PNG, JPEG or WebP image.
func Decode(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// Compare counts pixels whose largest channel difference exceeds threshold (0 to 1) and returns a highlight image of them.
func Compare(a, b image.Image, threshold float64) (Result, *image.RGBA) {
	ab, bb := a.Bounds(), b.Bounds()
	r := Result{Width: ab.Dx(), Height: ab.Dy(), SameSize: ab.Dx() == bb.Dx() && ab.Dy() == bb.Dy()}
	if !r.SameSize {
		r.Width, r.Height = max(ab.Dx(), bb.Dx()), max(ab.Dy(), bb.Dy())
		r.DiffPixels, r.Ratio = r.Width*r.Height, 1
		return r, nil
	}
	diff := image.NewRGBA(image.Rect(0, 0, r.Width, r.Height))
	limit := threshold * 0xffff
	for y := 0; y < r.Height; y++ {
		for x := 0; x < r.Width; x++ {
			ar, ag, abl, aa := a.At(ab.Min.X+x, ab.Min.Y+y).RGBA()
			br, bg, bbl, ba := b.At(bb.Min.X+x, bb.Min.Y+y).RGBA()
			d := max(absDiff(ar, br), absDiff(ag, bg), absDiff(abl, bbl), absDiff(aa, ba))
			if float64(d) > limit {
				r.DiffPixels++
				diff.Set(x, y, color.RGBA{255, 0, 0, 255})
				continue
			}
			gray := uint8((ar*299 + ag*587 + abl*114) / 1000 >> 8)
			faded := 255 - (255-gray)/4
			diff.Set(x, y, color.RGBA{faded, faded, faded, 255})
		}
	}
	if total := r.Width * r.Height; total > 0 {
		r.Ratio = float64(r.DiffPixels) / float64(total)
	}
	return r, diff
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// WritePNG saves img as a PNG file.
func WritePNG(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
