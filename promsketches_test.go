package promsketch

import (
	"encoding/csv"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/mem"
	"github.com/stretchr/testify/require"
	"github.com/zzylol/prometheus-sketches/model/labels"
)

func TestNewSketchCacheInstance(t *testing.T) {
	lset := labels.FromStrings("fake_metric", "machine0")
	ps := NewPromSketches()
	require.NotEmpty(t, ps)

	err := ps.NewSketchCacheInstance(lset, "avg_over_time", 100000, 100000, 10000)
	require.NoError(t, err)
	err = ps.NewSketchCacheInstance(lset, "quantile_over_time", 100000, 100000, 10000)
	require.NoError(t, err)
	err = ps.NewSketchCacheInstance(lset, "distinct_over_time", 1000000, 10000, 10000)
	require.NoError(t, err)
}

func TestLookUpWithQuantileQuery(t *testing.T) {
	lset := labels.FromStrings("fake_metric", "machine0")
	ps := NewPromSketches()
	require.NotEmpty(t, ps)

	lookup := ps.LookUp(lset, "quantile_over_time", 0, 10)
	require.Equal(t, false, lookup)

	err := ps.NewSketchCacheInstance(lset, "quantile_over_time", 100000, 100000, 10000)
	require.NoError(t, err)

	for time := 0; time < 20; time++ {
		err := ps.SketchInsert(lset, int64(time), 0.5+float64(time))
		require.NoError(t, err)
	}

	lookup = ps.LookUp(lset, "quantile_over_time", 0, 10)
	require.Equal(t, true, lookup)
}

func TestLookUpWithSumQuery(t *testing.T) {
	lset := labels.FromStrings("fake_metric", "machine0")
	ps := NewPromSketches()
	require.NotEmpty(t, ps)

	lookup := ps.LookUp(lset, "sum_over_time", 0, 10)
	require.Equal(t, false, lookup)

	err := ps.NewSketchCacheInstance(lset, "sum_over_time", 100000, 100000, 10000)
	require.NoError(t, err)

	for time := 0; time < 20; time++ {
		err := ps.SketchInsert(lset, int64(time), 0.5+float64(time))
		require.NoError(t, err)
	}

	lookup = ps.LookUp(lset, "sum_over_time", 0, 10)
	require.Equal(t, true, lookup)
}

func TestEvalQuantile(t *testing.T) {
	lset := labels.FromStrings("fake_metric", "machine0")
	ps := NewPromSketches()
	require.NotEmpty(t, ps)

	err := ps.NewSketchCacheInstance(lset, "quantile_over_time", 100000, 100000, 10000)
	require.NoError(t, err)

	for time := 0; time < 20; time++ {
		err := ps.SketchInsert(lset, int64(time), 0.5+float64(time))
		require.NoError(t, err)
	}
	lookup := ps.LookUp(lset, "quantile_over_time", 0, 10)
	require.Equal(t, true, lookup)

	vector, _ := ps.Eval("quantile_over_time", lset, 0.6, 1, 10, 10)
	require.Equal(t, vector, Vector{Sample{F: 6.5, T: 0}})
}

/*
func BenchmarkSketchInsertDefinedRules(b *testing.B) {
	sc := &SketchConfig{
		EH_univ_config:  EHUnivConfig{K: 20, Time_window_size: 1000000},
		EH_kll_config:   EHKLLConfig{K: 100, Kll_k: 256, Time_window_size: 1000000},
		Sampling_config: SamplingConfig{Sampling_rate: 0.05, Time_window_size: 1000000, Max_size: 50000},
	}

	lset := labels.FromStrings("fake_metric", "machine0")

	avgsmap := make(map[SketchType]bool)
	avgsmap[EHCount] = true
	entropysmap := make(map[SketchType]bool)
	entropysmap[SHUniv] = true
	entropysmap[EffSum] = true
	entropysmap[EHCount] = true
	quantilesmap := make(map[SketchType]bool)
	quantilesmap[EHDD] = true
	ruletest := []SketchRuleTest{
		{"avg_over_time", funcAvgOverTime, lset, -1, 1000000, 1000000, avgsmap},
		{"count_over_time", funcCountOverTime, lset, -1, 1000000, 1000000, avgsmap},
		{"entropy_over_time", funcEntropyOverTime, lset, -1, 1000000, 1000000, entropysmap},
		{"l1_over_time", funcL1OverTime, lset, -1, 1000000, 1000000, entropysmap},
		{"quantile_over_time", funcQuantileOverTime, lset, 0.5, 1000000, 1000000, quantilesmap},
	}

	ps := NewPromSketchesWithConfig(ruletest, sc)

	for n := 0; n < b.N; n++ {
		t := int64(time.Now().UnixMicro())
		value := float64(0)
		for {
			value = rand.NormFloat64() + 5000
			if value >= 0 && value <= 10000 {
				break
			}
		}
		err := ps.SketchInsertDefinedRules(lset, t, value)
		if err != nil {
			fmt.Println("sketch insert error")
			return
		}
	}
}
*/

var flagvar int
var flagthreads int
var flag_sample_window int64
var flag_algo string

const timeDelta = 100

func init() {
	flag.IntVar(&flagvar, "numts", 10000, "number of timeseries")
	flag.IntVar(&flagthreads, "numthreads", 1, "number of threads")
	flag.Int64Var(&flag_sample_window, "sample_window", 10000, "window size in sample number")
	flag.StringVar(&flag_algo, "algo", "ehkll", "algorithm to test")
}
func TestInsertThroughputZipf(t *testing.T) {
	scrapeCountBatch := 1080000 // 60 hours
	num_ts := flagvar
	sample_window := flag_sample_window
	algo := flag_algo
	promcache := NewPromSketches()

	lsets := make([]labels.Labels, 0)
	for j := 0; j < num_ts; j++ {
		fakeMetric := "machine" + strconv.Itoa(j)
		// inputLabel := labels.FromStrings("fake_metric", fakeMetric, fakeMetric1, fakeMetric2, fakeMetric3, fakeMetric4, fakeMetric5, fakeMetric6, fakeMetric7, fakeMetric8, fakeMetric9, fakeMetric10, fakeMetric11, fakeMetric12, fakeMetric13, fakeMetric14, fakeMetric15, fakeMetric16, fakeMetric17, fakeMetric18, fakeMetric19, fakeMetric20)
		inputLabel := labels.FromStrings("fake_metric", fakeMetric)
		lsets = append(lsets, inputLabel)
		switch algo {
		case "sampling":
			promcache.NewSketchCacheInstance(inputLabel, "sum_over_time", sample_window*100, sample_window, 10000)
		case "ehkll":
			promcache.NewSketchCacheInstance(inputLabel, "quantile_over_time", sample_window*100, sample_window, 10000)
		case "ehuniv":
			promcache.NewSketchCacheInstance(inputLabel, "entropy_over_time", sample_window*100, sample_window, 10000)
		default:
			fmt.Println("not supported algorithm to test.")
		}
	}

	start := time.Now()
	ingestScrapesZipf(lsets, scrapeCountBatch, flagthreads, promcache)

	since := time.Since(start)

	throughput := float64(scrapeCountBatch) * float64(num_ts) / float64(since.Seconds())
	t.Log(num_ts, since.Seconds(), throughput)

}

func ingestScrapesZipf(lbls []labels.Labels, scrapeCount int, num_threads int, promcache *PromSketches) (uint64, error) {
	var total atomic.Uint64

	scrapeCountBatch := 100
	lbl_batch := len(lbls) / num_threads
	if lbl_batch*num_threads < len(lbls) {
		lbl_batch += 1
	}
	fmt.Println("Each thread handles", lbl_batch, "timeseries.")
	start := time.Now()
	for i := 0; i < scrapeCount; i += scrapeCountBatch {
		var wg sync.WaitGroup
		lbls := lbls
		for len(lbls) > 0 {
			l := lbl_batch
			if len(lbls) < lbl_batch {
				l = len(lbls)
			}
			batch := lbls[:l]
			lbls = lbls[l:]

			wg.Add(1)
			go func() {
				defer wg.Done()

				// fmt.Println(i)
				// promcache.PrintEHUniv(batch[0])

				ts := int64(timeDelta * i)

				var ato_total atomic.Uint64

				var s float64 = 1.01
				var v float64 = 1
				var RAND *rand.Rand = rand.New(rand.NewSource(time.Now().Unix()))
				z := rand.NewZipf(RAND, s, v, uint64(100000))

				for j := 0; j < scrapeCountBatch; j++ {
					ts += timeDelta

					for k := 0; k < len(batch); k += 1 {
						err := promcache.SketchInsert(batch[k], ts, float64(z.Uint64()))
						if err != nil {
							panic(err)
						}
						ato_total.Add(1)
					}
				}

				total.Add(ato_total.Load())
			}()
		}
		wg.Wait()
		fmt.Println(i, float64(total.Load())/float64(time.Since(start).Seconds()))
	}
	fmt.Println("ingestion completed")

	return total.Load(), nil
}

func TestIndexingMemory(t *testing.T) {
	scrapeCountBatch := 1080000
	num_ts := flagvar
	sample_window := flag_sample_window
	algo := flag_algo
	promcache := NewPromSketches()

	lsets := make([]labels.Labels, 0)
	for j := 0; j < num_ts; j++ {
		fakeMetric := "machine" + strconv.Itoa(j)
		// inputLabel := labels.FromStrings("fake_metric", fakeMetric, fakeMetric1, fakeMetric2, fakeMetric3, fakeMetric4, fakeMetric5, fakeMetric6, fakeMetric7, fakeMetric8, fakeMetric9, fakeMetric10, fakeMetric11, fakeMetric12, fakeMetric13, fakeMetric14, fakeMetric15, fakeMetric16, fakeMetric17, fakeMetric18, fakeMetric19, fakeMetric20)
		inputLabel := labels.FromStrings("fake_metric", fakeMetric)
		lsets = append(lsets, inputLabel)
		switch algo {
		case "sampling":
			promcache.NewSketchCacheInstance(inputLabel, "sum_over_time", sample_window*100, sample_window, 10000)
		case "ehkll":
			promcache.NewSketchCacheInstance(inputLabel, "quantile_over_time", sample_window*100, sample_window, 10000)
		case "ehuniv":
			promcache.NewSketchCacheInstance(inputLabel, "entropy_over_time", sample_window*100, sample_window, 10000)
		default:
			fmt.Println("not supported algorithm to test.")
		}
	}

	switch dataset {
	case "Zipf":
	case "Dynamic":
	case "Google":
		readGoogle2019()
	case "CAIDA2019":
		readCAIDA2019()
	default:
		fmt.Println("not supported dataset.")
	}

	start := time.Now()
	switch dataset {
	case "Zipf":
		ingestScrapesZipf(lsets, scrapeCountBatch, flagthreads, promcache)
	case "Dynamic":
		ingestScrapesDynamic(lsets, scrapeCountBatch, flagthreads, promcache)
	case "Google":
		ingestScrapesGoogle(lsets, scrapeCountBatch, flagthreads, promcache)
	case "CAIDA2019":
		ingestScrapesGoogle(lsets, scrapeCountBatch, flagthreads, promcache) // ingest from cases[0]
	default:
		fmt.Println("not supported dataset.")
	}

	since := time.Since(start)
	t.Log("total memory: ", promcache.GetTotalMemory()/1024, "MB")
	// t.Log("total memory: ", promcache.GetTotalMemoryEHUniv()/1024, "MB")

	v, _ := mem.VirtualMemory()

	// almost every return value is a struct
	fmt.Printf("Total: %v, Free:%v, UsedPercent:%f%%\n", v.Total, v.Free, v.UsedPercent)
	fmt.Println()
	fmt.Println(v.UsedPercent/100*float64(v.Total)/1024/1024/1024, "GB")
	fmt.Println()
	// convert to JSON. String() is also implemented
	fmt.Println(v)

	throughput := float64(scrapeCountBatch) * float64(num_ts) / float64(since.Seconds())
	t.Log(num_ts, since.Seconds(), throughput)

}

var (
	const_1M int = 10000
	const_2M int = 20000
	const_3M int = 30000
)

func ingestScrapesDynamic(lbls []labels.Labels, scrapeCount int, num_threads int, promcache *PromSketches) (uint64, error) {
	var total atomic.Uint64
	scrapeCountBatch := 100
	lbl_batch := len(lbls) / num_threads
	if lbl_batch*num_threads < len(lbls) {
		lbl_batch += 1
	}
	fmt.Println("Each thread handles", lbl_batch, "timeseries.")
	start := time.Now()
	for i := 0; i < scrapeCount; i += scrapeCountBatch {
		var wg sync.WaitGroup
		lbls := lbls
		for len(lbls) > 0 {
			l := lbl_batch
			if len(lbls) < lbl_batch {
				l = len(lbls)
			}
			batch := lbls[:l]
			lbls = lbls[l:]

			wg.Add(1)
			go func() {
				defer wg.Done()

				// fmt.Println(i)
				// promcache.PrintEHUniv(batch[0])

				ts := int64(timeDelta * i)

				var ato_total atomic.Uint64

				var s float64 = 1.01
				var v float64 = 1
				var RAND *rand.Rand = rand.New(rand.NewSource(time.Now().Unix()))
				z := rand.NewZipf(RAND, s, v, uint64(100000))

				for j := 0; j < scrapeCountBatch; j++ {
					ts += timeDelta

					var value float64 = 0
					if (i+j)%const_3M < const_1M {
						value = float64(z.Uint64())
					} else if (i+j)%const_3M < const_2M {
						value = rand.Float64() * 100000
					} else {
						value = rand.NormFloat64()*50000 + 10000
					}

					for k := 0; k < len(batch); k += 1 {
						err := promcache.SketchInsert(batch[k], ts, value)
						if err != nil {
							panic(err)
						}
						ato_total.Add(1)
					}
				}

				total.Add(ato_total.Load())
			}()
		}
		wg.Wait()
		elapsed := time.Since(start)
		fmt.Println(i, "throughput:", float64(total.Load())/elapsed.Seconds())
	}
	fmt.Println("ingestion completed")

	v, _ := mem.VirtualMemory()

	// almost every return value is a struct
	fmt.Printf("Total: %v, Free:%v, UsedPercent:%f%%\n", v.Total, v.Free, v.UsedPercent)
	fmt.Println()
	fmt.Println(v.UsedPercent/100*float64(v.Total)/1024/1024/1024, "GB")
	fmt.Println()
	// convert to JSON. String() is also implemented
	fmt.Println(v)
	return total.Load(), nil
}

func readGoogle2019() {
	filename := "testdata/google2019.csv"
	vec := make(Vector, 0)

	f, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	reader := csv.NewReader(f)

	records, err := reader.ReadAll()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	for idx, record := range records {
		// fmt.Println(record, reflect.TypeOf(record))
		if idx == 0 {
			continue
		}
		F, _ := strconv.ParseFloat(strings.TrimSpace(record[0]), 64)
		vec = append(vec, Sample{T: int64(idx), F: F})
		if idx > 20000010 {
			break
		}
	}

	key := "google-cluster2019"
	tmp := TestCase{
		key: key,
		vec: vec,
	}
	cases = append(cases, tmp)
}

func TestReadGoogle2019(t *testing.T) {
	readGoogle2019()
}

func ingestScrapesGoogle(lbls []labels.Labels, scrapeCount int, num_threads int, promcache *PromSketches) (uint64, error) {
	var total atomic.Uint64

	scrapeCountBatch := 100
	lbl_batch := len(lbls) / num_threads
	if lbl_batch*num_threads < len(lbls) {
		lbl_batch += 1
	}
	fmt.Println("Each thread handles", lbl_batch, "timeseries.")
	for i := 0; i < scrapeCount; i += scrapeCountBatch {
		var wg sync.WaitGroup
		lbls := lbls
		for len(lbls) > 0 {
			l := lbl_batch
			if len(lbls) < lbl_batch {
				l = len(lbls)
			}
			batch := lbls[:l]
			lbls = lbls[l:]

			wg.Add(1)
			go func() {
				defer wg.Done()

				// fmt.Println(i)
				// promcache.PrintEHUniv(batch[0])

				ts := int64(timeDelta * i)

				var ato_total atomic.Uint64

				for j := 0; j < scrapeCountBatch; j++ {
					ts += timeDelta

					var value float64 = cases[0].vec[i+j].F

					for k := 0; k < len(batch); k += 1 {
						err := promcache.SketchInsert(batch[k], ts, value)
						if err != nil {
							panic(err)
						}
						ato_total.Add(1)
					}
				}

				total.Add(ato_total.Load())
			}()
		}
		wg.Wait()
	}
	fmt.Println("ingestion completed")

	return total.Load(), nil
}

func TestInsertThroughputDynamic(t *testing.T) {
	scrapeCountBatch := 2160000 // 60 hours
	num_ts := flagvar
	sample_window := flag_sample_window
	algo := flag_algo
	promcache := NewPromSketches()

	lsets := make([]labels.Labels, 0)
	for j := 0; j < num_ts; j++ {
		fakeMetric := "machine" + strconv.Itoa(j)
		// inputLabel := labels.FromStrings("fake_metric", fakeMetric, fakeMetric1, fakeMetric2, fakeMetric3, fakeMetric4, fakeMetric5, fakeMetric6, fakeMetric7, fakeMetric8, fakeMetric9, fakeMetric10, fakeMetric11, fakeMetric12, fakeMetric13, fakeMetric14, fakeMetric15, fakeMetric16, fakeMetric17, fakeMetric18, fakeMetric19, fakeMetric20)
		inputLabel := labels.FromStrings("fake_metric", fakeMetric)
		lsets = append(lsets, inputLabel)
		switch algo {
		case "sampling":
			promcache.NewSketchCacheInstance(inputLabel, "sum_over_time", sample_window*100, sample_window, 10000)
		case "ehkll":
			promcache.NewSketchCacheInstance(inputLabel, "quantile_over_time", sample_window*100, sample_window, 10000)
		case "ehuniv":
			promcache.NewSketchCacheInstance(inputLabel, "entropy_over_time", sample_window*100, sample_window, 10000)
		default:
			fmt.Println("not supported algorithm to test.")
		}
	}

	start := time.Now()
	ingestScrapesDynamic(lsets, scrapeCountBatch, flagthreads, promcache)

	since := time.Since(start)

	v, _ := mem.VirtualMemory()

	// almost every return value is a struct
	fmt.Printf("Total: %v, Free:%v, UsedPercent:%f%%\n", v.Total, v.Free, v.UsedPercent)
	fmt.Println()
	fmt.Println(v.UsedPercent/100*float64(v.Total)/1024/1024/1024, "GB")
	fmt.Println()
	// convert to JSON. String() is also implemented
	fmt.Println(v)

	throughput := float64(scrapeCountBatch) * float64(num_ts) / float64(since.Seconds())
	t.Log(num_ts, since.Seconds(), throughput)

}

func TestInsertThroughputGoogle(t *testing.T) {
	scrapeCountBatch := 2160000 // 60 hours
	num_ts := flagvar
	sample_window := flag_sample_window
	algo := flag_algo
	promcache := NewPromSketches()

	readGoogle2019()

	lsets := make([]labels.Labels, 0)
	for j := 0; j < num_ts; j++ {
		fakeMetric := "machine" + strconv.Itoa(j)
		// inputLabel := labels.FromStrings("fake_metric", fakeMetric, fakeMetric1, fakeMetric2, fakeMetric3, fakeMetric4, fakeMetric5, fakeMetric6, fakeMetric7, fakeMetric8, fakeMetric9, fakeMetric10, fakeMetric11, fakeMetric12, fakeMetric13, fakeMetric14, fakeMetric15, fakeMetric16, fakeMetric17, fakeMetric18, fakeMetric19, fakeMetric20)
		inputLabel := labels.FromStrings("fake_metric", fakeMetric)
		lsets = append(lsets, inputLabel)
		switch algo {
		case "sampling":
			promcache.NewSketchCacheInstance(inputLabel, "sum_over_time", sample_window*100, sample_window, 10000)
		case "ehkll":
			promcache.NewSketchCacheInstance(inputLabel, "quantile_over_time", sample_window*100, sample_window, 10000)
		case "ehuniv":
			promcache.NewSketchCacheInstance(inputLabel, "entropy_over_time", sample_window*100, sample_window, 10000)
		default:
			fmt.Println("not supported algorithm to test.")
		}
	}

	start := time.Now()
	ingestScrapesGoogle(lsets, scrapeCountBatch, flagthreads, promcache)

	since := time.Since(start)

	throughput := float64(scrapeCountBatch) * float64(num_ts) / float64(since.Seconds())
	t.Log(num_ts, since.Seconds(), throughput)

}
