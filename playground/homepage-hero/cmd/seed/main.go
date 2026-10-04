package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"math"
	"math/rand"
	"os"

	"example.com/ridu-homepage-hero/content"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/store"
)

const title = "Ridu: notes on building a CMS in Go"

// sections are the article body: each heading introduces one paragraph.
var sections = []struct{ heading, body string }{
	{
		"Why define content in code",
		"I wanted collections and fields to live in the repository, so changes show up in pull " +
			"requests and the same setup can be reused between projects instead of being clicked " +
			"together in an admin panel each time.",
	},
	{
		"Where the admin comes from",
		"The admin reads the same Go config to build its forms and lists. Adding a field in Go adds " +
			"it to the admin, the REST API and the generated TypeScript types, and the same access " +
			"rules and validation apply in each.",
	},
	{
		"Deploying it",
		"A build produces one executable with the API, the admin and background jobs in it. It runs " +
			"next to PostgreSQL, SQLite or MongoDB, and there is no separate Node process to host.",
	},
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	databasePath := os.Getenv("RIDU_HERO_DATABASE_PATH")
	uploadRoot := os.Getenv("RIDU_HERO_UPLOAD_ROOT")
	if databasePath == "" || uploadRoot == "" {
		return fmt.Errorf("RIDU_HERO_DATABASE_PATH and RIDU_HERO_UPLOAD_ROOT are required")
	}
	backend, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		return err
	}
	defer backend.Close()
	objects, err := localstorage.New(uploadRoot)
	if err != nil {
		return err
	}
	config := content.Config()
	config.Storage = objects
	application, err := ridu.New(config, backend)
	if err != nil {
		return err
	}
	editor, err := application.Local().Import(ctx, "users", store.Values{
		"email": store.String("editor@riducms.test"),
	}, ridu.ImportOptions{ID: "hero-editor", Status: store.StatusPublished})
	if err != nil {
		return fmt.Errorf("create editor: %w", err)
	}
	if err := application.SetPassword(ctx, "users", editor.ID, "ridu-homepage"); err != nil {
		return fmt.Errorf("set editor password: %w", err)
	}
	banner, err := application.Upload(ctx, "media", ridu.UploadInput{
		Filename: "banner.jpg",
		Reader:   bytes.NewReader(bannerJPEG()),
		Data:     store.Values{"alt": store.String("Soft pink and violet light across a dark background")},
		Actor:    &editor,
	})
	if err != nil {
		return fmt.Errorf("upload banner: %w", err)
	}
	actor := ridu.MutationOptions{Actor: &editor, ActorCollection: "users"}
	post, err := application.Local().Import(ctx, "posts", store.Values{
		"title":   store.String("Ridu: notes"),
		"banner":  store.String(banner.ID),
		"content": article(sections[:1]),
	}, ridu.ImportOptions{ID: "hero-post", Status: store.StatusPublished, Actor: &editor, ActorCollection: "users"})
	if err != nil {
		return fmt.Errorf("create post: %w", err)
	}
	// A few revisions give the document a real version history.
	for _, revision := range []store.Values{
		{"title": store.String("Ridu: notes on a Go CMS"), "content": article(sections[:2])},
		{"content": article(sections)},
		{"title": store.String(title), "metaTitle": store.String("Notes on building a CMS in Go")},
	} {
		if _, err := application.Local().PublishChanges(ctx, "posts", post.ID, revision, actor); err != nil {
			return fmt.Errorf("revise post: %w", err)
		}
	}
	return nil
}

func article(parts []struct{ heading, body string }) store.Value {
	children := make([]store.Value, 0, len(parts)*2)
	for _, part := range parts {
		children = append(children,
			node("heading", store.Values{"tag": store.String("h3")}, text(part.heading)),
			node("paragraph", nil, text(part.body)),
		)
	}
	return store.Object(store.Values{
		"version": store.Number(richtext.DocumentVersion),
		"root":    node("root", nil, children...),
	})
}

func node(kind string, extra store.Values, children ...store.Value) store.Value {
	values := store.Values{"type": store.String(kind), "children": store.List(children...)}
	for name, value := range extra {
		values[name] = value
	}
	return store.Object(values)
}

func text(value string) store.Value {
	return store.Object(store.Values{"type": store.String("text"), "text": store.String(value)})
}

// ribbon is a soft, slightly curved band of out-of-focus light.
type ribbon struct {
	cx, cy, angle, width, length, bend, intensity float64
	r, g, b                                       float64
}

// bannerJPEG renders an original abstract banner in Ridu's pink and violet.
func bannerJPEG() []byte {
	const width, height = 1600, 800
	ribbons := []ribbon{
		{cx: 300, cy: 600, angle: -1.05, width: 40, length: 300, bend: 0.4, intensity: 0.5, r: 1.0, g: 0.55, b: 0.35},
		{cx: 470, cy: 470, angle: -0.92, width: 70, length: 480, bend: 0.35, intensity: 0.42, r: 0.95, g: 0.25, b: 0.55},
		{cx: 980, cy: 330, angle: -0.70, width: 130, length: 620, bend: -0.25, intensity: 0.16, r: 0.55, g: 0.35, b: 1.0},
		{cx: 1240, cy: 300, angle: -0.95, width: 42, length: 320, bend: 0.22, intensity: 0.34, r: 0.78, g: 0.58, b: 1.0},
		{cx: 1420, cy: 640, angle: -0.35, width: 110, length: 380, bend: 0.1, intensity: 0.12, r: 0.30, g: 0.35, b: 0.95},
	}
	random := rand.New(rand.NewSource(7))
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			red, green, blue := 0.004, 0.0035, 0.007
			for _, band := range ribbons {
				dx, dy := float64(x)-band.cx, float64(y)-band.cy
				along := dx*math.Cos(band.angle) + dy*math.Sin(band.angle)
				across := -dx*math.Sin(band.angle) + dy*math.Cos(band.angle)
				across -= band.bend * along * along / band.length
				falloff := math.Exp(-along * along / (2 * band.length * band.length))
				glow := math.Exp(-across*across/(2*band.width*band.width)) * falloff
				core := math.Exp(-across*across/(2*band.width*band.width/9)) * falloff * 0.3
				amount := band.intensity * (glow + core)
				red += amount * band.r
				green += amount * band.g
				blue += amount * band.b
			}
			grain := (random.Float64() - 0.5) * 0.02
			canvas.SetRGBA(x, y, color.RGBA{R: tone(red, grain), G: tone(green, grain), B: tone(blue, grain), A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: 88}); err != nil {
		panic(err)
	}
	return encoded.Bytes()
}

// tone compresses accumulated light so bright overlaps roll off instead of clipping.
func tone(value, grain float64) uint8 {
	mapped := 1 - math.Exp(-1.3*value)
	encoded := math.Pow(mapped, 1/1.7) + grain
	return uint8(math.Round(math.Max(0, math.Min(1, encoded)) * 255))
}
