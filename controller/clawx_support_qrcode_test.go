package controller

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func performClawXSupportQRCodeUpload(t *testing.T, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/clawx/admin/support-qrcode", &body)
	ctx.Request.Host = "backend.example.test"
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
	ctx.Request.Header.Set("X-Forwarded-Proto", "https")
	AdminUploadClawXSupportQRCode(ctx)
	return recorder
}

func TestAdminUploadClawXSupportQRCodeStoresImageAndReturnsURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	t.Setenv("CLAWX_SUPPORT_QRCODE_DIR", dir)

	recorder := performClawXSupportQRCodeUpload(t, "support.png", []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d,
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	require.Contains(t, recorder.Body.String(), `"mimeType":"image/png"`)
	require.Contains(t, recorder.Body.String(), `"url":"https://backend.example.test/api/clawx/support-qrcodes/qrcode-`)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.True(t, strings.HasSuffix(entries[0].Name(), ".png"))
}

func TestAdminUploadClawXSupportQRCodeRejectsNonImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CLAWX_SUPPORT_QRCODE_DIR", t.TempDir())

	recorder := performClawXSupportQRCodeUpload(t, "support.txt", []byte("not an image"))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":false`)
	require.Contains(t, recorder.Body.String(), "仅支持 PNG、JPG 或 GIF")
}

func TestClawXSupportQRCodeRejectsPathTraversal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CLAWX_SUPPORT_QRCODE_DIR", t.TempDir())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "file", Value: "../secret.png"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/clawx/support-qrcodes/../secret.png", nil)

	ClawXSupportQRCode(ctx)

	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestClawXSupportQRCodeServesStoredFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	t.Setenv("CLAWX_SUPPORT_QRCODE_DIR", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "support.png"), []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	}, 0644))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "file", Value: "support.png"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/clawx/support-qrcodes/support.png", nil)

	ClawXSupportQRCode(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "public, max-age=31536000, immutable", recorder.Header().Get("Cache-Control"))
	require.Equal(t, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, recorder.Body.Bytes())
}
