package uploader

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/service_config"
	"github.com/apache/answer/internal/service/siteinfo_common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitUploadURL(t *testing.T) {
	const site = "https://buy.example.com"
	cases := []struct {
		name, url, dir, file string
		ok                   bool
	}{
		{"用户传的图", site + "/uploads/post/8kTq2xYz.jpg", "post", "8kTq2xYz.jpg", true},
		{"Agent 存的图", site + "/uploads/agent/a1B2.jpg", "agent", "a1B2.jpg", true},
		{"交付的文件", site + "/uploads/agent-files/a1B2.html", "agent-files", "a1B2.html", true},
		{"站点地址末尾有斜杠", site + "/uploads/agent/a.jpg", "agent", "a.jpg", true},
		{"别的网站：不读，不能变成替模型抓任意网址", "https://evil.example.com/uploads/agent/a.jpg", "", "", false},
		{"内网地址", "http://127.0.0.1:8081/uploads/agent/a.jpg", "", "", false},
		{"不在可读的目录里（头像、配置）", site + "/uploads/avatar/a.jpg", "", "", false},
		{"往上跳", site + "/uploads/agent/../../conf/config.yaml", "", "", false},
		{"里面还有一层", site + "/uploads/agent/x/a.jpg", "", "", false},
		{"带查询参数", site + "/uploads/agent/a.jpg?x=1", "", "", false},
		{"百分号编码的 ..", site + "/uploads/agent/%2e%2e%2fconf", "", "", false},
		{"反斜杠", site + "/uploads/agent/..\a.jpg", "", "", false},
		{"点开头", site + "/uploads/agent/.env", "", "", false},
		{"没有文件名", site + "/uploads/agent/", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			dir, file, err := splitUploadURL(site+"/", tc.url)

			// Assert
			if !tc.ok {
				assert.ErrorIs(t, err, ErrNotUpload)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.dir, dir)
			assert.Equal(t, tc.file, file)
		})
	}
	_, _, err := splitUploadURL("", "https://x/uploads/agent/a.jpg")
	assert.ErrorIs(t, err, ErrNotUpload, "没配站点地址就什么都不读")
}

func TestReadUpload(t *testing.T) {
	// Arrange：本地磁盘上，三个目录各有一个文件
	root := t.TempDir()
	write := func(dir, name string, data []byte) {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, dir, name), data, 0o644))
	}
	write(constant.PostSubPath, "p1.png", []byte("user-png"))
	write(constant.AgentSubPath, "a1.jpg", []byte("agent-jpg"))
	write(constant.AgentFilesSubPath, "f1.html", []byte("<h1>清单</h1>"))
	write(constant.AgentFilesSubPath, "big.txt", bytes.Repeat([]byte("a"), AgentFileMaxBytes+1))
	write(constant.AgentFilesSubPath, "tool.exe", []byte("MZ"))
	us := &uploaderService{
		serviceConfig:   &service_config.ServiceConfig{UploadPath: root},
		siteInfoService: fakeSiteInfo{url: "https://buy.example.com"},
	}
	const site = "https://buy.example.com/uploads/"

	t.Run("三个目录里的文件都读得到，连文件名一起交回", func(t *testing.T) {
		for url, want := range map[string]string{
			site + "post/p1.png":         "user-png",
			site + "agent/a1.jpg":        "agent-jpg",
			site + "agent-files/f1.html": "<h1>清单</h1>",
		} {
			// Act
			name, data, err := us.ReadUpload(context.Background(), url)

			// Assert
			require.NoError(t, err, url)
			assert.Equal(t, filepath.Base(url), name)
			assert.Equal(t, want, string(data))
		}
	})

	t.Run("文件不在了", func(t *testing.T) {
		_, _, err := us.ReadUpload(context.Background(), site+"agent/gone.jpg")
		assert.ErrorIs(t, err, ErrUploadNotFound)
	})

	t.Run("不是这个站存的、或者类型不在白名单里：不读", func(t *testing.T) {
		for _, url := range []string{"https://evil.example.com/uploads/agent/a1.jpg", site + "agent-files/tool.exe", site + "avatar/a.jpg"} {
			_, _, err := us.ReadUpload(context.Background(), url)
			assert.ErrorIs(t, err, ErrNotUpload, url)
		}
	})

	t.Run("超过上限的不读", func(t *testing.T) {
		_, _, err := us.ReadUpload(context.Background(), site+"agent-files/big.txt")
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrUploadNotFound)
	})
}

// fakeSiteInfo 只会说站点地址
type fakeSiteInfo struct {
	siteinfo_common.SiteInfoCommonService
	url string
}

func (f fakeSiteInfo) GetSiteGeneral(context.Context) (*schema.SiteGeneralResp, error) {
	return &schema.SiteGeneralResp{SiteUrl: f.url}, nil
}
