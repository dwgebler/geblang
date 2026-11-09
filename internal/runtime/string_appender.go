package runtime

import (
	"unsafe"
	"weak"
)

// minAppendBuffer is the result size below which plain concatenation is faster than buffer reuse.
const minAppendBuffer = 256

const appenderSlots = 4

// appendSlot refers to its buffer weakly, so the appender never keeps alive storage only it still remembers.
type appendSlot struct {
	data     weak.Pointer[byte]
	length   int
	capacity int
}

// StringAppender makes `s = s + x` loops linear by appending into spare buffer capacity; earlier strings stay valid prefixes. Not safe for concurrent use.
type StringAppender struct {
	slots [appenderSlots]appendSlot
	next  int
}

func (a *StringAppender) Concat(left, right string) string {
	total := len(left) + len(right)
	if total < minAppendBuffer || len(left) == 0 || len(right) == 0 {
		return left + right
	}
	leftData := unsafe.StringData(left)
	for i := range a.slots {
		slot := &a.slots[i]
		if slot.length != len(left) {
			continue
		}
		data := slot.data.Value()
		if data == nil || data != leftData {
			continue
		}
		buf := unsafe.Slice(data, slot.capacity)[:slot.length]
		if slot.capacity-slot.length < len(right) {
			grown := make([]byte, len(buf), total+total/2)
			copy(grown, buf)
			buf = grown
		}
		buf = append(buf, right...)
		a.store(i, buf)
		return unsafe.String(unsafe.SliceData(buf), len(buf))
	}
	buf := make([]byte, 0, total+total/2)
	buf = append(append(buf, left...), right...)
	a.store(a.next, buf)
	a.next = (a.next + 1) % appenderSlots
	return unsafe.String(unsafe.SliceData(buf), len(buf))
}

func (a *StringAppender) store(i int, buf []byte) {
	a.slots[i] = appendSlot{data: weak.Make(unsafe.SliceData(buf)), length: len(buf), capacity: cap(buf)}
}

func (a *StringAppender) Reset() {
	*a = StringAppender{}
}
