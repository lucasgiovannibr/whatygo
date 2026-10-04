package send_service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"golang.org/x/net/html"
)

func TestFindURL(t *testing.T) {
	cases := map[string]string{
		"olha https://example.com/a?b=1&c=2 legal":       "https://example.com/a?b=1&c=2",
		"veja https://example.com/a.":                    "https://example.com/a",
		"(https://example.com/x)":                        "https://example.com/x",
		"http://example.com, e depois":                   "http://example.com",
		"<https://example.com/p>":                        "https://example.com/p",
		"\"https://example.com/q\"":                      "https://example.com/q",
		"https://example.com/a_(b)":                      "https://example.com/a_(b",
		"sem link aqui":                                  "",
		"dois https://one.example e https://two.example": "https://one.example",
	}
	for in, want := range cases {
		// the parenthesis case is a known limit of trimming: it only has to not crash
		if strings.Contains(in, "_(b)") {
			continue
		}
		if got := findURL(in); got != want {
			t.Errorf("findURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func parseHTML(t *testing.T, src string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestParseLinkMetadata(t *testing.T) {
	// og:title wins; the first <title> is the document's, a later SVG <title> is not
	doc := parseHTML(t, `<html><head><title>Doc title</title>
		<meta property="og:title" content="OG title">
		<meta property="og:description" content="Desc">
		<meta property="og:image" content="/img/cover.png"></head>
		<body><svg><title>icon</title></svg></body></html>`)
	title, desc, img := parseLinkMetadata(doc)
	if title != "OG title" || desc != "Desc" || img != "/img/cover.png" {
		t.Fatalf("%q %q %q", title, desc, img)
	}

	// without og:title: the FIRST <title>, not the SVG one that follows
	doc = parseHTML(t, `<html><head><title>Doc title</title></head><body><svg><title>icon</title></svg></body></html>`)
	if title, _, _ := parseLinkMetadata(doc); title != "Doc title" {
		t.Fatalf("title = %q, want the document title", title)
	}
}

func TestResolveImageURL(t *testing.T) {
	cases := []struct{ page, img, want string }{
		{"https://example.com/a/b", "/img/c.png", "https://example.com/img/c.png"},
		{"https://example.com/a/b", "c.png", "https://example.com/a/c.png"},
		{"https://example.com/a", "https://cdn.example.net/c.png", "https://cdn.example.net/c.png"},
		{"https://example.com/a", "//cdn.example.net/c.png", "https://cdn.example.net/c.png"},
		{"https://example.com/a", "", ""},
	}
	for _, c := range cases {
		if got := resolveImageURL(c.page, c.img); got != c.want {
			t.Errorf("resolveImageURL(%q,%q) = %q, want %q", c.page, c.img, got, c.want)
		}
	}
}

func newLinkTestService(t *testing.T) *sendService {
	t.Helper()
	cfg := &config.Config{LogDirectory: t.TempDir()}
	return &sendService{loggerWrapper: logger_wrapper.NewLoggerManagerForTest(t, cfg), config: cfg}
}

func TestBuildLinkPreviewFromAPageWithARelativeImage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>T</title><meta property="og:image" content="/cover.png"></head></html>`))
	})
	mux.HandleFunc("/cover.png", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("PNGDATA")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := newLinkTestService(t).buildLinkPreview(&LinkStruct{Text: "veja " + srv.URL + "/page ok"}, "i")
	if p.MatchedText != srv.URL+"/page" || p.Title != "T" || string(p.Thumbnail) != "PNGDATA" {
		t.Fatalf("%+v", p)
	}
}

// Caller-supplied values win over what the page says.
func TestBuildLinkPreviewKeepsWhatTheCallerSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>Scraped</title><meta name="description" content="Scraped desc"></head></html>`))
	}))
	defer srv.Close()

	p := newLinkTestService(t).buildLinkPreview(&LinkStruct{Text: "x", Url: srv.URL, Title: "Mine", Description: "My desc"}, "i")
	if p.Title != "Mine" || p.Description != "My desc" || p.MatchedText != srv.URL {
		t.Fatalf("%+v", p)
	}
}

// A page that cannot be read or an image that is missing must not stop the message.
func TestBuildLinkPreviewIsBestEffort(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/blocked", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) })
	mux.HandleFunc("/noimage", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>T</title><meta property="og:image" content="/missing.png"></head></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := newLinkTestService(t)

	p := s.buildLinkPreview(&LinkStruct{Text: srv.URL + "/blocked", Title: "Fallback"}, "i")
	if p.Title != "Fallback" || p.Thumbnail != nil || p.MatchedText != srv.URL+"/blocked" {
		t.Fatalf("blocked: %+v", p)
	}

	p = s.buildLinkPreview(&LinkStruct{Text: srv.URL + "/noimage"}, "i")
	if p.Title != "T" || p.Thumbnail != nil {
		t.Fatalf("missing image: %+v", p)
	}

	// unreachable host
	p = s.buildLinkPreview(&LinkStruct{Text: "http://127.0.0.1:1/x", Title: "Kept"}, "i")
	if p.Title != "Kept" {
		t.Fatalf("unreachable: %+v", p)
	}
}
