package bench

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/trickleai/machine-probe/internal/fingerprint"
)

// Metric is the aggregate of one measured quantity across repeats.
type Metric struct {
	Unit    string    `json:"unit"`
	Better  string    `json:"better"` // "higher" | "lower"
	Median  float64   `json:"median"`
	Mean    float64   `json:"mean"`
	Min     float64   `json:"min"`
	Max     float64   `json:"max"`
	CV      float64   `json:"cv"` // percent
	N       int       `json:"n"`
	Samples []float64 `json:"samples"`
}

type KV struct {
	Key string
	M   Metric
}

// Metrics is an insertion-ordered map so the JSON keeps the order the test produced.
type Metrics []KV

func (ms Metrics) Get(k string) (Metric, bool) {
	for _, kv := range ms {
		if kv.Key == k {
			return kv.M, true
		}
	}
	return Metric{}, false
}

func (ms Metrics) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv.Key)
		v, err := json.Marshal(kv.M)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (ms *Metrics) UnmarshalJSON(data []byte) error {
	*ms = nil
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return err
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		var m Metric
		if err := dec.Decode(&m); err != nil {
			var te *json.UnmarshalTypeError
			if !errors.As(err, &te) {
				return err
			}
		}
		*ms = append(*ms, KV{t.(string), m})
	}
	return nil
}

type Series struct {
	Name   string       `json:"name"`
	XLabel string       `json:"xLabel"`
	YLabel string       `json:"yLabel"`
	Points [][2]float64 `json:"points"`
}

type Result struct {
	ID      string   `json:"id"`
	Group   string   `json:"group"`
	Title   string   `json:"title"`
	Tool    string   `json:"tool"`
	Repeats int      `json:"repeats"`
	Warmup  int      `json:"warmup"`
	Metrics Metrics  `json:"metrics"`
	Series  []Series `json:"series,omitempty"`
	Notes   string   `json:"notes,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type ProbeInfo struct {
	Version   string `json:"version"`
	GoVersion string `json:"goVersion"`
}

// Run is the result document written to disk.
type Run struct {
	Schema       int                      `json:"schema"`
	ID           string                   `json:"id"`
	Label        string                   `json:"label"`
	Provider     string                   `json:"provider"`
	PricePerHour *float64                 `json:"pricePerHour"`
	Profile      string                   `json:"profile"`
	Groups       []string                 `json:"groups"`
	StartedAt    int64                    `json:"startedAt"`
	DurationMs   int64                    `json:"durationMs"`
	Probe        *ProbeInfo               `json:"probe,omitempty"`
	Score        *Scorecard               `json:"score,omitempty"`
	Fingerprint  *fingerprint.Fingerprint `json:"fingerprint"`
	Results      []Result                 `json:"results"`
}

// Measure is one sample of one metric from a single repeat.
type Measure struct {
	Key    string
	Unit   string
	Better string
	Value  float64
}

// Rep is what one execution of a test returns.
type Rep struct {
	Measures []Measure
	Series   []Series
	Notes    string
}

func (r *Rep) Hi(key, unit string, v float64) {
	r.Measures = append(r.Measures, Measure{key, unit, "higher", v})
}
func (r *Rep) Lo(key, unit string, v float64) {
	r.Measures = append(r.Measures, Measure{key, unit, "lower", v})
}
