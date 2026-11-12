package native

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"geblang/internal/runtime"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/vector"
)

var imageNamedColors = map[string]color.NRGBA{
	"transparent": {0, 0, 0, 0},
	"black":       {0, 0, 0, 255},
	"white":       {255, 255, 255, 255},
	"red":         {255, 0, 0, 255},
	"green":       {0, 128, 0, 255},
	"lime":        {0, 255, 0, 255},
	"blue":        {0, 0, 255, 255},
	"yellow":      {255, 255, 0, 255},
	"cyan":        {0, 255, 255, 255},
	"magenta":     {255, 0, 255, 255},
	"orange":      {255, 165, 0, 255},
	"purple":      {128, 0, 128, 255},
	"pink":        {255, 192, 203, 255},
	"brown":       {165, 42, 42, 255},
	"gray":        {128, 128, 128, 255},
	"grey":        {128, 128, 128, 255},
	"silver":      {192, 192, 192, 255},
	"navy":        {0, 0, 128, 255},
	"teal":        {0, 128, 128, 255},
	"maroon":      {128, 0, 0, 255},
	"olive":       {128, 128, 0, 255},
}

func parseImageColor(text string) (color.NRGBA, error) {
	s := strings.ToLower(strings.TrimSpace(text))
	if c, ok := imageNamedColors[s]; ok {
		return c, nil
	}
	bad := fmt.Errorf("invalid color %q (use #rgb, #rgba, #rrggbb, #rrggbbaa, or a color name)", text)
	if !strings.HasPrefix(s, "#") {
		return color.NRGBA{}, bad
	}
	hex := s[1:]
	if len(hex) == 3 || len(hex) == 4 {
		var wide strings.Builder
		for _, ch := range hex {
			wide.WriteRune(ch)
			wide.WriteRune(ch)
		}
		hex = wide.String()
	}
	if len(hex) != 6 && len(hex) != 8 {
		return color.NRGBA{}, bad
	}
	var parts [4]uint8
	parts[3] = 255
	for i := 0; i < len(hex)/2; i++ {
		hi, ok1 := hexNibble(hex[2*i])
		lo, ok2 := hexNibble(hex[2*i+1])
		if !ok1 || !ok2 {
			return color.NRGBA{}, bad
		}
		parts[i] = hi<<4 | lo
	}
	return color.NRGBA{R: parts[0], G: parts[1], B: parts[2], A: parts[3]}, nil
}

func hexNibble(b byte) (uint8, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	}
	return 0, false
}

func formatImageColor(c color.NRGBA) string {
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}

func imageColorArg(v runtime.Value, label string) (color.NRGBA, error) {
	s, ok := v.(runtime.String)
	if !ok {
		return color.NRGBA{}, fmt.Errorf("%s: color must be a string", label)
	}
	c, err := parseImageColor(s.Value)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("%s: %w", label, err)
	}
	return c, nil
}

func imageOpts(args []runtime.Value, index int, label string) (runtime.Dict, error) {
	if index >= len(args) {
		return runtime.NewDict(), nil
	}
	switch v := args[index].(type) {
	case runtime.Null:
		return runtime.NewDict(), nil
	case runtime.Dict:
		return v, nil
	}
	return runtime.Dict{}, fmt.Errorf("%s: options must be a dict", label)
}

func imageOptColor(opts runtime.Dict, key, label string) (color.NRGBA, bool, error) {
	v, ok := dictLookup(opts, key)
	if !ok {
		return color.NRGBA{}, false, nil
	}
	if _, isNull := v.(runtime.Null); isNull {
		return color.NRGBA{}, false, nil
	}
	c, err := imageColorArg(v, label)
	return c, err == nil, err
}

func imageOptFloat(opts runtime.Dict, key string, def float64, label string) (float64, error) {
	v, ok := dictLookup(opts, key)
	if !ok {
		return def, nil
	}
	f, ok := asFloat64Strict(v)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%s: option %q must be a number", label, key)
	}
	return f, nil
}

func imageFloats(args []runtime.Value, label string) ([]float64, error) {
	out := make([]float64, len(args))
	for i, a := range args {
		f, ok := asFloat64Strict(a)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("%s: coordinates must be numbers", label)
		}
		out[i] = f
	}
	return out, nil
}

func cloneNRGBA(src image.Image) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	if n, ok := src.(*image.NRGBA); ok {
		for y := 0; y < b.Dy(); y++ {
			copy(dst.Pix[y*dst.Stride:y*dst.Stride+4*b.Dx()], n.Pix[n.PixOffset(b.Min.X, b.Min.Y+y):])
		}
		return dst
	}
	xdraw.Draw(dst, dst.Bounds(), src, b.Min, xdraw.Src)
	return dst
}

func fillRectNRGBA(dst *image.NRGBA, r image.Rectangle, c color.NRGBA) {
	r = r.Intersect(dst.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := dst.Pix[dst.PixOffset(r.Min.X, y) : dst.PixOffset(r.Max.X-1, y)+4]
		for i := 0; i < len(row); i += 4 {
			row[i], row[i+1], row[i+2], row[i+3] = c.R, c.G, c.B, c.A
		}
	}
}

// Straight-alpha source-over; cover scales the source alpha (0-255).
func blendNRGBA(pix []uint8, c color.NRGBA, cover uint32) {
	sa := uint32(c.A) * cover
	if sa == 0 {
		return
	}
	if sa == 255*255 {
		pix[0], pix[1], pix[2], pix[3] = c.R, c.G, c.B, 255
		return
	}
	da := uint32(pix[3]) * (255*255 - sa) / 255
	oa := sa + da
	mix := func(s, d uint8) uint8 { return uint8((uint32(s)*sa + uint32(d)*da + oa/2) / oa) }
	pix[0], pix[1], pix[2] = mix(c.R, pix[0]), mix(c.G, pix[1]), mix(c.B, pix[2])
	pix[3] = uint8((oa + 127) / 255)
}

// Decoded images are converted to NRGBA in place so drawing targets one lossless pixel format.
func mutateImage(v runtime.Value, label string, fn func(dst *image.NRGBA) error) error {
	obj, ok := v.(runtime.NativeObject)
	if !ok || obj.Kind != "Image" {
		return fmt.Errorf("%s: argument is not an Image handle", label)
	}
	box, ok := obj.Payload.(*imageBox)
	if !ok {
		return fmt.Errorf("%s: unknown Image handle (closed?)", label)
	}
	box.mu.Lock()
	defer box.mu.Unlock()
	if box.img == nil {
		return fmt.Errorf("%s: unknown Image handle (closed?)", label)
	}
	dst, ok := box.img.(*image.NRGBA)
	if !ok || dst.Bounds().Min != (image.Point{}) {
		dst = cloneNRGBA(box.img)
		box.img = dst
	}
	return fn(dst)
}

type drawPoint struct{ x, y float64 }

type drawPath []drawPoint

func (p drawPath) area() float64 {
	sum := 0.0
	for i := range p {
		j := (i + 1) % len(p)
		sum += p[i].x*p[j].y - p[j].x*p[i].y
	}
	return sum
}

// Same-direction paths union under the non-zero rule; the opposite direction cuts a hole.
func (p drawPath) oriented(positive bool) drawPath {
	if (p.area() > 0) == positive {
		return p
	}
	out := make(drawPath, len(p))
	for i := range p {
		out[i] = p[len(p)-1-i]
	}
	return out
}

func fillPaths(dst *image.NRGBA, paths []drawPath, c color.NRGBA) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range paths {
		for _, pt := range p {
			minX, maxX = math.Min(minX, pt.x), math.Max(maxX, pt.x)
			minY, maxY = math.Min(minY, pt.y), math.Max(maxY, pt.y)
		}
	}
	if minX > maxX {
		return
	}
	clip := func(v float64, hi int) int { return int(math.Max(0, math.Min(float64(hi), v))) }
	b := dst.Bounds()
	box := image.Rect(clip(math.Floor(minX), b.Dx()), clip(math.Floor(minY), b.Dy()), clip(math.Ceil(maxX), b.Dx()), clip(math.Ceil(maxY), b.Dy()))
	if box.Empty() {
		return
	}
	ox, oy := float64(box.Min.X), float64(box.Min.Y)
	r := vector.NewRasterizer(box.Dx(), box.Dy())
	for _, p := range paths {
		if len(p) < 3 {
			continue
		}
		r.MoveTo(float32(p[0].x-ox), float32(p[0].y-oy))
		for _, pt := range p[1:] {
			r.LineTo(float32(pt.x-ox), float32(pt.y-oy))
		}
		r.ClosePath()
	}
	mask := image.NewAlpha(image.Rect(0, 0, box.Dx(), box.Dy()))
	r.DrawOp = xdraw.Src
	r.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	for y := 0; y < box.Dy(); y++ {
		row := dst.Pix[dst.PixOffset(box.Min.X, box.Min.Y+y):]
		for x, cover := range mask.Pix[y*mask.Stride : y*mask.Stride+box.Dx()] {
			if cover != 0 {
				blendNRGBA(row[4*x:4*x+4], c, uint32(cover))
			}
		}
	}
}

func ellipsePath(cx, cy, rx, ry float64) drawPath {
	return arcPoints(cx, cy, rx, ry, 0, 2*math.Pi, false)
}

func arcPoints(cx, cy, rx, ry, from, to float64, includeEnd bool) drawPath {
	steps := int(math.Ceil(math.Abs(to-from) * math.Max(rx, ry) / 1.5))
	steps = max(8, min(steps, 2880))
	n := steps
	if includeEnd {
		n++
	}
	out := make(drawPath, n)
	for i := range out {
		t := from + (to-from)*float64(i)/float64(steps)
		out[i] = drawPoint{cx + rx*math.Cos(t), cy + ry*math.Sin(t)}
	}
	return out
}

func segmentQuad(a, b drawPoint, half float64, capEnds bool) drawPath {
	dx, dy := b.x-a.x, b.y-a.y
	length := math.Hypot(dx, dy)
	if length == 0 {
		dx, dy, length = 1, 0, 1
	}
	ux, uy := dx/length, dy/length
	if capEnds {
		a = drawPoint{a.x - ux*half, a.y - uy*half}
		b = drawPoint{b.x + ux*half, b.y + uy*half}
	}
	nx, ny := -uy*half, ux*half
	return drawPath{{a.x + nx, a.y + ny}, {b.x + nx, b.y + ny}, {b.x - nx, b.y - ny}, {a.x - nx, a.y - ny}}
}

func strokePolyline(points drawPath, closed bool, width float64) []drawPath {
	half := width / 2
	var paths []drawPath
	last := len(points) - 1
	if closed {
		last = len(points)
	}
	for i := 0; i < last; i++ {
		paths = append(paths, segmentQuad(points[i], points[(i+1)%len(points)], half, false).oriented(true))
	}
	for i, pt := range points {
		if !closed && (i == 0 || i == len(points)-1) {
			continue
		}
		paths = append(paths, ellipsePath(pt.x, pt.y, half, half).oriented(true))
	}
	return paths
}

type drawStyle struct {
	stroke    color.NRGBA
	fill      color.NRGBA
	hasStroke bool
	hasFill   bool
	width     float64
}

func imageDrawStyle(opts runtime.Dict, label string) (drawStyle, error) {
	var st drawStyle
	var err error
	if st.fill, st.hasFill, err = imageOptColor(opts, "fill", label); err != nil {
		return st, err
	}
	if st.stroke, st.hasStroke, err = imageOptColor(opts, "color", label); err != nil {
		return st, err
	}
	if st.width, err = imageOptFloat(opts, "width", 1, label); err != nil {
		return st, err
	}
	if st.width <= 0 {
		return st, fmt.Errorf("%s: option \"width\" must be positive", label)
	}
	if !st.hasFill && !st.hasStroke {
		st.stroke, st.hasStroke = color.NRGBA{A: 255}, true
	}
	return st, nil
}

func imagePointList(v runtime.Value, label string) (drawPath, error) {
	list, ok := v.(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s: points must be a list of [x, y] pairs", label)
	}
	out := make(drawPath, len(list.Elements))
	for i, el := range list.Elements {
		pair, ok := el.(*runtime.List)
		if !ok || len(pair.Elements) != 2 {
			return nil, fmt.Errorf("%s: points must be a list of [x, y] pairs", label)
		}
		xy, err := imageFloats(pair.Elements, label)
		if err != nil {
			return nil, err
		}
		out[i] = drawPoint{xy[0], xy[1]}
	}
	return out, nil
}

func registerImageDraw(r *Registry) {
	r.Register("imagenative", "copy", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("image.copy expects (handle)")
		}
		img, err := imageFromHandle(args[0], "image.copy")
		if err != nil {
			return nil, err
		}
		return putImage(cloneNRGBA(img)), nil
	})

	r.Register("imagenative", "flip", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("image.flip expects (handle, direction)")
		}
		img, err := imageFromHandle(args[0], "image.flip")
		if err != nil {
			return nil, err
		}
		dir, _ := args[1].(runtime.String)
		if dir.Value != "h" && dir.Value != "v" {
			return nil, fmt.Errorf("image.flip: direction must be \"h\" or \"v\"")
		}
		src := cloneNRGBA(img)
		w, h := src.Bounds().Dx(), src.Bounds().Dy()
		dst := image.NewNRGBA(src.Bounds())
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				sx, sy := x, y
				if dir.Value == "h" {
					sx = w - 1 - x
				} else {
					sy = h - 1 - y
				}
				dst.SetNRGBA(x, y, src.NRGBAAt(sx, sy))
			}
		}
		return putImage(dst), nil
	})

	r.Register("imagenative", "getPixel", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("image.getPixel expects (handle, x, y)")
		}
		img, err := imageFromHandle(args[0], "image.getPixel")
		if err != nil {
			return nil, err
		}
		x, ok1 := AsInt64(args[1])
		y, ok2 := AsInt64(args[2])
		b := img.Bounds()
		if !ok1 || !ok2 || x < 0 || y < 0 || x >= int64(b.Dx()) || y >= int64(b.Dy()) {
			return nil, fmt.Errorf("image.getPixel: (x, y) is outside the image")
		}
		c := color.NRGBAModel.Convert(img.At(b.Min.X+int(x), b.Min.Y+int(y))).(color.NRGBA)
		return runtime.String{Value: formatImageColor(c)}, nil
	})

	r.Register("imagenative", "setPixel", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 4 {
			return nil, fmt.Errorf("image.setPixel expects (handle, x, y, color)")
		}
		c, err := imageColorArg(args[3], "image.setPixel")
		if err != nil {
			return nil, err
		}
		x, ok1 := AsInt64(args[1])
		y, ok2 := AsInt64(args[2])
		return runtime.Null{}, mutateImage(args[0], "image.setPixel", func(dst *image.NRGBA) error {
			b := dst.Bounds()
			if !ok1 || !ok2 || x < 0 || y < 0 || x >= int64(b.Dx()) || y >= int64(b.Dy()) {
				return fmt.Errorf("image.setPixel: (x, y) is outside the image")
			}
			dst.SetNRGBA(int(x), int(y), c)
			return nil
		})
	})

	r.Register("imagenative", "fill", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("image.fill expects (handle, color)")
		}
		c, err := imageColorArg(args[1], "image.fill")
		if err != nil {
			return nil, err
		}
		return runtime.Null{}, mutateImage(args[0], "image.fill", func(dst *image.NRGBA) error {
			fillRectNRGBA(dst, dst.Bounds(), c)
			return nil
		})
	})

	r.Register("imagenative", "line", func(args []runtime.Value) (runtime.Value, error) {
		const label = "image.line"
		if len(args) < 5 || len(args) > 6 {
			return nil, fmt.Errorf("%s expects (handle, x1, y1, x2, y2, options)", label)
		}
		xy, err := imageFloats(args[1:5], label)
		if err != nil {
			return nil, err
		}
		opts, err := imageOpts(args, 5, label)
		if err != nil {
			return nil, err
		}
		st, err := imageDrawStyle(opts, label)
		if err != nil {
			return nil, err
		}
		if !st.hasStroke {
			st.stroke = st.fill
		}
		// Integer endpoints name pixel centres so axis-aligned lines stay crisp.
		a := drawPoint{xy[0] + 0.5, xy[1] + 0.5}
		b := drawPoint{xy[2] + 0.5, xy[3] + 0.5}
		return runtime.Null{}, mutateImage(args[0], label, func(dst *image.NRGBA) error {
			fillPaths(dst, []drawPath{segmentQuad(a, b, st.width/2, true)}, st.stroke)
			return nil
		})
	})

	r.Register("imagenative", "rect", func(args []runtime.Value) (runtime.Value, error) {
		const label = "image.rect"
		if len(args) < 5 || len(args) > 6 {
			return nil, fmt.Errorf("%s expects (handle, x, y, width, height, options)", label)
		}
		v, err := imageFloats(args[1:5], label)
		if err != nil {
			return nil, err
		}
		if v[2] <= 0 || v[3] <= 0 {
			return nil, fmt.Errorf("%s: width and height must be positive", label)
		}
		opts, err := imageOpts(args, 5, label)
		if err != nil {
			return nil, err
		}
		st, err := imageDrawStyle(opts, label)
		if err != nil {
			return nil, err
		}
		box := func(inset float64) drawPath {
			return drawPath{{v[0] + inset, v[1] + inset}, {v[0] + v[2] - inset, v[1] + inset}, {v[0] + v[2] - inset, v[1] + v[3] - inset}, {v[0] + inset, v[1] + v[3] - inset}}
		}
		return runtime.Null{}, mutateImage(args[0], label, func(dst *image.NRGBA) error {
			if st.hasFill {
				fillPaths(dst, []drawPath{box(0)}, st.fill)
			}
			if st.hasStroke {
				paths := []drawPath{box(0).oriented(true)}
				if 2*st.width < math.Min(v[2], v[3]) {
					paths = append(paths, box(st.width).oriented(false))
				}
				fillPaths(dst, paths, st.stroke)
			}
			return nil
		})
	})

	r.Register("imagenative", "ellipse", func(args []runtime.Value) (runtime.Value, error) {
		const label = "image.ellipse"
		if len(args) < 5 || len(args) > 6 {
			return nil, fmt.Errorf("%s expects (handle, cx, cy, rx, ry, options)", label)
		}
		v, err := imageFloats(args[1:5], label)
		if err != nil {
			return nil, err
		}
		if v[2] <= 0 || v[3] <= 0 {
			return nil, fmt.Errorf("%s: radii must be positive", label)
		}
		opts, err := imageOpts(args, 5, label)
		if err != nil {
			return nil, err
		}
		st, err := imageDrawStyle(opts, label)
		if err != nil {
			return nil, err
		}
		return runtime.Null{}, mutateImage(args[0], label, func(dst *image.NRGBA) error {
			if st.hasFill {
				fillPaths(dst, []drawPath{ellipsePath(v[0], v[1], v[2], v[3])}, st.fill)
			}
			if st.hasStroke {
				half := st.width / 2
				paths := []drawPath{ellipsePath(v[0], v[1], v[2]+half, v[3]+half).oriented(true)}
				if v[2] > half && v[3] > half {
					paths = append(paths, ellipsePath(v[0], v[1], v[2]-half, v[3]-half).oriented(false))
				}
				fillPaths(dst, paths, st.stroke)
			}
			return nil
		})
	})

	r.Register("imagenative", "arc", func(args []runtime.Value) (runtime.Value, error) {
		const label = "image.arc"
		if len(args) < 6 || len(args) > 7 {
			return nil, fmt.Errorf("%s expects (handle, cx, cy, radius, startDegrees, endDegrees, options)", label)
		}
		v, err := imageFloats(args[1:6], label)
		if err != nil {
			return nil, err
		}
		if v[2] <= 0 {
			return nil, fmt.Errorf("%s: radius must be positive", label)
		}
		opts, err := imageOpts(args, 6, label)
		if err != nil {
			return nil, err
		}
		st, err := imageDrawStyle(opts, label)
		if err != nil {
			return nil, err
		}
		from, to := v[3]*math.Pi/180, v[4]*math.Pi/180
		if math.Abs(to-from) > 2*math.Pi {
			to = from + 2*math.Pi
		}
		return runtime.Null{}, mutateImage(args[0], label, func(dst *image.NRGBA) error {
			if st.hasFill {
				pie := append(drawPath{{v[0], v[1]}}, arcPoints(v[0], v[1], v[2], v[2], from, to, true)...)
				fillPaths(dst, []drawPath{pie}, st.fill)
			}
			if st.hasStroke {
				half := st.width / 2
				band := arcPoints(v[0], v[1], v[2]+half, v[2]+half, from, to, true)
				inner := math.Max(v[2]-half, 0)
				band = append(band, arcPoints(v[0], v[1], inner, inner, to, from, true)...)
				fillPaths(dst, []drawPath{band}, st.stroke)
			}
			return nil
		})
	})

	r.Register("imagenative", "polygon", func(args []runtime.Value) (runtime.Value, error) {
		const label = "image.polygon"
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("%s expects (handle, points, options)", label)
		}
		points, err := imagePointList(args[1], label)
		if err != nil {
			return nil, err
		}
		opts, err := imageOpts(args, 2, label)
		if err != nil {
			return nil, err
		}
		st, err := imageDrawStyle(opts, label)
		if err != nil {
			return nil, err
		}
		closed := true
		if v, ok := dictLookup(opts, "closed"); ok {
			b, isBool := v.(runtime.Bool)
			if !isBool {
				return nil, fmt.Errorf("%s: option \"closed\" must be a bool", label)
			}
			closed = b.Value
		}
		if len(points) < 2 || (closed && len(points) < 3) {
			return nil, fmt.Errorf("%s: needs at least 3 points (2 when \"closed\" is false)", label)
		}
		return runtime.Null{}, mutateImage(args[0], label, func(dst *image.NRGBA) error {
			if st.hasFill {
				fillPaths(dst, []drawPath{points}, st.fill)
			}
			if st.hasStroke {
				fillPaths(dst, strokePolyline(points, closed, st.width), st.stroke)
			}
			return nil
		})
	})

	r.Register("imagenative", "paste", func(args []runtime.Value) (runtime.Value, error) {
		const label = "image.paste"
		if len(args) < 4 || len(args) > 5 {
			return nil, fmt.Errorf("%s expects (handle, source, x, y, options)", label)
		}
		src, err := imageFromHandle(args[1], label)
		if err != nil {
			return nil, err
		}
		x, ok1 := AsInt64(args[2])
		y, ok2 := AsInt64(args[3])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("%s: x and y must be ints", label)
		}
		opts, err := imageOpts(args, 4, label)
		if err != nil {
			return nil, err
		}
		opacity, err := imageOptFloat(opts, "opacity", 1, label)
		if err != nil {
			return nil, err
		}
		if opacity < 0 || opacity > 1 {
			return nil, fmt.Errorf("%s: option \"opacity\" must be between 0 and 1", label)
		}
		// Snapshot first: pasting an image onto itself must not read pixels it is writing.
		stamp := cloneNRGBA(src)
		cover := uint32(math.Round(opacity * 255))
		return runtime.Null{}, mutateImage(args[0], label, func(dst *image.NRGBA) error {
			target := stamp.Bounds().Add(image.Pt(int(x), int(y))).Intersect(dst.Bounds())
			for py := target.Min.Y; py < target.Max.Y; py++ {
				for px := target.Min.X; px < target.Max.X; px++ {
					at := dst.PixOffset(px, py)
					blendNRGBA(dst.Pix[at:at+4], stamp.NRGBAAt(px-int(x), py-int(y)), cover)
				}
			}
			return nil
		})
	})
}
