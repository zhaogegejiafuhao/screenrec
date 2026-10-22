package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 转写全文索引。
//
// 背景：MCP 的 search_transcripts 原来是「列记录 → 一条条 GET 详情 → 遍历段落」，
// 每条记录一次 HTTP + 一次 JSON 反序列化，上百条时明显变慢（而且原来还截断到 40 条）。
// 索引把每条记录的段落文本摊平存一份在 data/search-index.json，搜索时只查这一个文件。
//
// 索引不做实时同步，靠源 JSON 的 mtime 判断要不要重建该条：
//   - 搜索前扫一遍目录（只 stat，很快），mtime 变了就重读那一条
//   - 索引文件丢了/坏了/版本不对，就全量重建
// 这样转写完成、改名、删除都能自动跟上，也不用常驻 watcher。

const searchIndexVersion = 1

type indexSeg struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
	Trans string  `json:"trans,omitempty"`
}

type indexItem struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	StartedAt time.Time  `json:"startedAt"`
	ModNano   int64      `json:"modNano"` // 源记录文件的修改时间，用于增量判断
	Segs      []indexSeg `json:"segs"`
}

type searchIndex struct {
	Version int                  `json:"version"`
	BuiltAt time.Time            `json:"builtAt"`
	Items   map[string]indexItem `json:"items"`
}

// SearchHit 一条命中
type SearchHit struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
	Trans string  `json:"trans,omitempty"`
}

func (a *App) indexPath() string {
	return filepath.Join(a.dataDir, "search-index.json")
}

// loadIndex 读索引文件；读不出来就返回一个空索引，由 ensureIndex 补建
func (a *App) loadIndex() *searchIndex {
	b, err := os.ReadFile(a.indexPath())
	if err != nil {
		return &searchIndex{Items: map[string]indexItem{}}
	}
	var idx searchIndex
	if json.Unmarshal(b, &idx) != nil || idx.Items == nil || idx.Version != searchIndexVersion {
		return &searchIndex{Items: map[string]indexItem{}}
	}
	return &idx
}

// ensureIndex 按 mtime 增量更新索引，返回索引与「本轮重建了几条」
func (a *App) ensureIndex() (*searchIndex, int) {
	a.idxMu.Lock()
	defer a.idxMu.Unlock()
	if a.idx == nil {
		a.idx = a.loadIndex()
	}

	entries, err := os.ReadDir(a.recordsDir)
	if err != nil {
		return a.idx, 0
	}
	rebuilt := 0
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(a.recordsDir, e.Name())
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".json")
		seen[base] = true
		old, ok := a.idx.Items[base]
		if ok && old.ModNano == st.ModTime().UnixNano() {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var r Recording
		if json.Unmarshal(b, &r) != nil || r.ID == "" {
			continue
		}
		if r.ID != base {
			// 文件名与 ID 不一致（手工改过），按 ID 存、按文件名判重都可能漏，
			// 这里以 ID 为准，同时把旧文件名那条丢掉
			delete(a.idx.Items, base)
		}
		a.idx.Items[r.ID] = indexItemFrom(&r, st.ModTime().UnixNano())
		seen[r.ID] = true
		rebuilt++
	}
	for id := range a.idx.Items {
		if !seen[id] {
			delete(a.idx.Items, id)
			rebuilt++
		}
	}
	if rebuilt > 0 {
		a.idx.BuiltAt = time.Now()
		a.saveIndexLocked()
	}
	return a.idx, rebuilt
}

func (a *App) saveIndexLocked() {
	a.idx.Version = searchIndexVersion
	b, err := json.MarshalIndent(a.idx, "", "  ")
	if err != nil {
		return
	}
	tmp := a.indexPath() + ".tmp"
	if os.WriteFile(tmp, b, 0644) == nil {
		os.Rename(tmp, a.indexPath())
	}
}

func indexItemFrom(r *Recording, modNano int64) indexItem {
	segs := make([]indexSeg, 0, len(r.Segments))
	for _, s := range r.Segments {
		segs = append(segs, indexSeg{Start: s.Start, End: s.End, Text: s.Text, Trans: s.Trans})
	}
	return indexItem{
		ID:        r.ID,
		Title:     r.Title,
		StartedAt: r.StartedAt,
		ModNano:   modNano,
		Segs:      segs,
	}
}

// searchTranscripts 在索引里搜关键词（不区分大小写），按记录开始时间倒序返回
func (a *App) searchTranscripts(q string, limit int) ([]SearchHit, int) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil, 0
	}
	if limit <= 0 {
		limit = 20
	}
	idx, _ := a.ensureIndex()

	// 为了让结果稳定，先把记录按时间倒序排好再遍历
	items := make([]indexItem, 0, len(idx.Items))
	for _, it := range idx.Items {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt.After(items[j].StartedAt) })

	hits := make([]SearchHit, 0, limit)
	for _, it := range items {
		for _, s := range it.Segs {
			if strings.Contains(strings.ToLower(s.Text), q) ||
				(s.Trans != "" && strings.Contains(strings.ToLower(s.Trans), q)) {
				hits = append(hits, SearchHit{
					ID:    it.ID,
					Title: it.Title,
					Start: s.Start,
					End:   s.End,
					Text:  s.Text,
					Trans: s.Trans,
				})
				if len(hits) >= limit {
					return hits, len(idx.Items)
				}
			}
		}
	}
	return hits, len(idx.Items)
}

// RebuildSearchIndex 供外部（比如转写完成后）主动重建某条记录的索引
func (a *App) RebuildSearchIndex(id string) {
	a.idxMu.Lock()
	defer a.idxMu.Unlock()
	if a.idx == nil {
		a.idx = a.loadIndex()
	}
	a.mu.RLock()
	rec, ok := a.recordings[id]
	a.mu.RUnlock()
	if !ok {
		return
	}
	path := filepath.Join(a.recordsDir, id+".json")
	modNano := time.Now().UnixNano()
	if st, err := os.Stat(path); err == nil {
		modNano = st.ModTime().UnixNano()
	}
	a.idx.Items[id] = indexItemFrom(rec, modNano)
	a.idx.BuiltAt = time.Now()
	a.saveIndexLocked()
}
