package promsketch

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/OneOfOne/xxhash"
	"github.com/RoaringBitmap/roaring/v2"
)

type EHMap struct {
	min_idx     int
	max_idx     int
	max_time    int64
	min_time    int64
	bucket_size int
	l22         float64
	m           map[float64]int64
}

func NewMap() *EHMap {
	return &EHMap{
		min_idx:     0,
		max_idx:     0,
		max_time:    0,
		min_time:    0,
		bucket_size: 0,
		l22:         0,
		m:           make(map[float64]int64),
	}
}

func MergeMaps(m1 *EHMap, m2 *EHMap) {
	for k, v := range m2.m {
		if _, ok := (m1.m)[k]; !ok {
			m1.m[k] = v
		} else {
			m1.m[k] += v
		}
	}
}

type ExpoHistogramUnivOptimized struct {
	cs_seed1         []uint32
	cs_seed2         []uint32
	seed3            uint32
	s_count          int           // sketch count
	map_count        int           // array count
	univs            []*UnivSketch // larger bucket is a univsketch
	map_buckets      []*EHMap      // smaller bucket is exact, storing all samples
	max_map_size     int
	min_map_size     int
	k                int64
	time_window_size int64
	univPool         UnivSketchPool
	putch            chan int64

	ctx    context.Context
	cancel func()       // Cancellation function for background ehuniv cleaning.
	mutex  sync.RWMutex // when updating s_count and buckets, query should wait; when query, update() should wait; but multiple queries can read simultaneously
}

/*------------------------------------------------------------------------------
			Exponential Histogram for univmon
--------------------------------------------------------------------------------*/

func ExpoInitUnivOptimized(k int64, time_window_size int64) (ehu *ExpoHistogramUnivOptimized) {
	ehu = &ExpoHistogramUnivOptimized{
		k:                k,
		s_count:          0,
		map_count:        0,
		max_map_size:     EHUniv_MAX_MAP_SIZE,
		min_map_size:     1,
		time_window_size: time_window_size,
		univs:            make([]*UnivSketch, 0),
		map_buckets:      make([]*EHMap, 0),
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
		ehu.univPool.pool[i], _ = NewUnivSketchPyramid(TOPK_SIZE, CS_ROW_NO_Univ_ELEPHANT, CS_COL_NO_Univ_ELEPHANT, CS_LVLS, ehu.cs_seed1, ehu.cs_seed2, ehu.seed3, -1) // int64(i))
	}
	ehu.univPool.bm = roaring.New()

	ehu.ctx, ehu.cancel = context.WithCancel(context.Background())
	// ehu.StartBackgroundClean(ehu.ctx)

	return ehu
}

func (ehu *ExpoHistogramUnivOptimized) UpdateWindow(window int64) {
	ehu.mutex.Lock()
	ehu.time_window_size = window
	ehu.mutex.Unlock()
}

func (ehu *ExpoHistogramUnivOptimized) StartBackgroundClean(ctx context.Context) {
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

func (ehu *ExpoHistogramUnivOptimized) StopBackgroundClean() {
	close(ehu.putch)
	ehu.cancel()
}

func (ehu *ExpoHistogramUnivOptimized) putUnivSketch(pool_idx int64) {
	ehu.univPool.mutex.Lock()
	ehu.univPool.pool[pool_idx].Free()
	ehu.univPool.bm.Remove(uint32(pool_idx))
	atomic.AddUint32(&ehu.univPool.size, ^uint32(0))
	atomic.AddUint32(&ehu.univPool.toclean, ^uint32(0))
	ehu.univPool.mutex.Unlock()
}

func (ehu *ExpoHistogramUnivOptimized) GetUnivSketch() (*UnivSketch, error) {
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

func (ehu *ExpoHistogramUnivOptimized) PutUnivSketch(u *UnivSketch) error {
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

func (ehu *ExpoHistogramUnivOptimized) calc_l22(eh_arr_bucket *EHMap) float64 {
	var l2 float64 = 0
	for _, v := range eh_arr_bucket.m {
		l2 += float64(v * v)
	}
	return l2
}

func (ehu *ExpoHistogramUnivOptimized) Update(time_ int64, fvalue float64) {

	// ehu.print_buckets()

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

	removed = 0
	for i := 0; i < ehu.map_count; i++ {
		if ehu.map_buckets[i].max_time < time_-ehu.time_window_size {
			removed++
		} else {
			break
		}
	}

	if removed > 0 {

		for l := removed; l < ehu.map_count; l++ {
			ehu.map_buckets[l].min_idx -= ehu.map_buckets[removed-1].max_idx - 1
			ehu.map_buckets[l].max_idx -= ehu.map_buckets[removed-1].max_idx - 1
		}

		ehu.map_count = ehu.map_count - removed
		ehu.map_buckets = ehu.map_buckets[removed:]
	}

	// add value to new EH bucket (array)
	if ehu.map_count > 0 && ehu.map_buckets[ehu.map_count-1].bucket_size < ehu.min_map_size {
		if v, ok := ehu.map_buckets[ehu.map_count-1].m[fvalue]; !ok {
			ehu.map_buckets[ehu.map_count-1].m[fvalue] = 1
			ehu.map_buckets[ehu.map_count-1].l22 += 1
		} else {
			ehu.map_buckets[ehu.map_count-1].m[fvalue] += 1
			ehu.map_buckets[ehu.map_count-1].l22 += 2*float64(v) + 1 // incremental update
		}

		ehu.map_buckets[ehu.map_count-1].max_time = time_
		ehu.map_buckets[ehu.map_count-1].bucket_size += 1

	} else {
		tmp_map := NewMap()
		tmp_map.max_time, tmp_map.min_time = time_, time_
		tmp_map.bucket_size = 1
		tmp_map.l22 = 1
		tmp_map.m[fvalue] = 1
		ehu.map_buckets = append(ehu.map_buckets, tmp_map)
		ehu.map_count++
	}
	// fmt.Println(ehu.map_buckets[0].bucket_size, ehu.map_buckets[0].min_idx, ehu.map_buckets[0].max_idx)

	// Merge EH buckets (maps)
	sum_l22 := float64(0)
	for i := ehu.map_count - 2; i >= 0; i-- {
		if ehu.map_buckets[i].l22+ehu.map_buckets[i+1].l22 <= 1/float64(ehu.k)*sum_l22 {
			ehu.map_buckets[i].bucket_size += ehu.map_buckets[i+1].bucket_size
			ehu.map_buckets[i].max_time = ehu.map_buckets[i+1].max_time
			MergeMaps(ehu.map_buckets[i], ehu.map_buckets[i+1])
			ehu.map_buckets[i+1] = nil
			ehu.map_buckets = append(ehu.map_buckets[:i+1], ehu.map_buckets[i+2:]...)
			ehu.map_count -= 1
			ehu.map_buckets[i].l22 = ehu.calc_l22(ehu.map_buckets[i])
		} else {
			sum_l22 += ehu.map_buckets[i+1].l22
		}
	}

	if 2*len(ehu.map_buckets[0].m) >= ehu.max_map_size {
		// only under this, we may need to merge univmons

		// change oldest array into univmon

		// tmp, err := ehu.GetUnivSketch()

		tmp, err := NewUnivSketchPyramid(TOPK_SIZE, CS_ROW_NO_Univ_ELEPHANT, CS_COL_NO_Univ_ELEPHANT, CS_LVLS, ehu.cs_seed1, ehu.cs_seed2, ehu.seed3, -1)
		if err != nil {
			fmt.Println("[Expo Univ] memory full, cannot allocate UnivSketch")
			return
		}

		tmp.max_time, tmp.min_time = ehu.map_buckets[0].max_time, ehu.map_buckets[0].min_time
		ehu.univs = append(ehu.univs, tmp)

		for k, v := range ehu.map_buckets[0].m {
			value := strconv.FormatFloat(k, 'f', -1, 64)
			hash := xxhash.ChecksumString64S(value, uint64(tmp.seed))
			bottom_layer_num := findBottomLayerNum(hash, CS_LVLS)
			pos, sign := ehu.univs[0].cs_layers[0].position_and_sign([]byte(value))
			ehu.univs[ehu.s_count].univmon_processing_optimized(value, v, bottom_layer_num, &pos, &sign)
		}

		ehu.s_count++

		ehu.map_buckets[0] = nil
		ehu.map_buckets = ehu.map_buckets[1:]
		ehu.map_count -= 1

		// Merge EH buckets (univmon)
		for i := ehu.s_count - 2; i >= 0; i-- {
			if ehu.univs[i].cs_layers[0].cs_l22()+ehu.univs[i+1].cs_layers[0].cs_l22() <= 1/float64(ehu.k)*sum_l22 {
				ehu.univs[i].MergeWith(ehu.univs[i+1])
				ehu.univs[i].bucket_size += ehu.univs[i+1].bucket_size
				ehu.univs[i].max_time = MaxInt64(ehu.univs[i].max_time, ehu.univs[i+1].max_time)
				ehu.univs[i].min_time = MinInt64(ehu.univs[i].min_time, ehu.univs[i+1].min_time)
				ehu.PutUnivSketch(ehu.univs[i+1])
				ehu.univs[i+1] = nil
				ehu.univs = append(ehu.univs[:i+1], ehu.univs[i+2:]...)
				ehu.s_count -= 1
			} else {
				sum_l22 += ehu.univs[i+1].cs_layers[0].cs_l22()
			}
		}
	}

	ehu.mutex.Unlock()

}

func (eh *ExpoHistogramUnivOptimized) Cover(mint, maxt int64) bool {
	eh.mutex.RLock()
	if eh.s_count+eh.map_count == 0 {
		eh.mutex.RUnlock()
		return false
	}

	mint_time := eh.GetMinTime()
	// fmt.Println("EHoptimized Cover:", mint, maxt, mint_time, eh.array[eh.map_count-1].max_time)
	maxt_covered := (eh.map_buckets[eh.map_count-1].max_time >= maxt)
	mint_covered := (mint_time <= mint)
	isCovered := mint_covered && maxt_covered
	eh.mutex.RUnlock()
	return isCovered
}

func (eh *ExpoHistogramUnivOptimized) GetMinTime() int64 {
	if eh.s_count+eh.map_count == 0 {
		return -1
	}
	if eh.s_count > 0 {
		return eh.univs[0].min_time
	} else {
		return eh.map_buckets[0].min_time
	}
}

func (eh *ExpoHistogramUnivOptimized) GetMaxTime() int64 {
	if eh.s_count+eh.map_count == 0 {
		return -1
	}
	return eh.map_buckets[eh.map_count-1].max_time
}

func (ehu *ExpoHistogramUnivOptimized) print_buckets() {
	fmt.Println("k =", ehu.k)
	fmt.Println("s_count =", ehu.s_count)
	for i := 0; i < ehu.s_count; i++ {
		fmt.Println(i, ehu.univs[i].min_time, ehu.univs[i].max_time, ehu.univs[i].bucket_size)
	}
	fmt.Println("map_count =", ehu.map_count)
	for i := 0; i < ehu.map_count; i++ {
		fmt.Println(i, ehu.map_buckets[i].min_time, ehu.map_buckets[i].max_time, ehu.map_buckets[i].bucket_size)
	}
}

func (eh *ExpoHistogramUnivOptimized) GetMemoryKB() float64 {
	var total_mem float64 = 0
	for i := 0; i < eh.s_count; i++ {
		total_mem += eh.univs[i].GetMemoryKBPyramid()
	}

	for i := 0; i < eh.map_count; i++ {
		total_mem += float64(32+32+64+64+32+64) / 8 / 1024
		total_mem += float64(len(eh.map_buckets[i].m)) * 16 / 1024
		total_mem += float64(unsafe.Sizeof(eh.map_buckets[i].m)) / 1024
		// fmt.Println(i, float64(len(eh.map_buckets[i].m))*16/1024, len(eh.map_buckets[i].m))
	}
	// fmt.Println("k=", eh.k)
	// fmt.Println("s_count=", eh.s_count)
	// fmt.Println("map=", eh.map_count, total_mem)

	return total_mem
}

func (eh *ExpoHistogramUnivOptimized) GetTotalBucketSizes() int64 {
	var total_bucket_size int64 = 0
	for i := 0; i < eh.s_count; i++ {
		total_bucket_size += eh.univs[i].bucket_size
	}
	for i := 0; i < eh.map_count; i++ {
		total_bucket_size += int64((eh.map_buckets[i].bucket_size))
	}
	return total_bucket_size
}

func (ehu *ExpoHistogramUnivOptimized) QueryIntervalMergeUniv(t1, t2 int64, cur_t int64) (univ *UnivSketch, m *map[float64]int64, n float64, err error) {
	var from_bucket, to_bucket int = 0, 0
	ehu.mutex.RLock()

	// ehu.print_buckets()
	// fmt.Println(" ")

	for i := 0; i <= ehu.s_count-1; i++ {
		if t1 >= ehu.univs[i].min_time && t1 <= ehu.univs[i].max_time {
			from_bucket = i
			break
		}
	}

	for i := 0; i <= ehu.s_count-1; i++ {
		if t2 >= ehu.univs[i].min_time && t2 <= ehu.univs[i].max_time {
			to_bucket = i
			break
		}
	}

	for i := 0; i < ehu.map_count; i++ {
		if t1 >= ehu.map_buckets[i].min_time && t1 <= ehu.map_buckets[i].max_time {
			from_bucket = i + ehu.s_count
			break
		}
	}

	for i := 0; i < ehu.map_count; i++ {
		if t2 >= ehu.map_buckets[i].min_time && t2 <= ehu.map_buckets[i].max_time {
			to_bucket = i + ehu.s_count
			break
		}
	}

	if t2 > ehu.map_buckets[ehu.map_count-1].max_time {
		to_bucket = ehu.map_count - 1 + ehu.s_count
	}
	if ehu.s_count > 0 && t1 < ehu.univs[0].min_time {
		from_bucket = 0
	}

	if from_bucket < ehu.s_count {
		if AbsInt64(t1-ehu.univs[from_bucket].min_time) > AbsInt64(t1-ehu.univs[from_bucket].max_time) {
			from_bucket += 1
		}
	} else {
		if AbsInt64(t1-ehu.map_buckets[from_bucket-ehu.s_count].min_time) > AbsInt64(t1-ehu.map_buckets[from_bucket-ehu.s_count].max_time) {
			from_bucket += 1
		}
	}

	// fmt.Println("s_count =", ehu.s_count, "map_count =", ehu.map_count, "total =", ehu.s_count+ehu.map_count)
	// fmt.Println("from_bucket =", from_bucket)
	// fmt.Println("to_bucket =", to_bucket)

	if to_bucket < ehu.s_count {
		// only in sketch part
		if from_bucket < to_bucket {
			merged_univ, _ := NewUnivSketchPyramid(TOPK_SIZE, CS_ROW_NO_Univ_ELEPHANT, CS_COL_NO_Univ_ELEPHANT, CS_LVLS, ehu.cs_seed1, ehu.cs_seed2, ehu.seed3, -1)
			for i := from_bucket; i <= to_bucket; i++ {
				merged_univ.MergeWith(ehu.univs[i])
				merged_univ.bucket_size += ehu.univs[i].bucket_size
			}
			ehu.mutex.RUnlock()
			return merged_univ, nil, 0, nil
		} else {
			ehu.mutex.RUnlock()
			return ehu.univs[from_bucket], nil, 0, nil
		}
	} else if from_bucket >= ehu.s_count {
		// only in map part
		m := make(map[float64]int64)
		total_item := float64(0)
		for i := from_bucket - ehu.s_count; i <= to_bucket-ehu.s_count; i++ {
			for k, v := range ehu.map_buckets[i].m {
				if _, ok := m[k]; !ok {
					m[k] = v
				} else {
					m[k] += v
				}
			}
			total_item += float64(ehu.map_buckets[i].bucket_size)
		}

		ehu.mutex.RUnlock()
		return nil, &m, total_item, nil
	} else {
		// merge univ and array
		merged_univ, _ := NewUnivSketchPyramid(TOPK_SIZE, CS_ROW_NO_Univ_ELEPHANT, CS_COL_NO_Univ_ELEPHANT, CS_LVLS, ehu.cs_seed1, ehu.cs_seed2, ehu.seed3, -1)
		for i := from_bucket; i < ehu.s_count; i++ {
			merged_univ.MergeWith(ehu.univs[i])
			merged_univ.bucket_size += ehu.univs[i].bucket_size
		}

		tmp := make(map[float64]int64)
		for i := 0; i < ehu.map_count; i++ {
			for k, v := range ehu.map_buckets[i].m {
				if _, ok := tmp[k]; !ok {
					tmp[k] = v
				} else {
					tmp[k] += v
				}
			}
		}

		for k, v := range tmp {
			value := strconv.FormatFloat(k, 'f', -1, 64)
			hash := xxhash.ChecksumString64S(value, uint64(merged_univ.seed))
			bottom_layer_num := findBottomLayerNum(hash, CS_LVLS)
			pos, sign := ehu.univs[0].cs_layers[0].position_and_sign([]byte(value))
			merged_univ.univmon_processing_optimized(value, v, bottom_layer_num, &pos, &sign)
		}
		ehu.mutex.RUnlock()
		return merged_univ, nil, 0, nil
	}
}
