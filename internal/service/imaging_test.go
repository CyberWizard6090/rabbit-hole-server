package service

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func asymmetric() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for i := 0; i < 6; i++ {
		img.Set(i%3, i/3, color.NRGBA{R: uint8(i + 1), A: 255})
	}
	return img
}

func pixelOrder(img image.Image) []uint8 {
	b := img.Bounds()
	var out []uint8
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			out = append(out, uint8(r>>8))
		}
	}
	return out
}

func TestOrientAppliesEveryEXIFOrientation(t *testing.T) {
	src := asymmetric()
	cases := map[int]struct {
		w, h int
		want []uint8
	}{
		1: {3, 2, []uint8{1, 2, 3, 4, 5, 6}},
		2: {3, 2, []uint8{3, 2, 1, 6, 5, 4}},
		3: {3, 2, []uint8{6, 5, 4, 3, 2, 1}},
		4: {3, 2, []uint8{4, 5, 6, 1, 2, 3}},
		5: {2, 3, []uint8{1, 4, 2, 5, 3, 6}},
		6: {2, 3, []uint8{4, 1, 5, 2, 6, 3}},
		7: {2, 3, []uint8{6, 3, 5, 2, 4, 1}},
		8: {2, 3, []uint8{3, 6, 2, 5, 1, 4}},
	}
	for orientation, want := range cases {
		got := orient(src, orientation)
		if b := got.Bounds(); b.Dx() != want.w || b.Dy() != want.h {
			t.Errorf("orientation %d: size %v, want %dx%d", orientation, b, want.w, want.h)
			continue
		}
		if order := pixelOrder(got); !bytes.Equal(order, want.want) {
			t.Errorf("orientation %d: pixels %v, want %v", orientation, order, want.want)
		}
	}
	if got := orient(src, 0); got != image.Image(src) {
		t.Error("orientation 0 must return the image unchanged")
	}
}

func exifSegment(orientation uint16, bigEndian bool) []byte {
	var order binary.AppendByteOrder = binary.LittleEndian
	header := "II"
	if bigEndian {
		order, header = binary.BigEndian, "MM"
	}
	tiff := []byte(header)
	tiff = order.AppendUint16(tiff, 42)
	tiff = order.AppendUint32(tiff, 8)
	tiff = order.AppendUint16(tiff, 1)
	tiff = order.AppendUint16(tiff, 0x0112)
	tiff = order.AppendUint16(tiff, 3)
	tiff = order.AppendUint32(tiff, 1)
	tiff = order.AppendUint16(tiff, orientation)
	tiff = append(tiff, 0, 0)
	tiff = order.AppendUint32(tiff, 0)

	payload := append([]byte("Exif\x00\x00"), tiff...)
	segment := []byte{0xFF, 0xE1}
	segment = binary.BigEndian.AppendUint16(segment, uint16(len(payload)+2))
	return append(segment, payload...)
}

func jpegBytes(t *testing.T, extra []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	return append(append(append([]byte{}, data[:2]...), extra...), data[2:]...)
}

func TestJPEGOrientationReadsBothByteOrders(t *testing.T) {
	for _, bigEndian := range []bool{true, false} {
		for want := 1; want <= 8; want++ {
			data := jpegBytes(t, exifSegment(uint16(want), bigEndian))
			if got := jpegOrientation(data); got != want {
				t.Errorf("bigEndian=%t orientation %d: got %d", bigEndian, want, got)
			}
		}
	}
	if got := jpegOrientation(jpegBytes(t, nil)); got != 1 {
		t.Errorf("no EXIF: got %d, want 1", got)
	}
	if got := jpegOrientation(jpegBytes(t, exifSegment(9, true))); got != 1 {
		t.Errorf("out of range value: got %d, want 1", got)
	}
}

func TestJPEGOrientationSurvivesMalformedInput(t *testing.T) {
	valid := jpegBytes(t, exifSegment(6, true))
	inputs := [][]byte{
		nil, {0xFF}, {0xFF, 0xD8}, {0xFF, 0xD8, 0xFF}, {0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF},
		{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x02}, []byte("Exif not a jpeg"),
	}
	for cut := 0; cut < len(valid) && cut < 80; cut++ {
		inputs = append(inputs, valid[:cut])
	}
	broken := append([]byte{}, valid...)
	for i := 12; i < 40 && i < len(broken); i++ {
		broken[i] = 0xFF
	}
	inputs = append(inputs, broken)

	for i, data := range inputs {
		if got := jpegOrientation(data); got < 1 || got > 8 {
			t.Errorf("input %d: orientation %d out of range", i, got)
		}
	}
}

func pngWithSize(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	binary.BigEndian.PutUint32(data[16:20], uint32(width))
	binary.BigEndian.PutUint32(data[20:24], uint32(height))
	return data
}

func TestInspectImage(t *testing.T) {
	avif := append([]byte{0, 0, 0, 0x1c}, []byte("ftypavif")...)
	avif = append(avif, make([]byte, 20)...)

	cases := []struct {
		name string
		data []byte
		want error
		ct   string
	}{
		{"png", pngWithSize(t, 4, 4), nil, "image/png"},
		{"avif by signature", avif, nil, "image/avif"},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), domain.ErrUnsupportedFileType, ""},
		{"html", []byte("<!DOCTYPE html><html></html>"), domain.ErrUnsupportedFileType, ""},
		{"text", []byte("hello"), domain.ErrUnsupportedFileType, ""},
		{"zip", []byte("PK\x03\x04rest"), domain.ErrUnsupportedFileType, ""},
		{"truncated png", pngWithSize(t, 4, 4)[:20], domain.ErrInvalidImage, ""},
		{"too many pixels", pngWithSize(t, 15000, 15000), domain.ErrInvalidImage, ""},
		{"too wide", pngWithSize(t, 25000, 1), domain.ErrInvalidImage, ""},
		{"zero width", pngWithSize(t, 0, 4), domain.ErrInvalidImage, ""},
	}
	for _, tc := range cases {
		ct, _, err := inspectImage(tc.data, maxImagePixels)
		if !errors.Is(err, tc.want) || ct != tc.ct {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", tc.name, ct, err, tc.ct, tc.want)
		}
	}
}

func TestRenderAvatarOutputs(t *testing.T) {
	var opaque, translucent bytes.Buffer
	flat := image.NewNRGBA(image.Rect(0, 0, 600, 300))
	for i := range flat.Pix {
		flat.Pix[i] = 255
	}
	if err := png.Encode(&opaque, flat); err != nil {
		t.Fatal(err)
	}
	for i := 3; i < len(flat.Pix); i += 4 {
		flat.Pix[i] = 100
	}
	if err := png.Encode(&translucent, flat); err != nil {
		t.Fatal(err)
	}

	out, ct, err := renderAvatar(opaque.Bytes())
	if err != nil || ct != "image/jpeg" {
		t.Fatalf("opaque: %q %v", ct, err)
	}
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(out)); cfg.Width != avatarSize || cfg.Height != avatarSize {
		t.Fatalf("opaque avatar is %dx%d", cfg.Width, cfg.Height)
	}
	out, ct, err = renderAvatar(translucent.Bytes())
	if err != nil || ct != "image/png" {
		t.Fatalf("translucent: %q %v", ct, err)
	}
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(out)); cfg.Width != avatarSize {
		t.Fatalf("translucent avatar is %dx%d", cfg.Width, cfg.Height)
	}

	small := image.NewNRGBA(image.Rect(0, 0, 50, 80))
	for i := range small.Pix {
		small.Pix[i] = 255
	}
	var smallPNG bytes.Buffer
	_ = png.Encode(&smallPNG, small)
	out, _, err = renderAvatar(smallPNG.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(out)); cfg.Width != 50 || cfg.Height != 50 {
		t.Fatalf("a small image must not be upscaled: %dx%d", cfg.Width, cfg.Height)
	}
}

func TestRenderAvatarRejectsUnsafeInput(t *testing.T) {
	huge := pngWithSize(t, 5000, 5000)
	avif := append([]byte{0, 0, 0, 0x1c}, []byte("ftypavif")...)
	for name, data := range map[string][]byte{
		"svg":                 []byte("<svg/>"),
		"too many pixels":     huge,
		"avif is not decoded": avif,
		"garbage":             bytes.Repeat([]byte{1, 2, 3}, 300),
	} {
		if _, _, err := renderAvatar(data); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	long := strings.Repeat("я", 200) + ".txt"
	cases := map[string]string{
		"report.pdf":               "report.pdf",
		`C:\Users\evil\report.pdf`: "report.pdf",
		"../../etc/passwd":         "passwd",
		"/etc/shadow":              "shadow",
		"  spaced name.txt  ":      "spaced name.txt",
		"..":                       "file",
		".":                        "file",
		"":                         "file",
		"   ":                      "file",
		"a\x00b\x1f\nc.txt":        "abc.txt",
		"\xff\xfebroken.txt":       "broken.txt",
		"тест файл.docx":           "тест файл.docx",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
	got := sanitizeFilename(long)
	if len(got) > maxFilenameBytes || !strings.HasPrefix(got, "я") {
		t.Errorf("long name: %d bytes", len(got))
	}
	if got != strings.ToValidUTF8(got, "") {
		t.Error("truncation produced invalid UTF-8")
	}
}

func TestLimitedReader(t *testing.T) {
	exact := &limitedReader{r: strings.NewReader("12345"), left: 5}
	data, err := readAll(exact)
	if err != nil || string(data) != "12345" {
		t.Fatalf("exact: %q %v", data, err)
	}
	over := &limitedReader{r: strings.NewReader("123456"), left: 5}
	if _, err := readAll(over); !errors.Is(err, domain.ErrFileTooLarge) {
		t.Fatalf("over: %v", err)
	}
	if _, err := readLimited(strings.NewReader(""), 10); !errors.Is(err, domain.ErrEmptyFile) {
		t.Fatalf("empty: %v", err)
	}
}

func readAll(r *limitedReader) ([]byte, error) {
	return readLimited(r, 1<<30)
}
