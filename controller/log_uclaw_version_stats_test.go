package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGetUClawVersionUsageStatsRejectsInvalidTimeRanges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name  string
		query string
	}{
		{name: "reversed", query: "start_timestamp=200&end_timestamp=100"},
		{name: "longer than 31 days", query: "start_timestamp=1&end_timestamp=2678402"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/api/log/uclaw/version-stats?"+testCase.query, nil)

			GetUClawVersionUsageStats(context)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}
