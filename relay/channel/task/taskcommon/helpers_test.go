package taskcommon

import (
	"net/url"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/tidwall/gjson"
)

func withVideoURLSettings(t *testing.T, serverAddress, proxyAddress, mode string) {
	t.Helper()
	oldServerAddress := system_setting.ServerAddress
	oldProxyAddress := system_setting.VideoProxyAddress
	oldMode := system_setting.VideoResultURLMode
	oldSecret := system_setting.VideoProxySignSecret
	system_setting.ServerAddress = serverAddress
	system_setting.VideoProxyAddress = proxyAddress
	system_setting.VideoResultURLMode = mode
	system_setting.VideoProxySignSecret = ""
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
		system_setting.VideoProxyAddress = oldProxyAddress
		system_setting.VideoResultURLMode = oldMode
		system_setting.VideoProxySignSecret = oldSecret
	})
}

func finishedVideoTask() *model.Task {
	return &model.Task{
		TaskID: "task_public",
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://video.junfeiai.hk-proxy.lingzhiwuxian.com/v1/videos/task_upstream/content",
		},
	}
}

func TestPublicResultURLProxyMode(t *testing.T) {
	withVideoURLSettings(t, "https://zz-cn.lingzhiwuxian.com", "", "proxy")

	got := PublicResultURL(finishedVideoTask())
	want := "https://zz-cn.lingzhiwuxian.com/v1/videos/task_public/content"
	if got != want {
		t.Fatalf("PublicResultURL() = %q, want %q", got, want)
	}
}

func TestPublicResultURLDownstreamMode(t *testing.T) {
	withVideoURLSettings(t, "https://zz-cn.lingzhiwuxian.com", "", "downstream")

	got := PublicResultURL(finishedVideoTask())
	want := "https://video.junfeiai.hk-proxy.lingzhiwuxian.com/v1/videos/task_upstream/content"
	if got != want {
		t.Fatalf("PublicResultURL() = %q, want %q", got, want)
	}
}

func TestRewriteOpenAIVideoResultURL(t *testing.T) {
	withVideoURLSettings(t, "https://zz-cn.lingzhiwuxian.com", "", "downstream")

	data := []byte(`{"id":"task_public","status":"completed","metadata":{"url":"old"},"url":"old","result_url":"old","video":{"url":"old"}}`)
	got, err := RewriteOpenAIVideoResultURL(data, finishedVideoTask())
	if err != nil {
		t.Fatal(err)
	}

	want := "https://video.junfeiai.hk-proxy.lingzhiwuxian.com/v1/videos/task_upstream/content"
	for _, path := range []string{"metadata.url", "url", "result_url", "video.url"} {
		if value := gjson.GetBytes(got, path).String(); value != want {
			t.Fatalf("%s = %q, want %q", path, value, want)
		}
	}
}

func withVideoProxySignSecret(t *testing.T, secret string) {
	t.Helper()
	oldSecret := system_setting.VideoProxySignSecret
	system_setting.VideoProxySignSecret = secret
	t.Cleanup(func() {
		system_setting.VideoProxySignSecret = oldSecret
	})
}

func grokVideoTask(resultURL string) *model.Task {
	return &model.Task{
		TaskID: "task_grok",
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultURL: resultURL,
		},
	}
}

func TestBuildGrokVideoProxyURL(t *testing.T) {
	withVideoURLSettings(t, "https://video.junfeiai.com", "https://video.junfeiai.hk-proxy.lingzhiwuxian.com", "proxy")
	withVideoProxySignSecret(t, "secret")

	got := BuildGrokVideoProxyURL(grokVideoTask("https://assets.x.ai/videos/task.mp4"))
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "video.junfeiai.hk-proxy.lingzhiwuxian.com" || parsed.Path != "/video/grok/task_grok" {
		t.Fatalf("unexpected grok video proxy url: %q", got)
	}
	if !VerifyGrokVideoProxySignature("task_grok", parsed.Query().Get("exp"), parsed.Query().Get("sig")) {
		t.Fatalf("generated signature should verify: %q", got)
	}
}

func TestBuildGrokVideoProxyURLRejectsNonXAI(t *testing.T) {
	withVideoURLSettings(t, "https://video.junfeiai.com", "https://video.junfeiai.hk-proxy.lingzhiwuxian.com", "proxy")
	withVideoProxySignSecret(t, "secret")

	got := BuildGrokVideoProxyURL(grokVideoTask("https://example.com/video.mp4"))
	if got != "" {
		t.Fatalf("BuildGrokVideoProxyURL() = %q, want empty", got)
	}
}

func TestVerifyGrokVideoProxySignatureRejectsExpired(t *testing.T) {
	withVideoProxySignSecret(t, "secret")

	exp := time.Now().Add(-time.Minute).Unix()
	sig := signGrokVideoProxy("task_grok", exp, "secret")
	if VerifyGrokVideoProxySignature("task_grok", "0", sig) {
		t.Fatal("expired signature should be rejected")
	}
}
