package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResponsesRecoveryStreamRequest(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	r, selectChannel, err := getModelRequest(c)
	if err != nil || !selectChannel || !r.Stream || r.Model != "test" {
		t.Fatalf("stream flag must reach first channel selection: request=%+v select=%v err=%v", r, selectChannel, err)
	}
}
