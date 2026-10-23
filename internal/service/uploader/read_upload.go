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
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/base/objectstore"
)

// ErrNotUpload means a URL does not point at a file this site stores: it is not under
// SiteUrl/uploads/, or it is not in a directory the agent may read, or its name is not a plain file name.
var ErrNotUpload = stderrors.New("not a file stored on this site")

// ErrUploadNotFound means the URL is a stored file's URL but the file is not there any more.
var ErrUploadNotFound = stderrors.New("the file is not there")

// readableDirs are the directories under uploads/ the agent may read back into its workspace:
// what users uploaded to a post, and what the agent saved or delivered itself. The value says
// whether the directory can be on the object store (the agent's own) or only on the local disk.
var readableDirs = map[string]bool{
	constant.PostSubPath:       false,
	constant.AgentSubPath:      true,
	constant.AgentFilesSubPath: true,
}

// uploadName is a plain file name: no separator, no leading dot, and so no "..".
var uploadName = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,127}$`)

// ReadUpload reads a file this site stores, given its URL (SiteUrl/uploads/<dir>/<name>), for the
// agent to put in its workspace. It returns the file name and the bytes.
//
// The URL comes from the model, whose input comes from outside web pages, so it is treated as
// hostile: only this site's own uploads are read, only from the directories in readableDirs,
// and only a plain name in them. Anything else, an address on the internet included, is
// refused: this must never turn into "fetch any URL the model names".
// The agent's own directories are looked up in the object store first and on the local disk after,
// the same order the routes serve them in (static_router.go).
func (us *uploaderService) ReadUpload(ctx context.Context, fileURL string) (string, []byte, error) {
	siteGeneral, err := us.siteInfoService.GetSiteGeneral(ctx)
	if err != nil {
		return "", nil, err
	}
	dirName, name, err := splitUploadURL(siteGeneral.SiteUrl, fileURL)
	if err != nil {
		return "", nil, err
	}
	if _, ok := AgentFileTypes[strings.ToLower(path.Ext(name))]; !ok {
		return "", nil, fmt.Errorf("%w: %s", ErrNotUpload, "file type")
	}

	if onStore := readableDirs[dirName]; onStore && us.objectStore != nil {
		key := path.Join(dirName, name)
		body, _, err := us.objectStore.Get(ctx, key)
		if err == nil {
			defer func() { _ = body.Close() }()
			data, err := readLimited(body)
			return name, data, err
		}
		if !stderrors.Is(err, objectstore.ErrNotFound) {
			return "", nil, err
		}
	}

	file, err := os.Open(filepath.Join(us.serviceConfig.UploadPath, dirName, name))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, ErrUploadNotFound
		}
		return "", nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := readLimited(file)
	return name, data, err
}

// splitUploadURL cuts SiteUrl/uploads/<dir>/<name> into dir and name, refusing anything else.
func splitUploadURL(siteURL, fileURL string) (dirName, name string, err error) {
	prefix := strings.TrimSuffix(siteURL, "/") + "/uploads/"
	if siteURL == "" || !strings.HasPrefix(fileURL, prefix) {
		return "", "", ErrNotUpload
	}
	dirName, name, ok := strings.Cut(strings.TrimPrefix(fileURL, prefix), "/")
	if _, allowed := readableDirs[dirName]; !ok || !allowed || !uploadName.MatchString(name) {
		return "", "", ErrNotUpload
	}
	return dirName, name, nil
}

// readLimited reads at most AgentFileMaxBytes: a bigger file is refused, not cut off.
func readLimited(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, AgentFileMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > AgentFileMaxBytes {
		return nil, fmt.Errorf("the file is over %d MB", AgentFileMaxBytes>>20)
	}
	return data, nil
}
