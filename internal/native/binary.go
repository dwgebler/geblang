package native

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"strings"

	"geblang/internal/runtime"
)

type binaryEndian int

const (
	binaryBig binaryEndian = iota
	binaryLittle
	binaryNative
)

type binaryFieldKind int

const (
	bfInt8 binaryFieldKind = iota
	bfUint8
	bfInt16
	bfUint16
	bfInt32
	bfUint32
	bfInt64
	bfUint64
	bfFloat32
	bfFloat64
	bfFixedString
	bfPad
)

type binaryField struct {
	kind   binaryFieldKind
	count  int // for fixed string and pad: byte count; otherwise 1
	endian binaryEndian
}

func (f binaryField) byteSize() int {
	switch f.kind {
	case bfInt8, bfUint8:
		return 1
	case bfInt16, bfUint16:
		return 2
	case bfInt32, bfUint32, bfFloat32:
		return 4
	case bfInt64, bfUint64, bfFloat64:
		return 8
	case bfFixedString, bfPad:
		return f.count
	}
	return 0
}

func (f binaryField) takesArg() bool {
	return f.kind != bfPad
}

func (f binaryField) order() binary.ByteOrder {
	switch f.endian {
	case binaryLittle:
		return binary.LittleEndian
	case binaryNative:
		return nativeByteOrder()
	}
	return binary.BigEndian
}

func nativeByteOrder() binary.ByteOrder {
	probe := [2]byte{}
	binary.NativeEndian.PutUint16(probe[:], 1)
	if probe[0] == 1 {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

func parseBinaryFormat(format string) ([]binaryField, error) {
	endian := binaryBig
	i := 0
	if len(format) > 0 {
		switch format[0] {
		case '>', '!':
			endian = binaryBig
			i = 1
		case '<':
			endian = binaryLittle
			i = 1
		case '=':
			endian = binaryNative
			i = 1
		}
	}
	var fields []binaryField
	for i < len(format) {
		count := 0
		for i < len(format) && format[i] >= '0' && format[i] <= '9' {
			count = count*10 + int(format[i]-'0')
			i++
		}
		if i >= len(format) {
			return nil, fmt.Errorf("binary format: trailing count without type code")
		}
		code := format[i]
		i++
		kind, isFixedStr, isPad, err := lookupBinaryKind(code)
		if err != nil {
			return nil, err
		}
		switch {
		case isFixedStr:
			if count <= 0 {
				return nil, fmt.Errorf("binary format: 's' requires a positive byte count (e.g. \"10s\")")
			}
			fields = append(fields, binaryField{kind: bfFixedString, count: count, endian: endian})
		case isPad:
			if count == 0 {
				count = 1
			}
			fields = append(fields, binaryField{kind: bfPad, count: count, endian: endian})
		default:
			if count == 0 {
				count = 1
			}
			for r := 0; r < count; r++ {
				fields = append(fields, binaryField{kind: kind, count: 1, endian: endian})
			}
		}
	}
	return fields, nil
}

func lookupBinaryKind(code byte) (binaryFieldKind, bool, bool, error) {
	switch code {
	case 'b':
		return bfInt8, false, false, nil
	case 'B':
		return bfUint8, false, false, nil
	case 'h':
		return bfInt16, false, false, nil
	case 'H':
		return bfUint16, false, false, nil
	case 'i':
		return bfInt32, false, false, nil
	case 'I':
		return bfUint32, false, false, nil
	case 'q':
		return bfInt64, false, false, nil
	case 'Q':
		return bfUint64, false, false, nil
	case 'f':
		return bfFloat32, false, false, nil
	case 'd':
		return bfFloat64, false, false, nil
	case 's':
		return bfFixedString, true, false, nil
	case 'x':
		return bfPad, false, true, nil
	}
	return 0, false, false, fmt.Errorf("binary format: unknown type code %q", code)
}

func toUnsigned(value runtime.Value, max uint64, label string) (uint64, error) {
	switch v := value.(type) {
	case runtime.SmallInt:
		if v.Value < 0 {
			return 0, fmt.Errorf("%s: negative value not allowed for unsigned field", label)
		}
		u := uint64(v.Value)
		if u > max {
			return 0, fmt.Errorf("%s: value %d out of range for unsigned field (max %d)", label, v.Value, max)
		}
		return u, nil
	case runtime.Int:
		if v.Value.Sign() < 0 {
			return 0, fmt.Errorf("%s: negative value not allowed for unsigned field", label)
		}
		if v.Value.BitLen() > 64 {
			return 0, fmt.Errorf("%s: value out of range for unsigned field", label)
		}
		u := v.Value.Uint64()
		if u > max {
			return 0, fmt.Errorf("%s: value out of range for unsigned field (max %d)", label, max)
		}
		return u, nil
	}
	return 0, fmt.Errorf("%s: expected int, got %s", label, value.TypeName())
}

func toSigned(value runtime.Value, minV, maxV int64, label string) (int64, error) {
	switch v := value.(type) {
	case runtime.SmallInt:
		if v.Value < minV || v.Value > maxV {
			return 0, fmt.Errorf("%s: value %d out of range (%d to %d)", label, v.Value, minV, maxV)
		}
		return v.Value, nil
	case runtime.Int:
		if !v.Value.IsInt64() {
			return 0, fmt.Errorf("%s: value out of range", label)
		}
		n := v.Value.Int64()
		if n < minV || n > maxV {
			return 0, fmt.Errorf("%s: value %d out of range (%d to %d)", label, n, minV, maxV)
		}
		return n, nil
	}
	return 0, fmt.Errorf("%s: expected int, got %s", label, value.TypeName())
}

func toFloat(value runtime.Value, label string) (float64, error) {
	switch v := value.(type) {
	case runtime.Float:
		return v.Value, nil
	case runtime.SmallInt:
		return float64(v.Value), nil
	case runtime.Int:
		f, _ := new(big.Float).SetInt(v.Value).Float64()
		return f, nil
	}
	return 0, fmt.Errorf("%s: expected float, got %s", label, value.TypeName())
}

func toFixedBytes(value runtime.Value, n int, label string) ([]byte, error) {
	var src []byte
	switch v := value.(type) {
	case runtime.String:
		src = []byte(v.Value)
	case runtime.Bytes:
		src = v.Value
	default:
		return nil, fmt.Errorf("%s: expected string or bytes, got %s", label, value.TypeName())
	}
	out := make([]byte, n)
	if len(src) > n {
		return nil, fmt.Errorf("%s: value of %d bytes exceeds fixed length %d", label, len(src), n)
	}
	copy(out, src)
	return out, nil
}

func packFields(fields []binaryField, args []runtime.Value, label string) ([]byte, error) {
	expected := 0
	for _, f := range fields {
		if f.takesArg() {
			expected++
		}
	}
	if len(args) != expected {
		return nil, fmt.Errorf("%s: format expects %d argument(s), got %d", label, expected, len(args))
	}
	buf := bytes.NewBuffer(make([]byte, 0, binarySize(fields)))
	ai := 0
	for _, f := range fields {
		ord := f.order()
		switch f.kind {
		case bfInt8:
			n, err := toSigned(args[ai], math.MinInt8, math.MaxInt8, label)
			if err != nil {
				return nil, err
			}
			buf.WriteByte(byte(int8(n)))
		case bfUint8:
			n, err := toUnsigned(args[ai], math.MaxUint8, label)
			if err != nil {
				return nil, err
			}
			buf.WriteByte(byte(n))
		case bfInt16:
			n, err := toSigned(args[ai], math.MinInt16, math.MaxInt16, label)
			if err != nil {
				return nil, err
			}
			var tmp [2]byte
			ord.PutUint16(tmp[:], uint16(int16(n)))
			buf.Write(tmp[:])
		case bfUint16:
			n, err := toUnsigned(args[ai], math.MaxUint16, label)
			if err != nil {
				return nil, err
			}
			var tmp [2]byte
			ord.PutUint16(tmp[:], uint16(n))
			buf.Write(tmp[:])
		case bfInt32:
			n, err := toSigned(args[ai], math.MinInt32, math.MaxInt32, label)
			if err != nil {
				return nil, err
			}
			var tmp [4]byte
			ord.PutUint32(tmp[:], uint32(int32(n)))
			buf.Write(tmp[:])
		case bfUint32:
			n, err := toUnsigned(args[ai], math.MaxUint32, label)
			if err != nil {
				return nil, err
			}
			var tmp [4]byte
			ord.PutUint32(tmp[:], uint32(n))
			buf.Write(tmp[:])
		case bfInt64:
			n, err := toSigned(args[ai], math.MinInt64, math.MaxInt64, label)
			if err != nil {
				return nil, err
			}
			var tmp [8]byte
			ord.PutUint64(tmp[:], uint64(n))
			buf.Write(tmp[:])
		case bfUint64:
			n, err := toUnsigned(args[ai], math.MaxUint64, label)
			if err != nil {
				return nil, err
			}
			var tmp [8]byte
			ord.PutUint64(tmp[:], n)
			buf.Write(tmp[:])
		case bfFloat32:
			f, err := toFloat(args[ai], label)
			if err != nil {
				return nil, err
			}
			var tmp [4]byte
			ord.PutUint32(tmp[:], math.Float32bits(float32(f)))
			buf.Write(tmp[:])
		case bfFloat64:
			f, err := toFloat(args[ai], label)
			if err != nil {
				return nil, err
			}
			var tmp [8]byte
			ord.PutUint64(tmp[:], math.Float64bits(f))
			buf.Write(tmp[:])
		case bfFixedString:
			data, err := toFixedBytes(args[ai], f.count, label)
			if err != nil {
				return nil, err
			}
			buf.Write(data)
		case bfPad:
			buf.Write(make([]byte, f.count))
			continue
		}
		ai++
	}
	return buf.Bytes(), nil
}

func unpackFields(fields []binaryField, data []byte, label string) ([]runtime.Value, error) {
	if len(data) < binarySize(fields) {
		return nil, fmt.Errorf("%s: need %d byte(s), got %d", label, binarySize(fields), len(data))
	}
	r := bytes.NewReader(data)
	var out []runtime.Value
	for _, f := range fields {
		ord := f.order()
		switch f.kind {
		case bfInt8:
			b, _ := r.ReadByte()
			out = append(out, runtime.SmallInt{Value: int64(int8(b))})
		case bfUint8:
			b, _ := r.ReadByte()
			out = append(out, runtime.SmallInt{Value: int64(b)})
		case bfInt16:
			var tmp [2]byte
			r.Read(tmp[:])
			out = append(out, runtime.SmallInt{Value: int64(int16(ord.Uint16(tmp[:])))})
		case bfUint16:
			var tmp [2]byte
			r.Read(tmp[:])
			out = append(out, runtime.SmallInt{Value: int64(ord.Uint16(tmp[:]))})
		case bfInt32:
			var tmp [4]byte
			r.Read(tmp[:])
			out = append(out, runtime.SmallInt{Value: int64(int32(ord.Uint32(tmp[:])))})
		case bfUint32:
			var tmp [4]byte
			r.Read(tmp[:])
			out = append(out, runtime.SmallInt{Value: int64(ord.Uint32(tmp[:]))})
		case bfInt64:
			var tmp [8]byte
			r.Read(tmp[:])
			out = append(out, runtime.SmallInt{Value: int64(ord.Uint64(tmp[:]))})
		case bfUint64:
			var tmp [8]byte
			r.Read(tmp[:])
			u := ord.Uint64(tmp[:])
			if u <= math.MaxInt64 {
				out = append(out, runtime.SmallInt{Value: int64(u)})
			} else {
				out = append(out, runtime.Int{Value: new(big.Int).SetUint64(u)})
			}
		case bfFloat32:
			var tmp [4]byte
			r.Read(tmp[:])
			out = append(out, runtime.Float{Value: float64(math.Float32frombits(ord.Uint32(tmp[:])))})
		case bfFloat64:
			var tmp [8]byte
			r.Read(tmp[:])
			out = append(out, runtime.Float{Value: math.Float64frombits(ord.Uint64(tmp[:]))})
		case bfFixedString:
			tmp := make([]byte, f.count)
			r.Read(tmp)
			out = append(out, runtime.Bytes{Value: tmp})
		case bfPad:
			r.Seek(int64(f.count), 1)
		}
	}
	return out, nil
}

func binarySize(fields []binaryField) int {
	n := 0
	for _, f := range fields {
		n += f.byteSize()
	}
	return n
}

func registerBinary(r *Registry) {
	r.Register("binary", "pack", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("binary.pack expects format string and values")
		}
		format, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("binary.pack: format must be string")
		}
		fields, err := parseBinaryFormat(format.Value)
		if err != nil {
			return nil, err
		}
		data, err := packFields(fields, args[1:], "binary.pack")
		if err != nil {
			return nil, err
		}
		return runtime.Bytes{Value: data}, nil
	})
	r.Register("binary", "unpack", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("binary.unpack expects format and bytes")
		}
		format, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("binary.unpack: format must be string")
		}
		data, err := bytesArg(args[1], "binary.unpack")
		if err != nil {
			return nil, err
		}
		fields, err := parseBinaryFormat(format.Value)
		if err != nil {
			return nil, err
		}
		values, err := unpackFields(fields, data, "binary.unpack")
		if err != nil {
			return nil, err
		}
		return &runtime.List{Elements: values}, nil
	})
	r.Register("binary", "unpackNamed", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("binary.unpackNamed expects spec list and bytes")
		}
		spec, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("binary.unpackNamed: spec must be a list")
		}
		data, err := bytesArg(args[1], "binary.unpackNamed")
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(spec.Elements))
		var combined strings.Builder
		for i, elem := range spec.Elements {
			entry, ok := elem.(runtime.Dict)
			if !ok {
				return nil, fmt.Errorf("binary.unpackNamed: spec[%d] must be a dict", i)
			}
			name, err := dictStringField(entry, "name")
			if err != nil {
				return nil, fmt.Errorf("binary.unpackNamed: spec[%d]: %v", i, err)
			}
			typ, err := dictStringField(entry, "type")
			if err != nil {
				return nil, fmt.Errorf("binary.unpackNamed: spec[%d]: %v", i, err)
			}
			names = append(names, name)
			if i > 0 && (typ[0] == '<' || typ[0] == '>' || typ[0] == '=' || typ[0] == '!') {
				return nil, fmt.Errorf("binary.unpackNamed: spec[%d]: endianness prefix only allowed on first field", i)
			}
			combined.WriteString(typ)
		}
		fields, err := parseBinaryFormat(combined.String())
		if err != nil {
			return nil, err
		}
		nonPad := 0
		for _, f := range fields {
			if f.takesArg() {
				nonPad++
			}
		}
		if nonPad != len(names) {
			return nil, fmt.Errorf("binary.unpackNamed: spec produced %d non-pad fields but %d names", nonPad, len(names))
		}
		values, err := unpackFields(fields, data, "binary.unpackNamed")
		if err != nil {
			return nil, err
		}
		entries := make(map[string]runtime.DictEntry, len(names))
		vi := 0
		for _, f := range fields {
			if !f.takesArg() {
				continue
			}
			keyVal := runtime.String{Value: names[vi]}
			entries[DictKey(keyVal)] = runtime.DictEntry{Key: keyVal, Value: values[vi]}
			vi++
		}
		return runtime.DictFromEntries(entries, nil), nil
	})
	r.Register("binary", "size", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("binary.size expects format string")
		}
		format, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("binary.size: format must be string")
		}
		fields, err := parseBinaryFormat(format.Value)
		if err != nil {
			return nil, err
		}
		return runtime.SmallInt{Value: int64(binarySize(fields))}, nil
	})
}

func bytesArg(value runtime.Value, label string) ([]byte, error) {
	switch v := value.(type) {
	case runtime.Bytes:
		return v.Value, nil
	case runtime.String:
		return []byte(v.Value), nil
	}
	return nil, fmt.Errorf("%s: expected bytes, got %s", label, value.TypeName())
}
