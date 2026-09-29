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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_postImagePath(t *testing.T) {
	const site = "http://localhost:8081"
	uploadPath := filepath.Join("answer-data", "uploads")

	t.Run("帖子图片的 URL 换成上传目录里的文件", func(t *testing.T) {
		// Act
		local, err := postImagePath(site, uploadPath, site+"/uploads/post/5T95BLsLffu.png")

		// Assert
		require.NoError(t, err)
		require.Equal(t, filepath.Join("answer-data", "uploads", "post", "5T95BLsLffu.png"), local)
	})

	t.Run("站点地址末尾带斜杠也一样", func(t *testing.T) {
		// Act
		local, err := postImagePath(site+"/", uploadPath, site+"/uploads/post/a.png")

		// Assert
		require.NoError(t, err)
		require.Equal(t, filepath.Join("answer-data", "uploads", "post", "a.png"), local)
	})

	t.Run("走不出上传目录，也读不到帖子图片以外的文件", func(t *testing.T) {
		for _, imageURL := range []string{
			site + "/uploads/post/../../conf/config.yaml",
			site + `/uploads/post/..\..\conf\config.yaml`,
			site + "/uploads/post/sub/../../../x",
			site + "/uploads/avatar/a.png",
			site + "/uploads/files/post/a.pdf",
			site + "/uploads/post/",
			site + "/uploads/post/a.png?x=1",
			site + "/uploads/post//a.png",
			"https://cdn.example.com/uploads/post/a.png",
			"http://localhost:8081.evil.com/uploads/post/a.png",
			"",
		} {
			// Act
			local, err := postImagePath(site, uploadPath, imageURL)

			// Assert
			require.ErrorIs(t, err, ErrNotPostImage, imageURL)
			require.Empty(t, local, imageURL)
		}
	})

	t.Run("站点地址没配置时一律拒绝", func(t *testing.T) {
		// Act
		_, err := postImagePath("", uploadPath, "/uploads/post/a.png")

		// Assert
		require.ErrorIs(t, err, ErrNotPostImage)
	})
}
