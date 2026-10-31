package docker

import "testing"

// Story 38.31: the stored introspection is its JSON, never what an apworld printed before it.

func TestIntrospectionJSON_dropsWhatAnApworldPrintedBefore(t *testing.T) {
	out := []byte("C compression module not found.  Falling back to python.\n{\"options\":{},\"locations\":[\"A\"]}\n")

	got, err := introspectionJSON(out)
	if err != nil || string(got) != `{"options":{},"locations":["A"]}` {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestIntrospectionJSON_keepsAPureOutput(t *testing.T) {
	got, err := introspectionJSON([]byte(`{"options":{}}`))
	if err != nil || string(got) != `{"options":{}}` {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestIntrospectionJSON_isAnErrorWithoutJSON(t *testing.T) {
	if got, err := introspectionJSON([]byte("Traceback...\n{not json\n")); err == nil {
		t.Fatalf("an output without JSON must fail, got %q", got)
	}
}
