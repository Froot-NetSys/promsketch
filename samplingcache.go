package promsketch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/zzylol/prometheus-sketch-VLDB/uniform-sampling-caching/prometheus/model/labels"
)

type SamplingCacheEntry struct {
	id   int
	lset labels.Labels
	us   *UniformSampling
}

type SamplingCacheHashMap struct {
	unique    map[uint64]*SamplingCacheEntry
	conflicts map[uint64][]*SamplingCacheEntry
}

type stripeLock struct {
	sync.RWMutex
	// Padding to avoid multiple locks being on the same cache line.
	_ [40]byte
}

type SamplingCacheSeries struct { // stripeSeries
	size   int
	id     int
	hashes []SamplingCacheHashMap
	series []map[int]*SamplingCacheEntry
	locks  []stripeLock
}

type SamplingCache struct {
	lastSeriesID atomic.Uint64
	numSeries    atomic.Uint64
	series       *SamplingCacheSeries
}

func (m *SamplingCacheHashMap) get(hash uint64, lset labels.Labels) *SamplingCacheEntry {
	if s, found := m.unique[hash]; found {
		if labels.Equal(s.lset, lset) {
			return s
		}
	}
	for _, s := range m.conflicts[hash] {
		if labels.Equal(s.lset, lset) {
			return s
		}
	}
	return nil
}

func (s *SamplingCacheEntry) Append(t int64, val float64) {
	s.us.Insert(t, val)
}

func (m *SamplingCacheHashMap) set(hash uint64, s *SamplingCacheEntry) {
	if existing, found := m.unique[hash]; !found || labels.Equal(existing.lset, s.lset) {
		m.unique[hash] = s
		return
	}
	if m.conflicts == nil {
		m.conflicts = make(map[uint64][]*SamplingCacheEntry)
	}
	l := m.conflicts[hash]
	for i, prev := range l {
		if labels.Equal(prev.lset, s.lset) {
			l[i] = s
			return
		}
	}
	m.conflicts[hash] = append(l, s)
}

func (m *SamplingCacheHashMap) del(hash uint64, id int) {
	var rem []*SamplingCacheEntry
	unique, found := m.unique[hash]
	switch {
	case !found: // Supplied hash is not stored.
		return
	case unique.id == id:
		conflicts := m.conflicts[hash]
		if len(conflicts) == 0 { // Exactly one series with this hash was stored
			delete(m.unique, hash)
			return
		}
		m.unique[hash] = conflicts[0] // First remaining series goes in 'unique'.
		rem = conflicts[1:]           // Keep the rest.
	default: // The series to delete is somewhere in 'conflicts'. Keep all the ones that don't match.
		for _, s := range m.conflicts[hash] {
			if s.id != id {
				rem = append(rem, s)
			}
		}
	}
	if len(rem) == 0 {
		delete(m.conflicts, hash)
	} else {
		m.conflicts[hash] = rem
	}
}

func (s *SamplingCacheSeries) getByID(id int) *SamplingCacheEntry {
	if s.size == 0 {
		return nil
	}
	i := uint64(id) & uint64(s.size-1)

	s.locks[i].RLock()
	series := s.series[i][id]
	s.locks[i].RUnlock()

	return series
}

func (s *SamplingCacheSeries) getByHash(hash uint64, lset labels.Labels) *SamplingCacheEntry {
	if s.size == 0 {
		return nil
	}
	i := hash & uint64(s.size-1)
	s.locks[i].RLock()
	series := s.hashes[i].get(hash, lset)
	s.locks[i].RUnlock()

	return series
}

func newSamplingCacheEntry(lset labels.Labels, id int, max_time_window int64, sampling_rate float64, max_size int) *SamplingCacheEntry {
	s := &SamplingCacheEntry{
		lset: lset,
		id:   id,
		us:   NewUniformSampling(max_time_window, sampling_rate, max_size),
	}
	return s
}

func (sc *SamplingCache) getOrCreate(hash uint64, lset labels.Labels, sampling_rate float64, time_window_size int64, item_window_size int) (*SamplingCacheEntry, bool, error) {
	s := sc.series.getByHash(hash, lset)
	if s != nil {
		return s, false, nil
	}

	sc.series.id = sc.series.id + 1
	id := sc.series.id

	series := newSamplingCacheEntry(lset, id, time_window_size, sampling_rate, int(sampling_rate*float64(item_window_size)))

	i := hash & uint64(sc.series.size-1)
	sc.series.locks[i].Lock()
	sc.series.hashes[i].set(hash, series)
	sc.series.locks[i].Unlock()

	i = uint64(id) & uint64(sc.series.size-1)
	sc.series.locks[i].Lock()
	sc.series.series[i][id] = series
	sc.series.locks[i].Unlock()

	return series, true, nil
}

func (sc *SamplingCache) NewSamplingCacheEntry(lset labels.Labels, sampling_rate float64, time_window_size int64, item_window_size int) error {
	_, _, err := sc.getOrCreate(lset.Hash(), lset, sampling_rate, time_window_size, item_window_size)
	return err
}

func NewSamplingCacheSeries(stripeSize int) *SamplingCacheSeries {
	ss := &SamplingCacheSeries{ // TODO: use stripeSeries to reduce lock contention later
		size:   stripeSize,
		id:     0,
		hashes: make([]SamplingCacheHashMap, stripeSize),
		series: make([]map[int]*SamplingCacheEntry, stripeSize),
		locks:  make([]stripeLock, stripeSize),
	}

	for i := range ss.series {
		ss.series[i] = map[int]*SamplingCacheEntry{}
	}
	for i := range ss.hashes {
		ss.hashes[i] = SamplingCacheHashMap{
			unique:    map[uint64]*SamplingCacheEntry{},
			conflicts: nil,
		}
	}
	return ss
}

func NewSamplingCache() *SamplingCache {
	ss := NewSamplingCacheSeries(DefaultStripeSize)
	sc := &SamplingCache{
		series: ss,
	}
	sc.lastSeriesID.Store(0)
	sc.numSeries.Store(0)
	return sc
}

func (sc *SamplingCache) Insert(lset labels.Labels, t int64, val float64) error {
	s := sc.series.getByHash(lset.Hash(), lset)
	if s == nil {
		// return errors.New("SamplingCacheEntry not found")
		return nil
	}
	s.Append(t, val)
	return nil
}

func (sc *SamplingCache) Select(lset labels.Labels, t1 int64, t2 int64) ([]float64, error) {
	s := sc.series.getByHash(lset.Hash(), lset)
	if s == nil {
		return nil, errors.New("ts not cached")
	}
	values := make([]float64, 0)

	for i := 0; i < len(s.us.Arr); i++ {
		if s.us.Arr[i].T >= t1 && s.us.Arr[i].T <= t2 {
			values = append(values, s.us.Arr[i].F)
		}
		if s.us.Arr[i].T > t2 {
			break
		}
	}
	return values, nil
}

func (sc *SamplingCache) LookUp(funcName string, lset labels.Labels, mint, maxt int64) bool {
	if _, ok := SamplingFunctionCalls[funcName]; !ok {
		return false
	}
	s := sc.series.getByHash(lset.Hash(), lset)
	if s == nil {
		return false
	}
	return true
}

func (sc *SamplingCache) Eval(funcName string, lset labels.Labels, otherArgs float64, mint, maxt int64) (Vector, error) {
	sfunc := SamplingFunctionCalls[funcName]
	values, err := sc.Select(lset, mint, maxt)
	if err != nil {
		return Vector{}, err
	} else {
		if funcName == "sum_over_time" || funcName == "sum2_over_time" || funcName == "count_over_time" {
			s := sc.series.getByHash(lset.Hash(), lset)
			otherArgs = s.us.Sampling_rate
		}
		return sfunc(context.TODO(), values, otherArgs), nil
	}
}
