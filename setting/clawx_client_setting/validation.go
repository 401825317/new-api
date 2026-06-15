package clawx_client_setting

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

var validAnnouncementLevels = map[string]bool{
	"normal":    true,
	"important": true,
	"urgent":    true,
}

func ValidateClientSettings(settingsStr string, settingType string) error {
	if strings.TrimSpace(settingsStr) == "" {
		return nil
	}

	switch settingType {
	case "Announcements":
		return validateAnnouncements(settingsStr)
	case "Support":
		return validateSupport(settingsStr)
	default:
		return fmt.Errorf("未知的 ClawX 客户端设置类型：%s", settingType)
	}
}

func validateHTTPURL(raw string, field string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s URL 格式不正确", field)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s URL 只支持 http 或 https", field)
	}
	return nil
}

func validateTimestamp(value string, field string, required bool) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		if required {
			return fmt.Errorf("%s不能为空", field)
		}
		return nil
	}
	if _, err := time.Parse(time.RFC3339, trimmed); err != nil {
		return fmt.Errorf("%s格式错误，应使用 RFC3339 时间", field)
	}
	return nil
}

func validateAnnouncements(settingsStr string) error {
	var list []Announcement
	if err := common.UnmarshalJsonStr(settingsStr, &list); err != nil {
		return fmt.Errorf("ClawX 客户端公告格式错误：%s", err.Error())
	}
	if len(list) > 30 {
		return fmt.Errorf("ClawX 客户端公告数量不能超过30个")
	}

	seen := make(map[string]bool, len(list))
	for i, item := range list {
		index := i + 1
		if strings.TrimSpace(item.Id) == "" {
			return fmt.Errorf("第%d个客户端公告缺少ID", index)
		}
		if seen[item.Id] {
			return fmt.Errorf("第%d个客户端公告ID重复", index)
		}
		seen[item.Id] = true
		if strings.TrimSpace(item.Title) == "" {
			return fmt.Errorf("第%d个客户端公告缺少标题", index)
		}
		if len(item.Title) > 80 {
			return fmt.Errorf("第%d个客户端公告标题不能超过80字符", index)
		}
		if strings.TrimSpace(item.Content) == "" {
			return fmt.Errorf("第%d个客户端公告缺少内容", index)
		}
		if len(item.Content) > 1000 {
			return fmt.Errorf("第%d个客户端公告内容不能超过1000字符", index)
		}
		level := strings.TrimSpace(item.Level)
		if level == "" {
			level = "normal"
		}
		if !validAnnouncementLevels[level] {
			return fmt.Errorf("第%d个客户端公告级别不合法", index)
		}
		if err := validateTimestamp(item.PublishedAt, fmt.Sprintf("第%d个客户端公告发布时间", index), true); err != nil {
			return err
		}
		if err := validateTimestamp(item.ExpiresAt, fmt.Sprintf("第%d个客户端公告过期时间", index), false); err != nil {
			return err
		}
		if strings.TrimSpace(item.ExpiresAt) != "" {
			publishedAt, _ := time.Parse(time.RFC3339, item.PublishedAt)
			expiresAt, _ := time.Parse(time.RFC3339, item.ExpiresAt)
			if !expiresAt.After(publishedAt) {
				return fmt.Errorf("第%d个客户端公告过期时间必须晚于发布时间", index)
			}
		}
		if err := validateHTTPURL(item.Link, fmt.Sprintf("第%d个客户端公告链接", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateSupport(settingsStr string) error {
	var support Support
	if err := common.UnmarshalJsonStr(settingsStr, &support); err != nil {
		return fmt.Errorf("ClawX 客服配置格式错误：%s", err.Error())
	}
	if len(support.Title) > 60 {
		return fmt.Errorf("客服标题不能超过60字符")
	}
	if len(support.Description) > 300 {
		return fmt.Errorf("客服说明不能超过300字符")
	}
	if len(support.WorkHours) > 100 {
		return fmt.Errorf("客服服务时间不能超过100字符")
	}
	if len(support.WechatId) > 100 {
		return fmt.Errorf("客服微信号不能超过100字符")
	}
	if len(support.ExtraNote) > 200 {
		return fmt.Errorf("客服备注不能超过200字符")
	}
	if err := validateHTTPURL(support.QrCodeUrl, "客服二维码"); err != nil {
		return err
	}
	if len(support.Contacts) > 6 {
		return fmt.Errorf("客服联系人不能超过6个")
	}
	seen := make(map[string]bool, len(support.Contacts))
	for i, contact := range support.Contacts {
		index := i + 1
		id := strings.TrimSpace(contact.Id)
		if id == "" {
			return fmt.Errorf("第%d个客服联系人缺少ID", index)
		}
		if seen[id] {
			return fmt.Errorf("第%d个客服联系人ID重复", index)
		}
		seen[id] = true
		if len(contact.Label) > 60 {
			return fmt.Errorf("第%d个客服联系人名称不能超过60字符", index)
		}
		if len(contact.Description) > 200 {
			return fmt.Errorf("第%d个客服联系人说明不能超过200字符", index)
		}
		if len(contact.WorkHours) > 100 {
			return fmt.Errorf("第%d个客服联系人服务时间不能超过100字符", index)
		}
		if len(contact.WechatId) > 100 {
			return fmt.Errorf("第%d个客服联系人微信号不能超过100字符", index)
		}
		if len(contact.ExtraNote) > 200 {
			return fmt.Errorf("第%d个客服联系人备注不能超过200字符", index)
		}
		if err := validateHTTPURL(contact.QrCodeUrl, fmt.Sprintf("第%d个客服联系人二维码", index)); err != nil {
			return err
		}
	}
	return nil
}

func GetAnnouncements() []Announcement {
	if !GetClientSetting().AnnouncementsEnabled {
		return []Announcement{}
	}
	var list []Announcement
	if err := common.UnmarshalJsonStr(GetClientSetting().Announcements, &list); err != nil {
		return []Announcement{}
	}
	now := time.Now()
	active := make([]Announcement, 0, len(list))
	for _, item := range list {
		if !item.Enabled {
			continue
		}
		if strings.TrimSpace(item.Level) == "" {
			item.Level = "normal"
		}
		publishedAt, err := time.Parse(time.RFC3339, item.PublishedAt)
		if err != nil || publishedAt.After(now) {
			continue
		}
		if strings.TrimSpace(item.ExpiresAt) != "" {
			expiresAt, err := time.Parse(time.RFC3339, item.ExpiresAt)
			if err != nil || expiresAt.Before(now) {
				continue
			}
		}
		active = append(active, item)
	}
	sort.SliceStable(active, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339, active[i].PublishedAt)
		right, _ := time.Parse(time.RFC3339, active[j].PublishedAt)
		return left.After(right)
	})
	return active
}

func GetSupport() Support {
	if !GetClientSetting().SupportEnabled {
		return Support{}
	}
	var support Support
	if err := common.UnmarshalJsonStr(GetClientSetting().Support, &support); err != nil {
		return Support{}
	}
	support.Contacts = normalizeSupportContacts(support)
	return support
}

func normalizeSupportContacts(support Support) []SupportContact {
	contacts := make([]SupportContact, 0, len(support.Contacts)+1)
	for _, contact := range support.Contacts {
		if !contact.Enabled {
			continue
		}
		if strings.TrimSpace(contact.QrCodeUrl) == "" {
			continue
		}
		contact.Id = strings.TrimSpace(contact.Id)
		contact.Label = strings.TrimSpace(contact.Label)
		contact.Description = strings.TrimSpace(contact.Description)
		contact.QrCodeUrl = strings.TrimSpace(contact.QrCodeUrl)
		contact.WorkHours = strings.TrimSpace(contact.WorkHours)
		contact.WechatId = strings.TrimSpace(contact.WechatId)
		contact.ExtraNote = strings.TrimSpace(contact.ExtraNote)
		if contact.Label == "" {
			contact.Label = "官方客服"
		}
		contacts = append(contacts, contact)
	}
	if len(contacts) > 0 {
		return contacts
	}
	if strings.TrimSpace(support.QrCodeUrl) == "" {
		return []SupportContact{}
	}
	return []SupportContact{
		{
			Id:          "default",
			Label:       fallbackString(support.Title, "官方客服"),
			Description: strings.TrimSpace(support.Description),
			QrCodeUrl:   strings.TrimSpace(support.QrCodeUrl),
			WorkHours:   strings.TrimSpace(support.WorkHours),
			WechatId:    strings.TrimSpace(support.WechatId),
			ExtraNote:   strings.TrimSpace(support.ExtraNote),
			Enabled:     true,
		},
	}
}

func fallbackString(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
