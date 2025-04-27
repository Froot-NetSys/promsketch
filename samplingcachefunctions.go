package promsketch

import (
	"context"
	"math"
	"sort"
)

type SamplingFunctionCall func(ctx context.Context, values []float64, c float64) Vector

// FunctionCalls is a list of all functions supported by PromQL, including their types.
var SamplingFunctionCalls = map[string]SamplingFunctionCall{
	"avg_over_time":      funcSamplingAvgOverTime,
	"count_over_time":    funcSamplingCountOverTime,
	"entropy_over_time":  funcSamplingEntropyOverTime,
	"max_over_time":      funcSamplingMaxOverTime,
	"min_over_time":      funcSamplingMinOverTime,
	"stddev_over_time":   funcSamplingStddevOverTime,
	"stdvar_over_time":   funcSamplingStdvarOverTime,
	"sum_over_time":      funcSamplingSumOverTime,
	"sum2_over_time":     funcSamplingSum2OverTime,
	"distinct_over_time": funcSamplingCardOverTime,
	"l1_over_time":       funcSamplingL1OverTime,
	"l2_over_time":       funcSamplingL2OverTime,
	"quantile_over_time": funcSamplingQuantileOverTime,
}

func funcSamplingAvgOverTime(ctx context.Context, values []float64, c float64) Vector {
	var sum float64 = 0
	for _, v := range values {
		sum += v
	}
	avg := float64(sum) / float64(len(values))
	return Vector{Sample{
		F: avg,
	}}
}

func funcSamplingSumOverTime(ctx context.Context, values []float64, c float64) Vector {
	var sum float64 = 0
	for _, v := range values {
		sum += v
	}
	return Vector{Sample{
		F: sum / c, // c is sampling rate
	}}
}

func funcSamplingSum2OverTime(ctx context.Context, values []float64, c float64) Vector {
	var sum2 float64 = 0
	for _, v := range values {
		sum2 += v * v
	}
	return Vector{Sample{
		F: sum2 / c,
	}}
}

func funcSamplingCountOverTime(ctx context.Context, values []float64, c float64) Vector {
	return Vector{Sample{
		F: float64(len(values)) / c,
	}}
}

func funcSamplingStddevOverTime(ctx context.Context, values []float64, c float64) Vector {
	count := float64(len(values))
	var sum2 float64 = 0
	for _, v := range values {
		sum2 += v * v
	}
	var sum float64 = 0
	for _, v := range values {
		sum += v
	}
	stddev := math.Sqrt(sum2/count - math.Pow(sum/count, 2))
	return Vector{Sample{
		F: float64(stddev),
	}}
}

func funcSamplingStdvarOverTime(ctx context.Context, values []float64, c float64) Vector {
	count := float64(len(values))
	var sum2 float64 = 0
	for _, v := range values {
		sum2 += v * v
	}
	var sum float64 = 0
	for _, v := range values {
		sum += v
	}
	stdvar := sum2/count - math.Pow(sum/count, 2)
	return Vector{Sample{
		F: stdvar,
	}}

}

func funcSamplingEntropyOverTime(ctx context.Context, values []float64, c float64) Vector {
	m := make(map[float64]int)
	for _, v := range values {
		if _, ok := m[v]; !ok {
			m[v] = 1
		} else {
			m[v] += 1
		}
	}
	var entropy float64 = 0
	for _, v := range m {
		entropy += float64(v) * math.Log2(float64(v))
	}

	return Vector{Sample{
		F: math.Log2(float64(len(m))) - entropy/float64(len(m)),
	}}
}

func funcSamplingCardOverTime(ctx context.Context, values []float64, c float64) Vector {
	m := make(map[float64]int)
	for _, v := range values {
		m[v] = 1
	}
	return Vector{Sample{
		F: float64(len(m)),
	}}
}

func funcSamplingL1OverTime(ctx context.Context, values []float64, c float64) Vector {
	m := make(map[float64]int)
	for _, v := range values {
		if _, ok := m[v]; !ok {
			m[v] = 1
		} else {
			m[v] += 1
		}
	}
	var l1 float64 = 0
	for _, v := range m {
		l1 += float64(v)
	}
	return Vector{Sample{
		F: l1,
	}}
}

func funcSamplingL2OverTime(ctx context.Context, values []float64, c float64) Vector {
	m := make(map[float64]int)
	for _, v := range values {
		if _, ok := m[v]; !ok {
			m[v] = 1
		} else {
			m[v] += 1
		}
	}
	var l2 float64 = 0
	for _, v := range m {
		l2 += float64(v * v)
	}
	return Vector{Sample{
		F: math.Sqrt(l2),
	}}
}

func samplingquantile(q float64, values []float64) float64 {
	if len(values) == 0 || math.IsNaN(q) {
		return math.NaN()
	}
	if q < 0 {
		return math.Inf(-1)
	}
	if q > 1 {
		return math.Inf(+1)
	}
	sort.Float64s(values)

	n := float64(len(values))
	// When the quantile lies between two samples,
	// we use a weighted average of the two samples.
	rank := q * (n - 1)

	lowerIndex := math.Max(0, math.Floor(rank))
	upperIndex := math.Min(n-1, lowerIndex+1)

	weight := rank - math.Floor(rank)
	return values[int(lowerIndex)]*(1-weight) + values[int(upperIndex)]*weight
}

func funcSamplingQuantileOverTime(ctx context.Context, values []float64, phi float64) Vector {
	return Vector{Sample{F: samplingquantile(phi, values)}}
}

func funcSamplingMinOverTime(ctx context.Context, values []float64, c float64) Vector {
	var min float64 = values[0]
	for i := 1; i < len(values); i++ {
		if values[i] < min {
			min = values[i]
		}
	}
	return Vector{Sample{F: min}}
}

func funcSamplingMaxOverTime(ctx context.Context, values []float64, c float64) Vector {
	var max float64 = values[0]
	for i := 1; i < len(values); i++ {
		if values[i] > max {
			max = values[i]
		}
	}
	return Vector{Sample{F: max}}
}
