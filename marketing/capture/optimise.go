package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
)

// optimisePNG re-encodes a screenshot at maximum compression.
//
// Chrome encodes screenshots for speed, not size - it is producing them
// interactively for a debugger, not for a repository. Re-encoding the
// same pixels with Go's best compression typically takes a meaningful
// bite out of the file with no change to a single pixel.
//
// Lossless is the point. These images are evidence that the console
// looks like this, and anything that alters what they show would
// undermine that. Nothing here touches the image data: it decodes the
// PNG, checks the result, and encodes it again with different
// compression settings.
//
// If re-encoding somehow produces a larger file - possible, since PNG
// filtering heuristics differ between encoders - the original is kept.
func optimisePNG(raw []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decoding the captured screenshot: %w", err)
	}

	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("re-encoding the screenshot: %w", err)
	}

	if buf.Len() >= len(raw) {
		return raw, nil
	}

	// Cheap insurance against an encoder bug silently changing an
	// image: decode what we are about to write and confirm it still has
	// the dimensions the capture produced.
	check, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("re-encoded screenshot does not decode: %w", err)
	}
	if !sameBounds(img.Bounds(), check.Bounds()) {
		return nil, fmt.Errorf("re-encoding changed the image from %v to %v", img.Bounds(), check.Bounds())
	}

	return buf.Bytes(), nil
}

func sameBounds(a, b image.Rectangle) bool {
	return a.Dx() == b.Dx() && a.Dy() == b.Dy()
}
