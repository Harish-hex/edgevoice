package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"edgevoice/internal/iface"
)

func TestLooping(t *testing.T) {
	if !looping(strings.Fields("written by the Tamil Nadu Tamil Nadu Tamil Nadu")) || !looping([]string{"There", "are", "1000000000000000000000000000000000"}) {
		t.Error("loop not caught")
	}
	if looping(strings.Fields("the sky is blue because blue light scatters more")) {
		t.Error("false loop")
	}
}

func TestIsHedge(t *testing.T) {
	for _, h := range []string{"I'm not sure.", "I am unsure about that question.", "I don't know."} {
		if !IsHedge(h) {
			t.Errorf("hedge %q", h)
		}
	}
	if IsHedge("The capital is New Delhi.") {
		t.Error("answer flagged")
	}
}

func TestIsEcho(t *testing.T) {
	if !IsEcho("Indhiyaavoda capital enna adhu.", "indhiyaavooda capital enna adhu what is the capital of india?") {
		t.Error("echo not caught")
	}
	if IsEcho("The capital of India is New Delhi.", "what is the capital of india?") {
		t.Error("real answer flagged as echo")
	}
}

func TestStreamAndClauses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, tok := range []string{"Space", " is", " huge", ",", " and", " it", " is", " cold", ".", " Stars", " burn", "."} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", tok)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL}
	ch, err := c.Stream(context.Background(), []iface.Message{{Role: "user", Content: "x"}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for cl := range Clauses(ch, true) {
		got = append(got, cl)
	}
	if strings.Join(got, "|") != "Space is huge,|and it is cold.|Stars burn." && strings.Join(got, "|") != "Space is huge, and|it is cold.|Stars burn." {
		t.Fatalf("clauses %q", got)
	}
}
