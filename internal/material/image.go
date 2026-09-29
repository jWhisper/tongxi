package material

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"strings"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

type ImagePreview struct {
	DataURL string `json:"dataURL"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

func IsImage(format string) bool {
	switch strings.ToLower(strings.TrimPrefix(format, ".")) {
	case "png", "jpg", "jpeg", "webp":
		return true
	}
	return false
}

func imageConfig(data []byte) (image.Config, string, error) {
	c, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || !IsImage(format) {
		return c, format, errors.New("图片无法解码，请使用 PNG、JPEG 或 WebP")
	}
	if c.Width <= 0 || c.Height <= 0 || int64(c.Width)*int64(c.Height) > 25000000 {
		return c, format, errors.New("图片超过2500万像素，请缩小后添加")
	}
	return c, format, nil
}

// Previews and model input are derived in memory. Originals stay in the workspace.
func PrepareImage(ctx context.Context, data []byte, maxEdge int) (ImagePreview, error) {
	if len(data) == 0 || len(data) > MaxFileBytes {
		return ImagePreview{}, errors.New("图片为空或超过20MB")
	}
	if _, _, err := imageConfig(data); err != nil {
		return ImagePreview{}, err
	}
	if err := ctx.Err(); err != nil {
		return ImagePreview{}, err
	}
	im, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return ImagePreview{}, errors.New("图片损坏，无法读取")
	}
	width, height := im.Bounds().Dx(), im.Bounds().Dy()
	if width > maxEdge || height > maxEdge {
		im = imaging.Fit(im, maxEdge, maxEdge, imaging.Lanczos)
	}
	canvas := imaging.New(im.Bounds().Dx(), im.Bounds().Dy(), color.White)
	canvas = imaging.Overlay(canvas, im, image.Point{}, 1)
	var out bytes.Buffer
	if err = jpeg.Encode(&out, canvas, &jpeg.Options{Quality: 90}); err != nil {
		return ImagePreview{}, err
	}
	if err = ctx.Err(); err != nil {
		return ImagePreview{}, err
	}
	return ImagePreview{DataURL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), Width: width, Height: height}, nil
}
