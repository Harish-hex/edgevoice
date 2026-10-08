// Package llm talks to a local llama-server subprocess (loopback only) and splits its stream into clauses.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"edgevoice/internal/iface"
)

// Client implements iface.LLM against llama-server's OpenAI-compatible endpoint.
type Client struct {
	URL         string // e.g. http://127.0.0.1:8081
	PromptCache bool   // O4
	System      string
}

func (c *Client) body(msgs []iface.Message, maxTokens int, stream bool) []byte {
	all := append([]iface.Message{{Role: "system", Content: c.System}}, msgs...)
	type m struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var ms []m
	for _, x := range all {
		ms = append(ms, m{x.Role, x.Content})
	}
	b, _ := json.Marshal(map[string]any{
		"messages": ms, "max_tokens": maxTokens, "stream": stream, "temperature": 0.3, "repeat_penalty": 1.18, "frequency_penalty": 0.4, "presence_penalty": 0.2,
		"cache_prompt":         c.PromptCache,
		"chat_template_kwargs": map[string]any{"enable_thinking": false}, // Qwen3: no <think>
	})
	return b
}

// Stream returns token chunks; the channel closes at end of reply or ctx cancel.
func (c *Client) Stream(ctx context.Context, msgs []iface.Message, maxTokens int) (<-chan string, error) {
	req, _ := http.NewRequestWithContext(ctx, "POST", c.URL+"/v1/chat/completions", bytes.NewReader(c.body(msgs, maxTokens, true)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("llm: status %d", resp.StatusCode)
	}
	out := make(chan string, 32)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			if line == "[DONE]" {
				return
			}
			var ev struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(line), &ev) == nil && len(ev.Choices) > 0 && ev.Choices[0].Delta.Content != "" {
				select {
				case out <- ev.Choices[0].Delta.Content:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// Warm primes the KV cache with the system prompt (O4).
func (c *Client) Warm(ctx context.Context) error {
	if !c.PromptCache {
		return nil
	}
	ch, err := c.Stream(ctx, []iface.Message{{Role: "user", Content: "hi"}}, 1)
	if err != nil {
		return err
	}
	for range ch {
	}
	return nil
}
