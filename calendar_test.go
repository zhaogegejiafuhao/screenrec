package main

import (
	"strings"
	"testing"
	"time"
)

const sampleICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Microsoft Corporation//Outlook 16.0 MIMEDIR//EN
BEGIN:VTIMEZONE
TZID:China Standard Time
BEGIN:STANDARD
DTSTART:16010101T000000
TZOFFSETFROM:+0800
TZOFFSETTO:+0800
END:STANDARD
END:VTIMEZONE
BEGIN:VEVENT
UID:event-standup
DTSTART;TZID=China Standard Time:20261022T093000
DTEND;TZID=China Standard Time:20261022T094500
SUMMARY:每日站会
LOCATION:腾讯会议 https://meeting.tencent.com/dm/abc123?pwd=1 密码 1234
RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR;INTERVAL=1
END:VEVENT
BEGIN:VEVENT
UID:event-review
DTSTART:20261022T020000Z
DTEND:20261022T030000Z
SUMMARY:季度评审\n（第二轮）
DESCRIPTION:入会链接 https://us02web.zoom.us/j/1234567890
END:VEVENT
BEGIN:VEVENT
UID:event-cancelled
DTSTART:20261022T110000
DTEND:20261022T120000
SUMMARY:已取消的会
STATUS:CANCELLED
END:VEVENT
BEGIN:VEVENT
UID:event-duration
DTSTART:20261022T140000
DURATION:PT1H30M
SUMMARY:用 DURATION 的会
END:VEVENT
BEGIN:VEVENT
UID:event-allday
DTSTART;VALUE=DATE:20261022
DTEND;VALUE=DATE:20261023
SUMMARY:团队建设日
END:VEVENT
END:VCALENDAR
`

// 2026-10-22 是周四
func parseSample(t *testing.T) []Agenda {
	from := time.Date(2026, 10, 22, 0, 0, 0, 0, time.Local)
	to := from.AddDate(0, 0, 1)
	return ParseICS([]byte(sampleICS), from, to)
}

func findByUID(evs []Agenda, uid string) []Agenda {
	var out []Agenda
	for _, e := range evs {
		if e.UID == uid {
			out = append(out, e)
		}
	}
	return out
}

func TestParseICSBasic(t *testing.T) {
	evs := parseSample(t)
	if len(evs) == 0 {
		t.Fatal("一条都没解析出来")
	}
	// 已取消的不该出现
	if len(findByUID(evs, "event-cancelled")) != 0 {
		t.Fatal("STATUS:CANCELLED 的日程应该被丢掉")
	}
	// 摘要里的 \n 还原成换行
	rev := findByUID(evs, "event-review")
	if len(rev) != 1 {
		t.Fatalf("季度评审应解析出 1 条，实际 %d", len(rev))
	}
	if !strings.Contains(rev[0].Title, "第二轮") {
		t.Fatalf("标题解析异常：%q", rev[0].Title)
	}
	// UTC 时间要按本地时区落位：02:00Z = 本地 10:00
	if rev[0].Start.In(time.Local).Hour() != 10 {
		t.Fatalf("UTC 时间换算错了，本地应为 10 点，实际 %s", rev[0].Start.Format("15:04"))
	}
}

func TestParseICSRecurrence(t *testing.T) {
	evs := parseSample(t)
	stand := findByUID(evs, "event-standup")
	if len(stand) != 1 {
		t.Fatalf("周四周五的周会今天应只有 1 条，实际 %d", len(stand))
	}
	if stand[0].Title != "每日站会" {
		t.Fatalf("标题错了：%q", stand[0].Title)
	}
	// TZID=China Standard Time 在 Windows 上 Go 认不出来，靠兜底表按 +8 处理
	if stand[0].Start.Hour() != 9 || stand[0].Start.Minute() != 30 {
		t.Fatalf("时区兜底失败：%s", stand[0].Start.Format("15:04 -0700"))
	}
	if stand[0].End.Sub(stand[0].Start) != 15*time.Minute {
		t.Fatalf("时长错了：%v", stand[0].End.Sub(stand[0].Start))
	}
	// 一周的窗口应该展开出多个工作日
	week := ParseICS([]byte(sampleICS), time.Date(2026, 10, 22, 0, 0, 0, 0, time.Local),
		time.Date(2026, 10, 29, 0, 0, 0, 0, time.Local))
	if n := len(findByUID(week, "event-standup")); n < 4 {
		t.Fatalf("一周内周会应展开出 4~5 条，实际 %d", n)
	}
}

func TestParseICSDurationAndAllDay(t *testing.T) {
	evs := parseSample(t)
	d := findByUID(evs, "event-duration")
	if len(d) != 1 {
		t.Fatalf("DURATION 那条没解析出来")
	}
	if d[0].End.Sub(d[0].Start) != 90*time.Minute {
		t.Fatalf("PT1H30M 解析错了：%v", d[0].End.Sub(d[0].Start))
	}
	a := findByUID(evs, "event-allday")
	if len(a) != 1 || !a[0].AllDay {
		t.Fatalf("全天日程标记错了：%+v", a)
	}
}

func TestExtractJoinURL(t *testing.T) {
	evs := parseSample(t)
	stand := findByUID(evs, "event-standup")[0]
	if !strings.HasPrefix(stand.JoinURL, "https://meeting.tencent.com") {
		t.Fatalf("腾讯会议链接没抠出来：%q", stand.JoinURL)
	}
	if strings.Contains(stand.JoinURL, "密码") {
		t.Fatalf("链接后面多余内容没截掉：%q", stand.JoinURL)
	}
	rev := findByUID(evs, "event-review")[0]
	if !strings.Contains(rev.JoinURL, "zoom.us") {
		t.Fatalf("Zoom 链接没抠出来：%q", rev.JoinURL)
	}
}

func TestUnfoldLongLines(t *testing.T) {
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:x\r\nDTSTART:20261022T150000\r\n" +
		"SUMMARY:这是一条很长的标题，需要\r\n 折行才能放下\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	evs := ParseICS([]byte(ics), time.Date(2026, 10, 22, 0, 0, 0, 0, time.Local),
		time.Date(2026, 10, 23, 0, 0, 0, 0, time.Local))
	if len(evs) != 1 {
		t.Fatalf("折行后应仍是 1 条，实际 %d", len(evs))
	}
	if !strings.Contains(evs[0].Title, "折行才能放下") {
		t.Fatalf("折行拼接失败：%q", evs[0].Title)
	}
}

func TestDueEventWindow(t *testing.T) {
	base := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	ev := Agenda{UID: "u1", Title: "周会", Start: base, End: base.Add(time.Hour)}
	fired := map[string]bool{}

	// 提前 2 分钟：8:00 太早，9:58 命中，10:15 命中，10:30 已过窗口
	cases := []struct {
		at   time.Time
		want bool
	}{
		{base.Add(-30 * time.Minute), false},
		{base.Add(-3 * time.Minute), false},
		{base.Add(-90 * time.Second), true},
		{base.Add(5 * time.Minute), true},
		{base.Add(9 * time.Minute), true},
		{base.Add(11 * time.Minute), false},
	}
	for _, c := range cases {
		_, ok, _ := dueEvent([]Agenda{ev}, c.at, 2*time.Minute, fired)
		if ok != c.want {
			t.Fatalf("at=%s 期望 %v 实际 %v", c.at.Format("15:04"), c.want, ok)
		}
	}

	// 触发过一次就不再触发
	fired[fireKey(ev)] = true
	if _, ok, _ := dueEvent([]Agenda{ev}, base, 2*time.Minute, fired); ok {
		t.Fatal("已触发的日程不该再触发")
	}
	// 已结束的日程不触发
	old := Agenda{UID: "u2", Start: base.Add(-3 * time.Hour), End: base.Add(-2 * time.Hour)}
	if _, ok, _ := dueEvent([]Agenda{old}, base, 2*time.Minute, map[string]bool{}); ok {
		t.Fatal("已结束的日程不该触发")
	}
	// 全天日程：命中但要标记为跳过
	ad := Agenda{UID: "u3", Start: base, End: base.Add(24 * time.Hour), AllDay: true}
	if _, ok, skip := dueEvent([]Agenda{ad}, base, 2*time.Minute, map[string]bool{}); !ok || !skip {
		t.Fatalf("全天日程应命中并标记跳过，ok=%v skip=%v", ok, skip)
	}
}

func TestCalendarPrompt(t *testing.T) {
	base := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	ev := Agenda{Title: "周会", Start: base, End: base.Add(time.Hour)}
	if got := calendarPrompt(ev, base.Add(-2*time.Minute)); !strings.Contains(got, "2 分钟后") {
		t.Fatalf("提前提示文案不对：%q", got)
	}
	if got := calendarPrompt(ev, base.Add(time.Minute)); !strings.Contains(got, "已经开始") {
		t.Fatalf("迟到提示文案不对：%q", got)
	}
}

func TestParseDurationISO(t *testing.T) {
	if d := parseDuration("PT1H30M"); d != 90*time.Minute {
		t.Fatalf("PT1H30M -> %v", d)
	}
	if d := parseDuration("P1D"); d != 24*time.Hour {
		t.Fatalf("P1D -> %v", d)
	}
	if d := parseDuration("PT45M"); d != 45*time.Minute {
		t.Fatalf("PT45M -> %v", d)
	}
	if d := parseDuration(""); d != 0 {
		t.Fatalf("空时长应为 0，实际 %v", d)
	}
}
