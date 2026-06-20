package controller

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const (
	clawXSupportQRCodeMaxBytes       = 2 << 20
	clawXSupportQRCodeMultipartExtra = 64 << 10
)

var clawXSupportQRCodeMIMEs = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
}

func clawXSupportQRCodeDir() string {
	if dir := strings.TrimSpace(os.Getenv("CLAWX_SUPPORT_QRCODE_DIR")); dir != "" {
		return dir
	}
	return filepath.Join("data", "clawx", "support-qrcodes")
}

func randomClawXSupportQRCodeFileName(ext string) (string, error) {
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("qrcode-%d-%s%s", time.Now().UnixNano(), hex.EncodeToString(randomBytes), ext), nil
}

func firstHeaderValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if index := strings.Index(value, ","); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return value
}

func inferRequestOrigin(c *gin.Context) string {
	if raw := strings.TrimSpace(os.Getenv("CLAWX_PUBLIC_ORIGIN")); raw != "" {
		return strings.TrimRight(raw, "/")
	}
	proto := firstHeaderValue(c.GetHeader("X-Forwarded-Proto"))
	if proto == "" {
		proto = firstHeaderValue(c.GetHeader("X-Forwarded-Protocol"))
	}
	if proto == "" {
		proto = firstHeaderValue(c.GetHeader("X-Scheme"))
	}
	if proto != "http" && proto != "https" {
		if c.Request.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := firstHeaderValue(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	if host == "" {
		return clawXOrigin()
	}
	return strings.TrimRight(proto+"://"+host, "/")
}

func AdminUploadClawXSupportQRCode(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, clawXSupportQRCodeMaxBytes+clawXSupportQRCodeMultipartExtra)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		common.ApiErrorMsg(c, "请选择要上传的二维码图片")
		return
	}
	if fileHeader.Size > clawXSupportQRCodeMaxBytes {
		common.ApiErrorMsg(c, "二维码图片不能超过 2MB")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, clawXSupportQRCodeMaxBytes+1))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(data) == 0 {
		common.ApiErrorMsg(c, "二维码图片不能为空")
		return
	}
	if len(data) > clawXSupportQRCodeMaxBytes {
		common.ApiErrorMsg(c, "二维码图片不能超过 2MB")
		return
	}
	mimeType := http.DetectContentType(data)
	ext, ok := clawXSupportQRCodeMIMEs[mimeType]
	if !ok {
		common.ApiErrorMsg(c, "二维码图片仅支持 PNG、JPG 或 GIF 格式")
		return
	}

	dir := clawXSupportQRCodeDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		common.ApiError(c, err)
		return
	}
	fileName, err := randomClawXSupportQRCodeFileName(ext)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	filePath := filepath.Join(dir, fileName)
	out, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if _, err := io.Copy(out, bytes.NewReader(data)); err != nil {
		out.Close()
		_ = os.Remove(filePath)
		common.ApiError(c, err)
		return
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(filePath)
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"url":      inferRequestOrigin(c) + "/api/clawx/support-qrcodes/" + fileName,
		"fileName": fileName,
		"mimeType": mimeType,
		"size":     len(data),
	})
}

func ClawXSupportQRCode(c *gin.Context) {
	fileName := strings.TrimSpace(c.Param("file"))
	if fileName == "" || fileName != filepath.Base(fileName) || strings.Contains(fileName, "..") {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	filePath := filepath.Join(clawXSupportQRCodeDir(), fileName)
	if _, err := os.Stat(filePath); err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.File(filePath)
}
