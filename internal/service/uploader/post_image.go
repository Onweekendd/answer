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

package uploader

import (
	"context"
	stderrors "errors"
	"path"
	"path/filepath"
	"strings"

	"github.com/apache/answer/internal/base/constant"
)

// ErrNotPostImage means a URL does not point at an image uploaded to a post
// and stored on this server's disk, for instance one kept by a storage plugin.
var ErrNotPostImage = stderrors.New("not an image uploaded to a post on this site")

// PostImagePath maps the URL of an image uploaded to a post back to its file on
// disk, so the server can read the image instead of fetching its own URL.
func (us *uploaderService) PostImagePath(ctx context.Context, imageURL string) (string, error) {
	siteGeneral, err := us.siteInfoService.GetSiteGeneral(ctx)
	if err != nil {
		return "", err
	}
	return postImagePath(siteGeneral.SiteUrl, us.serviceConfig.UploadPath, imageURL)
}

// postImagePath reverses uploadImageFile, which stores a post image at
// UploadPath/post/<name> and serves it at SiteUrl/uploads/post/<name>.
//
// The URL comes from the database, where users wrote it, so it is treated as
// hostile: a "..", or a backslash that Windows reads as a separator, could
// otherwise walk out of the upload directory to the config file and its keys.
// Anything that does not land inside UploadPath/post is refused.
func postImagePath(siteURL, uploadPath, imageURL string) (string, error) {
	prefix := strings.TrimSuffix(siteURL, "/") + "/uploads/"
	if siteURL == "" || !strings.HasPrefix(imageURL, prefix) {
		return "", ErrNotPostImage
	}
	sub := strings.TrimPrefix(imageURL, prefix)
	if strings.ContainsAny(sub, `\?#`) || path.Clean(sub) != sub ||
		!strings.HasPrefix(sub, constant.PostSubPath+"/") {
		return "", ErrNotPostImage
	}

	postDir := filepath.Join(uploadPath, constant.PostSubPath)
	local := filepath.Join(uploadPath, filepath.FromSlash(sub))
	// belt and braces: whatever the checks above missed, the result must still
	// be a file directly inside the post directory
	if filepath.Dir(local) != postDir {
		return "", ErrNotPostImage
	}
	return local, nil
}
