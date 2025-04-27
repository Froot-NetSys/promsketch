package promsketch

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"
)

const TOPK_QUERY int = 500

func query_topk_array(values []float64) *TopKHeap {
	topk := NewTopKHeap(TOPK_QUERY)

	m := make(map[string]int64)
	for i := 0; i < len(values); i++ {
		if _, ok := m[strconv.FormatFloat(values[i], 'f', -1, 64)]; !ok {
			m[strconv.FormatFloat(values[i], 'f', -1, 64)] = 1
		} else {
			m[strconv.FormatFloat(values[i], 'f', -1, 64)] += 1
		}
	}

	type kv struct {
		key   string
		value int64
	}
	var ss []kv
	for k, v := range m {
		ss = append(ss, kv{k, v})
	}
	sort.Slice(ss, func(i, j int) bool {
		return ss[i].value > ss[j].value
	})

	for i, kv := range ss {
		if i < TOPK_QUERY {
			topk.Update(kv.key, kv.value)
		}
	}
	return topk
}

func query_topk_map(m *map[float64]int64) *TopKHeap {
	topk := NewTopKHeap(TOPK_QUERY)
	for k, v := range *m {
		topk.Update(strconv.FormatFloat(k, 'f', -1, 64), v)
	}
	return topk
}

// go test -v -timeout 0 -run ^TestExpoHistogramUnivMonOptimizedTopK$ github.com/zzylol/promsketch -dataset=CAIDA2019
func TestExpoHistogramUnivMonOptimizedTopK(t *testing.T) {

	// query_window_size_input := []int64{1000000, 100000, 10000}
	query_window_size_input := []int64{1000000}
	total_length := int64(2000000)
	var dataset_name string = "caida2018"
	switch ds := dataset; ds {
	case "CAIDA":
		readCAIDA()
	case "CAIDA2018":
		readProcessedCAIDA2018()
		dataset_name = "caida2018"
	case "CAIDA2019":
		readProcessedCAIDA2019()
		dataset_name = "caida2019"
	case "Zipf":
		readZipf()
		dataset_name = "zipf"
	case "Dynamic":
		readDynamic()
		dataset_name = "dynamic"
	case "Uniform":
		readUniform()
		dataset_name = "uniform"
	}

	for _, query_window_size := range query_window_size_input {
		cost_query_interval_gsum := int64(query_window_size / 10)
		// Create a scenario
		t1 := make([]int64, 0)
		t2 := make([]int64, 0)
		t1 = append(t1, int64(0))
		t2 = append(t2, query_window_size-1)

		// suffix length
		for i := int64(query_window_size / 10); i < int64(query_window_size); i += query_window_size / 10 {
			t1 = append(t1, query_window_size-i)
			t2 = append(t2, query_window_size-1)
		}

		// fmt.Println("t1:", t1)
		// fmt.Println("t2:", t2)

		fmt.Println("Finished reading input timeseries.")

		for test_case := 0; test_case < 1; test_case += 1 {
			// "ehuniv_cost_analysis_l2/"
			filename := "ehuniv_l2_parameter_analysis/" + dataset_name + "_2M_univconfig1_topk_ehuniv_optimized_cost_" + strconv.Itoa(int(query_window_size)) + "_" + strconv.Itoa(test_case) + ".txt"
			fmt.Println(filename)
			f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
			if err != nil {
				panic(err)
			}
			defer f.Close()
			w := bufio.NewWriter(f)

			fmt.Fprintln(w, "ELEPHANT_LAYER:", ELEPHANT_LAYER)
			fmt.Fprintln(w, "MICE_LAYER:", MICE_LAYER)
			fmt.Fprintln(w, "CS_LVLS:", CS_LVLS)
			fmt.Fprintln(w, "CS_ROW_NO_Univ_ELEPHANT:", CS_ROW_NO_Univ_ELEPHANT)
			fmt.Fprintln(w, "CS_COL_NO_Univ_ELEPHANT:", CS_COL_NO_Univ_ELEPHANT)
			fmt.Fprintln(w, "CS_ROW_NO_Univ_MICE:", CS_ROW_NO_Univ_MICE)
			fmt.Fprintln(w, "CS_COL_NO_Univ_MICE:", CS_COL_NO_Univ_MICE)
			fmt.Fprintln(w, "EHUniv_MAX_MAP_SIZE:", EHUniv_MAX_MAP_SIZE)

			fmt.Fprintln(w, "t1:", t1)
			fmt.Fprintln(w, "t2:", t2)
			w.Flush()

			// PromSketch, EHUniv
			k_input := []int64{2, 4, 6, 8, 10, 12, 16, 20, 30, 40, 100, 200, 500}
			// k_input := []int64{10}
			for _, k := range k_input {
				// fmt.Println("EHUnivOptimized", k)
				fmt.Fprintln(w, "EHUnivOptimized", k)

				ehu := ExpoInitUnivOptimized(k, query_window_size)

				total_gt_query_compute := 0.0
				total_total_query := 0.0

				total_compute := 0.0
				insert_compute := 0.0
				total_query := make([]int, len(t1))
				gt_query_time := make([]float64, len(t1))
				query_time := make([]float64, len(t1))
				total_distinct_err := make([]float64, len(t1))
				total_recall := make([]float64, len(t1))
				total_are := make([]float64, len(t1))
				total_l1_err := make([]float64, len(t1))
				total_l2_err := make([]float64, len(t1))
				total_entropy_err := make([]float64, len(t1))
				total_distinct_err2 := make([]float64, len(t1))
				total_l1_err2 := make([]float64, len(t1))
				total_l2_err2 := make([]float64, len(t1))
				total_entropy_err2 := make([]float64, len(t1))
				sampling_query_time := make([]float64, len(t1))
				total_sampling_distinct_err := make([]float64, len(t1))
				total_sampling_l1_err := make([]float64, len(t1))
				total_sampling_l2_err := make([]float64, len(t1))
				total_sampling_entropy_err := make([]float64, len(t1))
				total_sampling_distinct_err2 := make([]float64, len(t1))
				total_sampling_l1_err2 := make([]float64, len(t1))
				total_sampling_l2_err2 := make([]float64, len(t1))
				total_sampling_entropy_err2 := make([]float64, len(t1))
				for j := 0; j < len(t1); j++ {
					total_query[j] = 0
					total_recall[j] = 0
					total_are[j] = 0
					total_distinct_err[j] = 0
					total_l1_err[j] = 0
					total_l2_err[j] = 0
					total_entropy_err[j] = 0
					total_distinct_err2[j] = 0
					total_l1_err2[j] = 0
					total_l2_err2[j] = 0
					total_entropy_err2[j] = 0
					query_time[j] = 0
					gt_query_time[j] = 0
					sampling_query_time[j] = 0
					total_sampling_distinct_err[j] = 0
					total_sampling_l1_err[j] = 0
					total_sampling_l2_err[j] = 0
					total_sampling_entropy_err[j] = 0
					total_sampling_distinct_err2[j] = 0
					total_sampling_l1_err2[j] = 0
					total_sampling_l2_err2[j] = 0
					total_sampling_entropy_err2[j] = 0
				}

				for t := int64(0); t < total_length; t++ {
					start := time.Now()
					ehu.Update(t, cases[0].vec[t].F)
					elapsed := time.Since(start)
					insert_compute += float64(elapsed.Microseconds())

					if t == total_length-1 || (t >= query_window_size-1 && (t+1)%cost_query_interval_gsum == 0) {
						for j := range len(t1) {
							total_query[j] += 1
							total_total_query += 1
							start_t := t1[j] + t - query_window_size + 1
							end_t := t2[j] + t - query_window_size + 1

							// fmt.Println("t, start_t, end_t:", t, start_t, end_t)

							start := time.Now()
							merged_univ, m, _, _ := ehu.QueryIntervalMergeUniv(start_t, end_t, t)
							topk := NewTopKHeap(TOPK_QUERY)
							if merged_univ != nil && m == nil {
								topk = merged_univ.QueryTopK(TOPK_QUERY)
							} else if m != nil && merged_univ == nil {
								topk = query_topk_map(m)
							} else {
								fmt.Println("query error")
							}

							elapsed := time.Since(start)
							total_compute += float64(elapsed.Microseconds())
							query_time[j] += float64(elapsed.Microseconds())

							// fmt.Println("sketch estimate:", distinct, l1, entropy, l2)

							// fmt.Fprintln(w, t, j, distinct, l1, entropy, l2)

							start = time.Now()
							values := make([]float64, 0)
							for tt := start_t; tt <= end_t; tt++ {
								values = append(values, float64(cases[0].vec[tt].F))
							}
							gt_topk := query_topk_array(values)
							elapsed = time.Since(start)
							gt_query_time[j] += float64(elapsed.Microseconds()) * 4
							total_gt_query_compute += float64(elapsed.Microseconds()) * 4
							// fmt.Println("true:", gt_distinct, gt_l1, gt_entropy, gt_l2)

							// fmt.Println(gt_topk.heap)
							// fmt.Println(topk.heap)

							var same int = 0
							var are float64 = 0
							for _, gt_item := range gt_topk.heap {
								for _, item := range topk.heap {
									if gt_item.key == item.key {
										same += 1
										are += float64(AbsInt64(gt_item.count-item.count)) / float64(gt_item.count)
										break
									}
								}
							}
							// fmt.Println("same=", same, TOPK_QUERY, len(topk.heap), len(gt_topk.heap))
							recall := float64(same) / float64(TOPK_QUERY)
							are = are / float64(same)
							total_recall[j] += recall
							total_are[j] += are

							w.Flush()

						}
					}
				}
				// fmt.Fprintln(w,"distinct error:", ehu_distinct_error)
				// fmt.Fprintln(w,"l1 error:", ehu_l1_error)
				// fmt.Fprintln(w,"entropy error:", ehu_entropy_error)
				// fmt.Fprintln(w,"l2 error:", ehu_l2_error)

				fmt.Println("sketch insert compute/item:", insert_compute/float64(total_length), "us")
				fmt.Println("sketch query compute/query:", total_compute/total_total_query, "us")
				fmt.Println("exact baseline query compute/query:", total_gt_query_compute/total_total_query, "us")
				fmt.Println("total compute:", total_compute+insert_compute, "us")
				fmt.Println("memory:", ehu.GetMemoryKB()/2, "KB")
				fmt.Println("exact baseline memory:", query_window_size*8/1024, "KB")

				for j := 0; j < len(t1); j++ {
					// fmt.Println("sketch window size=", t2[j]-t1[j]+1, "avg err:", total_distinct_err[j]/float64(total_query[j]), total_l1_err[j]/float64(total_query[j]), total_entropy_err[j]/float64(total_query[j]), total_l2_err[j]/float64(total_query[j]))
					fmt.Fprintln(w, "sketch window size err=", t2[j]-t1[j]+1, "are:", total_are[j]/float64(total_query[j]), "recall:", total_recall[j]/float64(total_query[j]))
				}

				for j := 0; j < len(t1); j++ {
					fmt.Fprintln(w, "sketch estimate query time=", query_time[j]/float64(total_query[j]), "us", "gt query time=", gt_query_time[j]/float64(total_query[j]), "window size=", t2[j]-t1[j]+1)
				}

				w.Flush()

				fmt.Fprintln(w, "sketch insert compute/item:", insert_compute/float64(total_length), "us")
				fmt.Fprintln(w, "insert throughput:", float64(total_length)/insert_compute, "Mops")
				fmt.Fprintln(w, "sketch query compute/query:", total_compute/total_total_query, "us")
				fmt.Fprintln(w, "exact baseline query compute/query:", total_gt_query_compute/total_total_query, "us")
				fmt.Fprintln(w, "sketch total compute:", total_compute+insert_compute, "us")
				fmt.Fprintln(w, "sketch memory:", ehu.GetMemoryKB()/2, "KB")
				fmt.Fprintln(w, "ehu sketch num:", ehu.s_count, "map num:", ehu.map_count)
				fmt.Fprintln(w, "exact baseline memory:", query_window_size*8/1024, "KB")
				w.Flush()
			}
		}
	}
}
