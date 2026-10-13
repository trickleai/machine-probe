package bench

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Load reads a result document. Field type mismatches (e.g. files written by machine-lab)
// are tolerated: encoding/json fills every compatible field and reports only the first mismatch.
func Load(path string) (*Run, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Run
	if err := json.Unmarshal(b, &r); err != nil {
		var te *json.UnmarshalTypeError
		if !errors.As(err, &te) {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	if r.Results == nil && r.Fingerprint == nil {
		return nil, fmt.Errorf("%s: not a machine-probe / machine-lab result file", path)
	}
	return &r, nil
}
