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

// UploadGrantRequestItem names one upload document and optional image size.
type UploadGrantRequestItem struct {
	ID   string `json:"id"`
	Size string `json:"size,omitempty"`
}

// UploadGrantsRequest asks for short-lived delivery URLs. ExpiresIn is in
// seconds; zero selects the server default.
type UploadGrantsRequest struct {
	Items     []UploadGrantRequestItem `json:"items"`
	ExpiresIn int                      `json:"expiresIn,omitempty"`
}

// UploadGrant is a delivery URL that authorizes one object without carrying
// the session token.
type UploadGrant struct {
	ID        string `json:"id"`
	Size      string `json:"size,omitempty"`
	URL       string `json:"url"`
	ExpiresAt string `json:"expiresAt"`
}

// UploadGrantsEnvelope returns grants in request order.
type UploadGrantsEnvelope struct {
	Grants []UploadGrant `json:"grants"`
}
