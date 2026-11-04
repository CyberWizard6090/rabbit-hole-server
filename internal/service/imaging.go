package service

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"net/http"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"rabbit-hole-server/internal/domain"
)

const (
	avatarSize       = 256
	jpegQuality      = 85
	maxImagePixels   = 100_000_000
	maxAvatarPixels  = 16_000_000
	maxImageSidePx   = 20000
	sniffLength      = 512
	exifSearchWindow = 64 * 1024
)

var decodableImageTypes = map[string]struct{}{
	"image/png": {}, "image/jpeg": {}, "image/gif": {}, "image/webp": {},
}

func sniffContentType(head []byte) string {
	if len(head) >= 12 && string(head[4:8]) == "ftyp" && (string(head[8:12]) == "avif" || string(head[8:12]) == "avis") {
		return "image/avif"
	}
	if len(head) > sniffLength {
		head = head[:sniffLength]
	}
	return http.DetectContentType(head)
}

func inspectImage(data []byte, maxPixels int) (string, image.Config, error) {
	contentType := sniffContentType(data)
	if contentType == "image/avif" {
		return contentType, image.Config{}, nil
	}
	if _, ok := decodableImageTypes[contentType]; !ok {
		return "", image.Config{}, domain.ErrUnsupportedFileType
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", image.Config{}, domain.ErrInvalidImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxImageSidePx || cfg.Height > maxImageSidePx ||
		cfg.Width*cfg.Height > maxPixels {
		return "", image.Config{}, domain.ErrInvalidImage
	}
	return contentType, cfg, nil
}

func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	if len(data) > exifSearchWindow {
		data = data[:exifSearchWindow]
	}
	pos := 2
	for pos+4 <= len(data) {
		if data[pos] != 0xFF {
			return 1
		}
		marker := data[pos+1]
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}
		length := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		if length < 2 || pos+2+length > len(data) {
			return 1
		}
		if marker == 0xE1 {
			if o := exifOrientation(data[pos+4 : pos+2+length]); o != 0 {
				return o
			}
		}
		pos += 2 + length
	}
	return 1
}

func exifOrientation(segment []byte) int {
	if len(segment) < 14 || string(segment[:6]) != "Exif\x00\x00" {
		return 0
	}
	tiff := segment[6:]
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 0
	}
	offset := int(order.Uint32(tiff[4:8]))
	if offset < 8 || offset+2 > len(tiff) {
		return 0
	}
	count := int(order.Uint16(tiff[offset : offset+2]))
	for i := 0; i < count; i++ {
		entry := offset + 2 + i*12
		if entry+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[entry:entry+2]) == 0x0112 {
			value := int(order.Uint16(tiff[entry+8 : entry+10]))
			if value >= 1 && value <= 8 {
				return value
			}
			return 0
		}
	}
	return 0
}

func orient(src image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	swap := orientation >= 5
	dw, dh := w, h
	if swap {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var nx, ny int
			switch orientation {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			dst.Set(nx, ny, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func renderAvatar(data []byte) ([]byte, string, error) {
	contentType, _, err := inspectImage(data, maxAvatarPixels)
	if err != nil {
		return nil, "", err
	}
	if contentType == "image/avif" {
		return nil, "", domain.ErrUnsupportedFileType
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", domain.ErrInvalidImage
	}
	if contentType == "image/jpeg" {
		decoded = orient(decoded, jpegOrientation(data))
	}

	b := decoded.Bounds()
	side := min(b.Dx(), b.Dy())
	crop := image.Rect(
		b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2,
		b.Min.X+(b.Dx()-side)/2+side, b.Min.Y+(b.Dy()-side)/2+side,
	)
	target := min(side, avatarSize)
	scaled := image.NewNRGBA(image.Rect(0, 0, target, target))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), decoded, crop, draw.Src, nil)

	var out bytes.Buffer
	if scaled.Opaque() {
		if err := jpeg.Encode(&out, scaled, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return nil, "", err
		}
		return out.Bytes(), "image/jpeg", nil
	}
	if err := png.Encode(&out, scaled); err != nil {
		return nil, "", err
	}
	return out.Bytes(), "image/png", nil
}
