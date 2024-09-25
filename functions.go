package promsketch

import (
	"context"
	"math"
)

type FunctionCall func(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector

// FunctionCalls is a list of all functions supported by PromQL, including their types.
var FunctionCalls = map[string]FunctionCall{
	"change_over_time":   funcChangeOverTime,
	"avg_over_time":      funcAvgOverTime,
	"count_over_time":    funcCountOverTime,
	"entropy_over_time":  funcEntropyOverTime,
	"max_over_time":      funcMaxOverTime,
	"min_over_time":      funcMinOverTime,
	"stddev_over_time":   funcStddevOverTime,
	"stdvar_over_time":   funcStdvarOverTime,
	"sum_over_time":      funcSumOverTime,
	"sum2_over_time":     funcSum2OverTime,
	"distinct_over_time": funcCardOverTime,
	"l1_over_time":       funcL1OverTime,
	"l2_over_time":       funcL2OverTime,
	"quantile_over_time": funcQuantileOverTime,
}

func calc_entropy(values *[]float64) float64 {
	m := make(map[float64]int)
	n := float64(len(*values))
	for _, v := range *values {
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

	entropy = math.Log2(n) - entropy/n
	return entropy
}

func calc_l1(values *[]float64) float64 {
	m := make(map[float64]int)
	for _, v := range *values {
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

	return l1
}

func calc_distinct(values *[]float64) float64 {
	m := make(map[float64]int)
	for _, v := range *values {
		if _, ok := m[v]; !ok {
			m[v] = 1
		} else {
			m[v] += 1
		}
	}
	distinct := float64(len(m))
	return distinct
}

func calc_l2(values *[]float64) float64 {
	m := make(map[float64]int)
	for _, v := range *values {
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

	l2 = math.Sqrt(l2)
	return l2
}

// TODO: add last item value in the change data structure
func funcChangeOverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	count := series.sketchInstances.sampling.QueryCount(t1, t2)
	return Vector{Sample{
		F: count,
	}}
}

func funcAvgOverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	avg := series.sketchInstances.sampling.QueryAvg(t1, t2)
	return Vector{Sample{
		F: avg,
	}}
}

func funcSumOverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	sum := series.sketchInstances.sampling.QuerySum(t1, t2)
	return Vector{Sample{
		F: sum,
	}}
}

func funcSum2OverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	sum2 := series.sketchInstances.sampling.QuerySum2(t1, t2)
	return Vector{Sample{
		F: sum2,
	}}
}

func funcCountOverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	count := series.sketchInstances.sampling.QueryCount(t1, t2)
	return Vector{Sample{
		F: float64(count),
	}}
}

func funcStddevOverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	count := series.sketchInstances.sampling.QueryCount(t1, t2)
	sum := series.sketchInstances.sampling.QuerySum(t1, t2)
	sum2 := series.sketchInstances.sampling.QuerySum2(t1, t2)

	stddev := math.Sqrt(sum2/count - math.Pow(sum/count, 2))
	return Vector{Sample{
		F: float64(stddev),
	}}
}

func funcStdvarOverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	count := series.sketchInstances.sampling.QueryCount(t1, t2)
	sum := series.sketchInstances.sampling.QuerySum(t1, t2)
	sum2 := series.sketchInstances.sampling.QuerySum2(t1, t2)

	stdvar := sum2/count - math.Pow(sum/count, 2)
	return Vector{Sample{
		F: stdvar,
	}}
}

func funcEntropyOverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	merged_univ, samples, err := series.sketchInstances.ehuniv.QueryIntervalMergeUniv(t-t2, t-t1, t)
	if err != nil {
		return make(Vector, 0)
	}

	var entropy float64 = 0
	if merged_univ != nil && samples == nil {
		entropy = merged_univ.calcEntropy()
	} else {
		entropy = calc_entropy(samples)
	}

	return Vector{Sample{
		F: entropy,
	}}
}

func funcCardOverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	merged_univ, samples, err := series.sketchInstances.ehuniv.QueryIntervalMergeUniv(t-t2, t-t1, t)
	if err != nil {
		return make(Vector, 0)
	}
	var card float64 = 0
	if merged_univ != nil && samples == nil {
		card = merged_univ.calcCard()
	} else {
		card = calc_distinct(samples)
	}
	return Vector{Sample{
		F: card,
	}}
}

func funcL1OverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	merged_univ, samples, err := series.sketchInstances.ehuniv.QueryIntervalMergeUniv(t-t2, t-t1, t)
	if err != nil {
		return make(Vector, 0)
	}
	var l1 float64 = 0
	if merged_univ != nil && samples == nil {
		l1 = merged_univ.calcL1()
	} else {
		l1 = calc_l1(samples)
	}

	return Vector{Sample{
		F: l1,
	}}
}

func funcL2OverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	merged_univ, samples, err := series.sketchInstances.ehuniv.QueryIntervalMergeUniv(t-t2, t-t1, t)
	if err != nil {
		return make(Vector, 0)
	}
	var l2 float64 = 0
	if merged_univ != nil && samples == nil {
		l2 = merged_univ.calcL2()
	} else {
		l2 = calc_l2(samples)
	}

	return Vector{Sample{
		F: l2,
	}}
}

func funcQuantileOverTime(ctx context.Context, series *memSeries, phi float64, t1, t2, t int64) Vector {
	merged_kll := series.sketchInstances.ehkll.QueryIntervalMergeKLL(t1, t2)
	cdf := merged_kll.CDF()
	q_value := cdf.Query(phi)
	return Vector{Sample{
		F: q_value,
	}}
}

func funcMinOverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	merged_kll := series.sketchInstances.ehkll.QueryIntervalMergeKLL(t1, t2)
	cdf := merged_kll.CDF()
	q_value := cdf.Query(0)
	return Vector{Sample{
		F: q_value,
	}}
}

func funcMaxOverTime(ctx context.Context, series *memSeries, c float64, t1, t2, t int64) Vector {
	merged_kll := series.sketchInstances.ehkll.QueryIntervalMergeKLL(t1, t2)
	cdf := merged_kll.CDF()
	q_value := cdf.Query(1)
	return Vector{Sample{
		F: q_value,
	}}
}