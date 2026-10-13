package report

import (
	"strings"
	"testing"

	"github.com/SamdyChen2023/machine-probe/internal/bench"
	"github.com/SamdyChen2023/machine-probe/internal/fingerprint"
)

func TestRenderEmbedsEscapedJSON(t *testing.T) {
	r := &bench.Run{Label: "</script><b>x", Fingerprint: &fingerprint.Fingerprint{}, Results: []bench.Result{}}
	b, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, placeholder) || strings.Contains(s, "</script><b>") || !strings.Contains(s, `"fingerprintSummary"`) {
		t.Fatal("payload not embedded safely")
	}
}
