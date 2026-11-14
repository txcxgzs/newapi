package controller

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

const (
	defaultSelfUpdateRepo     = "txcxgzs/newapi"
	defaultSelfUpdateBranch   = "main"
	selfUpdateMaxBinaryBytes  = 512 << 20 // 512 MB
	selfUpdateMaxChecksumSize = 1 << 20   // 1 MB
	selfUpdateMaxAPIBodyBytes = 4 << 20   // 4 MB
)

var (
	selfUpdateGitHubAPIBase  = "https://api.github.com"
	selfUpdateHTTPClient     = &http.Client{Timeout: 30 * time.Second}
	selfUpdateDownloadClient = &http.Client{Timeout: 5 * time.Minute}
	selfUpdateExecutablePath = os.Executable
	selfUpdateRepoRootDir    = resolveSelfUpdateRepoRoot
	selfUpdateExitFunc       = os.Exit
	selfUpdateApplyMu        sync.Mutex
)

type gitHubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type gitHubRelease struct {
	TagName     string               `json:"tag_name"`
	Name        string               `json:"name"`
	HTMLURL     string               `json:"html_url"`
	PublishedAt string               `json:"published_at"`
	CreatedAt   string               `json:"created_at"`
	Draft       bool                 `json:"draft"`
	Prerelease  bool                 `json:"prerelease"`
	Assets      []gitHubReleaseAsset `json:"assets"`
}

type gitHubCommit struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Date string `json:"date"`
		} `json:"author"`
		Committer struct {
			Date string `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

// SelfUpdateReleaseInfo 描述可更新的 GitHub Release 及其匹配当前平台架构的产物信息。
type SelfUpdateReleaseInfo struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Prerelease  bool   `json:"prerelease"`
	AssetName   string `json:"asset_name,omitempty"`
	AssetURL    string `json:"asset_url,omitempty"`
	AssetSize   int64  `json:"asset_size,omitempty"`
	ChecksumURL string `json:"checksum_url,omitempty"`
}

// SelfUpdateStatusResponse 返回自定义分支/Release 的更新检查结果。
type SelfUpdateStatusResponse struct {
	Repo                  string                 `json:"repo"`
	Branch                string                 `json:"branch"`
	CurrentVersion        string                 `json:"current_version"`
	GOOS                  string                 `json:"goos"`
	GOARCH                string                 `json:"goarch"`
	LatestRelease         *SelfUpdateReleaseInfo `json:"latest_release"`
	ReleaseUpdateAvail    bool                   `json:"release_update_available"`
	GitAvailable          bool                   `json:"git_available"`
	LocalBranch           string                 `json:"local_branch,omitempty"`
	LocalCommit           string                 `json:"local_commit,omitempty"`
	RemoteCommit          string                 `json:"remote_commit,omitempty"`
	RemoteCommitURL       string                 `json:"remote_commit_url,omitempty"`
	RemoteCommitMessage   string                 `json:"remote_commit_message,omitempty"`
	RemoteCommitDate      string                 `json:"remote_commit_date,omitempty"`
	GitUpdateAvailable    bool                   `json:"git_update_available"`
	Warnings              []string               `json:"warnings"`
}

type selfUpdateApplyRequest struct {
	Mode    string `json:"mode"`
	Tag     string `json:"tag"`
	Restart bool   `json:"restart"`
}

// SelfUpdateApplyResponse 描述一次自更新执行结果。
type SelfUpdateApplyResponse struct {
	Mode            string `json:"mode"`
	PreviousVersion string `json:"previous_version,omitempty"`
	UpdatedVersion  string `json:"updated_version,omitempty"`
	AssetName       string `json:"asset_name,omitempty"`
	BackupPath      string `json:"backup_path,omitempty"`
	Restarting      bool   `json:"restarting,omitempty"`
	OldCommit       string `json:"old_commit,omitempty"`
	NewCommit       string `json:"new_commit,omitempty"`
	Message         string `json:"message"`
}

func getSelfUpdateRepo() string {
	raw := strings.TrimSpace(os.Getenv("SELF_UPDATE_GITHUB_REPO"))
	if raw == "" {
		return defaultSelfUpdateRepo
	}
	if !isValidGitHubRepoSlug(raw) {
		return defaultSelfUpdateRepo
	}
	return raw
}

func isValidGitHubRepoSlug(slug string) bool {
	if slug == "" || strings.Contains(slug, "..") || strings.ContainsAny(slug, " \t\r\n?#\\") {
		return false
	}
	parts := strings.Split(slug, "/")
	if len(parts) != 2 {
		return false
	}
	owner, repo := parts[0], parts[1]
	if owner == "" || repo == "" || owner == "." || repo == "." {
		return false
	}
	return true
}

func resolveSelfUpdateRepoRoot() string {
	if wd, err := os.Getwd(); err == nil {
		if root := findGitRootUpward(wd); root != "" {
			return root
		}
	}
	if execPath, err := selfUpdateExecutablePath(); err == nil && execPath != "" {
		if root := findGitRootUpward(filepath.Dir(execPath)); root != "" {
			return root
		}
	}
	return "."
}

func findGitRootUpward(startDir string) string {
	dir := filepath.Clean(startDir)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// matchSelfUpdateAssetName 根据 .github/workflows/release.yml 产物命名规范匹配对应平台二进制文件名。
func matchSelfUpdateAssetName(goos, goarch, tag string) string {
	switch {
	case goos == "windows":
		return fmt.Sprintf("new-api-%s.exe", tag)
	case goos == "darwin":
		return fmt.Sprintf("new-api-macos-%s", tag)
	case goos == "linux" && goarch == "arm64":
		return fmt.Sprintf("new-api-arm64-%s", tag)
	case goos == "linux":
		return fmt.Sprintf("new-api-%s", tag)
	default:
		return fmt.Sprintf("new-api-%s", tag)
	}
}

func matchSelfUpdateChecksumAssetName(goos string) string {
	switch goos {
	case "windows":
		return "checksums-windows.txt"
	case "darwin":
		return "checksums-macos.txt"
	case "linux":
		return "checksums-linux.txt"
	default:
		return ""
	}
}

// isNewerReleaseTag 判断 latestTag 是否比 currentVersion 更新。
func isNewerReleaseTag(currentVersion, latestTag string) bool {
	cur := strings.TrimSpace(currentVersion)
	cur = strings.TrimLeft(cur, "vV")
	latest := strings.TrimSpace(latestTag)
	latest = strings.TrimLeft(latest, "vV")

	if latest == "" {
		return false
	}
	if cur == "" || cur == "0.0.0" {
		return true
	}

	curCore, curPre, curOK := parseSemverParts(cur)
	latestCore, latestPre, latestOK := parseSemverParts(latest)
	if !curOK || !latestOK {
		return cur != latest
	}

	for i := 0; i < 3; i++ {
		if latestCore[i] != curCore[i] {
			return latestCore[i] > curCore[i]
		}
	}

	// 核心版本号相同时，正式版高于预发布版（alpha/beta/rc）。
	if curPre != "" && latestPre == "" {
		return true
	}
	if curPre == "" && latestPre != "" {
		return false
	}
	if curPre != latestPre {
		return latestPre > curPre
	}
	return false
}

func parseSemverParts(v string) ([3]int, string, bool) {
	var core [3]int
	if plusIdx := strings.IndexByte(v, '+'); plusIdx >= 0 {
		v = v[:plusIdx]
	}
	pre := ""
	if dashIdx := strings.IndexByte(v, '-'); dashIdx >= 0 {
		pre = v[dashIdx+1:]
		v = v[:dashIdx]
	}
	segments := strings.Split(v, ".")
	if len(segments) < 2 || len(segments) > 3 {
		return core, "", false
	}
	for i, seg := range segments {
		n, err := strconv.Atoi(seg)
		if err != nil || n < 0 {
			return core, "", false
		}
		core[i] = n
	}
	return core, pre, true
}

func doGitHubAPIJSONRequest(ctx context.Context, rawURL string, target any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "new-api-self-updater")

	resp, err := selfUpdateHTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, selfUpdateMaxAPIBodyBytes))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return resp.StatusCode, fmt.Errorf("GitHub API 请求受限 (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("GitHub API 返回状态 %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return resp.StatusCode, fmt.Errorf("解析 GitHub API 响应失败: %w", err)
	}
	return resp.StatusCode, nil
}

func fetchSelfUpdateReleases(ctx context.Context, repo string) ([]gitHubRelease, int, error) {
	base := strings.TrimRight(selfUpdateGitHubAPIBase, "/")
	endpoint := fmt.Sprintf("%s/repos/%s/releases?per_page=10", base, repo)
	var releases []gitHubRelease
	status, err := doGitHubAPIJSONRequest(ctx, endpoint, &releases)
	return releases, status, err
}

func fetchSelfUpdateLatestCommit(ctx context.Context, repo, branch string) (*gitHubCommit, int, error) {
	base := strings.TrimRight(selfUpdateGitHubAPIBase, "/")
	endpoint := fmt.Sprintf("%s/repos/%s/commits/%s", base, repo, url.PathEscape(branch))
	var commit gitHubCommit
	status, err := doGitHubAPIJSONRequest(ctx, endpoint, &commit)
	if err != nil {
		return nil, status, err
	}
	return &commit, status, nil
}

func buildSelfUpdateReleaseInfo(rel gitHubRelease, goos, goarch string) *SelfUpdateReleaseInfo {
	publishedAt := rel.PublishedAt
	if publishedAt == "" {
		publishedAt = rel.CreatedAt
	}
	info := &SelfUpdateReleaseInfo{
		TagName:     rel.TagName,
		Name:        rel.Name,
		HTMLURL:     rel.HTMLURL,
		PublishedAt: publishedAt,
		Prerelease:  rel.Prerelease,
	}
	expectedAsset := matchSelfUpdateAssetName(goos, goarch, rel.TagName)
	expectedChecksum := matchSelfUpdateChecksumAssetName(goos)
	for _, asset := range rel.Assets {
		if asset.Name == expectedAsset {
			info.AssetName = asset.Name
			info.AssetURL = asset.BrowserDownloadURL
			info.AssetSize = asset.Size
		} else if expectedChecksum != "" && asset.Name == expectedChecksum {
			info.ChecksumURL = asset.BrowserDownloadURL
		}
	}
	return info
}

func readLocalGitState(ctx context.Context, repoRoot string) (available bool, branch, shortSHA, fullSHA string) {
	if repoRoot == "" {
		return false, "", "", ""
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".git")); err != nil {
		return false, "", "", ""
	}
	shortOut, err := runGitCommand(ctx, repoRoot, "rev-parse", "--short", "HEAD")
	if err != nil {
		// .git 存在但可能尚未有任何 commit 或 git 命令不可用，仍标记 Git 可用状态以便反馈
		return true, "", "", ""
	}
	fullOut, _ := runGitCommand(ctx, repoRoot, "rev-parse", "HEAD")
	branchOut, _ := runGitCommand(ctx, repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	return true, branchOut, shortOut, fullOut
}

func runGitCommand(ctx context.Context, repoRoot string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", repoRoot}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		if trimmed != "" {
			return trimmed, fmt.Errorf("%w: %s", err, trimmed)
		}
		return "", err
	}
	return trimmed, nil
}

// GetSelfUpdateStatus 查询当前绑定分支（txcxgzs/newapi）的 Release 与 main 最新提交状态。
func GetSelfUpdateStatus(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	repo := getSelfUpdateRepo()
	branch := defaultSelfUpdateBranch
	warnings := make([]string, 0)

	resp := SelfUpdateStatusResponse{
		Repo:           repo,
		Branch:         branch,
		CurrentVersion: common.Version,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
		Warnings:       warnings,
	}

	releases, relStatus, relErr := fetchSelfUpdateReleases(ctx, repo)
	if relErr != nil {
		if relStatus == http.StatusNotFound {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("仓库 %s 暂未发布 GitHub Release，或仓库不可访问", repo))
		} else {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("获取 GitHub Release 失败: %v", relErr))
		}
	} else {
		var selected *gitHubRelease
		for i := range releases {
			if !releases[i].Draft {
				selected = &releases[i]
				break
			}
		}
		if selected == nil {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("仓库 %s 尚无已发布的 Release，可通过打 Tag 触发构建或使用 Git 源码拉取更新", repo))
		} else {
			info := buildSelfUpdateReleaseInfo(*selected, runtime.GOOS, runtime.GOARCH)
			resp.LatestRelease = info
			resp.ReleaseUpdateAvail = isNewerReleaseTag(common.Version, info.TagName)
			if info.AssetURL == "" {
				resp.Warnings = append(resp.Warnings, fmt.Sprintf("最新 Release %s 中未包含当前平台 (%s/%s) 的二进制产物", info.TagName, runtime.GOOS, runtime.GOARCH))
			}
		}
	}

	commit, _, commitErr := fetchSelfUpdateLatestCommit(ctx, repo, branch)
	var remoteFullSHA string
	if commitErr != nil {
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("获取远程 %s 分支最新提交失败: %v", branch, commitErr))
	} else if commit != nil && commit.SHA != "" {
		remoteFullSHA = commit.SHA
		if len(commit.SHA) > 7 {
			resp.RemoteCommit = commit.SHA[:7]
		} else {
			resp.RemoteCommit = commit.SHA
		}
		resp.RemoteCommitURL = commit.HTMLURL
		firstLine := strings.SplitN(commit.Commit.Message, "\n", 2)[0]
		resp.RemoteCommitMessage = strings.TrimSpace(firstLine)
		if commit.Commit.Committer.Date != "" {
			resp.RemoteCommitDate = commit.Commit.Committer.Date
		} else {
			resp.RemoteCommitDate = commit.Commit.Author.Date
		}
	}

	gitAvail, localBranch, localShort, localFull := readLocalGitState(ctx, selfUpdateRepoRootDir())
	resp.GitAvailable = gitAvail
	resp.LocalBranch = localBranch
	resp.LocalCommit = localShort
	if localFull != "" && remoteFullSHA != "" {
		resp.GitUpdateAvailable = !strings.EqualFold(localFull, remoteFullSHA) && !strings.HasPrefix(remoteFullSHA, localFull)
	}

	common.ApiSuccess(c, resp)
}

func isLoopbackHost(host string) bool {
	h := host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		h = parsedHost
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func isAllowedSelfUpdateAssetURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	allowedGitHubHosts := map[string]bool{
		"github.com":                         true,
		"api.github.com":                     true,
		"objects.githubusercontent.com":      true,
		"release-assets.githubusercontent.com": true,
	}
	if u.Scheme == "https" && allowedGitHubHosts[host] {
		return true
	}
	// 仅在自测场景下（selfUpdateGitHubAPIBase 指向本机 loopback）允许同名 loopback 测试地址。
	if baseURL, err := url.Parse(selfUpdateGitHubAPIBase); err == nil && isLoopbackHost(baseURL.Host) {
		return isLoopbackHost(u.Host) && (u.Scheme == "http" || u.Scheme == "https")
	}
	return false
}

func newAllowedDownloadClient() *http.Client {
	return &http.Client{
		Transport: selfUpdateDownloadClient.Transport,
		Timeout:   selfUpdateDownloadClient.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("下载重定向次数过多")
			}
			if !isAllowedSelfUpdateAssetURL(req.URL.String()) {
				return fmt.Errorf("拒绝跳转到非受信下载域名: %s", req.URL.Hostname())
			}
			return nil
		},
	}
}

func downloadSelfUpdateFile(ctx context.Context, rawURL, destPath string, maxBytes int64) (string, error) {
	if !isAllowedSelfUpdateAssetURL(rawURL) {
		return "", fmt.Errorf("下载地址不在允许的白名单域名内: %s", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "new-api-self-updater")

	client := newAllowedDownloadClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("下载更新包失败，HTTP 状态码: %d", resp.StatusCode)
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", fmt.Errorf("创建临时更新文件失败: %w", err)
	}

	hasher := sha256.New()
	limited := io.LimitReader(resp.Body, maxBytes+1)
	written, copyErr := io.Copy(io.MultiWriter(f, hasher), limited)
	closeErr := f.Close()

	if copyErr != nil {
		_ = os.Remove(destPath)
		return "", fmt.Errorf("写入更新文件失败: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(destPath)
		return "", fmt.Errorf("关闭更新文件失败: %w", closeErr)
	}
	if written == 0 {
		_ = os.Remove(destPath)
		return "", errors.New("下载的更新包为空文件")
	}
	if written > maxBytes {
		_ = os.Remove(destPath)
		return "", fmt.Errorf("下载的更新包体积超过上限 (%d MB)", maxBytes>>20)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func fetchAndParseChecksum(ctx context.Context, checksumURL, assetName string) (string, error) {
	if !isAllowedSelfUpdateAssetURL(checksumURL) {
		return "", fmt.Errorf("校验文件地址不在允许的白名单域名内: %s", checksumURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "new-api-self-updater")

	client := newAllowedDownloadClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("获取 SHA256 校验文件失败 (HTTP %d)", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, selfUpdateMaxChecksumSize))
	if err != nil {
		return "", err
	}
	expected := parseSHA256Checksum(string(body), assetName)
	if expected == "" {
		return "", fmt.Errorf("校验文件中未找到产物 %s 的 SHA256 记录", assetName)
	}
	return expected, nil
}

func parseSHA256Checksum(content, targetAsset string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := strings.TrimSpace(fields[0])
		fileName := strings.TrimSpace(fields[len(fields)-1])
		fileName = strings.TrimPrefix(fileName, "*")
		fileName = strings.TrimPrefix(fileName, "./")
		if filepath.Base(fileName) == targetAsset && len(hash) == 64 {
			return strings.ToLower(hash)
		}
	}
	return ""
}

func replaceExecutableBinary(execPath, newPath string) (string, error) {
	bakPath := execPath + ".bak"
	_ = os.Remove(bakPath)
	if err := os.Rename(execPath, bakPath); err != nil {
		_ = os.Remove(newPath)
		return "", fmt.Errorf("备份当前可执行文件失败: %w", err)
	}
	if err := os.Rename(newPath, execPath); err != nil {
		_ = os.Rename(bakPath, execPath)
		_ = os.Remove(newPath)
		return "", fmt.Errorf("替换可执行文件失败（已回滚）: %w", err)
	}
	return bakPath, nil
}

// ApplySelfUpdate 执行一键更新，支持 GitHub Release 二进制热替换与 Git 源码快进拉取两种模式。
func ApplySelfUpdate(c *gin.Context) {
	var req selfUpdateApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		common.ApiErrorMsg(c, "请求参数格式错误: "+err.Error())
		return
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "release"
	}

	if !selfUpdateApplyMu.TryLock() {
		common.ApiErrorMsg(c, "已有更新任务正在执行，请稍候完成后重试")
		return
	}
	defer selfUpdateApplyMu.Unlock()

	repo := getSelfUpdateRepo()

	switch mode {
	case "release":
		applyReleaseSelfUpdate(c, repo, strings.TrimSpace(req.Tag), req.Restart)
	case "git":
		applyGitSelfUpdate(c, repo)
	default:
		common.ApiErrorMsg(c, "不支持的更新模式: "+mode)
	}
}

func applyReleaseSelfUpdate(c *gin.Context, repo, requestedTag string, restart bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()

	releases, _, err := fetchSelfUpdateReleases(ctx, repo)
	if err != nil {
		common.ApiErrorMsg(c, fmt.Sprintf("查询 GitHub Release 失败: %v", err))
		return
	}

	var target *gitHubRelease
	for i := range releases {
		if releases[i].Draft {
			continue
		}
		if requestedTag == "" || releases[i].TagName == requestedTag {
			target = &releases[i]
			break
		}
	}
	if target == nil {
		common.ApiErrorMsg(c, "未找到当前系统架构对应的 Release 二进制产物，请先在 GitHub 创建 Release Tag 或使用 Git 源码拉取模式")
		return
	}

	info := buildSelfUpdateReleaseInfo(*target, runtime.GOOS, runtime.GOARCH)
	if info.AssetURL == "" || info.AssetName == "" {
		common.ApiErrorMsg(c, "未找到当前系统架构对应的 Release 二进制产物，请先在 GitHub 创建 Release Tag 或使用 Git 源码拉取模式")
		return
	}

	execPath, err := selfUpdateExecutablePath()
	if err != nil || execPath == "" {
		common.ApiErrorMsg(c, fmt.Sprintf("定位当前可执行文件路径失败: %v", err))
		return
	}

	newPath := execPath + ".new"
	actualSHA256, err := downloadSelfUpdateFile(ctx, info.AssetURL, newPath, selfUpdateMaxBinaryBytes)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	if info.ChecksumURL != "" {
		expectedSHA256, err := fetchAndParseChecksum(ctx, info.ChecksumURL, info.AssetName)
		if err != nil {
			_ = os.Remove(newPath)
			common.ApiErrorMsg(c, fmt.Sprintf("SHA256 校验失败: %v", err))
			return
		}
		if !strings.EqualFold(actualSHA256, expectedSHA256) {
			_ = os.Remove(newPath)
			common.ApiErrorMsg(c, fmt.Sprintf("二进制产物 SHA256 校验不匹配（期望 %s，实际 %s）", expectedSHA256, actualSHA256))
			return
		}
	}

	bakPath, err := replaceExecutableBinary(execPath, newPath)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	msg := fmt.Sprintf("已更新至 %s（产物 %s），旧版本已备份至 %s", info.TagName, info.AssetName, filepath.Base(bakPath))
	if restart {
		msg += "，服务将在 1.5 秒后退出并由守护进程重启"
	} else {
		msg += "，请手动重启服务进程使新版本生效"
	}

	logger.LogInfo(ctx, fmt.Sprintf("self-update release applied: repo=%s tag=%s asset=%s restart=%v", repo, info.TagName, info.AssetName, restart))

	common.ApiSuccess(c, SelfUpdateApplyResponse{
		Mode:            "release",
		PreviousVersion: common.Version,
		UpdatedVersion:  info.TagName,
		AssetName:       info.AssetName,
		BackupPath:      bakPath,
		Restarting:      restart,
		Message:         msg,
	})

	if restart {
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush()
		}
		go func() {
			time.Sleep(1500 * time.Millisecond)
			selfUpdateExitFunc(0)
		}()
	}
}

func applyGitSelfUpdate(c *gin.Context, repo string) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	repoRoot := selfUpdateRepoRootDir()
	if repoRoot == "" {
		common.ApiErrorMsg(c, "未找到 Git 源码目录")
		return
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".git")); err != nil {
		common.ApiErrorMsg(c, "当前运行目录不是 Git 源码仓库，无法使用 Git 源码拉取模式")
		return
	}

	oldCommit, err := runGitCommand(ctx, repoRoot, "rev-parse", "--short", "HEAD")
	if err != nil {
		common.ApiErrorMsg(c, fmt.Sprintf("读取本地 Git 提交失败: %v", err))
		return
	}

	remoteURL := fmt.Sprintf("https://github.com/%s.git", repo)
	if _, err := runGitCommand(ctx, repoRoot, "fetch", remoteURL, defaultSelfUpdateBranch); err != nil {
		common.ApiErrorMsg(c, fmt.Sprintf("拉取远程分支失败: %v", err))
		return
	}
	if _, err := runGitCommand(ctx, repoRoot, "merge", "--ff-only", "FETCH_HEAD"); err != nil {
		common.ApiErrorMsg(c, fmt.Sprintf("快进合并失败（可能存在本地未提交修改或分支分叉）: %v", err))
		return
	}

	newCommit, err := runGitCommand(ctx, repoRoot, "rev-parse", "--short", "HEAD")
	if err != nil {
		common.ApiErrorMsg(c, fmt.Sprintf("读取更新后 Git 提交失败: %v", err))
		return
	}

	msg := fmt.Sprintf("Git 源码已从 %s 更新到 %s（当前为源码更新，需重新编译/构建并重启服务生效）", oldCommit, newCommit)
	if oldCommit == newCommit {
		msg = fmt.Sprintf("Git 源码已是最新提交 %s，无需拉取新变更", newCommit)
	}

	logger.LogInfo(ctx, fmt.Sprintf("self-update git pulled: repo=%s %s -> %s", repo, oldCommit, newCommit))

	common.ApiSuccess(c, SelfUpdateApplyResponse{
		Mode:      "git",
		OldCommit: oldCommit,
		NewCommit: newCommit,
		Message:   msg,
	})
}
