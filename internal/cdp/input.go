package cdp

import (
	"fmt"
	"strings"
	"unicode"
)

type keyDef struct {
	key     string
	code    string
	keyCode int
	text    string
}

const (
	modAlt   = 1
	modCtrl  = 2
	modMeta  = 4
	modShift = 8
)

var modifierBits = map[string]int{"Alt": modAlt, "Control": modCtrl, "Meta": modMeta, "Shift": modShift}

var namedKeys = map[string]keyDef{
	"Enter":      {"Enter", "Enter", 13, "\r"},
	"Tab":        {"Tab", "Tab", 9, ""},
	"Escape":     {"Escape", "Escape", 27, ""},
	"Backspace":  {"Backspace", "Backspace", 8, ""},
	"Delete":     {"Delete", "Delete", 46, ""},
	"Insert":     {"Insert", "Insert", 45, ""},
	"Home":       {"Home", "Home", 36, ""},
	"End":        {"End", "End", 35, ""},
	"PageUp":     {"PageUp", "PageUp", 33, ""},
	"PageDown":   {"PageDown", "PageDown", 34, ""},
	"ArrowLeft":  {"ArrowLeft", "ArrowLeft", 37, ""},
	"ArrowUp":    {"ArrowUp", "ArrowUp", 38, ""},
	"ArrowRight": {"ArrowRight", "ArrowRight", 39, ""},
	"ArrowDown":  {"ArrowDown", "ArrowDown", 40, ""},
	"Space":      {" ", "Space", 32, " "},
	"Shift":      {"Shift", "ShiftLeft", 16, ""},
	"Control":    {"Control", "ControlLeft", 17, ""},
	"Alt":        {"Alt", "AltLeft", 18, ""},
	"Meta":       {"Meta", "MetaLeft", 91, ""},
}

// Editing shortcuts headless Chrome only performs when sent as explicit commands.
var ctrlCommands = map[string]string{"a": "selectAll", "c": "copy", "v": "paste", "x": "cut", "z": "undo", "y": "redo"}

func init() {
	for i := 1; i <= 12; i++ {
		name := fmt.Sprintf("F%d", i)
		namedKeys[name] = keyDef{name, name, 111 + i, ""}
	}
}

func lookupKey(name string) (keyDef, error) {
	if d, ok := namedKeys[name]; ok {
		return d, nil
	}
	r := []rune(name)
	if len(r) != 1 {
		return keyDef{}, fmt.Errorf("unknown key %q", name)
	}
	c := r[0]
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		up := unicode.ToUpper(c)
		return keyDef{name, "Key" + string(up), int(up), name}, nil
	case c >= '0' && c <= '9':
		return keyDef{name, "Digit" + name, int(c), name}, nil
	}
	return keyDef{name, "", 0, name}, nil
}

// ParseCombo splits "Control+Shift+A" into key names; a literal "+" key is allowed last.
func ParseCombo(combo string) ([]string, error) {
	if combo == "" {
		return nil, fmt.Errorf("empty key")
	}
	var keys []string
	rest := combo
	if strings.HasSuffix(combo, "++") || combo == "+" {
		rest = strings.TrimSuffix(combo, "+")
		rest = strings.TrimSuffix(rest, "+")
		keys = append(keys, "+")
	}
	if rest == "" {
		return keys, nil
	}
	parts := strings.Split(rest, "+")
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("malformed key combination %q", combo)
		}
	}
	return append(parts, keys...), nil
}

// KeyDown presses a key, holding modifiers until KeyUp.
func (p *Page) KeyDown(name string) error {
	d, err := lookupKey(name)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.modifiers |= modifierBits[name]
	mods := p.modifiers
	p.mu.Unlock()
	params := map[string]any{"type": "rawKeyDown", "key": d.key, "code": d.code, "windowsVirtualKeyCode": d.keyCode, "nativeVirtualKeyCode": d.keyCode, "modifiers": mods}
	text := d.text
	if mods&(modCtrl|modAlt|modMeta) != 0 {
		text = ""
		if cmd, ok := ctrlCommands[strings.ToLower(d.key)]; ok && mods&modCtrl != 0 {
			params["commands"] = []string{cmd}
		}
	}
	if text != "" {
		params["type"] = "keyDown"
		params["text"] = text
		params["unmodifiedText"] = text
	}
	_, err = p.Send("Input.dispatchKeyEvent", params)
	return err
}

// KeyUp releases a key pressed with KeyDown.
func (p *Page) KeyUp(name string) error {
	d, err := lookupKey(name)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.modifiers &^= modifierBits[name]
	mods := p.modifiers
	p.mu.Unlock()
	_, err = p.Send("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": d.key, "code": d.code, "windowsVirtualKeyCode": d.keyCode, "nativeVirtualKeyCode": d.keyCode, "modifiers": mods})
	return err
}

// Press presses and releases a key or combination such as "Control+A".
func (p *Page) Press(combo string) error {
	keys, err := ParseCombo(combo)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if _, err := lookupKey(k); err != nil {
			return err
		}
	}
	for _, k := range keys {
		if err := p.KeyDown(k); err != nil {
			return err
		}
	}
	for i := len(keys) - 1; i >= 0; i-- {
		if err := p.KeyUp(keys[i]); err != nil {
			return err
		}
	}
	return nil
}

var buttonBits = map[string]int{"left": 1, "right": 2, "middle": 4}

// ValidButton reports whether name is a mouse button this package understands.
func ValidButton(name string) bool {
	_, ok := buttonBits[name]
	return ok
}

func (p *Page) mouse(kind string, extra map[string]any) error {
	p.mu.Lock()
	params := map[string]any{"type": kind, "x": p.mouseX, "y": p.mouseY, "modifiers": p.modifiers, "buttons": p.buttons}
	p.mu.Unlock()
	for k, v := range extra {
		params[k] = v
	}
	_, err := p.Send("Input.dispatchMouseEvent", params)
	return err
}

// MouseMove moves the pointer to (x, y) in `steps` even moves.
func (p *Page) MouseMove(x, y float64, steps int) error {
	if steps < 1 {
		steps = 1
	}
	p.mu.Lock()
	fromX, fromY := p.mouseX, p.mouseY
	p.mu.Unlock()
	for i := 1; i <= steps; i++ {
		f := float64(i) / float64(steps)
		p.mu.Lock()
		p.mouseX, p.mouseY = fromX+(x-fromX)*f, fromY+(y-fromY)*f
		p.mu.Unlock()
		if err := p.mouse("mouseMoved", nil); err != nil {
			return err
		}
	}
	return nil
}

// MouseDown presses a button at the pointer.
func (p *Page) MouseDown(button string, clickCount int) error {
	p.mu.Lock()
	p.buttons |= buttonBits[button]
	p.mu.Unlock()
	return p.mouse("mousePressed", map[string]any{"button": button, "clickCount": clickCount})
}

// MouseUp releases a button at the pointer.
func (p *Page) MouseUp(button string, clickCount int) error {
	p.mu.Lock()
	p.buttons &^= buttonBits[button]
	p.mu.Unlock()
	return p.mouse("mouseReleased", map[string]any{"button": button, "clickCount": clickCount})
}

// MouseClick moves to (x, y) and clicks clickCount times.
func (p *Page) MouseClick(x, y float64, button string, clickCount int) error {
	if err := p.MouseMove(x, y, 1); err != nil {
		return err
	}
	for i := 1; i <= clickCount; i++ {
		if err := p.MouseDown(button, i); err != nil {
			return err
		}
		if err := p.MouseUp(button, i); err != nil {
			return err
		}
	}
	return nil
}

// Wheel scrolls by (dx, dy) at the pointer.
func (p *Page) Wheel(dx, dy float64) error {
	return p.mouse("mouseWheel", map[string]any{"deltaX": dx, "deltaY": dy})
}
