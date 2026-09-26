package uploads

import (
	"image"
	"image/color"
	"testing"

	"github.com/riducms/ridu/store"
)

func TestCropPreservesExactPixelDimensionsAndAlpha(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 3000, 1646))
	source.SetNRGBA(100, 100, color.NRGBA{R: 200, G: 40, B: 90, A: 120})
	cropped := cropImage(source, 0, 0, 2400.0/3000*100, 1400.0/1646*100)
	if cropped.Bounds().Size() != image.Pt(2400, 1400) {
		t.Fatalf("crop dimensions = %v", cropped.Bounds())
	}
	_, _, _, alpha := cropped.At(100, 100).RGBA()
	if alpha != 120*257 {
		t.Fatalf("crop alpha = %v", alpha)
	}
}

func TestImportedUploadKeysCannotCrossPublicAndPrivateRoles(t *testing.T) {
	manager := Manager{Namespace: "role-test"}
	prefix := "ridu/role-test/objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/"
	for _, values := range []store.Values{
		{"objectKey": store.String(prefix + "source/private.png")},
		{"objectKey": store.String(prefix + "public.png"), "source": store.Object(store.Values{"objectKey": store.String(prefix + "public.png")})},
		{"objectKey": store.String(prefix + "public.png"), "sizes": store.Object(store.Values{"thumb": store.Object(store.Values{"objectKey": store.String(prefix + "source/private.png")})})},
	} {
		if err := manager.ValidateObjectRoles(values); err == nil {
			t.Fatal("accepted a private/public role alias")
		}
	}
}
