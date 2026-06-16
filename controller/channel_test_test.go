package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

func TestNormalizeChannelTestEndpointDetectsImageGenerationModel(t *testing.T) {
	got := normalizeChannelTestEndpoint(&model.Channel{Type: constant.ChannelTypeOpenAI}, "gpt-image-2", "")
	if got != string(constant.EndpointTypeImageGeneration) {
		t.Fatalf("expected %q, got %q", constant.EndpointTypeImageGeneration, got)
	}
}

func TestNormalizeChannelTestEndpointKeepsExplicitEndpoint(t *testing.T) {
	got := normalizeChannelTestEndpoint(&model.Channel{Type: constant.ChannelTypeOpenAI}, "gpt-image-2", string(constant.EndpointTypeOpenAI))
	if got != string(constant.EndpointTypeOpenAI) {
		t.Fatalf("expected explicit endpoint %q, got %q", constant.EndpointTypeOpenAI, got)
	}
}
