package cli

import (
	"reflect"
	"testing"
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
