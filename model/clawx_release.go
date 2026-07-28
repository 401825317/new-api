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
package model

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	ClawXReleasePackageTypeInstaller   = "installer"
	ClawXReleasePackageTypePortableZip = "portable_zip"
)

type ClawXRelease struct {
	Id           int    `json:"id"`
	Channel      string `json:"channel" gorm:"type:varchar(32);index:idx_clawx_release_lookup,priority:1"`
	Platform     string `json:"platform" gorm:"type:varchar(32);index:idx_clawx_release_lookup,priority:2"`
	Arch         string `json:"arch" gorm:"type:varchar(32)"`
	PackageType  string `json:"package_type" gorm:"type:varchar(32);default:installer;index"`
	Version      string `json:"version" gorm:"type:varchar(64);index:idx_clawx_release_lookup,priority:3"`
	FileName     string `json:"file_name" gorm:"type:varchar(255)"`
	FileURL      string `json:"file_url" gorm:"type:text"`
	Sha512       string `json:"sha512" gorm:"type:text"`
	Size         int64  `json:"size" gorm:"bigint"`
	ReleaseDate  string `json:"release_date" gorm:"type:varchar(64)"`
	ReleaseNotes string `json:"release_notes" gorm:"type:text"`
	Enabled      bool   `json:"enabled" gorm:"default:true;index"`
	Mandatory    bool   `json:"mandatory" gorm:"default:false"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint"`
}

func NormalizeClawXRelease(release *ClawXRelease) {
	release.Channel = strings.TrimSpace(release.Channel)
	if release.Channel == "" {
		release.Channel = "latest"
	}
	release.Platform = strings.ToLower(strings.TrimSpace(release.Platform))
	release.Arch = strings.ToLower(strings.TrimSpace(release.Arch))
	release.PackageType = NormalizeClawXReleasePackageType(release.PackageType)
	release.Version = strings.TrimSpace(release.Version)
	release.FileName = strings.TrimSpace(release.FileName)
	release.FileURL = strings.TrimSpace(release.FileURL)
	release.Sha512 = strings.TrimSpace(release.Sha512)
	release.ReleaseDate = strings.TrimSpace(release.ReleaseDate)
	release.ReleaseNotes = strings.TrimSpace(release.ReleaseNotes)
}

func NormalizeClawXReleasePackageType(packageType string) string {
	switch strings.ToLower(strings.TrimSpace(packageType)) {
	case ClawXReleasePackageTypePortableZip:
		return ClawXReleasePackageTypePortableZip
	default:
		return ClawXReleasePackageTypeInstaller
	}
}

func applyClawXReleasePackageTypeFilter(query *gorm.DB, packageType string) *gorm.DB {
	packageType = NormalizeClawXReleasePackageType(packageType)
	if packageType == ClawXReleasePackageTypeInstaller {
		return query.Where("(package_type = ? OR package_type = '' OR package_type IS NULL)", packageType)
	}
	return query.Where("package_type = ?", packageType)
}

func ListClawXReleases(channel string, platform string, packageType string, startIdx int, num int) (releases []*ClawXRelease, total int64, err error) {
	channel = strings.TrimSpace(channel)
	platform = strings.ToLower(strings.TrimSpace(platform))
	packageType = strings.TrimSpace(packageType)
	query := DB.Model(&ClawXRelease{})
	if channel != "" {
		query = query.Where("channel = ?", channel)
	}
	if platform != "" {
		query = query.Where("platform = ?", platform)
	}
	if packageType != "" {
		query = applyClawXReleasePackageTypeFilter(query, packageType)
	}
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&releases).Error
	return releases, total, err
}

func GetClawXReleaseById(id int) (*ClawXRelease, error) {
	var release ClawXRelease
	err := DB.First(&release, "id = ?", id).Error
	return &release, err
}

func CreateClawXRelease(release *ClawXRelease) error {
	NormalizeClawXRelease(release)
	now := common.GetTimestamp()
	release.CreatedAt = now
	release.UpdatedAt = now
	return DB.Create(release).Error
}

func UpdateClawXRelease(release *ClawXRelease) error {
	NormalizeClawXRelease(release)
	release.UpdatedAt = common.GetTimestamp()
	return DB.Model(release).Select(
		"channel",
		"platform",
		"arch",
		"package_type",
		"version",
		"file_name",
		"file_url",
		"sha512",
		"size",
		"release_date",
		"release_notes",
		"enabled",
		"mandatory",
		"updated_at",
	).Updates(release).Error
}

func DeleteClawXReleaseById(id int) error {
	return DB.Delete(&ClawXRelease{}, id).Error
}

func GetLatestClawXRelease(channel string, platform string, packageType string, arch string) (*ClawXRelease, error) {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "latest"
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	packageType = NormalizeClawXReleasePackageType(packageType)
	arch = strings.ToLower(strings.TrimSpace(arch))
	query := DB.Where("channel = ? AND platform = ? AND enabled = ?", channel, platform, true)
	query = applyClawXReleasePackageTypeFilter(query, packageType)
	if arch != "" {
		latest, err := getLatestUsableClawXRelease(query.Where("arch = ?", arch))
		if err == nil {
			return latest, nil
		}
		if err != gorm.ErrRecordNotFound {
			return nil, err
		}
		query = DB.Where("channel = ? AND platform = ? AND enabled = ? AND arch = ?", channel, platform, true, "universal")
		query = applyClawXReleasePackageTypeFilter(query, packageType)
	}
	return getLatestUsableClawXRelease(query)
}

func getLatestUsableClawXRelease(query *gorm.DB) (*ClawXRelease, error) {
	var releases []*ClawXRelease
	if err := query.Find(&releases).Error; err != nil {
		return nil, err
	}

	var latest *ClawXRelease
	for _, release := range releases {
		NormalizeClawXRelease(release)
		if !isUsableClawXRelease(release) {
			continue
		}
		if latest == nil || compareClawXRelease(latest, release) < 0 {
			latest = release
		}
	}
	if latest == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return latest, nil
}

func isUsableClawXRelease(release *ClawXRelease) bool {
	return release.Version != "" && release.FileURL != "" && release.Sha512 != "" && release.Size > 0
}

// compareClawXRelease compares semantic versions first, then keeps equal versions stable by creation time and id.
func compareClawXRelease(left *ClawXRelease, right *ClawXRelease) int {
	leftVersion, leftValid := parseClawXSemanticVersion(left.Version)
	rightVersion, rightValid := parseClawXSemanticVersion(right.Version)
	if leftValid && rightValid {
		if comparison := compareClawXSemanticVersion(leftVersion, rightVersion); comparison != 0 {
			return comparison
		}
	} else if leftValid != rightValid {
		if leftValid {
			return 1
		}
		return -1
	}
	if left.CreatedAt != right.CreatedAt {
		if left.CreatedAt > right.CreatedAt {
			return 1
		}
		return -1
	}
	if left.Id > right.Id {
		return 1
	}
	if left.Id < right.Id {
		return -1
	}
	return 0
}

type clawXSemanticVersion struct {
	major      uint64
	minor      uint64
	patch      uint64
	prerelease []string
}

func parseClawXSemanticVersion(raw string) (clawXSemanticVersion, bool) {
	value := strings.TrimSpace(raw)
	if len(value) > 1 && (value[0] == 'v' || value[0] == 'V') {
		value = value[1:]
	}
	if plus := strings.IndexByte(value, '+'); plus >= 0 {
		value = value[:plus]
	}
	core := value
	prerelease := ""
	if dash := strings.IndexByte(value, '-'); dash >= 0 {
		core = value[:dash]
		prerelease = value[dash+1:]
		if prerelease == "" {
			return clawXSemanticVersion{}, false
		}
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return clawXSemanticVersion{}, false
	}
	numbers := [3]uint64{}
	for index, part := range parts {
		if !isClawXNumericIdentifier(part) {
			return clawXSemanticVersion{}, false
		}
		parsed, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return clawXSemanticVersion{}, false
		}
		numbers[index] = parsed
	}
	version := clawXSemanticVersion{major: numbers[0], minor: numbers[1], patch: numbers[2]}
	if prerelease != "" {
		version.prerelease = strings.Split(prerelease, ".")
		for _, part := range version.prerelease {
			if part == "" || !isClawXPrereleaseIdentifier(part) {
				return clawXSemanticVersion{}, false
			}
		}
	}
	return version, true
}

func isClawXNumericIdentifier(value string) bool {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func isClawXPrereleaseIdentifier(value string) bool {
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && character != '-' {
			return false
		}
	}
	return true
}

func compareClawXSemanticVersion(left clawXSemanticVersion, right clawXSemanticVersion) int {
	for _, pair := range [][2]uint64{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if pair[0] > pair[1] {
			return 1
		}
		if pair[0] < pair[1] {
			return -1
		}
	}
	if len(left.prerelease) == 0 || len(right.prerelease) == 0 {
		if len(left.prerelease) == 0 && len(right.prerelease) != 0 {
			return 1
		}
		if len(left.prerelease) != 0 && len(right.prerelease) == 0 {
			return -1
		}
		return 0
	}
	limit := len(left.prerelease)
	if len(right.prerelease) < limit {
		limit = len(right.prerelease)
	}
	for index := 0; index < limit; index++ {
		leftPart, rightPart := left.prerelease[index], right.prerelease[index]
		leftNumeric, rightNumeric := isClawXNumericIdentifier(leftPart), isClawXNumericIdentifier(rightPart)
		if leftNumeric && rightNumeric {
			leftNumber, _ := strconv.ParseUint(leftPart, 10, 64)
			rightNumber, _ := strconv.ParseUint(rightPart, 10, 64)
			if leftNumber > rightNumber {
				return 1
			}
			if leftNumber < rightNumber {
				return -1
			}
			continue
		}
		if leftNumeric != rightNumeric {
			if leftNumeric {
				return -1
			}
			return 1
		}
		if leftPart > rightPart {
			return 1
		}
		if leftPart < rightPart {
			return -1
		}
	}
	if len(left.prerelease) > len(right.prerelease) {
		return 1
	}
	if len(left.prerelease) < len(right.prerelease) {
		return -1
	}
	return 0
}

func GetLatestClawXFeedReleases(channel string, platform string, packageType string) ([]*ClawXRelease, error) {
	latest, err := GetLatestClawXRelease(channel, platform, packageType, "")
	if err != nil {
		return nil, err
	}
	channel = latest.Channel
	platform = latest.Platform
	packageType = latest.PackageType

	var releases []*ClawXRelease
	query := DB.Where("channel = ? AND platform = ? AND enabled = ?", channel, platform, true)
	query = applyClawXReleasePackageTypeFilter(query, packageType)
	err = query.
		Order("arch asc, id asc").
		Find(&releases).Error
	if err != nil {
		return nil, err
	}
	latestVersion, latestVersionValid := parseClawXSemanticVersion(latest.Version)
	usableReleases := make([]*ClawXRelease, 0, len(releases))
	for _, release := range releases {
		NormalizeClawXRelease(release)
		if !isUsableClawXRelease(release) {
			continue
		}
		if candidateVersion, candidateVersionValid := parseClawXSemanticVersion(release.Version); latestVersionValid && candidateVersionValid {
			if compareClawXSemanticVersion(latestVersion, candidateVersion) != 0 {
				continue
			}
		} else if release.Version != latest.Version {
			continue
		}
		usableReleases = append(usableReleases, release)
	}
	if len(usableReleases) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	if platform == "mac" && !hasCompleteMacClawXFeed(usableReleases) {
		return nil, gorm.ErrRecordNotFound
	}
	return usableReleases, nil
}

// electron-updater reads one macOS manifest for both Intel and Apple Silicon.
// Never publish a newer arm64-only (or x64-only) version through that shared
// manifest, because the other architecture could download an unusable app.
func hasCompleteMacClawXFeed(releases []*ClawXRelease) bool {
	var hasUniversal bool
	var hasX64 bool
	var hasArm64 bool
	for _, release := range releases {
		switch release.Arch {
		case "universal":
			hasUniversal = true
		case "x64":
			hasX64 = true
		case "arm64":
			hasArm64 = true
		}
	}
	return hasUniversal || (hasX64 && hasArm64)
}
