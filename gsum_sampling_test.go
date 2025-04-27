package promsketch

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"testing"
	"time"
)

// Test cost (compute + memory) and accuracy under sliding window
// Example command:
//
// go test -v -timeout 0 -run ^TestSamplingGSum$ github.com/zzylol/promsketch -dataset=CAIDA2019
// go test -v -timeout 0 -run ^TestSamplingGSum$ github.com/zzylol/promsketch -dataset=CAIDA2018
// go test -v -timeout 0 -run ^TestSamplingGSum$ github.com/zzylol/promsketch -dataset=Uniform
// go test -v -timeout 0 -run ^TestSamplingGSum$ github.com/zzylol/promsketch -dataset=Zipf
func TestSamplingGSum(t *testing.T) {

	// query_window_size_input := []int64{1000000, 100000, 10000}
	query_window_size_input := []int64{1000000}
	total_length := int64(20000000)
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

		t1 = append(t1, int64(query_window_size/3))
		t2 = append(t2, int64(query_window_size/3)*2)

		// suffix length
		for i := int64(query_window_size / 10); i < int64(query_window_size); i += query_window_size / 100 {
			t1 = append(t1, query_window_size-i)
			t2 = append(t2, query_window_size-1)
		}

		// fmt.Println("t1:", t1)
		// fmt.Println("t2:", t2)

		fmt.Println("Finished reading input timeseries.")

		for test_case := 0; test_case < 1; test_case += 1 {
			// "ehuniv_cost_analysis_l2/"
			filename := "ehuniv_cost_analysis_l2/" + dataset_name + "_20M_gsum_sampling_optimized_cost_" + strconv.Itoa(int(query_window_size)) + "_" + strconv.Itoa(test_case) + ".txt"
			fmt.Println(filename)
			f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
			if err != nil {
				panic(err)
			}
			defer f.Close()
			w := bufio.NewWriter(f)

			fmt.Fprintln(w, "t1:", t1)
			fmt.Fprintln(w, "t2:", t2)
			w.Flush()

			// PromSketch, EHUniv
			sampling_ratios := []float64{0.1, 0.2, 0.3, 0.4, 0.5}
			for _, rate := range sampling_ratios {
				fmt.Fprintln(w, "Sampling", rate)

				sampler := NewUniformSampling(query_window_size, rate, int(float64(query_window_size)*rate))

				total_gt_query_compute := 0.0
				total_total_query := 0.0

				total_compute := 0.0
				total_compute_sampling := 0.0
				insert_compute_sampling := 0.0
				insert_compute := 0.0
				total_query := make([]int, len(t1))
				gt_query_time := make([]float64, len(t1))
				query_time := make([]float64, len(t1))
				total_distinct_err := make([]float64, len(t1))
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
					// start := time.Now()
					// ehu.Update(t, cases[0].vec[t].F)
					// elapsed := time.Since(start)
					// insert_compute += float64(elapsed.Microseconds())

					start := time.Now()
					sampler.Insert(t, cases[0].vec[t].F)
					elapsed := time.Since(start)
					insert_compute_sampling += float64(elapsed.Microseconds())

					if t == total_length-1 || (t >= query_window_size-1 && (t+1)%cost_query_interval_gsum == 0) {
						for j := range len(t1) {
							total_query[j] += 1
							total_total_query += 1
							start_t := t1[j] + t - query_window_size + 1
							end_t := t2[j] + t - query_window_size + 1

							// fmt.Println("t, start_t, end_t:", t, start_t, end_t)

							// start := time.Now()
							// merged_univ, m, n, _ := ehu.QueryIntervalMergeUniv(start_t, end_t, t)
							// distinct := float64(0)
							// l1 := float64(0)
							// l2 := float64(0)
							// entropy := float64(0)
							// if merged_univ != nil && m == nil {
							// 	distinct = merged_univ.calcCard()
							// 	l1 = merged_univ.calcL1()
							// 	l2 = merged_univ.calcL2()
							// 	entropy = merged_univ.calcEntropy()
							// } else if m != nil && merged_univ == nil {
							// 	distinct, l1, entropy, l2 = gsum_from_map(m, n)
							// } else {
							// 	fmt.Println("query error")
							// }

							elapsed := time.Since(start)
							total_compute += float64(elapsed.Microseconds())
							query_time[j] += float64(elapsed.Microseconds())

							// fmt.Println("sketch estimate:", distinct, l1, entropy, l2)

							// fmt.Fprintln(w, t, j, distinct, l1, entropy, l2)

							start = time.Now()
							sampling_l1 := sampler.QueryL1(start_t, end_t)
							sampling_l2 := sampler.QueryL2(start_t, end_t)
							sampling_entropy := sampler.QueryEntropy(start_t, end_t)
							sampling_distinct := sampler.QueryDistinct(start_t, end_t)
							elapsed = time.Since(start)
							sampling_query_time[j] += float64(elapsed.Microseconds())
							total_compute_sampling += float64(elapsed.Microseconds())

							start = time.Now()
							values := make([]float64, 0)
							for tt := start_t; tt <= end_t; tt++ {
								values = append(values, float64(cases[0].vec[tt].F))
							}
							gt_distinct, gt_l1, gt_entropy, gt_l2 := gsum(values)
							elapsed = time.Since(start)
							gt_query_time[j] += float64(elapsed.Microseconds()) * 4
							total_gt_query_compute += float64(elapsed.Microseconds()) * 4
							// fmt.Println("true:", gt_distinct, gt_l1, gt_entropy, gt_l2)

							// distinct_err := AbsFloat64(gt_distinct-distinct) / gt_distinct * 100
							// l1_err := AbsFloat64(gt_l1-l1) / gt_l1 * 100
							// entropy_err := AbsFloat64(gt_entropy-entropy) / gt_entropy * 100
							// l2_err := AbsFloat64(gt_l2-l2) / gt_l2 * 100
							// fmt.Fprintln(w, t, j, "errors:", distinct_err, l1_err, entropy_err, l2_err)
							// fmt.Println(t, j, "sketch errors:", distinct_err, l1_err, entropy_err, l2_err)

							w.Flush()
							// total_distinct_err[j] += distinct_err
							// total_l1_err[j] += l1_err
							// total_l2_err[j] += l2_err
							// total_entropy_err[j] += entropy_err

							// total_distinct_err2[j] += distinct_err * distinct_err
							// total_l1_err2[j] += l1_err * l1_err
							// total_l2_err2[j] += l2_err * l2_err
							// total_entropy_err2[j] += entropy_err * entropy_err

							distinct_err := AbsFloat64(gt_distinct-sampling_distinct) / gt_distinct * 100
							l1_err := AbsFloat64(gt_l1-sampling_l1) / gt_l1 * 100
							l2_err := AbsFloat64(gt_l2-sampling_l2) / gt_l2 * 100
							entropy_err := AbsFloat64(gt_entropy-sampling_entropy) / gt_entropy * 100

							total_sampling_distinct_err[j] += distinct_err
							total_sampling_l1_err[j] += l1_err
							total_sampling_l2_err[j] += l2_err
							total_sampling_entropy_err[j] += entropy_err

							total_sampling_distinct_err2[j] += distinct_err * distinct_err
							total_sampling_l1_err2[j] += l1_err * l1_err
							total_sampling_l2_err2[j] += l2_err * l2_err
							total_sampling_entropy_err2[j] += entropy_err * entropy_err

							// fmt.Println(t, j, "sampling errors:", distinct_err, l1_err, entropy_err, l2_err)
							// fmt.Println()
						}
					}
				}
				// fmt.Fprintln(w,"distinct error:", ehu_distinct_error)
				// fmt.Fprintln(w,"l1 error:", ehu_l1_error)
				// fmt.Fprintln(w,"entropy error:", ehu_entropy_error)
				// fmt.Fprintln(w,"l2 error:", ehu_l2_error)

				fmt.Println("sketch insert compute/item:", insert_compute/float64(total_length), "us")
				fmt.Println("sampling insert compute/item:", insert_compute_sampling/float64(total_length), "us")
				fmt.Println("sketch query compute/query:", total_compute/total_total_query, "us")
				fmt.Println("sampling query compute/query:", total_compute_sampling/total_total_query, "us")
				fmt.Println("exact baseline query compute/query:", total_gt_query_compute/total_total_query, "us")
				fmt.Println("total compute:", total_compute+insert_compute, "us")
				// fmt.Println("memory:", ehu.GetMemoryKB(), "KB")
				fmt.Println("exact baseline memory:", query_window_size*8/1024, "KB")

				// for j := 0; j < len(t1); j++ {
				// 	// fmt.Println("sketch window size=", t2[j]-t1[j]+1, "avg err:", total_distinct_err[j]/float64(total_query[j]), total_l1_err[j]/float64(total_query[j]), total_entropy_err[j]/float64(total_query[j]), total_l2_err[j]/float64(total_query[j]))
				// 	fmt.Fprintln(w, "sketch window size err=", t2[j]-t1[j]+1, "avg err:", total_distinct_err[j]/float64(total_query[j]), total_l1_err[j]/float64(total_query[j]), total_entropy_err[j]/float64(total_query[j]), total_l2_err[j]/float64(total_query[j]))
				// 	stdvar_distinct := total_distinct_err2[j]/float64(total_query[j]) - math.Pow(total_distinct_err[j]/float64(total_query[j]), 2)
				// 	stdvar_l1 := total_l1_err2[j]/float64(total_query[j]) - math.Pow(total_l1_err[j]/float64(total_query[j]), 2)
				// 	stdvar_entropy := total_entropy_err2[j]/float64(total_query[j]) - math.Pow(total_entropy_err[j]/float64(total_query[j]), 2)
				// 	stdvar_l2 := total_l2_err2[j]/float64(total_query[j]) - math.Pow(total_l2_err[j]/float64(total_query[j]), 2)

				// 	stdvar_distinct = math.Sqrt(stdvar_distinct)
				// 	stdvar_l1 = math.Sqrt(stdvar_l1)
				// 	stdvar_entropy = math.Sqrt(stdvar_entropy)
				// 	stdvar_l2 = math.Sqrt(stdvar_l2)
				// 	fmt.Fprintln(w, "sketch window size stdvar=", t2[j]-t1[j]+1, "stdvar:", stdvar_distinct, stdvar_l1, stdvar_entropy, stdvar_l2)
				// }

				// for j := 0; j < len(t1); j++ {
				// 	fmt.Fprintln(w, "sketch estimate query time=", query_time[j]/float64(total_query[j]), "us", "gt query time=", gt_query_time[j]/float64(total_query[j]), "window size=", t2[j]-t1[j]+1)
				// }

				for j := 0; j < len(t1); j++ {
					// fmt.Println("sampling window size=", t2[j]-t1[j]+1, "avg err:", total_sampling_distinct_err[j]/float64(total_query[j]), total_sampling_l1_err[j]/float64(total_query[j]), total_sampling_entropy_err[j]/float64(total_query[j]), total_sampling_l2_err[j]/float64(total_query[j]))
					fmt.Fprintln(w, "sampling window size err=", t2[j]-t1[j]+1, "avg err:", total_sampling_distinct_err[j]/float64(total_query[j]), total_sampling_l1_err[j]/float64(total_query[j]), total_sampling_entropy_err[j]/float64(total_query[j]), total_sampling_l2_err[j]/float64(total_query[j]))

					stdvar_distinct := total_sampling_distinct_err2[j]/float64(total_query[j]) - math.Pow(total_sampling_distinct_err[j]/float64(total_query[j]), 2)
					stdvar_l1 := total_sampling_l1_err2[j]/float64(total_query[j]) - math.Pow(total_sampling_l1_err[j]/float64(total_query[j]), 2)
					stdvar_entropy := total_sampling_entropy_err2[j]/float64(total_query[j]) - math.Pow(total_sampling_entropy_err[j]/float64(total_query[j]), 2)
					stdvar_l2 := total_sampling_l2_err2[j]/float64(total_query[j]) - math.Pow(total_sampling_l2_err[j]/float64(total_query[j]), 2)

					stdvar_distinct = math.Sqrt(stdvar_distinct)
					stdvar_l1 = math.Sqrt(stdvar_l1)
					stdvar_entropy = math.Sqrt(stdvar_entropy)
					stdvar_l2 = math.Sqrt(stdvar_l2)
					fmt.Fprintln(w, "sampling window size stdvar=", t2[j]-t1[j]+1, "stdvar:", stdvar_distinct, stdvar_l1, stdvar_entropy, stdvar_l2)
				}

				for j := 0; j < len(t1); j++ {
					fmt.Fprintln(w, "sampling estimate query time=", sampling_query_time[j]/float64(total_query[j]), "us", "gt query time=", gt_query_time[j]/float64(total_query[j]), "window size=", t2[j]-t1[j]+1)
				}
				w.Flush()

				fmt.Fprintln(w, "sketch insert compute/item:", insert_compute/float64(total_length), "us")
				fmt.Fprintln(w, "sampling insert compute/item:", insert_compute_sampling/float64(total_length), "us")
				fmt.Fprintln(w, "sketch query compute/query:", total_compute/total_total_query, "us")
				fmt.Fprintln(w, "sampling query compute/query:", total_compute_sampling/total_total_query, "us")
				fmt.Fprintln(w, "exact baseline query compute/query:", total_gt_query_compute/total_total_query, "us")
				fmt.Fprintln(w, "sketch total compute:", total_compute+insert_compute, "us")
				fmt.Fprintln(w, "sampling total compute:", total_compute_sampling+insert_compute_sampling, "us")
				// fmt.Fprintln(w, "sketch memory:", ehu.GetMemoryKB(), "KB")
				// fmt.Fprintln(w, "ehu sketch num:", ehu.s_count, "map num:", ehu.map_count)
				fmt.Fprintln(w, "sampling memory:", sampler.GetMemory(), "KB")
				fmt.Fprintln(w, "exact baseline memory:", query_window_size*8/1024, "KB")
				w.Flush()
			}
		}
	}
}
