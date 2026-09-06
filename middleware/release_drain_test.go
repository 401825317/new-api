package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReleaseDrainKeepsExistingStream(t *testing.T) {
	oldEnabled, oldTracker := common.ReleaseDrainEnabled, common.ReleaseDrain
	common.ReleaseDrainEnabled = true
	common.ReleaseDrain = &common.DrainTracker{}
	defer func() { common.ReleaseDrainEnabled, common.ReleaseDrain = oldEnabled, oldTracker }()
	started, finish := make(chan struct{}), make(chan struct{})
	r := gin.New()
	r.Use(ReleaseDrain())
	r.GET("/stream", func(c *gin.Context) {
		c.Writer.WriteString("first\n")
		c.Writer.Flush()
		close(started)
		<-finish
		c.Writer.WriteString("completed\n")
	})
	server := httptest.NewServer(r)
	defer server.Close()
	response, err := http.Get(server.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	<-started
	common.ReleaseDrain.Begin()
	rejected, err := http.Get(server.URL + "/stream")
	if err != nil {
		close(finish)
		t.Fatal(err)
	}
	rejected.Body.Close()
	if rejected.StatusCode != 503 {
		close(finish)
		t.Fatal("new request admitted")
	}
	if common.ReleaseDrain.Snapshot().Active != 1 {
		close(finish)
		t.Fatal("lost in-flight request")
	}
	close(finish)
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "first\ncompleted\n" {
		t.Fatalf("stream interrupted: %q %v", body, err)
	}
}
