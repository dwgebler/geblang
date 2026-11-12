package native

import (
	"fmt"
	"image"
	"image/color"
	"sort"
	"strings"

	"geblang/internal/runtime"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/aztec"
	"github.com/boombuler/barcode/codabar"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/code39"
	"github.com/boombuler/barcode/code93"
	"github.com/boombuler/barcode/datamatrix"
	"github.com/boombuler/barcode/ean"
	"github.com/boombuler/barcode/pdf417"
	"github.com/boombuler/barcode/qr"
	"github.com/boombuler/barcode/twooffive"
)

const barcodeLabel = "barcode.encode"

const barcodeMaxSide = 16384

type barcodeEncoder func(data string, opts runtime.Dict) (barcode.Barcode, error)

var barcodeEncoders = map[string]barcodeEncoder{
	"aztec": func(data string, opts runtime.Dict) (barcode.Barcode, error) {
		ecc, err := barcodeIntOpt(opts, "level", aztec.DEFAULT_EC_PERCENT, 5, 95)
		if err != nil {
			return nil, err
		}
		return aztec.Encode([]byte(data), ecc, aztec.DEFAULT_LAYERS)
	},
	"codabar": func(data string, _ runtime.Dict) (barcode.Barcode, error) {
		return codabar.Encode(data)
	},
	"code128": func(data string, _ runtime.Dict) (barcode.Barcode, error) {
		return code128.Encode(data)
	},
	"code39": func(data string, opts runtime.Dict) (barcode.Barcode, error) {
		checksum, fullASCII, err := barcodeCodeFlags(opts, false)
		if err != nil {
			return nil, err
		}
		return code39.Encode(data, checksum, fullASCII)
	},
	"code93": func(data string, opts runtime.Dict) (barcode.Barcode, error) {
		checksum, fullASCII, err := barcodeCodeFlags(opts, true)
		if err != nil {
			return nil, err
		}
		return code93.Encode(data, checksum, fullASCII)
	},
	"datamatrix": func(data string, _ runtime.Dict) (barcode.Barcode, error) {
		return datamatrix.Encode(data)
	},
	"ean13": func(data string, _ runtime.Dict) (barcode.Barcode, error) {
		if len(data) != 12 && len(data) != 13 {
			return nil, fmt.Errorf("ean13 data must be 12 digits, or 13 with the check digit")
		}
		return ean.Encode(data)
	},
	"ean8": func(data string, _ runtime.Dict) (barcode.Barcode, error) {
		if len(data) != 7 && len(data) != 8 {
			return nil, fmt.Errorf("ean8 data must be 7 digits, or 8 with the check digit")
		}
		return ean.Encode(data)
	},
	"itf": func(data string, _ runtime.Dict) (barcode.Barcode, error) {
		return twooffive.Encode(data, true)
	},
	"pdf417": func(data string, opts runtime.Dict) (barcode.Barcode, error) {
		level, err := barcodeIntOpt(opts, "level", 2, 0, 8)
		if err != nil {
			return nil, err
		}
		return pdf417.Encode(data, byte(level))
	},
	"qr": func(data string, opts runtime.Dict) (barcode.Barcode, error) {
		level := qr.M
		if v, ok := dictLookup(opts, "level"); ok {
			s, _ := v.(runtime.String)
			switch strings.ToUpper(s.Value) {
			case "L":
				level = qr.L
			case "M":
				level = qr.M
			case "Q":
				level = qr.Q
			case "H":
				level = qr.H
			default:
				return nil, fmt.Errorf("qr option \"level\" must be \"L\", \"M\", \"Q\", or \"H\"")
			}
		}
		return qr.Encode(data, level, qr.Auto)
	},
	"upca": func(data string, _ runtime.Dict) (barcode.Barcode, error) {
		if len(data) != 11 && len(data) != 12 {
			return nil, fmt.Errorf("upca data must be 11 digits, or 12 with the check digit")
		}
		// UPC-A is the EAN-13 subset with a leading zero.
		return ean.Encode("0" + data)
	},
}

func barcodeFormatNames() []string {
	names := make([]string, 0, len(barcodeEncoders))
	for name := range barcodeEncoders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func barcodeIntOpt(opts runtime.Dict, key string, def, lo, hi int) (int, error) {
	v, ok := dictLookup(opts, key)
	if !ok {
		return def, nil
	}
	n, isInt := AsInt64(v)
	if !isInt || n < int64(lo) || n > int64(hi) {
		return 0, fmt.Errorf("option %q must be an int between %d and %d", key, lo, hi)
	}
	return int(n), nil
}

func barcodeBoolOpt(opts runtime.Dict, key string, def bool) (bool, error) {
	v, ok := dictLookup(opts, key)
	if !ok {
		return def, nil
	}
	b, isBool := v.(runtime.Bool)
	if !isBool {
		return false, fmt.Errorf("option %q must be a bool", key)
	}
	return b.Value, nil
}

func barcodeCodeFlags(opts runtime.Dict, defaultChecksum bool) (bool, bool, error) {
	checksum, err := barcodeBoolOpt(opts, "checksum", defaultChecksum)
	if err != nil {
		return false, false, err
	}
	fullASCII, err := barcodeBoolOpt(opts, "fullAscii", false)
	return checksum, fullASCII, err
}

func barcodeColorOpt(opts runtime.Dict, key string, def color.NRGBA) (color.NRGBA, error) {
	c, ok, err := imageOptColor(opts, key, barcodeLabel)
	if err != nil {
		return c, err
	}
	if !ok {
		return def, nil
	}
	return c, nil
}

func renderBarcode(bc barcode.Barcode, opts runtime.Dict) (*image.NRGBA, error) {
	modules := bc.Bounds()
	linear := bc.Metadata().Dimensions == 1
	defaultMargin, defaultScale := 4, 4
	if linear {
		defaultMargin, defaultScale = 10, 2
	}
	margin, err := barcodeIntOpt(opts, "margin", defaultMargin, 0, 1000)
	if err != nil {
		return nil, err
	}
	width, err := barcodeIntOpt(opts, "width", 0, 1, barcodeMaxSide)
	if err != nil {
		return nil, err
	}
	height, err := barcodeIntOpt(opts, "height", 0, 1, barcodeMaxSide)
	if err != nil {
		return nil, err
	}
	fg, err := barcodeColorOpt(opts, "color", color.NRGBA{A: 255})
	if err != nil {
		return nil, err
	}
	bg, err := barcodeColorOpt(opts, "background", color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}

	cols, rows := modules.Dx()+2*margin, modules.Dy()+2*margin
	fit := 0
	if width > 0 {
		fit = width / cols
	}
	if height > 0 && !linear && (fit == 0 || height/rows < fit) {
		fit = height / rows
	}
	if (width > 0 || (height > 0 && !linear)) && fit < 1 {
		return nil, fmt.Errorf("width/height too small: this code needs at least %d x %d pixels", cols, rows)
	}
	if fit > 0 {
		defaultScale = fit
	}
	scale, err := barcodeIntOpt(opts, "scale", defaultScale, 1, 256)
	if err != nil {
		return nil, err
	}

	bodyW, bodyH := modules.Dx()*scale, modules.Dy()*scale
	if linear {
		bodyH = 80
		if height > 0 {
			bodyH = height - 2*margin*scale
		}
	}
	if width == 0 {
		width = bodyW + 2*margin*scale
	}
	if height == 0 {
		height = bodyH + 2*margin*scale
	}
	if width > barcodeMaxSide || height > barcodeMaxSide {
		return nil, fmt.Errorf("image would exceed %d pixels per side; lower \"scale\"", barcodeMaxSide)
	}
	if bodyW > width || bodyH > height || bodyH < 1 {
		return nil, fmt.Errorf("width/height too small for the code at scale %d", scale)
	}

	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	fillRectNRGBA(dst, dst.Bounds(), bg)
	left, top := (width-bodyW)/2, (height-bodyH)/2
	for my := 0; my < modules.Dy(); my++ {
		for mx := 0; mx < modules.Dx(); mx++ {
			r, g, b, _ := bc.At(modules.Min.X+mx, modules.Min.Y+my).RGBA()
			if r+g+b >= 3*0x8000 {
				continue
			}
			cell := image.Rect(left+mx*scale, top+my*scale, left+(mx+1)*scale, top+(my+1)*scale)
			if linear {
				cell.Min.Y, cell.Max.Y = top, top+bodyH
			}
			fillRectNRGBA(dst, cell, fg)
		}
	}
	return dst, nil
}

func registerImageBarcode(r *Registry) {
	r.Register("imagenative", "barcode", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("%s expects (data, options)", barcodeLabel)
		}
		data, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s: data must be a string", barcodeLabel)
		}
		if data.Value == "" {
			return nil, fmt.Errorf("%s: data must not be empty", barcodeLabel)
		}
		opts, err := imageOpts(args, 1, barcodeLabel)
		if err != nil {
			return nil, err
		}
		format, _ := dictLookup(opts, "format")
		name, _ := format.(runtime.String)
		encode, ok := barcodeEncoders[strings.ToLower(name.Value)]
		if !ok {
			return nil, fmt.Errorf("%s: option \"format\" must be one of %s", barcodeLabel, strings.Join(barcodeFormatNames(), ", "))
		}
		bc, err := encode(data.Value, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", barcodeLabel, strings.ToLower(name.Value), err)
		}
		img, err := renderBarcode(bc, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", barcodeLabel, err)
		}
		return putImage(img), nil
	})

	r.Register("imagenative", "barcodeFormats", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("barcode.formats expects no arguments")
		}
		names := barcodeFormatNames()
		out := make([]runtime.Value, len(names))
		for i, n := range names {
			out[i] = runtime.String{Value: n}
		}
		return &runtime.List{Elements: out}, nil
	})
}
