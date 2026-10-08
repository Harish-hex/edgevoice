package config

import "testing"

func TestAblationChain(t *testing.T) {
	b, err := Load("../../config/ablations/01_baseline.yaml")
	if err != nil || b.ASR.Engine != "whisper" || b.Endpoint.SilenceMs != 800 || b.NLU.FastPath || b.LLM.PromptCache {
		t.Fatalf("baseline %v %+v", err, b)
	}
	s, err := Load("../../config/ablations/09_semantic_endpoint.yaml")
	if err != nil || s.ASR.Engine != "zipformer" || s.Endpoint.SilenceMs != 400 || s.Endpoint.EarlySilenceMs != 200 || !s.NLU.FastPath || !s.Reply.Clips || !s.LLM.PromptCache || !s.LLM.ClauseTTS {
		t.Fatalf("row9 %v %+v", err, s)
	}
}
