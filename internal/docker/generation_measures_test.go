package docker

import "testing"

// Story 56.1: the slot measures line a generation prints on stderr.
func TestGenerationMeasuresReadsTheSentinelLine(t *testing.T) {
	stderr := []byte("Generating...\n" +
		"###ARCHILAN-WARNING### {\"message\":\"accessibility\"}\n" +
		"###ARCHILAN-MEASURES### [{\"slot\":1,\"name\":\"Solo\",\"game\":\"Hollow Knight\",\"locations\":283,\"ownProgression\":196,\"hostedProgression\":0}]\n" +
		"Done\n")

	got := generationMeasures(stderr)

	want := `[{"slot":1,"name":"Solo","game":"Hollow Knight","locations":283,"ownProgression":196,"hostedProgression":0}]`
	if string(got) != want {
		t.Fatalf("measures = %s, want %s", got, want)
	}
}

func TestGenerationMeasuresIsNilWithoutAValidLine(t *testing.T) {
	for name, stderr := range map[string]string{
		"older image": "Generating...\nDone\n",
		"broken json": "###ARCHILAN-MEASURES### [{\"slot\":\n",
		"not a list":  "###ARCHILAN-MEASURES### {\"slot\":1}\n",
	} {
		if got := generationMeasures([]byte(stderr)); got != nil {
			t.Errorf("%s: measures = %s, want nil", name, got)
		}
	}
}
