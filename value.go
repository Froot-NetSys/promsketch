package promsketch

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/zzylol/prometheus-sketches/model/labels"
)

// String represents a string value.
type String struct {
	T int64
	V string
}

func (s String) String() string {
	return s.V
}

func (s String) MarshalJSON() ([]byte, error) {
	return json.Marshal([...]interface{}{float64(s.T) / 1000, s.V})
}

// Scalar is a data point that's explicitly not associated with a metric.
type Scalar struct {
	T int64
	V float64
}

func (s Scalar) String() string {
	v := strconv.FormatFloat(s.V, 'f', -1, 64)
	return fmt.Sprintf("scalar: %v @[%v]", v, s.T)
}

func (s Scalar) MarshalJSON() ([]byte, error) {
	v := strconv.FormatFloat(s.V, 'f', -1, 64)
	return json.Marshal([...]interface{}{float64(s.T) / 1000, v})
}

// Series is a stream of data points belonging to a metric.
type Series struct {
	Metric labels.Labels `json:"metric"`
	Floats []FPoint      `json:"values,omitempty"`
}

func (s Series) String() string {
	// TODO(beorn7): This currently renders floats first and then
	// histograms, each sorted by timestamp. Maybe, in mixed series, that's
	// fine. Maybe, however, primary sorting by timestamp is preferred, in
	// which case this has to be changed.
	vals := make([]string, 0, len(s.Floats))
	for _, f := range s.Floats {
		vals = append(vals, f.String())
	}
	return fmt.Sprintf("%s =>\n%s", s.Metric, strings.Join(vals, "\n"))
}

// FPoint represents a single float data point for a given timestamp.
type FPoint struct {
	T int64
	F float64
}

func (p FPoint) String() string {
	s := strconv.FormatFloat(p.F, 'f', -1, 64)
	return fmt.Sprintf("%s @[%v]", s, p.T)
}

// MarshalJSON implements json.Marshaler.
//
// JSON marshaling is only needed for the HTTP API. Since FPoint is such a
// frequently marshaled type, it gets an optimized treatment directly in
// web/api/v1/api.go. Therefore, this method is unused within Prometheus. It is
// still provided here as convenience for debugging and for other users of this
// code. Also note that the different marshaling implementations might lead to
// slightly different results in terms of formatting and rounding of the
// timestamp.
func (p FPoint) MarshalJSON() ([]byte, error) {
	v := strconv.FormatFloat(p.F, 'f', -1, 64)
	return json.Marshal([...]interface{}{float64(p.T) / 1000, v})
}

// Sample is a single sample belonging to a metric. It represents either a float
// sample or a histogram sample. If H is nil, it is a float sample. Otherwise,
// it is a histogram sample.
type Sample struct {
	T int64
	F float64
}

func (s Sample) String() string {
	var str string
	p := FPoint{T: s.T, F: s.F}
	str = p.String()
	return fmt.Sprintf("%s", str)
}

// MarshalJSON is mirrored in web/api/v1/api.go with jsoniter because FPoint and
// HPoint wouldn't be marshaled with jsoniter otherwise.
func (s Sample) MarshalJSON() ([]byte, error) {
	f := struct {
		F FPoint `json:"value"`
	}{

		F: FPoint{T: s.T, F: s.F},
	}
	return json.Marshal(f)
}

type SSample struct {
	T int64
	F string
}
type SVector []SSample

// Vector is basically only an alias for []Sample, but the contract is that
// in a Vector, all Samples have the same timestamp.
type Vector []Sample

func (vec Vector) String() string {
	entries := make([]string, len(vec))
	for i, s := range vec {
		entries[i] = s.String()
	}
	return strings.Join(entries, "\n")
}

// TotalSamples returns the total number of samples in the series within a vector.
// Float samples have a weight of 1 in this number, while histogram samples have a higher
// weight according to their size compared with the size of a float sample.
// See HPoint.size for details.
func (vec Vector) TotalSamples() int {
	return len(vec)
}

// Matrix is a slice of Series that implements sort.Interface and
// has a String method.
type Matrix []Series

func (m Matrix) String() string {
	// TODO(fabxc): sort, or can we rely on order from the querier?
	strs := make([]string, len(m))

	for i, ss := range m {
		strs[i] = ss.String()
	}

	return strings.Join(strs, "\n")
}

// TotalSamples returns the total number of samples in the series within a matrix.
// Float samples have a weight of 1 in this number, while histogram samples have a higher
// weight according to their size compared with the size of a float sample.
// See HPoint.size for details.
func (m Matrix) TotalSamples() int {
	numSamples := 0
	for _, series := range m {
		numSamples += len(series.Floats)
	}
	return numSamples
}

func (m Matrix) Len() int           { return len(m) }
func (m Matrix) Less(i, j int) bool { return labels.Compare(m[i].Metric, m[j].Metric) < 0 }
func (m Matrix) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }

// ContainsSameLabelset checks if a matrix has samples with the same labelset.
// Such a behavior is semantically undefined.
// https://github.com/prometheus/prometheus/issues/4562
func (m Matrix) ContainsSameLabelset() bool {
	switch len(m) {
	case 0, 1:
		return false
	case 2:
		return m[0].Metric.Hash() == m[1].Metric.Hash()
	default:
		l := make(map[uint64]struct{}, len(m))
		for _, ss := range m {
			hash := ss.Metric.Hash()
			if _, ok := l[hash]; ok {
				return true
			}
			l[hash] = struct{}{}
		}
		return false
	}
}
