package promsketch

import (
	"context"
	"math"
)

type VMFunctionCall func(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64

// VMFunctionCalls is a list of all funcVMtions supported by PromQL, including their types.
var VMFunctionCalls = map[string]VMFunctionCall{
	"change_over_time":   funcVMChangeOverTime,
	"avg_over_time":      funcVMAvgOverTime,
	"count_over_time":    funcVMCountOverTime,
	"entropy_over_time":  funcVMEntropyOverTime,
	"max_over_time":      funcVMMaxOverTime,
	"min_over_time":      funcVMMinOverTime,
	"stddev_over_time":   funcVMStddevOverTime,
	"stdvar_over_time":   funcVMStdvarOverTime,
	"sum_over_time":      funcVMSumOverTime,
	"sum2_over_time":     funcVMSum2OverTime,
	"distinct_over_time": funcVMCardOverTime,
	"l1_over_time":       funcVML1OverTime,
	"l2_over_time":       funcVML2OverTime,
	"quantile_over_time": funcVMQuantileOverTime,
	// "quantiles_over_time": funcVMQuantilesOverTime,
}

// TODO: add last item value in the change data structure
func funcVMChangeOverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {
	count := sketchIns.sampling.QueryCount(t1, t2)
	return count
}

func funcVMAvgOverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {
	// fmt.Println("in VM avg_over_time", sketchIns.sampling.Sampling_rate, len(sketchIns.sampling.Arr), sketchIns.sampling.Max_size)
	// fmt.Println("in VM avg_over_time", sketchIns.sampling.Time_window_size, sketchIns.sampling.GetMinTime(), sketchIns.sampling.GetMaxTime(), t1, t2)
	avg := sketchIns.sampling.QueryAvg(t1, t2)
	return avg
}

func funcVMSumOverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {

	sum := sketchIns.sampling.QuerySum(t1, t2)
	return sum
}

func funcVMSum2OverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {

	sum2 := sketchIns.sampling.QuerySum2(t1, t2)
	return sum2
}

func funcVMCountOverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {

	count := sketchIns.sampling.QueryCount(t1, t2)
	return count
}

func funcVMStddevOverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {

	// count := sketchIns.sampling.QueryCount(t1, t2)
	// sum := sketchIns.sampling.QuerySum(t1, t2)
	// sum2 := sketchIns.sampling.QuerySum2(t1, t2)

	// stddev := math.Sqrt(sum2/count - math.Pow(sum/count, 2))
	stddev := sketchIns.sampling.QueryStddev(t1, t2)
	return stddev
}

func funcVMStdvarOverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {

	// count := sketchIns.sampling.QueryCount(t1, t2)
	// sum := sketchIns.sampling.QuerySum(t1, t2)
	// sum2 := sketchIns.sampling.QuerySum2(t1, t2)

	// stdvar := sum2/count - math.Pow(sum/count, 2)
	stdvar := sketchIns.sampling.QueryStdvar(t1, t2)
	return stdvar
}

func funcVMEntropyOverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {

	merged_univ, m, n, err := sketchIns.ehuniv.QueryIntervalMergeUniv(t1, t2, t)
	if err != nil {
		return 0
	}

	var entropy float64 = 0
	if merged_univ != nil && m == nil {
		entropy = merged_univ.calcEntropy()
	} else {
		entropy = calc_entropy_map(m, n)
	}

	return entropy
}

func funcVMCardOverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {

	merged_univ, m, _, err := sketchIns.ehuniv.QueryIntervalMergeUniv(t1, t2, t)
	if err != nil {
		return 0
	}
	var card float64 = 0
	if merged_univ != nil && m == nil {
		card = merged_univ.calcCard()
	} else {
		card = calc_distinct_map(m)
	}
	return card
}

func funcVML1OverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {

	merged_univ, m, _, err := sketchIns.ehuniv.QueryIntervalMergeUniv(t1, t2, t)
	if err != nil {
		return 0
	}
	var l1 float64 = 0
	if merged_univ != nil && m == nil {
		l1 = merged_univ.calcL1()
	} else {
		l1 = calc_l1_map(m)
	}

	return l1
}

func funcVML2OverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {

	merged_univ, m, _, err := sketchIns.ehuniv.QueryIntervalMergeUniv(t1, t2, t)
	if err != nil {
		return 0
	}
	var l2 float64 = 0
	if merged_univ != nil && m == nil {
		l2 = merged_univ.calcL2()
	} else {
		l2 = calc_l2_map(m)
	}

	return l2
}

func funcVMQuantileOverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {
	if len(c) < 1 {
		return math.NaN()
	}
	phi := c[0]
	merged_kll := sketchIns.ehkll.QueryIntervalMergeKLL(t1, t2)
	cdf := merged_kll.CDF()
	q_value := cdf.Query(phi)
	return q_value
}

func funcVMQuantilesOverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) []float64 {
	if len(c) < 1 {
		return []float64{math.NaN()}
	}
	merged_kll := sketchIns.ehkll.QueryIntervalMergeKLL(t1, t2)
	cdf := merged_kll.CDF()
	q_values := make([]float64, 0)
	for _, phi := range c {
		q_values = append(q_values, cdf.Query(phi))
	}
	return q_values
}

func funcVMMinOverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {
	merged_kll := sketchIns.ehkll.QueryIntervalMergeKLL(t1, t2)
	cdf := merged_kll.CDF()
	q_value := cdf.Query(0)
	return q_value
}

func funcVMMaxOverTime(ctx context.Context, sketchIns *SketchInstances, c []float64, t1, t2, t int64) float64 {
	merged_kll := sketchIns.ehkll.QueryIntervalMergeKLL(t1, t2)
	cdf := merged_kll.CDF()
	q_value := cdf.Query(1)
	return q_value
}
