package uploads

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand/v2"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/disintegration/imaging"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
)

// postFiles is the upload configuration the memory report came from.
var postFiles = &schema.UploadSettings{MaxFileSize: 64 << 20, MimeTypes: []string{"image/*"}, ImageSizes: []schema.ImageSize{
	{Name: "medium", Width: 800, Height: 800, Fit: "contain"},
	{Name: "large", Width: 1600, Height: 1600, Fit: "contain"},
}}

// photoJPEG encodes a gradient with noise, so the encoder and decoder do the
// work a camera photo needs rather than compressing a flat picture away.
func photoJPEG(t testing.TB, width, height int, gray bool) []byte {
	t.Helper()
	random := rand.New(rand.NewPCG(uint64(width), uint64(height)))
	var picture image.Image
	if gray {
		canvas := image.NewGray(image.Rect(0, 0, width, height))
		for index := range canvas.Pix {
			canvas.Pix[index] = uint8(index%251) ^ uint8(random.IntN(32))
		}
		picture = canvas
	} else {
		canvas := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := range height {
			for x := range width {
				offset := canvas.PixOffset(x, y)
				canvas.Pix[offset] = uint8(x*255/width) ^ uint8(random.IntN(32))
				canvas.Pix[offset+1] = uint8(y*255/height) ^ uint8(random.IntN(32))
				canvas.Pix[offset+2] = uint8((x+y)%256) ^ uint8(random.IntN(32))
				canvas.Pix[offset+3] = 255
			}
		}
		picture = canvas
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// rotatedJPEG adds EXIF orientation 6, so imaging decodes an upright copy.
func rotatedJPEG(encoded []byte) []byte {
	exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	rotated := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(exif) + 2)}, exif...)
	return append(rotated, encoded[2:]...)
}

func allocated(run func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	run()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// jpegHeader builds the segments before a scan: the given APP segments, a
// frame header for three components with the given identifiers and sampling
// factors, and the start of a scan.
func jpegHeader(marker byte, identifiers string, sampling [3]byte, apps ...[]byte) []byte {
	header := []byte{0xff, 0xd8}
	for _, app := range apps {
		header = append(header, app...)
	}
	header = append(header, 0xff, marker, 0, 17, 8, 0, 16, 0, 32, 3)
	for index := range 3 {
		header = append(header, identifiers[index], sampling[index], 0)
	}
	return append(header, 0xff, 0xda, 0, 2)
}

var (
	jfifSegment  = []byte{0xff, 0xe0, 0, 7, 'J', 'F', 'I', 'F', 0}
	adobeUnknown = []byte{0xff, 0xee, 0, 14, 'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, 0}
)

func TestReadJPEGFrameFindsSamplingProgressionAndRGB(t *testing.T) {
	color, ok := readJPEGFrame(photoJPEG(t, 32, 16, false))
	if !ok || color.progressive || color.rgb || !reflect.DeepEqual(color.sampling, [][2]int{{2, 2}, {1, 1}, {1, 1}}) {
		t.Fatalf("baseline 4:2:0 frame = %+v, %v", color, ok)
	}
	gray, ok := readJPEGFrame(photoJPEG(t, 32, 16, true))
	if !ok || !reflect.DeepEqual(gray.sampling, [][2]int{{1, 1}}) {
		t.Fatalf("grayscale frame = %+v, %v", gray, ok)
	}
	// A fill byte before the progressive frame header.
	progressive := jpegHeader(0xc2, "\x01\x02\x03", [3]byte{0x11, 0x11, 0x11}, jfifSegment, []byte{0xff})
	if frame, ok := readJPEGFrame(progressive); !ok || !frame.progressive || frame.rgb || !reflect.DeepEqual(frame.sampling, [][2]int{{1, 1}, {1, 1}, {1, 1}}) {
		t.Fatalf("progressive frame = %+v, %v", frame, ok)
	}
	for name, test := range map[string]struct {
		header []byte
		rgb    bool
	}{
		"Adobe transform 0":           {header: jpegHeader(0xc0, "\x01\x02\x03", [3]byte{0x11, 0x11, 0x11}, adobeUnknown), rgb: true},
		"components named R, G and B": {header: jpegHeader(0xc0, "RGB", [3]byte{0x11, 0x11, 0x11}), rgb: true},
		"JFIF wins over the names":    {header: jpegHeader(0xc0, "RGB", [3]byte{0x11, 0x11, 0x11}, jfifSegment)},
	} {
		if frame, ok := readJPEGFrame(test.header); !ok || frame.rgb != test.rgb {
			t.Fatalf("%s: frame = %+v, %v", name, frame, ok)
		}
	}
	twoFrames := jpegHeader(0xc0, "\x01\x02\x03", [3]byte{0x22, 0x11, 0x11})
	twoFrames = append(twoFrames[:len(twoFrames)-4], jpegHeader(0xc0, "\x01\x02\x03", [3]byte{0x11, 0x11, 0x11})[2:]...)
	for name, encoded := range map[string][]byte{
		"not a JPEG":        []byte("GIF89a"),
		"truncated":         progressive[:len(progressive)-8],
		"no frame header":   progressive[:len(progressive)-4][:2],
		"scan before frame": {0xff, 0xd8, 0xff, 0xda, 0, 2},
		"two frame headers": twoFrames,
	} {
		if _, ok := readJPEGFrame(encoded); ok {
			t.Fatalf("%s: read a frame", name)
		}
	}
}

func TestFlexibleJPEGSamplingReservesFullPlanes(t *testing.T) {
	const width, height = 8000, 5000
	for _, sampling := range [][3]byte{{0x22, 0x11, 0x22}, {0x11, 0x11, 0x22}, {0x24, 0x11, 0x11}} {
		frame, ok := readJPEGFrame(jpegHeader(0xc0, "\x01\x02\x03", sampling, jfifSegment))
		if !ok {
			t.Fatalf("sampling %x: no frame", sampling)
		}
		// Three full planes and the upright copy, as Go 1.27 allocates.
		if reserved, least := frame.decodeBytes(width, height), int64(3+4)*width*height; reserved < least {
			t.Fatalf("sampling %x reserves %d bytes, below the %d decoded", sampling, reserved, least)
		}
	}
}

func TestJPEGDecodeEstimateBoundsTheDecoder(t *testing.T) {
	for _, gray := range []bool{false, true} {
		encoded := photoJPEG(t, 1201, 901, gray)
		frame, _ := readJPEGFrame(encoded)
		estimate := frame.decodeBytes(1201, 901)
		plain := allocated(func() {
			if _, err := jpeg.Decode(bytes.NewReader(encoded)); err != nil {
				t.Fatal(err)
			}
		})
		// Without EXIF orientation there is no upright copy to make.
		if limit := estimate - 4*1201*901 + 256<<10; plain > uint64(limit) {
			t.Fatalf("gray=%v: decoding allocated %d bytes, above the %d estimated", gray, plain, limit)
		}
		rotated := rotatedJPEG(encoded)
		upright := allocated(func() {
			if _, err := imaging.Decode(bytes.NewReader(rotated), imaging.AutoOrientation(true)); err != nil {
				t.Fatal(err)
			}
		})
		if limit := estimate + 256<<10; upright > uint64(limit) {
			t.Fatalf("gray=%v: decoding and orienting allocated %d bytes, above the %d estimated", gray, upright, limit)
		}
	}
	// Renaming the components R, G and B makes Go decode the picture as RGB,
	// converting it into an extra RGBA copy.
	encoded := photoJPEG(t, 1201, 901, false)
	for offset := 0; offset+1 < len(encoded); offset++ {
		if encoded[offset] == 0xff && (encoded[offset+1] == 0xc0 || encoded[offset+1] == 0xda) {
			first := offset + 10
			if encoded[offset+1] == 0xda {
				first = offset + 5
			}
			step := 3
			if encoded[offset+1] == 0xda {
				step = 2
			}
			for index, name := range []byte("RGB") {
				encoded[first+step*index] = name
			}
		}
	}
	frame, ok := readJPEGFrame(encoded)
	if !ok || !frame.rgb {
		t.Fatalf("renamed frame = %+v, %v", frame, ok)
	}
	rotated := rotatedJPEG(encoded)
	upright := allocated(func() {
		if _, err := imaging.Decode(bytes.NewReader(rotated), imaging.AutoOrientation(true)); err != nil {
			t.Fatal(err)
		}
	})
	if limit := frame.decodeBytes(1201, 901) + 256<<10; upright > uint64(limit) {
		t.Fatalf("decoding and orienting an RGB JPEG allocated %d bytes, above the %d estimated", upright, limit)
	}
}

func TestImageWorkTakesCurrentPhonePhotosWithinTheDefaultBudget(t *testing.T) {
	configuration := image.Config{ColorModel: color.YCbCrModel}
	work, err := newImageWork(postFiles, configuration, "jpeg", photoJPEG(t, 32, 16, false), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, photo := range []struct {
		name          string
		width, height int
		within        int64
	}{
		{name: "12 MP", width: 4032, height: 3024, within: 160 << 20},
		{name: "24.5 MP, the iPhone 15 default", width: 5712, height: 4284, within: 256 << 20},
		{name: "the 40 MP limit", width: 7300, height: 5475, within: defaultImageBudgetBytes},
	} {
		if reserved := work.bytes(photo.width, photo.height); reserved > photo.within {
			t.Errorf("%s reserves %d MiB, want at most %d MiB", photo.name, reserved>>20, photo.within>>20)
		}
	}
}

func TestImageBudgetFollowsTheGoMemoryLimit(t *testing.T) {
	for limit, want := range map[int64]int64{math.MaxInt64: defaultImageBudgetBytes, 0: defaultImageBudgetBytes, 400 << 20: 200 << 20, 8 << 30: 4 << 30} {
		if got := imageBudget(limit); got != want {
			t.Errorf("imageBudget(%d) = %d, want %d", limit, got, want)
		}
	}
}

func TestTooLargeImageErrorSaysWhatFits(t *testing.T) {
	work, err := newImageWork(postFiles, image.Config{ColorModel: color.YCbCrModel}, "jpeg", photoJPEG(t, 32, 16, false), nil)
	if err != nil {
		t.Fatal(err)
	}
	const budget = 120 << 20
	message := work.tooLargeError(5712, 4284, budget).Error()
	match := regexp.MustCompile(`^image is 5712×4284 \(24\.5 megapixels\), more than this server can process at once; upload an image of at most about ([0-9.]+) megapixels$`).FindStringSubmatch(message)
	if match == nil {
		t.Fatalf("message = %q", message)
	}
	limit, _ := strconv.ParseFloat(match[1], 64)
	scale := math.Sqrt(limit * 1_000_000 / (5712 * 4284))
	if limit < 1 || work.bytes(int(5712*scale), int(4284*scale)) > budget {
		t.Fatalf("an image of the %v megapixels offered does not fit the budget", limit)
	}
	if message := work.tooLargeError(5712, 4284, 1<<20).Error(); !strings.Contains(message, "image memory budget is too small") {
		t.Fatalf("message without any fitting size = %q", message)
	}
}

func TestPrepareAllocatesNoMoreThanItReserves(t *testing.T) {
	if testing.Short() {
		t.Skip("encodes and processes a 12 MP photo")
	}
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	encoded := rotatedJPEG(photoJPEG(t, 4032, 3024, false))
	collection := schema.Collection{Slug: "post-files", Upload: postFiles}
	work, err := newImageWork(postFiles, image.Config{ColorModel: color.YCbCrModel}, "jpeg", encoded, nil)
	if err != nil {
		t.Fatal(err)
	}
	reserved := work.bytes(4032, 3024)
	manager := Manager{Backend: backend, Locker: teststore.New(), Namespace: "memory"}
	var prepared Prepared
	used := allocated(func() {
		prepared, err = manager.Prepare(context.Background(), collection, Input{Filename: "photo.jpg", Reader: bytes.NewReader(encoded)})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	// Reading the upload and writing objects allocate besides the image work.
	limit := uint64(reserved) + 4*uint64(len(encoded)) + 4<<20
	t.Log(fmt.Sprintf("reserved %d MiB, allocated %d MiB", reserved>>20, used>>20))
	if used > limit {
		t.Fatalf("Prepare allocated %d bytes, above the %d reserved for its image work", used, limit)
	}
	if width, _ := prepared.Values["width"].NumberValue(); width != 3024 {
		t.Fatalf("upright width = %v, want 3024", width)
	}
}
