package bench

import (
	"fmt"
	"math"
	"strings"
)

// Scorecard is the absolute (reference-based) score of one run: 0–100 per metric, per group and overall.
type Scorecard struct {
	Reference   string       `json:"reference"`
	Overall     int          `json:"overall"`
	Rating      string       `json:"rating"` // excellent | good | fair | weak
	RatingLabel string       `json:"ratingLabel"`
	Scale       string       `json:"scale"`
	Groups      []GroupScore `json:"groups"`
	Notes       []string     `json:"notes,omitempty"`
}

type GroupScore struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Weight      float64       `json:"weight"`
	Score       int           `json:"score"`
	Rating      string        `json:"rating"`
	RatingLabel string        `json:"ratingLabel"`
	Metrics     []MetricScore `json:"metrics"`
}

type MetricScore struct {
	Test        string  `json:"test"`
	Key         string  `json:"key"`
	Label       string  `json:"label"`
	Hint        string  `json:"hint"`
	Value       float64 `json:"value"`
	Unit        string  `json:"unit"`
	Better      string  `json:"better"`
	Score       int     `json:"score"`
	Rating      string  `json:"rating"`
	RatingLabel string  `json:"ratingLabel"`
	Weak        float64 `json:"weak"`
	Typical     float64 `json:"typical"`
	Excellent   float64 `json:"excellent"`
	Linear      bool    `json:"linear,omitempty"` // anchors interpolate linearly (percentages near zero)
	Noisy       bool    `json:"noisy,omitempty"`
}

const scaleNote = "0–100 分：40 = 偏弱水平，65 = 主流云主机水平，90 = 高端水平。≥85 优秀，65–84 良好，40–64 一般，<40 偏弱。"

func Rate(score float64) (string, string) {
	switch {
	case score >= 85:
		return "excellent", "优秀"
	case score >= 65:
		return "good", "良好"
	case score >= 40:
		return "fair", "一般"
	}
	return "weak", "偏弱"
}

// PointScore maps a value onto the 0–100 scale through the Weak/Typical/Excellent anchors.
func PointScore(v float64, r Ref) float64 {
	lowerBetter := r.Excellent < r.Weak
	f := math.Log
	if r.Linear {
		f = func(x float64) float64 { return x }
	} else if v <= 0 {
		if lowerBetter {
			return 100
		}
		return 0
	}
	x, a, b, c := f(v), f(r.Weak), f(r.Typical), f(r.Excellent)
	var s float64
	if (x-b)*(c-b) >= 0 {
		s = 65 + 25*(x-b)/(c-b)
	} else {
		s = 65 - 25*(x-b)/(a-b)
	}
	return math.Max(0, math.Min(100, s))
}

// Score rates a run against References. It returns nil when no reference metric is present
// (e.g. a result file from another tool).
func Score(run *Run) *Scorecard {
	sc := &Scorecard{Reference: RefVersion, Scale: scaleNote}
	diskDir, diskNote := scoredDiskDir(run.Results)
	if diskNote != "" {
		sc.Notes = append(sc.Notes, diskNote)
	}
	byGroup := map[string]*GroupScore{}
	var order []string // References is already in group order
	for _, ref := range References {
		test := ref.Test
		if strings.HasPrefix(test, "disk.") {
			if diskDir == "" {
				continue
			}
			test += "@" + diskDir
		}
		res, m, ok := lookup(run.Results, test, ref.Key)
		if !ok {
			continue
		}
		g := byGroup[ref.Group]
		if g == nil {
			g = &GroupScore{ID: ref.Group, Title: GroupTitles[ref.Group], Weight: GroupWeights[ref.Group]}
			byGroup[ref.Group] = g
			order = append(order, ref.Group)
		}
		s := PointScore(m.Median, ref)
		rating, label := Rate(s)
		g.Metrics = append(g.Metrics, MetricScore{
			Test: res, Key: ref.Key, Label: ref.Label, Hint: ref.Hint, Value: m.Median, Unit: m.Unit, Better: m.Better,
			Score: int(math.Round(s)), Rating: rating, RatingLabel: label,
			Weak: ref.Weak, Typical: ref.Typical, Excellent: ref.Excellent, Linear: ref.Linear, Noisy: m.CV > 25,
		})
	}
	if len(order) == 0 {
		return nil
	}
	weights := map[string]float64{}
	for _, ref := range References {
		w := ref.Weight
		if w == 0 {
			w = 1
		}
		weights[ref.Group+"\x00"+ref.Key] = w
	}
	var total, wsum float64
	for _, id := range order {
		g := byGroup[id]
		var s, w float64
		for _, m := range g.Metrics {
			mw := weights[id+"\x00"+m.Key]
			s += float64(m.Score) * mw
			w += mw
		}
		gs := s / w
		g.Score = int(math.Round(gs))
		g.Rating, g.RatingLabel = Rate(gs)
		sc.Groups = append(sc.Groups, *g)
		total += gs * g.Weight
		wsum += g.Weight
	}
	overall := total / wsum
	sc.Overall = int(math.Round(overall))
	sc.Rating, sc.RatingLabel = Rate(overall)

	var missing []string
	for _, id := range Groups {
		if byGroup[id] == nil {
			missing = append(missing, GroupTitles[id])
		}
	}
	if len(missing) > 0 {
		sc.Notes = append(sc.Notes, fmt.Sprintf("未包含分组：%s，总分按已有分组的权重重新归一。", strings.Join(missing, "、")))
	}
	if run.Profile == "quick" {
		sc.Notes = append(sc.Notes, "quick 档没有持续负载和网络吞吐测试，相关指标未计分；需要完整评分请用 --profile full。")
	}
	for _, g := range sc.Groups {
		for _, m := range g.Metrics {
			if m.Noisy {
				sc.Notes = append(sc.Notes, fmt.Sprintf("%s 多次测量波动较大（CV > 25%%），该项得分参考价值有限。", m.Label))
			}
		}
	}
	return sc
}

// scoredDiskDir picks the directory whose disk tests are scored: the first one where O_DIRECT worked
// (a real block device), else the first one tested.
func scoredDiskDir(results []Result) (dir, note string) {
	var dirs []string
	for _, r := range results {
		if d, ok := strings.CutPrefix(r.ID, "disk.seq@"); ok && r.Error == "" {
			dirs = append(dirs, d)
			if dir == "" && r.Notes == "" {
				dir = d
			}
		}
	}
	switch {
	case len(dirs) == 0:
		return "", ""
	case dir == "":
		return dirs[0], fmt.Sprintf("%s 不支持 O_DIRECT，磁盘读写可能被页缓存加速，磁盘得分可能偏高。", dirs[0])
	case len(dirs) > 1:
		return dir, fmt.Sprintf("磁盘得分基于 %s；其它目录（如 /dev/shm 内存盘）只列出不计分。", dir)
	}
	return dir, ""
}

func lookup(results []Result, test, key string) (string, Metric, bool) {
	for _, r := range results {
		if r.ID != test && !strings.HasPrefix(r.ID, test+"@") {
			continue
		}
		switch key {
		case "mp_multi_per_vcpu":
			s, ok1 := r.Metrics.Get("mp_single_score")
			e, ok2 := r.Metrics.Get("mp_efficiency")
			if ok1 && ok2 {
				return r.ID, Metric{Unit: "pts", Better: "higher", Median: math.Round(s.Median * e.Median / 100), CV: math.Max(s.CV, e.CV)}, true
			}
		case "mp_https_median_ms":
			var xs []float64
			cv := 0.0
			for _, kv := range r.Metrics {
				if strings.HasPrefix(kv.Key, "mp_https_") && kv.Key != key {
					xs = append(xs, kv.M.Median)
					cv = math.Max(cv, kv.M.CV)
				}
			}
			if len(xs) > 0 {
				return r.ID, Metric{Unit: "ms", Better: "lower", Median: round(median(xs)), CV: cv}, true
			}
		default:
			if m, ok := r.Metrics.Get(key); ok {
				return r.ID, m, true
			}
		}
	}
	return "", Metric{}, false
}
