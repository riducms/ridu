package uploads

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
)

// imageWorkOverhead covers resampling weights, per-goroutine scan lines and
// encoder state, which do not grow with the picture.
const imageWorkOverhead = 8 << 20

// imageWork estimates, as an upper bound, the memory one image upload
// allocates while Ridu decodes, orients, caps, crops and resizes it. The
// admission budget reserves this much before decoding.
type imageWork struct {
	settings *schema.UploadSettings
	// decode is the decoded picture plus the decoder's own buffers and, for
	// JPEG, the upright copy EXIF orientation makes.
	decode func(width, height int) int64
	// encodeBytesPerPixel bounds an encoder's output buffer, which grows by
	// doubling, for incompressible pictures.
	encodeBytesPerPixel int64
	// editable pictures, JPEG and PNG, are cropped and capped.
	editable bool
	// cropWidth and cropHeight are the cropped fractions of the picture, or
	// zero when it is not cropped.
	cropWidth, cropHeight float64
}

func newImageWork(settings *schema.UploadSettings, configuration image.Config, format string, encoded []byte, edit *protocol.UploadImageEdit) (imageWork, error) {
	if len(settings.ImageSizes) > maximumImageVariants {
		return imageWork{}, fmt.Errorf("image configuration exceeds the %d-variant processing limit", maximumImageVariants)
	}
	var aggregatePixels int64
	for _, configured := range settings.ImageSizes {
		if err := validateImageDimensions(configured.Width, configured.Height); err != nil {
			return imageWork{}, fmt.Errorf("image size %q: %w", configured.Name, err)
		}
		aggregatePixels += int64(configured.Width) * int64(configured.Height)
	}
	if aggregatePixels > maximumVariantPixels {
		return imageWork{}, fmt.Errorf("image variants exceed the %d-pixel aggregate processing limit", maximumVariantPixels)
	}
	work := imageWork{settings: settings, decode: decodeWork(format, configuration.ColorModel, encoded), encodeBytesPerPixel: 10}
	if format == "jpeg" {
		work.encodeBytesPerPixel = 6
	}
	work.editable = format == "jpeg" || format == "png"
	if edit != nil && edit.CropWidth > 0 && edit.CropHeight > 0 {
		work.cropWidth, work.cropHeight = edit.CropWidth/100, edit.CropHeight/100
	}
	return work, nil
}

// bytes estimates the work for a picture of the given size. EXIF orientation
// may swap its width and height, so each stage assumes the longer side runs
// in whichever direction costs more.
func (work imageWork) bytes(width, height int) int64 {
	pixels := int64(width) * int64(height)
	side := int64(max(width, height))
	total := work.decode(width, height) + imageWorkOverhead
	dimension := int64(work.settings.MaxImageDimension)
	capped := func() {
		if !work.editable || dimension < 1 || side <= dimension {
			return
		}
		scaled := int64(math.Ceil(float64(pixels) * float64(dimension) / float64(side) * float64(dimension) / float64(side)))
		// One resampling pass keeps the full length of the other side.
		total += 4*dimension*side + (4+work.encodeBytesPerPixel)*scaled
		pixels, side = scaled, dimension
	}
	capped()
	if work.cropWidth > 0 {
		cropped := int64(math.Ceil(float64(pixels) * work.cropWidth * work.cropHeight))
		total += (4 + work.encodeBytesPerPixel) * cropped
		pixels, side = cropped, int64(math.Ceil(float64(side)*max(work.cropWidth, work.cropHeight)))
		// A source stored before the cap was configured is capped after cropping.
		capped()
	}
	// Sizes are made one at a time, each resampled from the full picture into
	// an intermediate as wide as the size and as tall as the picture.
	var largest int64
	for _, size := range work.settings.ImageSizes {
		sizeWidth, sizeHeight := int64(size.Width), int64(size.Height)
		largest = max(largest, 4*sizeWidth*side+(8+work.encodeBytesPerPixel)*sizeWidth*sizeHeight)
	}
	return total + largest
}

// largestFittingPixels returns about the most pixels a picture of the same
// shape can have while its work stays within budget, or zero.
func (work imageWork) largestFittingPixels(width, height int, budget int64) int64 {
	low, high := 0.0, 1.0
	for range 32 {
		middle := (low + high) / 2
		if work.bytes(max(1, int(float64(width)*middle)), max(1, int(float64(height)*middle))) <= budget {
			low = middle
		} else {
			high = middle
		}
	}
	return int64(float64(width)*low) * int64(float64(height)*low)
}

// tooLargeError tells the person uploading what the server can take.
func (work imageWork) tooLargeError(width, height int, budget int64) error {
	upload := fmt.Sprintf("image is %d×%d (%s megapixels)", width, height, megapixels(int64(width)*int64(height)))
	fitting := work.largestFittingPixels(width, height, budget)
	// Below a tenth of a megapixel there is nothing useful to offer.
	if fitting < 100_000 {
		return fmt.Errorf("%s, and this server's image memory budget is too small to make this collection's image sizes", upload)
	}
	// Round the limit down, so an image of that size is accepted.
	return fmt.Errorf("%s, more than this server can process at once; upload an image of at most about %.1f megapixels", upload, math.Floor(float64(fitting)/100_000)/10)
}

func megapixels(pixels int64) string {
	return fmt.Sprintf("%.1f", float64(pixels)/1_000_000)
}

// decodeWork returns the memory decoding a picture takes, by format.
func decodeWork(format string, model color.Model, encoded []byte) func(width, height int) int64 {
	switch format {
	case "jpeg":
		if frame, ok := readJPEGFrame(encoded); ok {
			return frame.decodeBytes
		}
		// Unreadable frame: assume four progressive components.
		return func(width, height int) int64 { return 28 * int64(width) * int64(height) }
	case "png":
		bytesPerPixel := int64(4)
		switch model {
		case color.GrayModel:
			bytesPerPixel = 1
		case color.Gray16Model:
			bytesPerPixel = 2
		case color.RGBA64Model, color.NRGBA64Model:
			bytesPerPixel = 8
		default:
			if _, paletted := model.(color.Palette); paletted {
				bytesPerPixel = 1
			}
		}
		// An interlaced PNG decodes each pass into its own picture first.
		if len(encoded) > 28 && encoded[28] == 1 {
			bytesPerPixel *= 2
		}
		return func(width, height int) int64 { return bytesPerPixel * int64(width) * int64(height) }
	case "gif":
		return func(width, height int) int64 { return 2 * int64(width) * int64(height) }
	default:
		return func(width, height int) int64 { return 8 * int64(width) * int64(height) }
	}
}

// jpegFrame is what a JPEG's headers say about the memory Go's decoder
// allocates for it.
type jpegFrame struct {
	progressive bool
	// sampling holds each component's horizontal and vertical sampling factors.
	sampling [][2]int
	// rgb records a three-component picture Go decodes as RGB rather than
	// YCbCr, which it converts into an extra RGBA copy.
	rgb bool
}

// readJPEGFrame reads the headers before the first scan: the frame header,
// and the JFIF and Adobe segments that decide whether the components are RGB.
func readJPEGFrame(encoded []byte) (jpegFrame, bool) {
	if len(encoded) < 4 || encoded[0] != 0xff || encoded[1] != 0xd8 {
		return jpegFrame{}, false
	}
	var frame jpegFrame
	var identifiers []byte
	found, jfif, adobeUnknownTransform := false, false, false
	for offset := 2; offset+4 <= len(encoded); {
		if encoded[offset] != 0xff {
			return jpegFrame{}, false
		}
		marker := encoded[offset+1]
		if marker == 0xff {
			// Fill byte before a marker.
			offset++
			continue
		}
		offset += 2
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd8 {
			// Markers without a length.
			continue
		}
		length := int(encoded[offset])<<8 | int(encoded[offset+1])
		if length < 2 || offset+length > len(encoded) {
			return jpegFrame{}, false
		}
		segment := encoded[offset+2 : offset+length]
		switch marker {
		case 0xc0, 0xc1, 0xc2:
			if found {
				// Go's decoder refuses a second frame header.
				return jpegFrame{}, false
			}
			// Precision, height, width, component count, then three bytes a component.
			if len(segment) < 6 {
				return jpegFrame{}, false
			}
			count := int(segment[5])
			if count < 1 || count > 4 || len(segment) < 6+3*count {
				return jpegFrame{}, false
			}
			frame.progressive = marker == 0xc2
			frame.sampling = make([][2]int, count)
			identifiers = make([]byte, count)
			for index := range count {
				identifiers[index] = segment[6+3*index]
				factors := segment[6+3*index+1]
				horizontal, vertical := int(factors>>4), int(factors&0x0f)
				if horizontal < 1 || horizontal > 4 || vertical < 1 || vertical > 4 {
					return jpegFrame{}, false
				}
				frame.sampling[index] = [2]int{horizontal, vertical}
			}
			found = true
		case 0xe0:
			jfif = jfif || len(segment) >= 5 && string(segment[:5]) == "JFIF\x00"
		case 0xee:
			if len(segment) >= 12 && string(segment[:5]) == "Adobe" {
				adobeUnknownTransform = segment[11] == 0
			}
		case 0xda:
			if !found {
				return jpegFrame{}, false
			}
			// Go's decoder treats the picture as RGB the same way.
			frame.rgb = len(identifiers) == 3 && !jfif && (adobeUnknownTransform || string(identifiers) == "RGB")
			return frame, true
		}
		offset += length
	}
	return jpegFrame{}, false
}

// flexible reports a three- or four-component sampling Go 1.27's decoder
// stores as full 4:4:4 planes: Cb and Cr sampled differently, luma not the
// most sampled, or a ratio image.YCbCr has no constant for. Go 1.26 refuses
// such a picture instead.
func (frame jpegFrame) flexible(maxHorizontal, maxVertical int) bool {
	if len(frame.sampling) < 3 {
		return false
	}
	luma, cb, cr := frame.sampling[0], frame.sampling[1], frame.sampling[2]
	if cb != cr || luma[0] != maxHorizontal || luma[1] != maxVertical {
		return true
	}
	switch (maxHorizontal/cb[0])<<4 | maxVertical/cb[1] {
	case 0x11, 0x12, 0x21, 0x22, 0x41, 0x42:
		return false
	}
	return true
}

// decodeBytes follows Go's decoder: one byte for each sample of every
// component's plane, padded to whole MCUs, or three full planes for a
// flexible sampling; four more for each sample of a progressive frame, which
// keeps every DCT coefficient as an int32; an RGBA or CMYK picture converted
// from an RGB or four-component one; and the upright NRGBA copy imaging makes
// when EXIF orientation rotates or flips the picture, which is assumed.
func (frame jpegFrame) decodeBytes(width, height int) int64 {
	maxHorizontal, maxVertical := 1, 1
	for _, factors := range frame.sampling {
		maxHorizontal, maxVertical = max(maxHorizontal, factors[0]), max(maxVertical, factors[1])
	}
	mcuWidth, mcuHeight := int64(8*maxHorizontal), int64(8*maxVertical)
	paddedWidth := (int64(width) + mcuWidth - 1) / mcuWidth * mcuWidth
	paddedHeight := (int64(height) + mcuHeight - 1) / mcuHeight * mcuHeight
	plane := func(factors [2]int) int64 {
		return paddedWidth * int64(factors[0]) / int64(maxHorizontal) * paddedHeight * int64(factors[1]) / int64(maxVertical)
	}
	var samples, planes int64
	for index, factors := range frame.sampling {
		samples += plane(factors)
		if index < 3 && frame.flexible(maxHorizontal, maxVertical) {
			planes += paddedWidth * paddedHeight
		} else {
			planes += plane(factors)
		}
	}
	pixels := int64(width) * int64(height)
	total := planes + 4*pixels
	if frame.progressive {
		total += 4 * samples
	}
	if len(frame.sampling) == 4 || frame.rgb {
		total += 4 * pixels
	}
	return total
}
