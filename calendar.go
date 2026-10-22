package main

// 日历日程：读 ICS（本机 Outlook / .ics 文件 / 订阅链接），
// 到点前提醒或直接开录，结束后自动停。
//
// 为什么还要日历：原来只靠「检测麦克风占用」是事后发现，等提示弹出来，
// 前面几句已经说完了。日历是事前已知的，能提前就位。

import (
	"screenrec/internal/winhide"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Agenda 一条日程
type Agenda struct {
	UID      string    `json:"uid"`
	Title    string    `json:"title"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Location string    `json:"location,omitempty"`
	JoinURL  string    `json:"joinUrl,omitempty"`
	AllDay   bool      `json:"allDay"`
	// 已经提醒过 / 开录过，不再重复打扰
	Fired bool `json:"fired"`
	// 距开始还有多少秒，负数表示已开始
	InSec int `json:"inSec"`
}

// ---------------------------------------------------------------- ICS 解析

// unfold 把 ICS 的折行（行首空格/制表符续行）拼回一行
func unfold(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, ln := range raw {
		if ln == "" {
			continue
		}
		if (ln[0] == ' ' || ln[0] == '\t') && len(out) > 0 {
			out[len(out)-1] += ln[1:]
			continue
		}
		out = append(out, ln)
	}
	return out
}

func unescapeICS(s string) string {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\N", "\n")
	s = strings.ReplaceAll(s, "\\,", ",")
	s = strings.ReplaceAll(s, "\\;", ";")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	return strings.TrimSpace(s)
}

// propValue 取 "DTSTART;TZID=Asia/Shanghai:20261022T100000" 冒号后的值
func propValue(line string) string {
	if i := strings.IndexByte(line, ':'); i >= 0 {
		return line[i+1:]
	}
	return ""
}

// propParam 取属性上的参数（TZID / VALUE …）
func propParam(line, key string) string {
	head := line
	if i := strings.IndexByte(line, ':'); i >= 0 {
		head = line[:i]
	}
	for _, p := range strings.Split(head, ";")[1:] {
		if kv := strings.SplitN(p, "=", 2); len(kv) == 2 && strings.EqualFold(kv[0], key) {
			return kv[1]
		}
	}
	return ""
}

// Windows 上 Go 的 LoadLocation 认不出 Outlook 导出的 TZID，给一张兜底表
var tzidFixedOffsets = map[string]int{
	"China Standard Time":          8 * 60,
	"Asia/Shanghai":                8 * 60,
	"Asia/Chongqing":               8 * 60,
	"Asia/Hong_Kong":               8 * 60,
	"Asia/Macau":                   8 * 60,
	"Asia/Taipei":                  8 * 60,
	"Taipei Standard Time":         8 * 60,
	"Singapore Standard Time":      8 * 60,
	"Tokyo Standard Time":          9 * 60,
	"Asia/Tokyo":                   9 * 60,
	"Korea Standard Time":          9 * 60,
	"Asia/Seoul":                   9 * 60,
	"India Standard Time":          330,
	"UTC":                          0,
	"GMT":                          0,
	"Greenwich Standard Time":      0,
	"W. Europe Standard Time":      60,
	"Central Europe Standard Time": 60,
	"Eastern Standard Time":        -5 * 60,
	"US Eastern Standard Time":     -5 * 60,
	"Central Standard Time":        -6 * 60,
	"Mountain Standard Time":       -7 * 60,
	"Pacific Standard Time":        -8 * 60,
	"US Pacific Standard Time":     -8 * 60,
}

// locate 解析 ICS 时区；认不出来就按本地时间（浮动时间）处理
func locate(tzid string) *time.Location {
	if tzid == "" {
		return time.Local
	}
	if loc, err := time.LoadLocation(tzid); err == nil {
		return loc
	}
	if off, ok := tzidFixedOffsets[tzid]; ok {
		return time.FixedZone(tzid, off*60)
	}
	low := strings.ToLower(tzid)
	for k, off := range tzidFixedOffsets {
		if strings.Contains(low, strings.ToLower(k)) {
			return time.FixedZone(tzid, off*60)
		}
	}
	return time.Local
}

// parseICSDate 支持 20261022T100000Z / 20261022T100000 / 20261022
func parseICSDate(v, tzid, valueType string) (time.Time, bool, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false, fmt.Errorf("空日期")
	}
	if strings.EqualFold(valueType, "DATE") || len(v) == 8 {
		t, err := time.ParseInLocation("20060102", v, time.Local)
		return t, true, err
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.ParseInLocation("20060102T150405Z", v, time.UTC)
		return t, false, err
	}
	loc := locate(tzid)
	t, err := time.ParseInLocation("20060102T150405", v, loc)
	return t, false, err
}

// parseDuration 解析 ISO8601 时长：PT1H30M / P1D
func parseDuration(s string) time.Duration {
	s = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(s)), "P")
	if s == "" {
		return 0
	}
	days := 0
	var d time.Duration
	if i := strings.IndexByte(s, 'T'); i >= 0 {
		datePart, timePart := s[:i], s[i+1:]
		if n := strings.IndexByte(datePart, 'D'); n >= 0 {
			days, _ = strconv.Atoi(datePart[:n])
		}
		num := ""
		for _, r := range timePart {
			switch r {
			case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
				num += string(r)
			case 'H':
				n, _ := strconv.Atoi(num)
				d += time.Duration(n) * time.Hour
				num = ""
			case 'M':
				n, _ := strconv.Atoi(num)
				d += time.Duration(n) * time.Minute
				num = ""
			case 'S':
				n, _ := strconv.Atoi(num)
				d += time.Duration(n) * time.Second
				num = ""
			}
		}
	} else if n := strings.IndexByte(s, 'D'); n >= 0 {
		days, _ = strconv.Atoi(s[:n])
	}
	return d + time.Duration(days)*24*time.Hour
}

type rrule struct {
	freq     string // DAILY / WEEKLY / MONTHLY / YEARLY
	interval int
	byDay    []time.Weekday
	count    int
	until    time.Time
}

var weekdayCode = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

func parseRRULE(v string) rrule {
	r := rrule{freq: "DAILY", interval: 1}
	for _, p := range strings.Split(v, ";") {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch strings.ToUpper(kv[0]) {
		case "FREQ":
			r.freq = strings.ToUpper(kv[1])
		case "INTERVAL":
			if n, _ := strconv.Atoi(kv[1]); n > 0 {
				r.interval = n
			}
		case "BYDAY":
			for _, d := range strings.Split(kv[1], ",") {
				if w, ok := weekdayCode[strings.TrimSpace(strings.ToUpper(d))]; ok {
					r.byDay = append(r.byDay, w)
				}
			}
		case "COUNT":
			r.count, _ = strconv.Atoi(kv[1])
		case "UNTIL":
			if t, _, err := parseICSDate(kv[1], "", ""); err == nil {
				r.until = t
			}
		}
	}
	return r
}

// expand 把重复规则在 [from,to] 内展开成具体开始时间
func (r rrule) expand(start, from, to time.Time) []time.Time {
	var out []time.Time
	cur := start
	limit := to.AddDate(1, 0, 0)
	for i := 0; i < 600; i++ {
		if !cur.Before(from) && !cur.After(to) {
			if len(r.byDay) == 0 {
				out = append(out, cur)
			} else {
				for _, w := range r.byDay {
					if cur.Weekday() == w {
						out = append(out, cur)
						break
					}
				}
			}
		}
		if !r.until.IsZero() && cur.After(r.until) {
			break
		}
		if r.count > 0 && i+1 >= r.count {
			break
		}
		if cur.After(limit) {
			break
		}
		switch r.freq {
		case "WEEKLY":
			if len(r.byDay) > 1 {
				cur = cur.AddDate(0, 0, 1)
			} else {
				cur = cur.AddDate(0, 0, 7*r.interval)
			}
		case "MONTHLY":
			cur = cur.AddDate(0, r.interval, 0)
		case "YEARLY":
			cur = cur.AddDate(r.interval, 0, 0)
		default:
			cur = cur.AddDate(0, 0, r.interval)
		}
	}
	return out
}

var joinKeys = []string{"zoom.us", "meeting.tencent.com", "wemeet", "teams.microsoft",
	"meeting.feishu", "dingtalk.com", "meet.google", "voovmeeting", "welink"}

// extractJoinURL 从 LOCATION / DESCRIPTION 里抠出入会链接
func extractJoinURL(s string) string {
	lower := strings.ToLower(s)
	for _, k := range joinKeys {
		i := strings.Index(lower, k)
		if i < 0 {
			continue
		}
		start := strings.LastIndex(s[:i], "http")
		if start < 0 {
			continue
		}
		end := len(s)
		for _, stop := range []string{" ", "\t", "\n", ",", "，", ")", "）", "】", "<", "\""} {
			if j := strings.Index(s[start:end], stop); j > 0 {
				if start+j < end {
					end = start + j
				}
			}
		}
		u := strings.TrimSpace(s[start:end])
		if strings.HasPrefix(strings.ToLower(u), "http") {
			return u
		}
	}
	return ""
}

// ParseICS 解析 ICS 文本，返回 [from,to] 窗口内的日程（按开始时间排序）
func ParseICS(data []byte, from, to time.Time) []Agenda {
	lines := unfold(string(data))
	var out []Agenda
	inEvent := false
	var e Agenda
	var tzStart, rruleLine string
	var dur time.Duration

	flush := func() {
		if !inEvent || e.Start.IsZero() {
			inEvent = false
			return
		}
		inEvent = false
		if e.End.IsZero() {
			switch {
			case dur > 0:
				e.End = e.Start.Add(dur)
			case e.AllDay:
				e.End = e.Start.AddDate(0, 0, 1)
			default:
				e.End = e.Start.Add(30 * time.Minute)
			}
		}
		if e.Title == "" {
			e.Title = "（无标题日程）"
		}
		if rruleLine != "" {
			r := parseRRULE(rruleLine)
			span := e.End.Sub(e.Start)
			for _, st := range r.expand(e.Start, from, to) {
				c := e
				c.Start = st
				c.End = st.Add(span)
				out = append(out, c)
			}
			return
		}
		out = append(out, e)
	}

	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "BEGIN:VEVENT"):
			inEvent, e = true, Agenda{}
			tzStart, rruleLine, dur = "", "", 0
		case strings.HasPrefix(ln, "END:VEVENT"):
			flush()
		case !inEvent:
			continue
		case strings.HasPrefix(ln, "UID:"):
			e.UID = propValue(ln)
		case strings.HasPrefix(ln, "SUMMARY"):
			e.Title = unescapeICS(propValue(ln))
		case strings.HasPrefix(ln, "LOCATION"):
			e.Location = unescapeICS(propValue(ln))
			if e.JoinURL == "" {
				e.JoinURL = extractJoinURL(e.Location)
			}
		case strings.HasPrefix(ln, "DESCRIPTION"):
			if e.JoinURL == "" {
				e.JoinURL = extractJoinURL(unescapeICS(propValue(ln)))
			}
		case strings.HasPrefix(ln, "STATUS:"):
			if strings.EqualFold(strings.TrimSpace(propValue(ln)), "CANCELLED") {
				inEvent = false // 已取消的日程不提醒
			}
		case strings.HasPrefix(ln, "RECURRENCE-ID"):
			inEvent = false // 重复日程的单次改期，跳过以免重复
		case strings.HasPrefix(ln, "DTSTART"):
			tzStart = propParam(ln, "TZID")
			t, allDay, _ := parseICSDate(propValue(ln), tzStart, propParam(ln, "VALUE"))
			e.Start, e.AllDay = t, allDay
		case strings.HasPrefix(ln, "DTEND"):
			tz := propParam(ln, "TZID")
			if tz == "" {
				tz = tzStart
			}
			t, _, _ := parseICSDate(propValue(ln), tz, propParam(ln, "VALUE"))
			e.End = t
		case strings.HasPrefix(ln, "DURATION"):
			dur = parseDuration(propValue(ln))
		case strings.HasPrefix(ln, "RRULE"):
			rruleLine = propValue(ln)
		}
	}
	flush()

	kept := make([]Agenda, 0, len(out))
	seen := map[string]bool{}
	for _, e := range out {
		if e.End.Before(from) || e.Start.After(to) {
			continue
		}
		key := e.UID + "|" + e.Start.Format(time.RFC3339)
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, e)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Start.Before(kept[j].Start) })
	return kept
}

// ---------------------------------------------------------------- 数据源

// fetchICS 按配置的数据源取 ICS 文本
// 空或 "outlook"：读本机 Outlook；http(s)：下载订阅；其余当文件路径
func fetchICS(source string) ([]byte, error) {
	source = strings.TrimSpace(source)
	switch {
	case source == "" || strings.EqualFold(source, "outlook"):
		return exportOutlookICS()
	case strings.HasPrefix(source, "http://"), strings.HasPrefix(source, "https://"):
		cli := &http.Client{Timeout: 15 * time.Second}
		resp, err := cli.Get(source)
		if err != nil {
			return nil, fmt.Errorf("下载日历失败：%v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("日历返回 %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if err != nil {
			return nil, fmt.Errorf("读取日历失败：%v", err)
		}
		return b, nil
	default:
		b, err := os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("读日历文件失败：%v", err)
		}
		return b, nil
	}
}

// outlookScript 从 Outlook 导出未来两天的日程。
// 用 UTF-8 写文件而不是打到控制台，避免 PowerShell 控制台编码把中文搞乱。
const outlookScript = `$ErrorActionPreference='Stop'
$out = $args[0]
function Esc($s) {
  if ($null -eq $s) { return '' }
  return ([string]$s).Replace('\','\\').Replace(';','\;').Replace(',','\,').Replace("` + "`" + `n",'\n').Replace("` + "`" + `r",'')
}
try {
  $ol = New-Object -ComObject Outlook.Application
  $ns = $ol.GetNamespace('MAPI')
  $cal = $ns.GetDefaultFolder(9)
  $items = $cal.Items
  $items.Sort('[Start]')
  $items.IncludeRecurrences = $true
  $from = (Get-Date).Date
  $to = $from.AddDays(2)
  $filter = "[Start] >= '" + $from.ToString('yyyy-MM-dd HH:mm') + "' AND [Start] <= '" + $to.ToString('yyyy-MM-dd HH:mm') + "'"
  $sb = New-Object System.Text.StringBuilder
  [void]$sb.AppendLine('BEGIN:VCALENDAR')
  [void]$sb.AppendLine('VERSION:2.0')
  [void]$sb.AppendLine('PRODID:-//screenrec//Outlook//CN')
  foreach ($it in $items.Restrict($filter)) {
    if ($it.Class -ne 26) { continue }
    $uid = [string]$it.GlobalAppointmentID
    if (-not $uid) { $uid = [string]$it.EntryID }
    [void]$sb.AppendLine('BEGIN:VEVENT')
    [void]$sb.AppendLine('UID:' + $uid)
    [void]$sb.AppendLine('DTSTART:' + $it.Start.ToString('yyyyMMddTHHmmss'))
    [void]$sb.AppendLine('DTEND:' + $it.End.ToString('yyyyMMddTHHmmss'))
    [void]$sb.AppendLine('SUMMARY:' + (Esc $it.Subject))
    if ($it.Location) { [void]$sb.AppendLine('LOCATION:' + (Esc $it.Location)) }
    [void]$sb.AppendLine('END:VEVENT')
  }
  [void]$sb.AppendLine('END:VCALENDAR')
  [System.IO.File]::WriteAllText($out, $sb.ToString(), (New-Object System.Text.UTF8Encoding($false)))
  exit 0
} catch {
  [System.IO.File]::WriteAllText($out, '', (New-Object System.Text.UTF8Encoding($false)))
  exit 1
}
`

// exportOutlookICS 调 PowerShell 从本机 Outlook 导出日程
func exportOutlookICS() ([]byte, error) {
	dir, err := os.MkdirTemp("", "screenrec-cal")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	ps1 := filepath.Join(dir, "cal.ps1")
	out := filepath.Join(dir, "cal.ics")
	if err := os.WriteFile(ps1, []byte(outlookScript), 0644); err != nil {
		return nil, err
	}
	cmd := winhide.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", ps1, out)
	b, _ := cmd.CombinedOutput()
	data, rerr := os.ReadFile(out)
	if rerr != nil || len(strings.TrimSpace(string(data))) == 0 {
		msg := strings.TrimSpace(string(b))
		if len(msg) > 120 {
			msg = msg[:120]
		}
		if msg == "" {
			msg = "Outlook 未安装或未登录"
		}
		return nil, fmt.Errorf("Outlook 导出失败：%s（也可以改填 .ics 文件路径或订阅链接）", msg)
	}
	return data, nil
}

// ---------------------------------------------------------------- 到点触发

// CalendarWatcher 按日程到点提醒 / 开录
type CalendarWatcher struct {
	app *App

	mu       sync.Mutex
	events   []Agenda
	pending  string // 悬浮条上待响应的提示文案
	pendingE Agenda
	fired    map[string]bool // key = UID|RFC3339
	lastErr  string
	loadedAt time.Time
	// 由日历自动开录的那一条，到结束时间自动停
	autoID    string
	autoEndAt time.Time
}

func NewCalendarWatcher(app *App) *CalendarWatcher {
	return &CalendarWatcher{app: app, fired: map[string]bool{}}
}

// Pending 悬浮条待展示的提示
func (c *CalendarWatcher) Pending() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending
}

// Clear 用户已响应
func (c *CalendarWatcher) Clear() {
	c.mu.Lock()
	c.pending = ""
	c.mu.Unlock()
}

// Snooze 忽略本次（不重复提醒）
func (c *CalendarWatcher) Snooze() {
	c.mu.Lock()
	if c.pending != "" {
		c.fired[fireKey(c.pendingE)] = true
	}
	c.pending = ""
	c.mu.Unlock()
}

func fireKey(e Agenda) string { return e.UID + "|" + e.Start.Format(time.RFC3339) }

// Loop 后台轮询：10 分钟拉一次日历，20 秒检查一次是否该触发
func (c *CalendarWatcher) Loop(stop <-chan struct{}) {
	c.refresh()
	ref := time.NewTicker(10 * time.Minute)
	tick := time.NewTicker(20 * time.Second)
	defer ref.Stop()
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ref.C:
			c.refresh()
		case <-tick.C:
			c.tick()
		}
	}
}

// Events 返回当前日程（带 fired / inSec），给 API 用
func (c *CalendarWatcher) Events() ([]Agenda, string, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	out := make([]Agenda, 0, len(c.events))
	for _, e := range c.events {
		if e.End.Before(now) {
			continue // 已结束的不展示
		}
		e.Fired = c.fired[fireKey(e)]
		e.InSec = int(e.Start.Sub(now).Seconds())
		out = append(out, e)
	}
	return out, c.lastErr, c.loadedAt
}

// Refresh 立刻重新拉一次（网页点「刷新」）
func (c *CalendarWatcher) Refresh() error {
	return c.refresh()
}

func (c *CalendarWatcher) refresh() error {
	enabled, source, _, _, _, _ := c.app.set.CalendarConfig()
	if !enabled {
		c.mu.Lock()
		c.events, c.pending, c.lastErr = nil, "", ""
		c.mu.Unlock()
		return nil
	}
	now := time.Now()
	from := now.AddDate(0, 0, -1)
	to := now.AddDate(0, 0, 2)
	data, err := fetchICS(source)
	if err != nil {
		c.mu.Lock()
		c.lastErr = err.Error()
		c.mu.Unlock()
		log.Printf("[calendar] 刷新失败: %v", err)
		return err
	}
	evs := ParseICS(data, from, to)
	c.mu.Lock()
	c.events = evs
	c.lastErr = ""
	c.loadedAt = now
	// 清掉两天前的 fired 记录，免得内存里越攒越多
	cut := now.AddDate(0, 0, -2)
	for k := range c.fired {
		if ts := strings.SplitN(k, "|", 2); len(ts) == 2 {
			if t, err := time.Parse(time.RFC3339, ts[1]); err == nil && t.Before(cut) {
				delete(c.fired, k)
			}
		}
	}
	c.mu.Unlock()
	log.Printf("[calendar] 刷新成功，%d 条日程", len(evs))
	return nil
}

func (c *CalendarWatcher) tick() {
	enabled, _, leadMin, autoStart, autoStop, _ := c.app.set.CalendarConfig()
	if !enabled {
		return
	}

	// 日历开录的那条：到结束时间（+5 分钟宽限）自动停
	if autoStop {
		c.mu.Lock()
		id, endAt := c.autoID, c.autoEndAt
		c.mu.Unlock()
		if id != "" && !endAt.IsZero() && time.Now().After(endAt.Add(5*time.Minute)) {
			c.mu.Lock()
			c.autoID, c.autoEndAt = "", time.Time{}
			c.mu.Unlock()
			if rec := c.app.recorder.Current(); rec != nil && rec.ID == id && rec.Status == "recording" {
				log.Printf("[calendar] 日程已结束，自动停止录制 %s", id)
				_ = c.app.stopRecording()
				c.app.flashPrompt("日程已到结束时间，录制已停止")
			}
		}
	}

	// 正在录制时不再提醒
	if rec := c.app.recorder.Current(); rec != nil && rec.Status == "recording" {
		c.Clear()
		return
	}

	c.mu.Lock()
	now := time.Now()
	// 还有一条没被响应（开始 / 忽略）：别用下一条顶掉它。
	// 但人不在电脑前时提示会一直挂着，所以过了触发窗口就自动撤掉。
	if c.pending != "" {
		if !c.pendingE.Start.IsZero() &&
			(now.After(c.pendingE.Start.Add(10*time.Minute)) || now.After(c.pendingE.End)) {
			c.fired[fireKey(c.pendingE)] = true
			c.pending, c.pendingE = "", Agenda{}
		} else {
			c.mu.Unlock()
			return
		}
	}
	hit, found, skipAllDay := dueEvent(c.events, now, time.Duration(leadMin)*time.Minute, c.fired)
	if !found {
		c.pending = ""
		c.mu.Unlock()
		return
	}
	// 全天日程（生日、节假日）不提醒，但也要标记，免得每 20 秒都进来一次
	if skipAllDay || hit.AllDay {
		c.fired[fireKey(hit)] = true
		c.mu.Unlock()
		return
	}

	c.fired[fireKey(hit)] = true
	if autoStart {
		c.pending = ""
		c.pendingE = Agenda{}
		c.mu.Unlock()
		c.autoStart(hit)
		return
	}
	c.pendingE = hit
	c.pending = calendarPrompt(hit, now)
	c.mu.Unlock()
}

// dueEvent 在日程里挑一条「该触发了」的：
//   - 窗口是 [开始 - lead, 开始 + 10 分钟]，过了开始 10 分钟就不再打扰
//   - 已结束 / 已触发过的跳过
//   - 全天日程返回 skipAllDay=true，让调用方标记掉而不提示
func dueEvent(events []Agenda, now time.Time, lead time.Duration, fired map[string]bool) (Agenda, bool, bool) {
	for _, e := range events {
		if e.End.Before(now) {
			continue
		}
		if fired[fireKey(e)] {
			continue
		}
		if now.Before(e.Start.Add(-lead)) || now.After(e.Start.Add(10*time.Minute)) {
			continue
		}
		return e, true, e.AllDay
	}
	return Agenda{}, false, false
}

func calendarPrompt(e Agenda, now time.Time) string {
	d := e.Start.Sub(now)
	switch {
	case d > 0:
		// 向上取整：剩 119 秒说「2 分钟后」比「1 分钟后」实在
		mins := int(d / time.Minute)
		if d%time.Minute != 0 {
			mins++
		}
		return fmt.Sprintf("「%s」%d 分钟后开始，要开始录制吗？", e.Title, mins)
	case now.Before(e.End):
		return fmt.Sprintf("「%s」已经开始了，要开始录制吗？", e.Title)
	default:
		return fmt.Sprintf("「%s」要开始录制吗？", e.Title)
	}
}

// autoStart 日历到点直接开录（用户在设置里明确开启才会走这里）
func (c *CalendarWatcher) autoStart(e Agenda) {
	opt := c.app.lastOptions()
	_, _, _, _, _, calMode := c.app.set.CalendarConfig()
	switch calMode {
	case "audio", "screen":
		opt.Mode = calMode
	}
	if opt.Mode == "" {
		opt.Mode = "audio"
	}
	opt.Title = e.Title
	rec, err := c.app.startRecording(opt)
	if err != nil {
		log.Printf("[calendar] 自动开录失败: %v", err)
		return
	}
	c.mu.Lock()
	c.autoID, c.autoEndAt = rec.ID, e.End
	c.mu.Unlock()
	log.Printf("[calendar] 已按日程自动开录：%s（%s）", e.Title, rec.ID)
}

// Accept 用户在悬浮条上点了「开始」：用当前日程标题开录
func (c *CalendarWatcher) Accept() {
	c.mu.Lock()
	e := c.pendingE
	c.pending = ""
	c.pendingE = Agenda{}
	c.mu.Unlock()
	if e.Title == "" {
		c.app.requestStart()
		return
	}
	opt := c.app.lastOptions()
	_, _, _, _, _, calMode := c.app.set.CalendarConfig()
	switch calMode {
	case "audio":
		opt.Mode = "audio"
	case "screen":
		opt.Mode = "screen"
	}
	opt.Title = e.Title
	if _, err := c.app.startRecording(opt); err != nil {
		c.app.flashPrompt("启动失败：" + err.Error())
		return
	}
	c.mu.Lock()
	c.autoEndAt = e.End
	c.mu.Unlock()
}
