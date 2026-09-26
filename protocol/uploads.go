package protocol

// UploadImageEdit describes a reversible crop and focal point on the oriented
// source image. Coordinates are percentages of that source, not the rendition.
// A zero width and height clear the crop.
type UploadImageEdit struct {
	FocalX     float64 `json:"focalX"`
	FocalY     float64 `json:"focalY"`
	CropX      float64 `json:"cropX"`
	CropY      float64 `json:"cropY"`
	CropWidth  float64 `json:"cropWidth"`
	CropHeight float64 `json:"cropHeight"`
}
