/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Package imgprep turns an uploaded image into something a vision model can
// take in a request: decoded by its content, shrunk if large, re-encoded as
// JPEG and wrapped in a data: URI.
package imgprep

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg" // image.DecodeConfig reads the size of JPEGs and PNGs
	_ "image/png"
	"os"

	"github.com/disintegration/imaging"
)

// DefaultMaxSide is the longest side, in pixels, an image keeps. Phone
// screenshots are tall and narrow, so a small limit would shrink their width
// until the small print is unreadable; the provider scales images down itself
// anyway, so this is about request size, not tokens.
const DefaultMaxSide = 2000

// DataURI reads the image at path and returns it as a data:image/jpeg;base64
// URI, no side longer than maxSide.
//
// The format is read from the file's content, not its name: an upload called
// .png can be a JPEG. Photos are turned upright from their EXIF orientation.
func DataURI(path string, maxSide int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("imgprep: open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	img, err := imaging.Decode(file, imaging.AutoOrientation(true))
	if err != nil {
		return "", fmt.Errorf("imgprep: decode %s: %w", path, err)
	}

	// Fit returns a new image rather than shrinking this one in place
	if bounds := img.Bounds(); bounds.Dx() > maxSide || bounds.Dy() > maxSide {
		img = imaging.Fit(img, maxSide, maxSide, imaging.Lanczos)
	}

	var buf bytes.Buffer
	err = imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(85))
	if err != nil {
		return "", fmt.Errorf("imgprep: encode %s: %w", path, err)
	}

	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// FitWidth shrinks an image to at most maxWidth pixels wide, keeping its shape,
// and re-encodes it as JPEG at the given quality. An image already narrow
// enough comes back as it was, with resized false: encoding it again would only
// lose detail. Only the header is read in that case, not the pixels.
func FitWidth(data []byte, maxWidth, quality int) (out []byte, width, height int, resized bool, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("imgprep: read size: %w", err)
	}
	if cfg.Width <= maxWidth {
		return data, cfg.Width, cfg.Height, false, nil
	}

	img, err := imaging.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("imgprep: decode: %w", err)
	}
	img = imaging.Resize(img, maxWidth, 0, imaging.Lanczos)

	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(quality)); err != nil {
		return nil, 0, 0, false, fmt.Errorf("imgprep: encode: %w", err)
	}
	b := img.Bounds()
	return buf.Bytes(), b.Dx(), b.Dy(), true, nil
}
