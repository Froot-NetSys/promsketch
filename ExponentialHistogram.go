package promsketch

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/DataDog/sketches-go/ddsketch"
	"github.com/OneOfOne/xxhash"
	"github.com/RoaringBitmap/roaring/v2"

	"github.com/zzylol/go-kll"
)

type ExpoHistogramKLL struct {
	s_count          int // sketch count
	klls             []*kll.Sketch
	max_time         []int64
	min_time         []int64
	bucketsize       []int64
	k                int64
	time_window_size int64
	kll_k            int
	mutex            sync.RWMutex // when updating s_count and buckets, query should wait; when query, update() should wait
}

type ExpoHistogramDD struct {
	s_count          int // sketch count
	dd               []*ddsketch.DDSketch
	max_time         []int64
	min_time         []int64
	bucketsize       []int64
	ddAccuracy       float64
	k                int64
	time_window_size int64
}

type ExpoHistogramUniv struct {
	cs_seed1         []uint32
	cs_seed2         []uint32
	seed3            uint32
	s_count          int           // sketch count
	univs            []*UnivSketch // each bucket is a univsketch
	k                int64
	time_window_size int64
	univPool         UnivSketchPool
	putch            chan int64

	ctx    context.Context
	cancel func()     // Cancellation function for background ehuniv cleaning.
	mutex  sync.Mutex // when updating s_count and buckets, query should wait; when query, update() should wait

}

type ExpoHistogramCount struct {
	buckets          []CountBucket
	k                int64
	time_window_size int64
	s_count          int
}

type ExpoHistogramCS struct {
	seed1            []uint32
	seed2            []uint32
	s_count          int // sketch count
	cs_instances     []*CountSketch
	k                int64
	time_window_size int64
}

/*------------------------------------------------------------------------------
			Exponential Histogram for KLL
--------------------------------------------------------------------------------*/

func ExpoInitKLL(k int64, kll_k int, time_window_size int64) (ehkll *ExpoHistogramKLL) {
	ehkll = &ExpoHistogramKLL{
		k:                k,
		kll_k:            kll_k,
		s_count:          0,
		time_window_size: time_window_size,
		klls:             make([]*kll.Sketch, 0),
	}

	return ehkll
}

func (ehkll *ExpoHistogramKLL) UpdateWindow(window int64) {
	ehkll.mutex.Lock()
	ehkll.time_window_size = window
	// fmt.Println("cur window=", ehkll.time_window_size)
	ehkll.mutex.Unlock()
}

func (ehkll *ExpoHistogramKLL) Update(time int64, value float64) {
	ehkll.mutex.Lock()

	// remove expired buckets
	removed := 0
	for i := 0; i < ehkll.s_count; i++ {
		if ehkll.max_time[i] < time-ehkll.time_window_size {
			removed++
		} else {
			break
		}
	}

	if removed > 0 {
		ehkll.s_count = ehkll.s_count - removed
		ehkll.klls = ehkll.klls[removed:]
		ehkll.max_time = ehkll.max_time[removed:]
		ehkll.min_time = ehkll.min_time[removed:]
		ehkll.bucketsize = ehkll.bucketsize[removed:]
	}

	// init new KLL
	tmp := kll.New(ehkll.kll_k)
	ehkll.klls = append(ehkll.klls, tmp)
	ehkll.klls[ehkll.s_count].Update(value)
	ehkll.bucketsize = append(ehkll.bucketsize, 1)
	ehkll.max_time = append(ehkll.max_time, time)
	ehkll.min_time = append(ehkll.min_time, time)
	ehkll.s_count++

	// Merge EH buckets
	same_size_bucket := 1
	for i := ehkll.s_count - 2; i >= 0; i-- {
		if ehkll.bucketsize[i] == ehkll.bucketsize[i+1] {
			same_size_bucket += 1
		} else {
			if float64(same_size_bucket) >= float64(ehkll.k)/2.0+2 {
				ehkll.klls[i+1].Merge(ehkll.klls[i+2])
				ehkll.bucketsize[i+1] += ehkll.bucketsize[i+2]
				ehkll.max_time[i+1] = MaxInt64(ehkll.max_time[i+1], ehkll.max_time[i+2])
				ehkll.min_time[i+1] = MinInt64(ehkll.min_time[i+1], ehkll.min_time[i+2])
				ehkll.klls[i+2] = nil
				ehkll.klls = append(ehkll.klls[:i+2], ehkll.klls[i+3:]...)
				ehkll.max_time = append(ehkll.max_time[:i+2], ehkll.max_time[i+3:]...)
				ehkll.min_time = append(ehkll.min_time[:i+2], ehkll.min_time[i+3:]...)
				ehkll.bucketsize = append(ehkll.bucketsize[:i+2], ehkll.bucketsize[i+3:]...)
				ehkll.s_count -= 1
			}
			same_size_bucket = 1
			if ehkll.bucketsize[i+1] == ehkll.bucketsize[i] {
				same_size_bucket += 1
			}
		}
	}

	if float64(same_size_bucket) >= float64(ehkll.k)/2.0+2 {
		ehkll.klls[0].Merge(ehkll.klls[1])
		ehkll.bucketsize[0] += ehkll.bucketsize[1]
		ehkll.max_time[0] = MaxInt64(ehkll.max_time[0], ehkll.max_time[1])
		ehkll.min_time[0] = MinInt64(ehkll.min_time[0], ehkll.min_time[1])
		ehkll.klls[1] = nil
		ehkll.klls = append(ehkll.klls[:1], ehkll.klls[2:]...)
		ehkll.max_time = append(ehkll.max_time[:1], ehkll.max_time[2:]...)
		ehkll.min_time = append(ehkll.min_time[:1], ehkll.min_time[2:]...)
		ehkll.bucketsize = append(ehkll.bucketsize[:1], ehkll.bucketsize[2:]...)
		ehkll.s_count -= 1
	}

	ehkll.mutex.Unlock()
}

func (ehkll *ExpoHistogramKLL) Cover(mint, maxt int64) bool {
	// fmt.Println("ehkll s_count =", ehkll.s_count)

	ehkll.mutex.RLock()
	if ehkll.s_count == 0 {
		ehkll.mutex.RUnlock()
		return false
	}

	// fmt.Println("cover search:", mint, maxt, ehkll.min_time[0], ehkll.max_time[ehkll.s_count-1])
	isCovered := ehkll.max_time[ehkll.s_count-1] >= maxt && ehkll.min_time[0] <= mint
	ehkll.mutex.RUnlock()
	return isCovered
	// return ehkll.max_time[ehkll.s_count-1]-ehkll.time_window_size <= mint && ehkll.min_time[0] <= maxt
}

func (ehkll *ExpoHistogramKLL) GetMaxTime() int64 {
	if ehkll.s_count == 0 {
		return -1
	}

	return ehkll.max_time[ehkll.s_count-1]
}

func (ehkll *ExpoHistogramKLL) GetMinTime() int64 {
	if ehkll.s_count == 0 {
		return -1
	}

	return ehkll.min_time[0]
}

func (ehkll *ExpoHistogramKLL) GetMemory() float64 {
	var total_mem float64 = 0
	for i := 0; i < ehkll.s_count; i++ {
		total_mem += float64(unsafe.Sizeof(*ehkll.klls[i]))
		for j := range ehkll.klls[i].Compactors {
			total_mem += float64(len(ehkll.klls[i].Compactors[j])) * 8
		}
	}
	total_mem += float64(unsafe.Sizeof(ehkll))
	total_mem += float64(len(ehkll.max_time)) * 24
	return total_mem / 1024 // KB
}

func (ehkll *ExpoHistogramKLL) print_buckets() {
	fmt.Println("s_count =", ehkll.s_count)
	fmt.Println("k =", ehkll.k)
	for i := 0; i < ehkll.s_count; i++ {
		fmt.Println(i, ehkll.min_time[i], ehkll.max_time[i], ehkll.bucketsize[i])
	}
}

func (ehkll *ExpoHistogramKLL) get_memory() (int, []int64) {
	return ehkll.s_count, ehkll.bucketsize
}

func (ehkll *ExpoHistogramKLL) QueryIntervalMergeKLL(t1, t2 int64) *kll.Sketch {
	var from_bucket, to_bucket int = 0, 0
	ehkll.mutex.RLock()

	if ehkll.s_count == 0 {
		ehkll.mutex.RUnlock()
		return nil
	}

	for i := 0; i < ehkll.s_count; i++ {
		if t1 >= ehkll.min_time[i] && t1 <= ehkll.max_time[i] {
			from_bucket = i
			break
		}
	}

	for i := 0; i < ehkll.s_count; i++ {
		if t2 >= ehkll.min_time[i] && t2 <= ehkll.max_time[i] {
			to_bucket = i
			break
		}
	}

	// fmt.Println("t1, t2=", t1, t2)
	// fmt.Println("min time, max time=", ehkll.min_time[0], ehkll.GetMaxTime())

	if t2 > ehkll.max_time[ehkll.s_count-1] {
		to_bucket = ehkll.s_count - 1
	}
	if t1 < ehkll.min_time[0] {
		from_bucket = 0
	}
	if AbsInt64(t1-ehkll.min_time[from_bucket]) > AbsInt64(t1-ehkll.max_time[from_bucket]) {
		from_bucket += 1
	}

	/*
		fmt.Println("s_count =", ehkll.s_count)
		fmt.Println("from_bucket =", from_bucket)
		fmt.Println("to_bucket =", to_bucket)
	*/

	if to_bucket >= ehkll.s_count {
		to_bucket = ehkll.s_count - 1
	}

	if from_bucket < to_bucket {
		merged_kll := kll.New(ehkll.kll_k)
		for i := from_bucket; i <= to_bucket; i++ {
			merged_kll.Merge(ehkll.klls[i])
		}
		ehkll.mutex.RUnlock()
		return merged_kll
	} else {
		ehkll.mutex.RUnlock()
		return ehkll.klls[from_bucket]
	}
}

/*------------------------------------------------------------------------------
			Exponential Histogram for DDSketch
--------------------------------------------------------------------------------*/

func ExpoInitDD(k int64, time_window_size int64, ddAccuracy float64) (ehdd *ExpoHistogramDD) {
	ehdd = &ExpoHistogramDD{
		k:                k,
		s_count:          0,
		time_window_size: time_window_size,
		dd:               make([]*ddsketch.DDSketch, 0),
		bucketsize:       make([]int64, 0),
		min_time:         make([]int64, 0),
		max_time:         make([]int64, 0),
		ddAccuracy:       ddAccuracy,
	}

	return ehdd
}

func (ehdd *ExpoHistogramDD) Update(time int64, value float64) {
	// remove expired buckets
	removed := 0
	for i := 0; i < ehdd.s_count; i++ {
		if ehdd.max_time[i] < time-ehdd.time_window_size {
			removed++
		} else {
			break
		}
	}

	if removed > 0 {
		ehdd.s_count = ehdd.s_count - removed
		ehdd.dd = ehdd.dd[removed:]
		ehdd.max_time = ehdd.max_time[removed:]
		ehdd.min_time = ehdd.min_time[removed:]
		ehdd.bucketsize = ehdd.bucketsize[removed:]
	}

	// init new DDSketch
	tmp, _ := ddsketch.NewDefaultDDSketch(ehdd.ddAccuracy)
	ehdd.dd = append(ehdd.dd, tmp)
	ehdd.dd[ehdd.s_count].Add(value)
	ehdd.bucketsize = append(ehdd.bucketsize, 1)
	ehdd.max_time = append(ehdd.max_time, time)
	ehdd.min_time = append(ehdd.min_time, time)
	ehdd.s_count++

	// Merge EH buckets
	same_size_bucket := 1
	for i := ehdd.s_count - 2; i >= 0; i-- {
		if ehdd.bucketsize[i] == ehdd.bucketsize[i+1] {
			same_size_bucket += 1
		} else {
			if float64(same_size_bucket) >= float64(ehdd.k)/2.0+2 {
				ehdd.dd[i+1].MergeWith(ehdd.dd[i+2])
				ehdd.bucketsize[i+1] += ehdd.bucketsize[i+2]
				ehdd.max_time[i+1] = MaxInt64(ehdd.max_time[i+1], ehdd.max_time[i+2])
				ehdd.min_time[i+1] = MinInt64(ehdd.min_time[i+1], ehdd.min_time[i+2])
				ehdd.dd = append(ehdd.dd[:i+2], ehdd.dd[i+3:]...)
				ehdd.max_time = append(ehdd.max_time[:i+2], ehdd.max_time[i+3:]...)
				ehdd.min_time = append(ehdd.min_time[:i+2], ehdd.min_time[i+3:]...)
				ehdd.bucketsize = append(ehdd.bucketsize[:i+2], ehdd.bucketsize[i+3:]...)
				ehdd.s_count -= 1
			}
			same_size_bucket = 1
			if ehdd.bucketsize[i+1] == ehdd.bucketsize[i] {
				same_size_bucket += 1
			}
		}
	}

	if float64(same_size_bucket) >= float64(ehdd.k)/2.0+2 {
		ehdd.dd[0].MergeWith(ehdd.dd[1])
		ehdd.dd = append(ehdd.dd[:1], ehdd.dd[2:]...)
		ehdd.bucketsize[0] += ehdd.bucketsize[1]
		ehdd.max_time[0] = MaxInt64(ehdd.max_time[0], ehdd.max_time[1])
		ehdd.min_time[0] = MinInt64(ehdd.min_time[0], ehdd.min_time[1])
		ehdd.max_time = append(ehdd.max_time[:1], ehdd.max_time[2:]...)
		ehdd.min_time = append(ehdd.min_time[:1], ehdd.min_time[2:]...)
		ehdd.bucketsize = append(ehdd.bucketsize[:1], ehdd.bucketsize[2:]...)
		ehdd.s_count -= 1
	}
}

func (ehdd *ExpoHistogramDD) print_buckets() {
	fmt.Println("s_count =", ehdd.s_count)
	fmt.Println("k =", ehdd.k)
	for i := 0; i < ehdd.s_count; i++ {
		fmt.Println(i, ehdd.bucketsize[i], ehdd.min_time[i], ehdd.max_time[i])
		// cdf := ehkll.klls[i].cdf()
		// fmt.Println(cdf[:15])
	}
}

func (ehdd *ExpoHistogramDD) GetMemory() float64 {
	var total_mem float64 = 0
	var value_scale float64 = 100000
	for i := 0; i < ehdd.s_count; i++ {
		total_mem += float64(math.Log(value_scale)/math.Log((1+ehdd.ddAccuracy)/(1-ehdd.ddAccuracy))*16 + float64(unsafe.Sizeof(ehdd.dd[i])))
	}
	return total_mem / 1024 // KB
}

func (ehdd *ExpoHistogramDD) QueryIntervalMergeDD(t1, t2 int64) *ddsketch.DDSketch {
	if ehdd.s_count == 0 {
		return nil
	}

	var from_bucket, to_bucket int = ehdd.s_count, ehdd.s_count
	for i := 0; i < ehdd.s_count; i++ {
		// fmt.Println("mintime, maxtime:", i, ehdd.min_time[i], ehdd.max_time[i])
		if t1 <= ehdd.max_time[i] && t1 >= ehdd.min_time[i] {
			from_bucket = i
		}
		if t2 <= ehdd.max_time[i] && t2 >= ehdd.min_time[i] {
			to_bucket = i
		}
	}
	if t2 > ehdd.max_time[ehdd.s_count-1] {
		to_bucket = ehdd.s_count - 1
	}
	if t1 < ehdd.min_time[0] {
		from_bucket = 0
	}

	if AbsInt64(t1-ehdd.min_time[from_bucket]) > AbsInt64(t1-ehdd.max_time[from_bucket]) {
		from_bucket += 1
	}

	/*
		fmt.Println(t1, t2, ehdd.s_count)
		fmt.Println("from_bucket =", from_bucket)
		fmt.Println("to_bucket =", to_bucket)
	*/

	if from_bucket < to_bucket {
		merged_dd, _ := ddsketch.NewDefaultDDSketch(ehdd.ddAccuracy)
		for i := from_bucket; i <= to_bucket; i++ {
			merged_dd.MergeWith(ehdd.dd[i])
		}
		return merged_dd
	} else {
		return ehdd.dd[from_bucket]
	}
}

func (ehdd *ExpoHistogramDD) Cover(mint, maxt int64) bool {
	if ehdd.s_count == 0 {
		return false
	}
	return mint >= ehdd.max_time[ehdd.s_count-1]-ehdd.time_window_size
	// return (ehdd.min_time[0] <= mint) // && ehdd.max_time[ehdd.s_count-1] >= maxt)
}

/*------------------------------------------------------------------------------
			Exponential Histogram for binary counting
--------------------------------------------------------------------------------*/

func ExpoInitCount(k int64, time_window_size int64) (eh *ExpoHistogramCount) {
	eh = &ExpoHistogramCount{
		k:                k,
		s_count:          0,
		time_window_size: time_window_size,
		buckets:          make([]CountBucket, 0),
	}

	return eh
}

func (eh *ExpoHistogramCount) Update(time int64, value float64) {
	// remove expired buckets
	removed := 0
	for i := 0; i < eh.s_count; i++ {
		if eh.buckets[i].max_time < time-eh.time_window_size {
			// fmt.Println("remove:", eh.buckets[i].max_time, time, eh.time_window_size)
			removed++
		} else {
			break
		}
	}

	if removed > 0 {
		eh.s_count = eh.s_count - removed
		eh.buckets = eh.buckets[removed:]
	}

	tmp := CountBucket{
		count:      1,
		max_time:   time,
		min_time:   time,
		bucketsize: 1,
	}
	eh.buckets = append(eh.buckets, tmp)
	eh.s_count++

	// Merge EH buckets
	same_size_bucket := 1
	start_idx := int(eh.s_count - 2)
	for i := start_idx; i >= 0; i-- {
		if eh.buckets[i].bucketsize == eh.buckets[i+1].bucketsize {
			same_size_bucket += 1
		} else {
			if float64(same_size_bucket) >= float64(eh.k)/2.0+2 {
				eh.buckets[i+1].count += eh.buckets[i+2].count
				eh.buckets[i+1].bucketsize += eh.buckets[i+2].bucketsize
				eh.buckets[i+1].max_time = eh.buckets[i+2].max_time
				eh.buckets = append(eh.buckets[:i+2], eh.buckets[i+3:]...)
				eh.s_count -= 1
			}
			same_size_bucket = 1
			if eh.buckets[i+1].bucketsize == eh.buckets[i].bucketsize {
				same_size_bucket += 1
			}
		}
	}

	if float64(same_size_bucket) >= float64(eh.k)/2.0+2 {
		eh.buckets[0].count += eh.buckets[1].count
		eh.buckets[0].bucketsize += eh.buckets[1].bucketsize
		eh.buckets[0].max_time = eh.buckets[1].max_time
		eh.buckets = append(eh.buckets[:1], eh.buckets[2:]...)
		eh.s_count -= 1
	}

	// eh.print_buckets()
}

func (eh *ExpoHistogramCount) Cover(mint, maxt int64) bool {
	if eh.s_count == 0 {
		return false
	}
	/*
		fmt.Println("mint= ", mint)
		fmt.Println("ehc min_time=", eh.buckets[0].min_time)
		fmt.Println("ehc max_time=", eh.buckets[eh.s_count-1].max_time)
		fmt.Println("ehc s_count =", eh.s_count, "windowsize=", eh.time_window_size)
	*/
	return mint >= eh.buckets[eh.s_count-1].max_time-eh.time_window_size
	// return (eh.buckets[0].min_time <= mint) // && eh.buckets[eh.s_count-1].max_time >= maxt)
}

func (eh *ExpoHistogramCount) print_buckets() {
	fmt.Println("s_count =", eh.s_count)
	fmt.Println("k =", eh.k)
	for i := 0; i < eh.s_count; i++ {
		fmt.Println(i, eh.buckets[i].bucketsize, eh.buckets[i].min_time, eh.buckets[i].max_time)
	}
}

func (eh *ExpoHistogramCount) GetMemory() float64 {
	return 44 * float64(eh.s_count) / 1024 // KBytes
}

func (eh *ExpoHistogramCount) QueryIntervalMergeCount(t1, t2 int64) (CountBucket, error) {
	// var diff1, diff2 int64 = math.MaxInt64, math.MaxInt64
	var from_bucket, to_bucket int = 0, 0
	for i := 0; i < eh.s_count; i++ {
		if t1 >= eh.buckets[i].min_time && t1 <= eh.buckets[i].max_time {
			from_bucket = i
		}
		if t2 >= eh.buckets[i].min_time && t2 <= eh.buckets[i].max_time {
			to_bucket = i
		}
		/*
			curdiff1 := AbsInt64(t1 - eh.buckets[i].min_time)
			curdiff2 := AbsInt64(t2 - eh.buckets[i].max_time)
			if curdiff1 < diff1 {
				diff1 = curdiff1
				from_bucket = i
			}
			if curdiff2 < diff2 {
				diff2 = curdiff2
				to_bucket = i
			}
		*/
	}

	// fmt.Println("from_bucket =", from_bucket)
	// fmt.Println("to_bucket =", to_bucket)
	if eh.s_count > 1 {
		merged_bucket := CountBucket{
			count: 0,
			// sum:   0,
			// sum2:  0,
		}
		for i := from_bucket; i <= to_bucket; i++ {
			merged_bucket.count += eh.buckets[i].count
			// merged_bucket.sum += eh.buckets[i].sum
			// merged_bucket.sum2 += eh.buckets[i].sum2
		}

		return merged_bucket, nil
	} else if eh.s_count == 1 {
		return eh.buckets[0], nil
	} else {
		return CountBucket{}, errors.New("bucket not found")
	}
}

/*------------------------------------------------------------------------------
			Exponential Histogram for count, sum, sum2
--------------------------------------------------------------------------------*/
// Note: metrics with the same meaning (unit) can be placed in one CS, otherwise, the error will be too large

func ExpoInitCountCS(k int64, time_window_size int64) (eh *ExpoHistogramCS) {
	eh = &ExpoHistogramCS{
		k:                k,
		s_count:          0,
		time_window_size: time_window_size,
		cs_instances:     make([]*CountSketch, 0),
	}

	eh.seed1 = make([]uint32, CS_ROW_NO_Univ_ELEPHANT)
	eh.seed2 = make([]uint32, CS_ROW_NO_Univ_ELEPHANT)
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < CS_ROW_NO_Univ_ELEPHANT; i++ {
		eh.seed1[i] = rand.Uint32()
		eh.seed2[i] = rand.Uint32()
	}

	return eh
}

func (eh *ExpoHistogramCS) Update(time int64, key string, value float64) {
	// remove expired buckets
	removed := 0
	for i := 0; i < eh.s_count; i++ {
		if eh.cs_instances[i].max_time < time-eh.time_window_size {
			removed++
		} else {
			break
		}
	}

	if removed > 0 {
		eh.s_count = eh.s_count - removed
		eh.cs_instances = eh.cs_instances[removed:]
	}

	tmp, err := NewCountSketch(CS_ROW_NO_Univ_ELEPHANT, CS_COL_NO_Univ_ELEPHANT, eh.seed1, eh.seed2)
	if err != nil {
		fmt.Println("[Error] failed to allocate memory for new bucket of CountSketch...")
	}
	tmp.max_time, tmp.min_time = time, time
	tmp.bucketsize = 1
	eh.cs_instances = append(eh.cs_instances, tmp)
	eh.cs_instances[eh.s_count].UpdateString(key, value)
	eh.s_count++

	// Merge EH buckets
	same_size_bucket := 1
	for i := eh.s_count - 2; i >= 0; i-- {
		if eh.cs_instances[i].bucketsize == eh.cs_instances[i+1].bucketsize {
			same_size_bucket += 1
		} else {
			if float64(same_size_bucket) >= float64(eh.k)/2.0+2 {
				eh.cs_instances[i+1].MergeWith(eh.cs_instances[i+2])
				eh.cs_instances[i+1].bucketsize += eh.cs_instances[i+2].bucketsize
				eh.cs_instances[i+1].max_time = MaxInt64(eh.cs_instances[i+1].max_time, eh.cs_instances[i+2].max_time)
				eh.cs_instances[i+1].min_time = MinInt64(eh.cs_instances[i+1].min_time, eh.cs_instances[i+2].min_time)
				eh.cs_instances = append(eh.cs_instances[:i+2], eh.cs_instances[i+3:]...)
				eh.s_count -= 1
			}
			same_size_bucket = 1
			if eh.cs_instances[i+1].bucketsize == eh.cs_instances[i].bucketsize {
				same_size_bucket += 1
			}
		}
	}

	if float64(same_size_bucket) >= float64(eh.k)/2.0+2 {
		eh.cs_instances[0].MergeWith(eh.cs_instances[1])
		eh.cs_instances[0].bucketsize += eh.cs_instances[1].bucketsize
		eh.cs_instances[0].max_time = MaxInt64(eh.cs_instances[0].max_time, eh.cs_instances[1].max_time)
		eh.cs_instances[0].min_time = MinInt64(eh.cs_instances[0].min_time, eh.cs_instances[1].min_time)
		eh.cs_instances = append(eh.cs_instances[:1], eh.cs_instances[2:]...)
		eh.s_count -= 1
	}

	// eh.print_buckets()
}

func (eh *ExpoHistogramCS) print_buckets() {
	fmt.Println("s_count =", eh.s_count)
	fmt.Println("k =", eh.k)
	for i := 0; i < eh.s_count; i++ {
		fmt.Println(i, eh.cs_instances[i].bucketsize, eh.cs_instances[i].min_time, eh.cs_instances[i].max_time)
	}
}

func (eh *ExpoHistogramCS) GetMemory() float64 {
	return float64(eh.s_count) * (float64(CS_COL_NO_Univ_ELEPHANT) * float64(CS_ROW_NO_Univ_ELEPHANT) * 8) / 1024
}

func (eh *ExpoHistogramCS) Cover(mint, maxt int64) bool {
	if eh.s_count == 0 {
		return false
	}
	return (eh.cs_instances[0].min_time <= mint) // && eh.cs_instances[eh.s_count-1].max_time >= maxt)
}

func (eh *ExpoHistogramCS) QueryIntervalMergeCount(t1, t2 int64) *CountSketch {
	// var diff1, diff2 int64 = math.MaxInt64, math.MaxInt64
	var from_bucket, to_bucket int = 0, 0
	for i := 0; i < eh.s_count; i++ {
		if t1 >= eh.cs_instances[i].min_time && t1 <= eh.cs_instances[i].max_time {
			from_bucket = i
		}
		if t2 >= eh.cs_instances[i].min_time && t2 <= eh.cs_instances[i].max_time {
			to_bucket = i
		}
		/*
			curdiff1 := AbsInt64(t1 - eh.cs_instances[i].min_time)
			curdiff2 := AbsInt64(t2 - eh.cs_instances[i].max_time)
			if curdiff1 < diff1 {
				diff1 = curdiff1
				from_bucket = i
			}
			if curdiff2 < diff2 {
				diff2 = curdiff2
				to_bucket = i
			}
		*/
	}

	// fmt.Println("from_bucket =", from_bucket)
	// fmt.Println("to_bucket =", to_bucket)
	if eh.s_count > 1 {
		merged_bucket, err := NewCountSketch(CS_ROW_NO_Univ_ELEPHANT, CS_COL_NO_Univ_ELEPHANT, eh.seed1, eh.seed2)
		if err != nil {
			fmt.Println("[Error] failed to allocate memory for new bucket of CountSketch...")
		}
		for i := from_bucket; i <= to_bucket; i++ {
			merged_bucket.MergeWith(eh.cs_instances[i])
		}

		return merged_bucket
	} else if eh.s_count == 1 {
		return eh.cs_instances[0]
	} else {
		return nil
	}
}

/*------------------------------------------------------------------------------
			Exponential Histogram for univmon
--------------------------------------------------------------------------------*/

func ExpoInitUniv(k int64, time_window_size int64) (ehu *ExpoHistogramUniv) {
	ehu = &ExpoHistogramUniv{
		k:                k,
		s_count:          0,
		time_window_size: time_window_size,
		univs:            make([]*UnivSketch, 0),
	}

	ehu.putch = make(chan int64, 100)

	ehu.cs_seed1 = make([]uint32, CS_ROW_NO_Univ_ELEPHANT)
	ehu.cs_seed2 = make([]uint32, CS_ROW_NO_Univ_ELEPHANT)
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < CS_ROW_NO_Univ_ELEPHANT; i++ {
		ehu.cs_seed1[i] = rand.Uint32()
		ehu.cs_seed2[i] = rand.Uint32()
	}
	ehu.seed3 = rand.Uint32()

	ehu.univPool = UnivSketchPool{pool: make([]*UnivSketch, UnivPoolCAP), size: 0, max_size: UnivPoolCAP}
	for i := uint32(0); i < UnivPoolCAP; i++ {
		ehu.univPool.pool[i], _ = NewUnivSketchPyramid(TOPK_SIZE, CS_ROW_NO_Univ_ELEPHANT, CS_COL_NO_Univ_ELEPHANT, CS_LVLS, ehu.cs_seed1, ehu.cs_seed2, ehu.seed3, int64(i))
	}
	ehu.univPool.bm = roaring.New()

	ehu.ctx, ehu.cancel = context.WithCancel(context.Background())
	ehu.StartBackgroundClean(ehu.ctx)
	ehu.StartBackgroundClean(ehu.ctx)

	return ehu
}

func (ehu *ExpoHistogramUniv) StartBackgroundClean(ctx context.Context) {
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			pool_idx, more := <-ehu.putch
			if more {
				ehu.putUnivSketch(pool_idx)
			} else {
				return
			}
		}
	}(ctx)
}

func (ehu *ExpoHistogramUniv) StopBackgroundClean() {
	close(ehu.putch)
	ehu.cancel()
}

func (ehu *ExpoHistogramUniv) putUnivSketch(pool_idx int64) {
	ehu.univPool.mutex.Lock()
	ehu.univPool.pool[pool_idx].Free()
	ehu.univPool.bm.Remove(uint32(pool_idx))
	atomic.AddUint32(&ehu.univPool.size, ^uint32(0))
	atomic.AddUint32(&ehu.univPool.toclean, ^uint32(0))
	ehu.univPool.mutex.Unlock()
}

func (ehu *ExpoHistogramUniv) GetUnivSketch() (*UnivSketch, error) {
	ehu.univPool.mutex.Lock()
	if ehu.univPool.size == ehu.univPool.max_size {
		tmp, err := NewUnivSketchPyramid(TOPK_SIZE, CS_ROW_NO_Univ_ELEPHANT, CS_COL_NO_Univ_ELEPHANT, CS_LVLS, ehu.cs_seed1, ehu.cs_seed2, ehu.seed3, int64(ehu.univPool.max_size)) // New && init UnivMon
		if err != nil {
			return nil, errors.New("univ sketch allocation failed")
		}

		ehu.univPool.pool = append(ehu.univPool.pool, tmp)
		ehu.univPool.bm.Add(ehu.univPool.max_size)
		atomic.AddUint32(&ehu.univPool.max_size, 1)
		atomic.AddUint32(&ehu.univPool.size, 1)
		ehu.univPool.mutex.Unlock()
		return tmp, nil
	}

	iter := ehu.univPool.bm.Iterator()

	idx := uint32(0)
	last := uint32(0)
	last = last - 1
	flag := false
	if ehu.univPool.bm.Contains(0) == false {
		flag = true
	} else {
		for iter.HasNext() {
			item := iter.Next()
			if item > 0 && last != item-1 {
				idx = item - 1
				flag = true
				break
			}
			last = item
		}

		if !flag {
			idx = last + 1
		}
	}

	// fmt.Println(idx)

	univ := ehu.univPool.pool[idx]
	ehu.univPool.bm.Add(idx)
	atomic.AddUint32(&ehu.univPool.size, 1)
	ehu.univPool.mutex.Unlock()
	return univ, nil
}

func (ehu *ExpoHistogramUniv) PutUnivSketch(u *UnivSketch) error {
	if u.pool_idx == -1 {
		return nil
	}
	// t_now := time.Now()
	// fmt.Println("put ", u.pool_idx)
	atomic.AddUint32(&ehu.univPool.toclean, 1)
	for {
		if len(ehu.putch) == cap(ehu.putch) {
			// channel is full
			continue
		} else {
			ehu.putch <- u.pool_idx
			break
		}
	}
	// since := time.Since(t_now)
	// fmt.Println("put time=", since)
	return nil
}

func (ehu *ExpoHistogramUniv) Update(time_ int64, fvalue float64) {

	value := strconv.FormatFloat(fvalue, 'f', -1, 64)

	ehu.mutex.Lock()
	removed := 0
	for i := 0; i < ehu.s_count; i++ {
		if ehu.univs[i].max_time < time_-ehu.time_window_size {
			removed++
		} else {
			break
		}
	}

	if removed > 0 {
		ehu.s_count = ehu.s_count - removed
		for i := 0; i < removed; i++ {
			ehu.PutUnivSketch(ehu.univs[i])
		}
		ehu.univs = ehu.univs[removed:]
	}

	// since := time.Since(t_now)
	// fmt.Println("ehuniv remove time=", since.Seconds())

	// t_now := time.Now()
	tmp, err := ehu.GetUnivSketch()
	if err != nil {
		fmt.Println("[Expo Univ] memory full, cannot allocate UnivSketch")
		return
	}

	// since := time.Since(t_now)
	// fmt.Println("ehuniv get univ time=", since)

	// t_now = time.Now()

	tmp.max_time, tmp.min_time = time_, time_
	ehu.univs = append(ehu.univs, tmp)
	hash := xxhash.ChecksumString64S(value, uint64(tmp.seed))
	bottom_layer_num := findBottomLayerNum(hash, CS_LVLS)

	pos, sign := ehu.univs[0].cs_layers[0].position_and_sign([]byte(value))

	ehu.univs[ehu.s_count].univmon_processing_optimized(value, 1, bottom_layer_num, &pos, &sign)
	ehu.s_count++

	// since = time.Since(t_now)
	// fmt.Println("ehuniv insertion time=", since)

	// t_now = time.Now()
	// Merge EH buckets
	same_size_bucket := 1
	for i := ehu.s_count - 2; i >= 0; i-- {
		if ehu.univs[i].bucket_size == ehu.univs[i+1].bucket_size {
			same_size_bucket += 1
		} else {
			if float64(same_size_bucket) >= float64(ehu.k)/2.0+2 {
				// t_now := time.Now()
				ehu.univs[i+1].MergeWith(ehu.univs[i+2])
				// since := time.Since(t_now)
				// fmt.Println("merge with time=", since)
				ehu.univs[i+1].bucket_size += ehu.univs[i+2].bucket_size
				ehu.univs[i+1].max_time = MaxInt64(ehu.univs[i+1].max_time, ehu.univs[i+2].max_time)
				ehu.univs[i+1].min_time = MinInt64(ehu.univs[i+1].min_time, ehu.univs[i+2].min_time)
				ehu.PutUnivSketch(ehu.univs[i+2])
				ehu.univs = append(ehu.univs[:i+2], ehu.univs[i+3:]...)
				ehu.s_count -= 1
				// since = time.Since(t_now)
				// fmt.Println("single merge time=", since)
			}
			same_size_bucket = 1
			if ehu.univs[i+1].bucket_size == ehu.univs[i].bucket_size {
				same_size_bucket += 1
			}
		}
	}

	// since = time.Since(t_now)
	// fmt.Println("ehuniv merge 1 time=", since)

	// t_now = time.Now()
	if float64(same_size_bucket) >= float64(ehu.k)/2.0+2 {
		ehu.univs[0].MergeWith(ehu.univs[1])
		ehu.univs[0].bucket_size += ehu.univs[1].bucket_size
		ehu.univs[0].max_time = MaxInt64(ehu.univs[0].max_time, ehu.univs[1].max_time)
		ehu.univs[0].min_time = MinInt64(ehu.univs[0].min_time, ehu.univs[1].min_time)
		ehu.PutUnivSketch(ehu.univs[1])
		ehu.univs = append(ehu.univs[:1], ehu.univs[2:]...)
		ehu.s_count -= 1
	}
	// since = time.Since(t_now)
	// fmt.Println("ehuniv merge 2 time=", since)
	ehu.mutex.Unlock()

}

func (eh *ExpoHistogramUniv) Cover(mint, maxt int64) bool {
	if eh.s_count == 0 {
		return false
	}
	return (eh.univs[eh.s_count-1].max_time-eh.time_window_size <= mint)
}

func (ehu *ExpoHistogramUniv) print_buckets() {
	fmt.Println("s_count =", ehu.s_count)
	fmt.Println("k =", ehu.k)
	for i := 0; i < ehu.s_count; i++ {
		fmt.Println(i, ehu.univs[i].bucket_size, ehu.univs[i].min_time, ehu.univs[i].max_time)
	}
}

func (eh *ExpoHistogramUniv) GetMemory() float64 {
	var total_mem float64 = 0
	for i := 0; i < eh.s_count; i++ {
		total_mem += eh.univs[i].GetMemoryKBPyramid()
	}
	return total_mem
}

func (ehu *ExpoHistogramUniv) QueryIntervalMergeUniv(t1, t2 int64, cur_t int64) (univ *UnivSketch, err error) {
	var from_bucket, to_bucket int = 0, 0
	ehu.mutex.Lock()

	for i := 0; i < ehu.s_count; i++ {
		fmt.Println(i, ehu.univs[i].min_time, ehu.univs[i].max_time, ehu.univs[i].bucket_size)
	}
	fmt.Println(" ")

	for i := 0; i < ehu.s_count; i++ {
		if t1 >= ehu.univs[i].min_time && t1 <= ehu.univs[i].max_time {
			from_bucket = i
		}
		if t2 >= ehu.univs[i].min_time && t2 <= ehu.univs[i].max_time {
			to_bucket = i
		}
	}

	if t2 > ehu.univs[ehu.s_count-1].max_time {
		to_bucket = ehu.s_count - 1
	}
	if t1 < ehu.univs[0].min_time {
		from_bucket = 0
	}

	if AbsInt64(t1-ehu.univs[from_bucket].min_time) > AbsInt64(t1-ehu.univs[from_bucket].max_time) {
		from_bucket += 1
	}

	fmt.Println("s_count =", ehu.s_count)
	fmt.Println("from_bucket =", from_bucket)
	fmt.Println("to_bucket =", to_bucket)

	if from_bucket < to_bucket {
		merged_univ, _ := NewUnivSketchPyramid(TOPK_SIZE, CS_ROW_NO_Univ_ELEPHANT, CS_COL_NO_Univ_ELEPHANT, CS_LVLS, ehu.cs_seed1, ehu.cs_seed2, ehu.seed3, -1)
		for i := from_bucket; i <= to_bucket; i++ {
			merged_univ.MergeWith(ehu.univs[i])
			merged_univ.bucket_size += ehu.univs[i].bucket_size
		}
		ehu.mutex.Unlock()
		return merged_univ, nil
	} else {
		ehu.mutex.Unlock()
		return ehu.univs[from_bucket], nil
	}
}
