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
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type ClawXRelease struct {
	Id           int    `json:"id"`
	Channel      string `json:"channel" gorm:"type:varchar(32);index:idx_clawx_release_lookup,priority:1"`
	Platform     string `json:"platform" gorm:"type:varchar(32);index:idx_clawx_release_lookup,priority:2"`
	Arch         string `json:"arch" gorm:"type:varchar(32)"`
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
	release.Version = strings.TrimSpace(release.Version)
	release.FileName = strings.TrimSpace(release.FileName)
	release.FileURL = strings.TrimSpace(release.FileURL)
	release.Sha512 = strings.TrimSpace(release.Sha512)
	release.ReleaseDate = strings.TrimSpace(release.ReleaseDate)
	release.ReleaseNotes = strings.TrimSpace(release.ReleaseNotes)
}

func ListClawXReleases(channel string, platform string, startIdx int, num int) (releases []*ClawXRelease, total int64, err error) {
	channel = strings.TrimSpace(channel)
	platform = strings.ToLower(strings.TrimSpace(platform))
	query := DB.Model(&ClawXRelease{})
	if channel != "" {
		query = query.Where("channel = ?", channel)
	}
	if platform != "" {
		query = query.Where("platform = ?", platform)
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

func GetLatestClawXFeedReleases(channel string, platform string) ([]*ClawXRelease, error) {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "latest"
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	var latest ClawXRelease
	err := DB.Where("channel = ? AND platform = ? AND enabled = ?", channel, platform, true).
		Order("created_at desc, id desc").
		First(&latest).Error
	if err != nil {
		return nil, err
	}

	var releases []*ClawXRelease
	err = DB.Where("channel = ? AND platform = ? AND version = ? AND enabled = ?", channel, platform, latest.Version, true).
		Order("arch asc, id asc").
		Find(&releases).Error
	return releases, err
}
