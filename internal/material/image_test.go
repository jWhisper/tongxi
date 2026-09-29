package material

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestImageValidationAndPreparation(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 2000, 1000))
	im.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(context.Background(), "photo.png", b.Bytes())
	if err != nil || len(doc.Segments) != 0 || doc.Note != "图片 · 2000 × 1000" {
		t.Fatal(doc, err)
	}
	p, err := PrepareImage(context.Background(), b.Bytes(), 1536)
	if err != nil || p.Width != 2000 || p.Height != 1000 {
		t.Fatal(p.Width, p.Height, err)
	}
	encoded := strings.TrimPrefix(p.DataURL, "data:image/jpeg;base64,")
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	converted, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "jpeg" || converted.Bounds().Dx() != 1536 || converted.Bounds().Dy() != 768 {
		t.Fatal(format, err)
	}
	r, g, blue, _ := converted.At(700, 400).RGBA()
	if r < 64000 || g < 64000 || blue < 64000 {
		t.Fatal("transparent pixels must be white")
	}
	if _, err = Parse(context.Background(), "wrong.jpg", b.Bytes()); err == nil {
		t.Fatal("mismatched format accepted")
	}
	if _, err = PrepareImage(context.Background(), []byte("not an image"), 1536); err == nil {
		t.Fatal("invalid image accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = PrepareImage(ctx, b.Bytes(), 1536); err != context.Canceled {
		t.Fatal(err)
	}
}
