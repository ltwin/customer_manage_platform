package creativeimageedit

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"

	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"golang.org/x/image/webp"
)

// admitSourceSize reads only the verified format's header. Writing a .v
// intermediate already decodes every pixel, so this must precede libvips.
func (c *Codec) admitSourceSize(path, mime string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	var cfg image.Config
	switch mime {
	case "image/jpeg":
		cfg, err = jpeg.DecodeConfig(r)
	case "image/png":
		cfg, err = png.DecodeConfig(r)
	case "image/webp":
		cfg, err = webp.DecodeConfig(r)
	default:
		return fmt.Errorf("%w: %s", creativemedia.ErrUnsupported, mime)
	}
	if err != nil {
		return fmt.Errorf("%w: source dimensions: %v", creativemedia.ErrUnsupported, err)
	}
	return c.admitDimensions(cfg.Width, cfg.Height)
}

// scanSource reads the container structure that libvips would silently
// paper over: APNG loads as its first frame, and an unparsable ICC profile
// is dropped at load. It reports whether the file declares a profile and
// refuses animation. Only the headers before the pixel data are read.
func scanSource(path, mime string) (declaresProfile bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	switch mime {
	case "image/png":
		declaresProfile, err = scanPNG(r)
	case "image/jpeg":
		declaresProfile, err = scanJPEG(r)
	case "image/webp":
		declaresProfile, err = scanWebP(r)
	}
	if err != nil {
		return false, fmt.Errorf("%w: %v", creativemedia.ErrUnsupported, err)
	}
	return declaresProfile, nil
}

func scanPNG(r *bufio.Reader) (bool, error) {
	if _, err := r.Discard(8); err != nil {
		return false, err
	}
	profile := false
	head := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, head); err != nil {
			return false, err
		}
		switch string(head[4:8]) {
		case "acTL":
			return false, fmt.Errorf("animated png")
		case "iCCP":
			profile = true
		case "IDAT", "IEND":
			return profile, nil
		}
		// Skip the payload and its CRC.
		if _, err := r.Discard(int(binary.BigEndian.Uint32(head[:4])) + 4); err != nil {
			return false, err
		}
	}
}

func scanJPEG(r *bufio.Reader) (bool, error) {
	soi := make([]byte, 2)
	if _, err := io.ReadFull(r, soi); err != nil || soi[0] != 0xFF || soi[1] != 0xD8 {
		return false, fmt.Errorf("jpeg signature")
	}
	for {
		b, err := r.ReadByte()
		if err != nil {
			return false, err
		}
		if b != 0xFF {
			return false, fmt.Errorf("jpeg marker")
		}
		marker, err := r.ReadByte()
		for err == nil && marker == 0xFF {
			marker, err = r.ReadByte()
		}
		if err != nil {
			return false, err
		}
		switch {
		case marker == 0xDA || marker == 0xD9:
			// Start of scan or end of image: every profile segment precedes it.
			return false, nil
		case (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01:
			continue
		}
		size := make([]byte, 2)
		if _, err := io.ReadFull(r, size); err != nil {
			return false, err
		}
		length := int(binary.BigEndian.Uint16(size)) - 2
		if length < 0 {
			return false, fmt.Errorf("jpeg segment length")
		}
		if marker == 0xE2 && length >= 12 {
			id, err := r.Peek(12)
			if err != nil {
				return false, err
			}
			if bytes.Equal(id, []byte("ICC_PROFILE\x00")) {
				return true, nil
			}
		}
		if _, err := r.Discard(length); err != nil {
			return false, err
		}
	}
}

func scanWebP(r *bufio.Reader) (bool, error) {
	riff := make([]byte, 12)
	if _, err := io.ReadFull(r, riff); err != nil || string(riff[:4]) != "RIFF" || string(riff[8:12]) != "WEBP" {
		return false, fmt.Errorf("webp signature")
	}
	profile := false
	head := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, head); err != nil {
			if err == io.EOF {
				return profile, nil
			}
			return false, err
		}
		switch string(head[:4]) {
		case "ANIM", "ANMF":
			return false, fmt.Errorf("animated webp")
		case "ICCP":
			profile = true
		case "VP8 ", "VP8L":
			return profile, nil
		}
		size := int(binary.LittleEndian.Uint32(head[4:8]))
		if _, err := r.Discard(size + size%2); err != nil {
			return false, err
		}
	}
}
