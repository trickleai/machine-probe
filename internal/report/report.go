// Package report renders a result document into a self-contained HTML file.
package report

import (
	"bytes"
	_ "embed"
	"encoding/json"

	"github.com/SamdyChen2023/machine-probe/internal/bench"
	"github.com/SamdyChen2023/machine-probe/internal/fingerprint"
)

//go:embed report.html
var tmpl []byte

const placeholder = "__RESULT_JSON__"

type doc struct {
	*bench.Run
	FingerprintSummary []fingerprint.Row `json:"fingerprintSummary"`
}

// Render embeds the run (plus a flattened fingerprint summary) into the template. The score is
// recomputed with the current reference table so re-rendering an old file picks up new anchors.
// json.Marshal escapes <, > and &, so the payload cannot close the <script> tag.
func Render(run *bench.Run) ([]byte, error) {
	scored := *run
	scored.Score = bench.Score(run)
	b, err := json.Marshal(doc{&scored, fingerprint.Summarize(run.Fingerprint)})
	if err != nil {
		return nil, err
	}
	return bytes.Replace(tmpl, []byte(placeholder), b, 1), nil
}
