package runtime

// Copies share this backing store through growth and map promotion.
const dictInlineMax = 8

type dictItem struct {
	key   string
	entry DictEntry
}

// dictSlot records an entry's index in order, so a delete can tombstone it instead of shifting the slice.
type dictSlot struct {
	entry DictEntry
	pos   int
}

// Encoded keys always start with a type prefix, so "" marks a deleted order position.
const dictTombstone = ""

type dictData struct {
	items         []dictItem
	m             map[string]dictSlot
	order         []string
	dead          int
	nonStringKeys int
}

func (d *dictData) length() int {
	if d.m != nil {
		return len(d.m)
	}
	return len(d.items)
}

func (d *dictData) get(key string) (DictEntry, bool) {
	if d.m != nil {
		slot, ok := d.m[key]
		return slot.entry, ok
	}
	for _, item := range d.items {
		if item.key == key {
			return item.entry, true
		}
	}
	return DictEntry{}, false
}

func (d *dictData) replaceKeyType(old, next Value) {
	if old != nil {
		if _, ok := old.(String); !ok {
			d.nonStringKeys--
		}
	}
	if next != nil {
		if _, ok := next.(String); !ok {
			d.nonStringKeys++
		}
	}
}

func (d *dictData) set(key string, entry DictEntry) {
	if d.m != nil {
		old, hit := d.m[key]
		if !hit {
			old.pos = len(d.order)
			d.order = append(d.order, key)
		}
		d.replaceKeyType(old.entry.Key, entry.Key)
		d.m[key] = dictSlot{entry: entry, pos: old.pos}
		return
	}
	for i := range d.items {
		if d.items[i].key == key {
			d.replaceKeyType(d.items[i].entry.Key, entry.Key)
			d.items[i].entry = entry
			return
		}
	}
	d.replaceKeyType(nil, entry.Key)
	if len(d.items) < dictInlineMax {
		d.items = append(d.items, dictItem{key, entry})
		return
	}
	d.m = make(map[string]dictSlot, dictInlineMax*2)
	d.order = make([]string, 0, dictInlineMax*2)
	for _, item := range d.items {
		d.m[item.key] = dictSlot{entry: item.entry, pos: len(d.order)}
		d.order = append(d.order, item.key)
	}
	d.items = nil
	d.m[key] = dictSlot{entry: entry, pos: len(d.order)}
	d.order = append(d.order, key)
}

func (d *dictData) del(key string) {
	if d.m != nil {
		old, hit := d.m[key]
		if !hit {
			return
		}
		d.replaceKeyType(old.entry.Key, nil)
		delete(d.m, key)
		d.order[old.pos] = dictTombstone
		d.dead++
		if d.dead > 32 && d.dead > len(d.m) {
			d.compactOrder()
		}
		return
	}
	for i, item := range d.items {
		if item.key == key {
			d.replaceKeyType(item.entry.Key, nil)
			copy(d.items[i:], d.items[i+1:])
			d.items[len(d.items)-1] = dictItem{}
			d.items = d.items[:len(d.items)-1]
			return
		}
	}
}

// compactOrder drops tombstones; deletes stay amortised O(1) because it runs only once they outnumber live keys.
func (d *dictData) compactOrder() {
	live := make([]string, 0, len(d.m))
	for _, k := range d.order {
		if k == dictTombstone {
			continue
		}
		slot := d.m[k]
		slot.pos = len(live)
		d.m[k] = slot
		live = append(live, k)
	}
	d.order = live
	d.dead = 0
}

func (d *dictData) clear() {
	d.nonStringKeys = 0
	d.dead = 0
	d.m = nil
	d.order = nil
	clear(d.items)
	d.items = d.items[:0]
}

func (d *dictData) forEach(fn func(string, DictEntry) bool) {
	if d.m != nil {
		for _, k := range d.order {
			if k == dictTombstone {
				continue
			}
			if slot, ok := d.m[k]; ok {
				if !fn(k, slot.entry) {
					return
				}
			}
		}
		return
	}
	for i := 0; i < len(d.items); i++ {
		item := d.items[i]
		if !fn(item.key, item.entry) {
			return
		}
	}
}

func (d *dictData) orderedKeys() []string {
	out := make([]string, 0, d.length())
	if d.m != nil {
		if d.dead == 0 {
			return append(out, d.order...)
		}
		for _, k := range d.order {
			if k != dictTombstone {
				out = append(out, k)
			}
		}
		return out
	}
	for _, item := range d.items {
		out = append(out, item.key)
	}
	return out
}
