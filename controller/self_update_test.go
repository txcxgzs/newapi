package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchSelfUpdateAssetName(t *testing.T) {
	assert.Equal(t, "new-api-v1.2.3", matchSelfUpdateAssetName("linux", "amd64", "v1.2.3"))
	assert.Equal(t, "new-api-arm64-v1.2.3", matchSelfUpdateAssetName("linux", "arm64", "v1.2.3"))
	assert.Equal(t, "new-api-macos-v1.2.3", matchSelfUpdateAssetName("darwin", "arm64", "v1.2.3"))
	assert.Equal(t, "new-api-v1.2.3.exe", matchSelfUpdateAssetName("windows", "amd64", "v1.2.3"))
}

func TestIsNewerReleaseTag(t *testing.T) {
	assert.True(t, isNewerReleaseTag("v0.0.0", "v1.2.0"))
	assert.True(t, isNewerReleaseTag("", "v1.2.0"))
	assert.True(t, isNewerReleaseTag("v1.2.0", "v1.2.1"))
	assert.True(t, isNewerReleaseTag("v1.2.0-rc.1", "v1.2.0"))
	assert.False(t, isNewerReleaseTag("v1.2.0", "v1.2.0"))
	assert.False(t, isNewerReleaseTag("v1.2.0", "1.2.0"))
	assert.False(t, isNewerReleaseTag("v1.2.1", "v1.2.0"))
	assert.False(t, isNewerReleaseTag("v1.2.0", ""))
}

func TestSelfUpdateStatusAndApplyRelease(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldBytes := []byte("old-binary-content-v1.0.0")
	newBytes := []byte("new-binary-content-v1.2.0")
	sum := sha256.Sum256(newBytes)
	expectedSHA := hex.EncodeToString(sum[:])

	tag := "v1.2.0"
	expectedAssetName := matchSelfUpdateAssetName(runtime.GOOS, runtime.GOARCH, tag)
	expectedChecksumName := matchSelfUpdateChecksumAssetName(runtime.GOOS)

	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/txcxgzs/newapi/releases":
			assets := []gitHubReleaseAsset{
				{
					Name:               expectedAssetName,
					BrowserDownloadURL: serverURL + "/download/binary",
					Size:               int64(len(newBytes)),
				},
			}
			if expectedChecksumName != "" {
				assets = append(assets, gitHubReleaseAsset{
					Name:               expectedChecksumName,
					BrowserDownloadURL: serverURL + "/download/checksums",
					Size:               int64(len(expectedSHA) + len(expectedAssetName) + 4),
				})
			}
			_ = json.NewEncoder(w).Encode([]gitHubRelease{
				{
					TagName:     tag,
					Name:        "Release " + tag,
					HTMLURL:     "https://github.com/txcxgzs/newapi/releases/tag/" + tag,
					PublishedAt: "2026-11-14T10:00:00Z",
					Assets:      assets,
				},
			})
		case "/repos/txcxgzs/newapi/commits/main":
			var c gitHubCommit
			c.SHA = "1234567890abcdef"
			c.HTMLURL = "https://github.com/txcxgzs/newapi/commit/1234567890abcdef"
			c.Commit.Message = "feat(update): release v1.2.0\n\nbody"
			c.Commit.Committer.Date = "2026-11-14T10:00:00Z"
			_ = json.NewEncoder(w).Encode(c)
		case "/download/binary":
			_, _ = w.Write(newBytes)
		case "/download/checksums":
			_, _ = fmt.Fprintf(w, "%s  %s\n", expectedSHA, expectedAssetName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	serverURL = srv.URL

	tmpDir := t.TempDir()
	fakeExecPath := filepath.Join(tmpDir, "new-api-test-bin")
	if runtime.GOOS == "windows" {
		fakeExecPath += ".exe"
	}
	require.NoError(t, os.WriteFile(fakeExecPath, oldBytes, 0o755))

	origAPIBase := selfUpdateGitHubAPIBase
	origExecFn := selfUpdateExecutablePath
	origRepoRootFn := selfUpdateRepoRootDir
	origVersion := common.Version
	t.Cleanup(func() {
		selfUpdateGitHubAPIBase = origAPIBase
		selfUpdateExecutablePath = origExecFn
		selfUpdateRepoRootDir = origRepoRootFn
		common.Version = origVersion
	})

	selfUpdateGitHubAPIBase = srv.URL
	selfUpdateExecutablePath = func() (string, error) { return fakeExecPath, nil }
	selfUpdateRepoRootDir = func() string { return tmpDir }
	common.Version = "v1.0.0"

	// 1. 测试 GetSelfUpdateStatus
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/system_update/status", nil)
	GetSelfUpdateStatus(c)

	require.Equal(t, http.StatusOK, w.Code)
	var statusResp struct {
		Success bool                     `json:"success"`
		Message string                   `json:"message"`
		Data    SelfUpdateStatusResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &statusResp))
	require.True(t, statusResp.Success, statusResp.Message)
	assert.Equal(t, "txcxgzs/newapi", statusResp.Data.Repo)
	assert.True(t, statusResp.Data.ReleaseUpdateAvail)
	require.NotNil(t, statusResp.Data.LatestRelease)
	assert.Equal(t, expectedAssetName, statusResp.Data.LatestRelease.AssetName)
	assert.Equal(t, "1234567", statusResp.Data.RemoteCommit)
	assert.Equal(t, "feat(update): release v1.2.0", statusResp.Data.RemoteCommitMessage)
	assert.False(t, statusResp.Data.GitAvailable)

	// 2. 测试 ApplySelfUpdate (mode: "release", restart: false)
	wApply := httptest.NewRecorder()
	cApply, _ := gin.CreateTestContext(wApply)
	cApply.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/system_update/apply",
		strings.NewReader(`{"mode":"release","restart":false}`),
	)
	cApply.Request.Header.Set("Content-Type", "application/json")
	ApplySelfUpdate(cApply)

	require.Equal(t, http.StatusOK, wApply.Code)
	var applyResp struct {
		Success bool                    `json:"success"`
		Message string                  `json:"message"`
		Data    SelfUpdateApplyResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(wApply.Body.Bytes(), &applyResp))
	require.True(t, applyResp.Success, applyResp.Message)
	assert.Equal(t, "release", applyResp.Data.Mode)
	assert.Equal(t, "v1.2.0", applyResp.Data.UpdatedVersion)
	assert.Equal(t, expectedAssetName, applyResp.Data.AssetName)
	assert.False(t, applyResp.Data.Restarting)

	installed, err := os.ReadFile(fakeExecPath)
	require.NoError(t, err)
	assert.Equal(t, newBytes, installed)

	backup, err := os.ReadFile(fakeExecPath + ".bak")
	require.NoError(t, err)
	assert.Equal(t, oldBytes, backup)
}
