/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type clawXReleasePayload struct {
	Channel          string `json:"channel"`
	Platform         string `json:"platform"`
	Arch             string `json:"arch"`
	PackageType      string `json:"package_type"`
	PackageTypeCamel string `json:"packageType"`
	Version          string `json:"version"`
	FileName         string `json:"file_name"`
	FileURL          string `json:"file_url"`
	Sha512           string `json:"sha512"`
	Size             int64  `json:"size"`
	ReleaseDate      string `json:"release_date"`
	ReleaseNotes     string `json:"release_notes"`
	Enabled          bool   `json:"enabled"`
	Mandatory        bool   `json:"mandatory"`
}

func AdminListClawXReleases(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	packageType := strings.TrimSpace(c.Query("package_type"))
	if packageType == "" {
		packageType = strings.TrimSpace(c.Query("packageType"))
	}
	releases, total, err := model.ListClawXReleases(c.Query("channel"), c.Query("platform"), packageType, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(releases)
	common.ApiSuccess(c, pageInfo)
}

func AdminGetClawXRelease(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	release, err := model.GetClawXReleaseById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, release)
}

func AdminCreateClawXRelease(c *gin.Context) {
	release, ok := bindClawXReleasePayload(c)
	if !ok {
		return
	}
	if err := model.CreateClawXRelease(release); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, release)
}

func AdminUpdateClawXRelease(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	release, err := model.GetClawXReleaseById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	next, ok := bindClawXReleasePayload(c)
	if !ok {
		return
	}
	next.Id = release.Id
	next.CreatedAt = release.CreatedAt
	if err := model.UpdateClawXRelease(next); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, next)
}

func AdminDeleteClawXRelease(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteClawXReleaseById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminPreviewClawXReleaseFeed(c *gin.Context) {
	channel := strings.TrimSpace(c.Query("channel"))
	if channel == "" {
		channel = "latest"
	}
	platform := strings.TrimSpace(c.Query("platform"))
	if platform == "" {
		platform = "mac"
	}
	feed, ok, err := clawXBuildUpdateFeedYAML(channel, platform)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !ok {
		common.ApiErrorMsg(c, "未配置可用版本")
		return
	}
	c.String(http.StatusOK, feed)
}

func bindClawXReleasePayload(c *gin.Context) (*model.ClawXRelease, bool) {
	var payload clawXReleasePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	packageType := strings.TrimSpace(payload.PackageType)
	if packageType == "" {
		packageType = strings.TrimSpace(payload.PackageTypeCamel)
	}
	release := &model.ClawXRelease{
		Channel:      payload.Channel,
		Platform:     payload.Platform,
		Arch:         payload.Arch,
		PackageType:  packageType,
		Version:      payload.Version,
		FileName:     payload.FileName,
		FileURL:      payload.FileURL,
		Sha512:       payload.Sha512,
		Size:         payload.Size,
		ReleaseDate:  payload.ReleaseDate,
		ReleaseNotes: payload.ReleaseNotes,
		Enabled:      payload.Enabled,
		Mandatory:    payload.Mandatory,
	}
	model.NormalizeClawXRelease(release)
	if !isSupportedClawXReleasePackageType(packageType) {
		common.ApiErrorMsg(c, "package_type must be installer or portable_zip")
		return nil, false
	}
	if release.Platform == "" || !isSupportedClawXReleasePlatform(release.Platform) {
		common.ApiErrorMsg(c, "平台只能是 mac、win 或 linux")
		return nil, false
	}
	if release.Arch == "" {
		release.Arch = "universal"
	}
	if release.Version == "" || release.FileURL == "" || release.Sha512 == "" || release.Size <= 0 {
		common.ApiErrorMsg(c, "版本号、下载地址、sha512 和文件大小不能为空")
		return nil, false
	}
	if release.FileName == "" {
		release.FileName = clawXFileNameFromURL(release.FileURL)
	}
	if release.ReleaseDate == "" {
		release.ReleaseDate = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return release, true
}

func isSupportedClawXReleasePlatform(platform string) bool {
	switch platform {
	case "mac", "win", "linux":
		return true
	default:
		return false
	}
}

func isSupportedClawXReleasePackageType(packageType string) bool {
	switch strings.ToLower(strings.TrimSpace(packageType)) {
	case "", model.ClawXReleasePackageTypeInstaller, model.ClawXReleasePackageTypePortableZip:
		return true
	default:
		return false
	}
}

func clawXFileNameFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Path != "" {
		if base := path.Base(parsed.Path); base != "." && base != "/" {
			return base
		}
	}
	return path.Base(strings.TrimSpace(rawURL))
}

func clawXFeedPlatform(file string) string {
	file = strings.ToLower(strings.TrimSpace(file))
	switch {
	case strings.Contains(file, "mac"):
		return "mac"
	case strings.Contains(file, "linux"):
		return "linux"
	case strings.HasSuffix(file, ".yml") || strings.HasSuffix(file, ".yaml"):
		return "win"
	default:
		return ""
	}
}

func clawXReleaseDownloadURL(release *model.ClawXRelease) string {
	rawURL := strings.TrimSpace(release.FileURL)
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "/") {
		return strings.TrimRight(clawXOrigin(), "/") + rawURL
	}
	return rawURL
}

func clawXLatestReleasePayload(channel string, platform string, packageType string, arch string) (gin.H, bool, error) {
	release, err := model.GetLatestClawXRelease(channel, platform, packageType, arch)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	downloadURL := clawXReleaseDownloadURL(release)
	feedURL := fmt.Sprintf("%s/api/clawx/updates/feed/%s", clawXOrigin(), url.PathEscape(release.Channel))
	return gin.H{
		"channel":       release.Channel,
		"platform":      release.Platform,
		"arch":          release.Arch,
		"packageType":   release.PackageType,
		"package_type":  release.PackageType,
		"version":       release.Version,
		"releaseDate":   release.ReleaseDate,
		"release_date":  release.ReleaseDate,
		"releaseNotes":  release.ReleaseNotes,
		"release_notes": release.ReleaseNotes,
		"downloadUrl":   downloadURL,
		"download_url":  downloadURL,
		"fileName":      release.FileName,
		"file_name":     release.FileName,
		"sha512":        release.Sha512,
		"size":          release.Size,
		"mandatory":     release.Mandatory,
		"feedUrl":       feedURL,
		"feed_url":      feedURL,
	}, true, nil
}

func clawXBuildUpdateFeedYAML(channel string, platform string) (string, bool, error) {
	releases, err := model.GetLatestClawXFeedReleases(channel, platform, model.ClawXReleasePackageTypeInstaller)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if len(releases) == 0 {
		return "", false, nil
	}

	primary := releases[0]
	var b strings.Builder
	b.WriteString("version: ")
	b.WriteString(strconv.Quote(primary.Version))
	b.WriteByte('\n')
	b.WriteString("files:\n")
	for _, release := range releases {
		b.WriteString("  - url: ")
		b.WriteString(strconv.Quote(clawXReleaseDownloadURL(release)))
		b.WriteByte('\n')
		b.WriteString("    sha512: ")
		b.WriteString(strconv.Quote(release.Sha512))
		b.WriteByte('\n')
		b.WriteString("    size: ")
		b.WriteString(strconv.FormatInt(release.Size, 10))
		b.WriteByte('\n')
	}
	b.WriteString("path: ")
	b.WriteString(strconv.Quote(clawXReleaseDownloadURL(primary)))
	b.WriteByte('\n')
	b.WriteString("sha512: ")
	b.WriteString(strconv.Quote(primary.Sha512))
	b.WriteByte('\n')
	b.WriteString("releaseDate: ")
	b.WriteString(strconv.Quote(primary.ReleaseDate))
	b.WriteByte('\n')
	if primary.ReleaseNotes != "" {
		b.WriteString("releaseNotes: ")
		b.WriteString(strconv.Quote(primary.ReleaseNotes))
		b.WriteByte('\n')
	}
	return b.String(), true, nil
}
