package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFirstCorrelationHeaderPrefersGatewayHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request.Header.Set("X-Request-ID", "client-id")
	c.Request.Header.Set(common.ParentRequestIdKey, "cf-global-id")

	require.Equal(t, "cf-global-id", firstCorrelationHeader(c))
}

func TestRequestIdKeepsParentCorrelationWithoutReplacingLocalID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request.Header.Set(common.ParentRequestIdKey, "cf-global-id")

	RequestId()(c)

	require.Equal(t, "cf-global-id", c.GetString(common.ParentRequestIdKey))
	require.NotEmpty(t, c.GetString(common.RequestIdKey))
	require.NotEqual(t, "cf-global-id", c.GetString(common.RequestIdKey))
}

func TestFirstCorrelationHeaderRejectsUnsafeValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request.Header.Set(common.ParentRequestIdKey, "bad\r\nid")
	require.Empty(t, firstCorrelationHeader(c))
}
