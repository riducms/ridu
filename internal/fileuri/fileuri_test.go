package fileuri

import (
	"net/url"
	"strings"
	"testing"
)

func TestPathsRoundTripThroughParsedFileURLs(t *testing.T) {
	for _, test := range []struct {
		name    string
		windows bool
		path    string
		url     string
	}{
		{name: "Windows drive", windows: true, path: `C:\Users\hanie\my app\.ridu\development.sqlite`, url: "file:///C:/Users/hanie/my%20app/.ridu/development.sqlite"},
		{name: "Windows drive root", windows: true, path: `D:\`, url: "file:///D:/"},
		{name: "Windows UNC", windows: true, path: `\\server\share\ridu\development.sqlite`, url: "file://server/share/ridu/development.sqlite"},
		{name: "Unix", path: "/home/ridu/my app/#1?/development.sqlite", url: "file:///home/ridu/my%20app/%231%3F/development.sqlite"},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded := fromPath(test.path, test.windows).String()
			if encoded != test.url {
				t.Fatalf("fromPath(%q) = %q, want %q", test.path, encoded, test.url)
			}
			parsed, err := url.Parse(encoded)
			if err != nil {
				t.Fatalf("parse %q: %v", encoded, err)
			}
			decoded, err := toPath(parsed, test.windows)
			if err != nil || decoded != test.path {
				t.Fatalf("toPath(%q) = %q, %v; want %q", encoded, decoded, err, test.path)
			}
		})
	}
}

func TestFromPathDropsWindowsLongPathPrefixes(t *testing.T) {
	for path, want := range map[string]string{
		`\\?\C:\data\app.sqlite`:          "file:///C:/data/app.sqlite",
		`\\?\UNC\server\share\app.sqlite`: "file://server/share/app.sqlite",
	} {
		if encoded := fromPath(path, true).String(); encoded != want {
			t.Fatalf("fromPath(%q) = %q, want %q", path, encoded, want)
		}
		if _, err := url.Parse(want); err != nil {
			t.Fatalf("parse %q: %v", want, err)
		}
	}
}

func TestToPathAcceptsEquivalentFileURLForms(t *testing.T) {
	for _, test := range []struct {
		name    string
		windows bool
		url     string
		path    string
	}{
		{name: "Windows opaque drive", windows: true, url: "file:C:/Users/hanie/app.sqlite", path: `C:\Users\hanie\app.sqlite`},
		{name: "Windows localhost drive", windows: true, url: "file://localhost/C:/app.sqlite", path: `C:\app.sqlite`},
		{name: "Windows four-slash UNC", windows: true, url: "file:////server/share/app.sqlite", path: `\\server\share\app.sqlite`},
		{name: "Windows project-relative", windows: true, url: "file:.ridu/development.sqlite?mode=rwc", path: `.ridu\development.sqlite`},
		{name: "Unix project-relative", url: "file:.ridu/development%20copy.sqlite?mode=rwc", path: ".ridu/development copy.sqlite"},
		{name: "Unix localhost", url: "file://localhost/var/ridu.sqlite", path: "/var/ridu.sqlite"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := url.Parse(test.url)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := toPath(parsed, test.windows)
			if err != nil || decoded != test.path {
				t.Fatalf("toPath(%q) = %q, %v; want %q", test.url, decoded, err, test.path)
			}
		})
	}
}

func TestToPathRejectsRemoteHostsOutsideWindowsAndOtherSchemes(t *testing.T) {
	remote, err := url.Parse("file://server/share/app.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if path, err := toPath(remote, false); err == nil || !strings.Contains(err.Error(), `names host "server"`) {
		t.Fatalf("toPath(remote) = %q, %v", path, err)
	}
	web, err := url.Parse("https://example.com/app.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if path, err := toPath(web, true); err == nil || !strings.Contains(err.Error(), "is not file") {
		t.Fatalf("toPath(https) = %q, %v", path, err)
	}
}
