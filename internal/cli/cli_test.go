package cli

import (
	"reflect"
	"testing"
	"time"
)

func TestReorderArgsMovesPositionalsToEnd(t *testing.T) {
	fs := newFlagSet("test")
	progress := fs.Int("progress", -1, "")
	fs.IntVar(progress, "p", -1, "")
	title := fs.String("title", "", "")
	fs.StringVar(title, "t", "", "")

	got := reorderArgs(fs, []string{"3", "-p", "50", "-t", "写周报"})
	if err := fs.Parse(got); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if fs.NArg() != 1 || fs.Arg(0) != "3" {
		t.Fatalf("positional = %v", fs.Args())
	}
	if *progress != 50 || *title != "写周报" {
		t.Fatalf("flags = %d / %q", *progress, *title)
	}
}

func TestReorderArgsKeepsPositionalOrder(t *testing.T) {
	fs := newFlagSet("test")
	got := reorderArgs(fs, []string{"bob", "admin"})
	want := []string{"bob", "admin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseTimeBound(t *testing.T) {
	from, err := parseTimeBound("2026-10-01", false)
	if err != nil {
		t.Fatal(err)
	}
	wantFrom := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	if !from.Equal(wantFrom) {
		t.Fatalf("from = %v, want %v", from, wantFrom)
	}
	to, err := parseTimeBound("2026-10-01", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := to.Sub(from); got != 24*time.Hour-time.Millisecond {
		t.Fatalf("纯日期终点应覆盖当天, span = %v", got)
	}
	withTime, err := parseTimeBound("2026-10-01 08:30", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 1, 8, 30, 0, 0, time.Local); !withTime.Equal(want) {
		t.Fatalf("带时间的终点不应被扩展: %v", withTime)
	}
	zoned, err := parseTimeBound("2026-10-01T00:00:00Z", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, offset := zoned.Zone(); offset != 0 {
		t.Fatalf("RFC3339 时区应保留, offset = %d", offset)
	}
	if _, err := parseTimeBound("not-a-time", false); err == nil {
		t.Fatal("非法时间应返回错误")
	}
}

func TestReorderArgsHandlesNegativeValue(t *testing.T) {
	fs := newFlagSet("test")
	progress := fs.Int("progress", 0, "")
	fs.IntVar(progress, "p", 0, "")
	got := reorderArgs(fs, []string{"7", "-p", "-5"})
	if err := fs.Parse(got); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if *progress != -5 || fs.Arg(0) != "7" {
		t.Fatalf("progress=%d args=%v", *progress, fs.Args())
	}
}
