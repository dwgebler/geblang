package native

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"

	"geblang/internal/runtime"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var imageBuiltinFontData = map[string][]byte{
	"regular": goregular.TTF,
	"bold":    gobold.TTF,
	"italic":  goitalic.TTF,
	"mono":    gomono.TTF,
}

var (
	imageBuiltinFontMu sync.Mutex
	imageBuiltinFonts  = map[string]*opentype.Font{}
)

func imageBuiltinFont(name string) (*opentype.Font, error) {
	data, ok := imageBuiltinFontData[name]
	if !ok {
		return nil, fmt.Errorf("unknown font %q (regular, bold, italic, mono, or a loaded Font)", name)
	}
	imageBuiltinFontMu.Lock()
	defer imageBuiltinFontMu.Unlock()
	if f, ok := imageBuiltinFonts[name]; ok {
		return f, nil
	}
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, err
	}
	imageBuiltinFonts[name] = f
	return f, nil
}

func imageTextFace(opts runtime.Dict, label string) (font.Face, error) {
	var f *opentype.Font
	var err error
	switch v, _ := dictLookup(opts, "font"); fv := v.(type) {
	case nil, runtime.Null:
		f, err = imageBuiltinFont("regular")
	case runtime.String:
		f, err = imageBuiltinFont(fv.Value)
	case runtime.NativeObject:
		loaded, ok := fv.Payload.(*opentype.Font)
		if fv.Kind != "Font" || !ok {
			return nil, fmt.Errorf("%s: option \"font\" is not a Font", label)
		}
		f = loaded
	default:
		return nil, fmt.Errorf("%s: option \"font\" must be a font name or a Font", label)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	size, err := imageOptFloat(opts, "size", 16, label)
	if err != nil {
		return nil, err
	}
	if size <= 0 || size > 4096 {
		return nil, fmt.Errorf("%s: option \"size\" must be between 0 and 4096", label)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return face, nil
}

func imageTextSize(face font.Face, lines []string) (int, int) {
	m := face.Metrics()
	width := fixed.Int26_6(0)
	for _, line := range lines {
		width = max(width, font.MeasureString(face, line))
	}
	height := m.Ascent + m.Descent + m.Height.Mul(fixed.I(len(lines)-1))
	return width.Ceil(), height.Ceil()
}

func registerImageText(r *Registry) {
	r.Register("imagenative", "loadFont", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleBytes(args, "image.loadFont")
		if err != nil {
			return nil, err
		}
		f, err := opentype.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("image.loadFont: not a TrueType or OpenType font: %w", err)
		}
		return runtime.NativeObject{Kind: "Font", ID: nextSyncID(), Payload: f}, nil
	})

	r.Register("imagenative", "measureText", func(args []runtime.Value) (runtime.Value, error) {
		const label = "image.measureText"
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("%s expects (text, options)", label)
		}
		text, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s: text must be a string", label)
		}
		opts, err := imageOpts(args, 1, label)
		if err != nil {
			return nil, err
		}
		face, err := imageTextFace(opts, label)
		if err != nil {
			return nil, err
		}
		defer face.Close()
		w, h := imageTextSize(face, strings.Split(text.Value, "\n"))
		d := runtime.NewDictHint(2)
		for _, kv := range []struct {
			key string
			n   int
		}{{"width", w}, {"height", h}} {
			k := runtime.String{Value: kv.key}
			d.PutEntry(DictKey(k), runtime.DictEntry{Key: k, Value: runtime.SmallInt{Value: int64(kv.n)}})
		}
		return d, nil
	})

	r.Register("imagenative", "text", func(args []runtime.Value) (runtime.Value, error) {
		const label = "image.text"
		if len(args) < 4 || len(args) > 5 {
			return nil, fmt.Errorf("%s expects (handle, text, x, y, options)", label)
		}
		text, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s: text must be a string", label)
		}
		xy, err := imageFloats(args[2:4], label)
		if err != nil {
			return nil, err
		}
		opts, err := imageOpts(args, 4, label)
		if err != nil {
			return nil, err
		}
		c, hasColor, err := imageOptColor(opts, "color", label)
		if err != nil {
			return nil, err
		}
		if !hasColor {
			c = color.NRGBA{A: 255}
		}
		face, err := imageTextFace(opts, label)
		if err != nil {
			return nil, err
		}
		defer face.Close()
		return runtime.Null{}, mutateImage(args[0], label, func(dst *image.NRGBA) error {
			m := face.Metrics()
			d := font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: face}
			baseline := fixed.Int26_6(xy[1]*64) + m.Ascent
			for _, line := range strings.Split(text.Value, "\n") {
				d.Dot = fixed.Point26_6{X: fixed.Int26_6(xy[0] * 64), Y: baseline}
				d.DrawString(line)
				baseline += m.Height
			}
			return nil
		})
	})
}
