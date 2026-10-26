package cdp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Download is a file the browser has started or finished downloading.
type Download struct {
	Seq           int64
	GUID          string
	URL           string
	SuggestedName string
	FrameID       string
	State         string
}

type downloadLog struct {
	mu    sync.Mutex
	seq   int64
	items map[string]*Download
	order []*Download
}

func (l *downloadLog) handle(method string, params json.RawMessage) {
	switch method {
	case "Browser.downloadWillBegin":
		var ev struct {
			FrameID           string `json:"frameId"`
			GUID              string `json:"guid"`
			URL               string `json:"url"`
			SuggestedFilename string `json:"suggestedFilename"`
		}
		if json.Unmarshal(params, &ev) != nil {
			return
		}
		l.mu.Lock()
		if l.items == nil {
			l.items = map[string]*Download{}
		}
		l.seq++
		d := &Download{Seq: l.seq, GUID: ev.GUID, URL: ev.URL, SuggestedName: ev.SuggestedFilename, FrameID: ev.FrameID, State: "inProgress"}
		l.items[ev.GUID] = d
		l.order = appendCapped(l.order, d)
		l.mu.Unlock()
	case "Browser.downloadProgress":
		var ev struct {
			GUID  string `json:"guid"`
			State string `json:"state"`
		}
		if json.Unmarshal(params, &ev) != nil {
			return
		}
		l.mu.Lock()
		if d := l.items[ev.GUID]; d != nil {
			d.State = ev.State
		}
		l.mu.Unlock()
	}
}

func (p *Page) markDownload(seq int64) {
	p.mu.Lock()
	p.dlMark = seq
	p.mu.Unlock()
}

// SetDownloadPath saves this page's context's downloads into dir.
func (p *Page) SetDownloadPath(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	params := map[string]any{"behavior": "allowAndName", "downloadPath": abs, "eventsEnabled": true}
	if p.ContextID != "" {
		params["browserContextId"] = p.ContextID
	}
	if _, err := p.browser.Send("", "Browser.setDownloadBehavior", params); err != nil {
		return err
	}
	p.mu.Lock()
	p.downloadDir = abs
	p.mu.Unlock()
	return nil
}

// WaitDownload waits for the earliest download from this page no earlier wait returned to finish, then names it after the suggested file name.
func (p *Page) WaitDownload(timeout time.Duration) (Download, string, error) {
	p.mu.Lock()
	dir, since := p.downloadDir, p.dlMark
	p.mu.Unlock()
	if dir == "" {
		return Download{}, "", fmt.Errorf("call setDownloadPath before waiting for a download")
	}
	l := &p.browser.downloads
	deadline := time.Now().Add(timeout)
	for {
		l.mu.Lock()
		var found *Download
		for _, d := range l.order {
			if d.Seq > since && d.FrameID == p.TargetID {
				found = d
				break
			}
		}
		var d Download
		if found != nil {
			d = *found
		}
		l.mu.Unlock()
		if found != nil {
			switch d.State {
			case "completed":
				p.markDownload(d.Seq)
				path, err := nameDownload(dir, d)
				return d, path, err
			case "canceled":
				p.markDownload(d.Seq)
				return d, "", fmt.Errorf("download of %s was canceled", d.URL)
			}
		}
		if time.Now().After(deadline) {
			return Download{}, "", timeoutf("no finished download within %v", timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func nameDownload(dir string, d Download) (string, error) {
	from := filepath.Join(dir, d.GUID)
	name := filepath.Base(d.SuggestedName)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = d.GUID
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	to := filepath.Join(dir, name)
	for i := 1; ; i++ {
		if _, err := os.Stat(to); os.IsNotExist(err) {
			break
		}
		to = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
	if err := os.Rename(from, to); err != nil {
		return "", err
	}
	return to, nil
}
