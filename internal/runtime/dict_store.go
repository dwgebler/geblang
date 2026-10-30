package runtime

// Copies share this backing store through growth and map promotion.
const dictInlineMax = 8

type dictItem struct {
	key   string
	entry DictEntry
}

type dictData struct {
	items         []dictItem
	m             map[string]DictEntry
	order         []string
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
		e, ok := d.m[key]
		return e, ok
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
			d.order = append(d.order, key)
		}
		d.replaceKeyType(old.Key, entry.Key)
		d.m[key] = entry
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
	d.m = make(map[string]DictEntry, dictInlineMax*2)
	d.order = make([]string, 0, dictInlineMax*2)
	for _, item := range d.items {
		d.m[item.key] = item.entry
		d.order = append(d.order, item.key)
	}
	d.items = nil
	d.m[key] = entry
	d.order = append(d.order, key)
}

func (d *dictData) del(key string) {
	if d.m != nil {
		old, hit := d.m[key]
		if !hit {
			return
		}
		d.replaceKeyType(old.Key, nil)
		delete(d.m, key)
		for i, k := range d.order {
			if k == key {
				copy(d.order[i:], d.order[i+1:])
				d.order[len(d.order)-1] = ""
				d.order = d.order[:len(d.order)-1]
				break
			}
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

func (d *dictData) clear() {
	d.nonStringKeys = 0
	d.m = nil
	d.order = nil
	clear(d.items)
	d.items = d.items[:0]
}

func (d *dictData) forEach(fn func(string, DictEntry) bool) {
	if d.m != nil {
		for _, k := range d.order {
			if e, ok := d.m[k]; ok {
				if !fn(k, e) {
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
		return append(out, d.order...)
	}
	for _, item := range d.items {
		out = append(out, item.key)
	}
	return out
}
