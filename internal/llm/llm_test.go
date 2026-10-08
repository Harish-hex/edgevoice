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
	if strings.Join(got, "|") != "Space is huge,|and it is cold.|Stars burn." {
		t.Fatalf("clauses %q", got)
	}
}
