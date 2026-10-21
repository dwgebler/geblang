package native

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"sync"

	"geblang/internal/runtime"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register webp decoder
)

// imageBox is an Image handle's payload; close empties it. The handle owns
// the box, so the image is freed with the last reference to the handle.
type imageBox struct {
	mu  sync.Mutex
	img image.Image
}

func putImage(img image.Image) runtime.Value {
	return runtime.NativeObject{Kind: "Image", ID: nextSyncID(), Payload: &imageBox{img: img}}
}

func imageFromHandle(v runtime.Value, label string) (image.Image, error) {
	obj, ok := v.(runtime.NativeObject)
	if !ok || obj.Kind != "Image" {
		return nil, fmt.Errorf("%s: argument is not an Image handle", label)
	}
	box, ok := obj.Payload.(*imageBox)
	if !ok {
		return nil, fmt.Errorf("%s: unknown Image handle (closed?)", label)
	}
	box.mu.Lock()
	img := box.img
	box.mu.Unlock()
	if img == nil {
		return nil, fmt.Errorf("%s: unknown Image handle (closed?)", label)
	}
	return img, nil
}

func registerImage(r *Registry) {
	r.Register("imagenative", "blank", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("image.blank expects (width, height)")
		}
		w, ok1 := AsInt64(args[0])
		h, ok2 := AsInt64(args[1])
		if !ok1 || !ok2 || w <= 0 || h <= 0 {
			return nil, fmt.Errorf("image.blank: width and height must be positive ints")
		}
		return putImage(image.NewRGBA(image.Rect(0, 0, int(w), int(h)))), nil
	})

	r.Register("imagenative", "decode", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleBytes(args, "image.decode")
		if err != nil {
			return nil, err
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("image.decode: %w", err)
		}
		return putImage(img), nil
	})

	r.Register("imagenative", "width", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("image.width expects (handle)")
		}
		img, err := imageFromHandle(args[0], "image.width")
		if err != nil {
			return nil, err
		}
		return runtime.SmallInt{Value: int64(img.Bounds().Dx())}, nil
	})

	r.Register("imagenative", "height", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("image.height expects (handle)")
		}
		img, err := imageFromHandle(args[0], "image.height")
		if err != nil {
			return nil, err
		}
		return runtime.SmallInt{Value: int64(img.Bounds().Dy())}, nil
	})

	r.Register("imagenative", "resize", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("image.resize expects (handle, width, height)")
		}
		img, err := imageFromHandle(args[0], "image.resize")
		if err != nil {
			return nil, err
		}
		w, ok1 := AsInt64(args[1])
		h, ok2 := AsInt64(args[2])
		if !ok1 || !ok2 || w <= 0 || h <= 0 {
			return nil, fmt.Errorf("image.resize: width and height must be positive ints")
		}
		dst := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), xdraw.Over, nil)
		return putImage(dst), nil
	})

	r.Register("imagenative", "crop", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 5 {
			return nil, fmt.Errorf("image.crop expects (handle, x, y, width, height)")
		}
		img, err := imageFromHandle(args[0], "image.crop")
		if err != nil {
			return nil, err
		}
		x, _ := AsInt64(args[1])
		y, _ := AsInt64(args[2])
		w, _ := AsInt64(args[3])
		h, _ := AsInt64(args[4])
		b := img.Bounds()
		rect := image.Rect(b.Min.X+int(x), b.Min.Y+int(y), b.Min.X+int(x+w), b.Min.Y+int(y+h))
		rect = rect.Intersect(b)
		if rect.Empty() {
			return nil, fmt.Errorf("image.crop: rectangle is outside the image")
		}
		dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
		xdraw.Draw(dst, dst.Bounds(), img, rect.Min, xdraw.Src)
		return putImage(dst), nil
	})

	r.Register("imagenative", "rotate", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("image.rotate expects (handle, degrees)")
		}
		img, err := imageFromHandle(args[0], "image.rotate")
		if err != nil {
			return nil, err
		}
		deg, _ := AsInt64(args[1])
		deg = ((deg % 360) + 360) % 360
		if deg%90 != 0 {
			return nil, fmt.Errorf("image.rotate: only multiples of 90 are supported")
		}
		return putImage(rotate90s(img, int(deg))), nil
	})

	r.Register("imagenative", "encode", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("image.encode expects (handle, format)")
		}
		img, err := imageFromHandle(args[0], "image.encode")
		if err != nil {
			return nil, err
		}
		format, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("image.encode: format must be a string")
		}
		var buf bytes.Buffer
		switch format.Value {
		case "png":
			err = png.Encode(&buf, img)
		case "jpeg", "jpg":
			err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
		case "gif":
			err = gif.Encode(&buf, img, nil)
		default:
			return nil, fmt.Errorf("image.encode: unsupported format %q (png, jpeg, gif)", format.Value)
		}
		if err != nil {
			return nil, fmt.Errorf("image.encode: %w", err)
		}
		return runtime.Bytes{Value: buf.Bytes()}, nil
	})

	r.Register("imagenative", "close", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("image.close expects (handle)")
		}
		obj, ok := args[0].(runtime.NativeObject)
		if !ok || obj.Kind != "Image" {
			return nil, fmt.Errorf("image.close: argument is not an Image handle")
		}
		if box, ok := obj.Payload.(*imageBox); ok {
			box.mu.Lock()
			box.img = nil
			box.mu.Unlock()
		}
		return runtime.Null{}, nil
	})
}

func rotate90s(src image.Image, deg int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	var dst *image.RGBA
	switch deg {
	case 0:
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.Draw(dst, dst.Bounds(), src, b.Min, xdraw.Src)
		return dst
	case 180:
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(w-1-x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
	default: // 90 or 270
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				c := src.At(b.Min.X+x, b.Min.Y+y)
				if deg == 90 {
					dst.Set(h-1-y, x, c)
				} else {
					dst.Set(y, w-1-x, c)
				}
			}
		}
	}
	return dst
}
