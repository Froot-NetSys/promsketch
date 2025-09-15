package promsketch

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zzylol/prometheus-sketches/model/labels"
)

func TestSamplingCache(t *testing.T) {
	sc := NewSamplingCache()
	time_window_size := int64(100000)
	scrape_interval := int64(100)
	item_window_size := int(time_window_size / scrape_interval)
	sampling_rate := 0.1
	lset := labels.FromStrings("fake_metric", "machine0")
	FuncName := "count_over_time"
	var mint, maxt int64
	mint = 900000
	maxt = 1000000

	lookup := sc.LookUp(FuncName, lset, mint, maxt)
	t.Log("lookup=", lookup)

	if lookup == false {
		err := sc.NewSamplingCacheEntry(lset, sampling_rate, time_window_size, item_window_size) // Then the max_size of sampling cache is 0.1 * 1000 = 100 items
		require.NoError(t, err)
	}

	lookup = sc.LookUp(FuncName, lset, mint, maxt)
	t.Log("lookup=", lookup)

	outVec, _ := sc.Eval(FuncName, lset, sampling_rate, mint, maxt)
	fmt.Println("outVec=", outVec)

	for i := 0; i < 10000; i++ {
		err := sc.Insert(lset, int64(i*int(scrape_interval)), 0.1*float64(i))
		// s := sc.series.getByHash(lset.Hash(), lset)
		// 	fmt.Println(len(s.us.Arr), s.us.Cur_time, s.us.Time_window_size)
		require.NoError(t, err)
	}
	floats, err := sc.Select(lset, 0, 5)
	fmt.Println(floats, err)

	floats, err = sc.Select(lset, 2, 11)
	fmt.Println(floats, err)

	floats, err = sc.Select(lset, mint, maxt)
	fmt.Println(floats, err)

	outVec, _ = sc.Eval(FuncName, lset, sampling_rate, mint, maxt)
	fmt.Println(outVec)

}
