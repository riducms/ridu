package environment

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParsersReadBlankAsZeroAndNameTheVariableInErrors(t *testing.T) {
	t.Setenv("RIDU_TEST_VALUE", "  ")
	if value, err := Bool("RIDU_TEST_VALUE"); value || err != nil {
		t.Fatalf("blank bool = %v, %v", value, err)
	}
	if value, err := Count("RIDU_TEST_VALUE"); value != 0 || err != nil {
		t.Fatalf("blank count = %v, %v", value, err)
	}
	if value, err := Duration("RIDU_TEST_VALUE"); value != 0 || err != nil {
		t.Fatalf("blank duration = %v, %v", value, err)
	}
	if values := List("RIDU_TEST_VALUE"); values != nil {
		t.Fatalf("blank list = %#v", values)
	}

	t.Setenv("RIDU_TEST_VALUE", " true ")
	if value, err := Bool("RIDU_TEST_VALUE"); !value || err != nil {
		t.Fatalf("bool = %v, %v", value, err)
	}
	t.Setenv("RIDU_TEST_VALUE", "12")
	if value, err := Count32("RIDU_TEST_VALUE"); value != 12 || err != nil {
		t.Fatalf("count32 = %v, %v", value, err)
	}
	t.Setenv("RIDU_TEST_VALUE", "-1s")
	if value, err := Duration("RIDU_TEST_VALUE"); value != -time.Second || err != nil {
		t.Fatalf("negative duration = %v, %v", value, err)
	}
	t.Setenv("RIDU_TEST_VALUE", " a.test, ,b.test ")
	if values := List("RIDU_TEST_VALUE"); !reflect.DeepEqual(values, []string{"a.test", "b.test"}) {
		t.Fatalf("list = %#v", values)
	}

	for raw, parse := range map[string]func(string) error{
		"yes":         func(name string) error { _, err := Bool(name); return err },
		"-1":          func(name string) error { _, err := Count(name); return err },
		"4294967296":  func(name string) error { _, err := Count32(name); return err },
		"ten seconds": func(name string) error { _, err := Duration(name); return err },
	} {
		t.Setenv("RIDU_TEST_VALUE", raw)
		if err := parse("RIDU_TEST_VALUE"); err == nil || !strings.Contains(err.Error(), "RIDU_TEST_VALUE") || !strings.Contains(err.Error(), raw) {
			t.Errorf("%q error = %v", raw, err)
		}
	}
}

func TestSQLitePathAbsoluteMatchesTheServer(t *testing.T) {
	absolute := filepath.Join(t.TempDir(), "content.sqlite")
	cases := map[string]bool{
		"":                 false,
		":memory:":         false,
		"content.sqlite":   false,
		"file:data.sqlite": false,
		absolute:           true,
		"file:" + filepath.ToSlash(absolute) + "?_pragma=busy_timeout(5000)": true,
	}
	if runtime.GOOS == "windows" {
		cases["file:///C:/data/content.sqlite"] = true
		cases["file://server/share/content.sqlite"] = true
	} else {
		cases["file:///var/lib/ridu/content.sqlite"] = true
	}
	for path, want := range cases {
		if got := SQLitePathAbsolute(path); got != want {
			t.Errorf("SQLitePathAbsolute(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestSQLitePathRequiresAnAbsoluteFile(t *testing.T) {
	for raw, message := range map[string]string{
		"":               "is required",
		":memory:":       "must be a file",
		"content.sqlite": "absolute",
	} {
		t.Setenv("RIDU_SQLITE_PATH", raw)
		if _, err := SQLitePath(); err == nil || !strings.Contains(err.Error(), message) {
			t.Errorf("SQLitePath(%q) error = %v", raw, err)
		}
	}
	absolute := filepath.Join(t.TempDir(), "content.sqlite")
	t.Setenv("RIDU_SQLITE_PATH", " "+absolute+" ")
	if path, err := SQLitePath(); path != absolute || err != nil {
		t.Fatalf("SQLitePath = %q, %v", path, err)
	}
}
