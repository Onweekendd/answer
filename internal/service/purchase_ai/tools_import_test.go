package purchaseai

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/apache/answer/internal/service/uploader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeResources 代替我们自己的存储：files 里是网址对应的内容，errs 里是读它时要出的错
type fakeResources struct {
	files map[string][]byte
	errs  map[string]error
	asked []string
}

func (f *fakeResources) ReadUpload(_ context.Context, fileURL string) (string, []byte, error) {
	f.asked = append(f.asked, fileURL)
	if err := f.errs[fileURL]; err != nil {
		return "", nil, err
	}
	data, ok := f.files[fileURL]
	if !ok {
		return "", nil, uploader.ErrNotUpload
	}
	return fileURL[strings.LastIndex(fileURL, "/")+1:], data, nil
}

func Test_importFile(t *testing.T) {
	const site = "https://buy.example.com/uploads/"
	// withShots 是有浏览器和工作台的任务，已经截了 2 张图（内容是 shot1、shot2）
	withShots := func(t *testing.T) (*toolRun, *fakeWorkspace, *fakeResources) {
		t.Helper()
		ws := newFakeWorkspace()
		store := &fakeResources{
			files: map[string][]byte{site + "post/me.jpg": []byte("my-photo"), site + "agent-files/a1.html": []byte("<h1>上次的网页</h1>")},
			errs:  map[string]error{site + "agent/gone.jpg": uploader.ErrUploadNotFound, site + "agent/down.jpg": errors.New("s3 is down")},
		}
		r := openedRun(t, newFakeBrowser(), func(o *taskOptions) {
			o.workspaces = &fakeWorkspaces{ws: ws}
			o.resources = store
		})
		for range 2 {
			_, _ = r.Screenshot(context.Background(), `{}`)
		}
		return r, ws, store
	}

	t.Run("图 N：按内容起名放进 images/，网页里能直接引用，图不再发给模型", func(t *testing.T) {
		// Arrange
		r, ws, _ := withShots(t)

		// Act
		res, err := callTool(t, r, toolImport, `{"source":"图2"}`)

		// Assert
		require.NoError(t, err)
		require.False(t, res.Failed, res.Text)
		require.Len(t, ws.files, 1)
		for p, data := range ws.files {
			assert.Regexp(t, `^/home/user/images/[0-9a-f]{8}\.jpg$`, p)
			assert.Equal(t, []byte("shot2"), data)
			assert.Contains(t, res.Text, `<img src="`+strings.TrimPrefix(p, "/home/user/")+`">`)
		}
		assert.Empty(t, res.Image)
	})

	t.Run("图 N 的几种写法", func(t *testing.T) {
		for _, source := range []string{"图1", "[图1]", "【图 1】", " 图1 "} {
			// Arrange
			r, ws, _ := withShots(t)

			// Act
			res, _ := callTool(t, r, toolImport, `{"source":"`+source+`"}`)

			// Assert
			assert.False(t, res.Failed, source+" "+res.Text)
			assert.Len(t, ws.files, 1, source)
		}
	})

	t.Run("同一张图放两次是同一个文件", func(t *testing.T) {
		// Arrange
		r, ws, _ := withShots(t)

		// Act
		_, _ = callTool(t, r, toolImport, `{"source":"图1"}`)
		_, _ = callTool(t, r, toolImport, `{"source":"图1"}`)

		// Assert
		assert.Len(t, ws.files, 1)
	})

	t.Run("没编过号的图告诉模型现在有几张", func(t *testing.T) {
		// Arrange
		r, ws, _ := withShots(t)

		// Act
		res, err := callTool(t, r, toolImport, `{"source":"图9"}`)

		// Assert
		require.NoError(t, err)
		assert.True(t, res.Failed)
		assert.Contains(t, res.Text, "现在编了 2 张图")
		assert.Empty(t, ws.files)
	})

	t.Run("我们自己存的文件：用户传的图、交付过的网页，保留原名放进 resources/", func(t *testing.T) {
		// Arrange
		r, ws, store := withShots(t)

		// Act
		photo, _ := callTool(t, r, toolImport, `{"source":"`+site+`post/me.jpg"}`)
		page, _ := callTool(t, r, toolImport, `{"source":"`+site+`agent-files/a1.html"}`)

		// Assert
		assert.Equal(t, []byte("my-photo"), ws.files["/home/user/resources/me.jpg"])
		assert.Equal(t, []byte("<h1>上次的网页</h1>"), ws.files["/home/user/resources/a1.html"])
		assert.Contains(t, photo.Text, `<img src="resources/me.jpg">`, "图片告诉怎么引用")
		assert.NotContains(t, page.Text, "<img", "网页不是图")
		assert.Contains(t, page.Text, "bash、read_file")
		assert.Equal(t, []string{site + "post/me.jpg", site + "agent-files/a1.html"}, store.asked)
	})

	t.Run("给了文件名就用它，只能在 /home/user 里面", func(t *testing.T) {
		// Arrange
		r, ws, _ := withShots(t)

		// Act
		ok, _ := callTool(t, r, toolImport, `{"source":"图1","path":"images/jd-mini-se.jpg"}`)
		abs, _ := callTool(t, r, toolImport, `{"source":"`+site+`post/me.jpg","path":"/home/user/in/me.png"}`)
		up, _ := callTool(t, r, toolImport, `{"source":"图1","path":"../etc/x.jpg"}`)

		// Assert
		assert.False(t, ok.Failed)
		assert.False(t, abs.Failed)
		var got []string
		for p := range ws.files {
			got = append(got, p)
		}
		slices.Sort(got)
		assert.Equal(t, []string{"/home/user/images/jd-mini-se.jpg", "/home/user/in/me.png"}, got)
		assert.True(t, up.Failed)
	})

	t.Run("不是我们存的文件：不读，告诉模型怎么办", func(t *testing.T) {
		// Arrange
		r, ws, _ := withShots(t)

		// Act
		res, err := callTool(t, r, toolImport, `{"source":"https://img.jd.com/a.jpg"}`)

		// Assert
		require.NoError(t, err)
		assert.True(t, res.Failed)
		assert.Contains(t, res.Text, "不是我们服务器上存的文件")
		assert.Empty(t, ws.files)
	})

	t.Run("文件已经不在了和临时读不出来，给模型不同的话", func(t *testing.T) {
		// Arrange
		r, _, _ := withShots(t)

		// Act
		gone, _ := callTool(t, r, toolImport, `{"source":"`+site+`agent/gone.jpg"}`)
		down, _ := callTool(t, r, toolImport, `{"source":"`+site+`agent/down.jpg"}`)

		// Assert
		assert.True(t, gone.Failed)
		assert.Contains(t, gone.Text, "不要再试")
		assert.True(t, down.Failed)
		assert.Contains(t, down.Text, "可以再试一次")
	})

	t.Run("没接文件存储：只能放图 N", func(t *testing.T) {
		// Arrange
		r := openedRun(t, newFakeBrowser(), func(o *taskOptions) { o.workspaces = &fakeWorkspaces{ws: newFakeWorkspace()} })

		// Act
		res, _ := callTool(t, r, toolImport, `{"source":"`+site+`post/me.jpg"}`)

		// Assert
		assert.True(t, res.Failed)
		assert.Contains(t, res.Text, "只能放「图N」")
	})
}
