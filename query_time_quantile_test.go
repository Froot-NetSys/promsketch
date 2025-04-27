package promsketch

import (
	"bufio"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func readDynamicFloat() {
	filename := "./testdata/dynamic_ehkll.txt"
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	vec := make(Vector, 0)
	lines := 0
	for scanner.Scan() {
		if lines == 20000001 {
			break
		}
		splits := strings.Split(scanner.Text(), " ")
		F, _ := strconv.ParseFloat(strings.TrimSpace(splits[1]), 64)
		T, _ := strconv.ParseFloat(strings.TrimSpace(splits[0]), 64)
		vec = append(vec, Sample{T: int64(T), F: F})
		lines += 1
	}
	key := "dynamic"
	tmp := TestCase{
		key: key,
		vec: vec,
	}
	cases = append(cases, tmp)
}

func readUniformFloat() {
	filename := "./testdata/uniform_ehkll.txt"
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	vec := make(Vector, 0)
	lines := 0
	for scanner.Scan() {
		if lines == 10000001 {
			break
		}
		splits := strings.Split(scanner.Text(), " ")
		F, _ := strconv.ParseFloat(strings.TrimSpace(splits[1]), 64)
		T, _ := strconv.ParseFloat(strings.TrimSpace(splits[0]), 64)
		vec = append(vec, Sample{T: int64(T), F: F})
		lines += 1
	}
	key := "uniform"
	tmp := TestCase{
		key: key,
		vec: vec,
	}
	cases = append(cases, tmp)
}

func readZipfFloat() {
	filename := "./testdata/zipf_ehkll.txt"
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	vec := make(Vector, 0)
	lines := 0
	for scanner.Scan() {
		if lines == 20000001 {
			break
		}
		splits := strings.Split(scanner.Text(), " ")
		F, _ := strconv.ParseFloat(strings.TrimSpace(splits[1]), 64)
		T, _ := strconv.ParseFloat(strings.TrimSpace(splits[0]), 64)
		vec = append(vec, Sample{T: int64(T), F: F})
		lines += 1
	}
	key := "zipf"
	tmp := TestCase{
		key: key,
		vec: vec,
	}
	cases = append(cases, tmp)
}

// example usage:
// go test -v -timeout 0 -run ^TestQueryTimeQuantile$ github.com/zzylol/promsketch -dataset=Zipf
// go test -v -timeout 0 -run ^TestQueryTimeQuantile$ github.com/zzylol/promsketch -dataset=Uniform
// go test -v -timeout 0 -run ^TestQueryTimeQuantile$ github.com/zzylol/promsketch -dataset=Google2019
func TestQueryTimeQuantile(t *testing.T) {
	total_length := int64(20000000)
	// sliding_window_sizes := []int64{10000, 100000, 1000000, 10000000}
	sliding_window_sizes := []int64{1000000}

	var dataset_name string = "power"
	switch ds := dataset; ds {
	case "Power":
		readPowerDataset()
		dataset_name = "power"
	case "Google2019":
		readGoogle2019()
		dataset_name = "google2019"
	case "Google2009":
		readGoogleClusterData2009()
		dataset_name = "google2009"
	case "Zipf":
		readZipfFloat()
		dataset_name = "zipf"
	case "Dynamic":
		readDynamicFloat()
		dataset_name = "dynamic"
	case "Uniform":
		readUniformFloat()
		dataset_name = "uniform"
	}

	for test_case := 0; test_case < 5; test_case++ {

		filename := "query_time/" + dataset_name + "_20M_quantile_EHKLL_10sampling_" + strconv.Itoa(test_case) + ".txt"
		fmt.Println(filename)
		f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		w := bufio.NewWriter(f)

		for _, query_window_size := range sliding_window_sizes {
			if query_window_size > total_length {
				break
			}
			cost_query_interval_quantile := int64(query_window_size / 10)
			t1 := make([]int64, 0)
			t2 := make([]int64, 0)
			t1 = append(t1, int64(0))
			t2 = append(t2, query_window_size-1)

			for i := 1; i <= 10; i++ {
				t1 = append(t1, query_window_size/10*int64(i-1))
				t2 = append(t2, query_window_size/10*int64(i)-1)
			}

			start_t := t1[len(t1)-1]

			for i := 1; i <= 10; i++ {
				t1 = append(t1, start_t+query_window_size/10/10*int64(i-1))
				t2 = append(t2, start_t+query_window_size/10/10*int64(i)-1)
			}

			start_t = t1[len(t1)-1]
			for i := 1; i <= 10; i++ {
				t1 = append(t1, start_t+query_window_size/10/10/10*int64(i-1))
				t2 = append(t2, start_t+query_window_size/10/10/10*int64(i)-1)
			}

			t1 = append(t1, query_window_size/3)
			t2 = append(t2, query_window_size/3*2)

			fmt.Fprintln(w, "t1:", t1)
			fmt.Fprintln(w, "t2:", t2)

			fmt.Fprintln(w, "sliding window size:", query_window_size)
			w.Flush()

			k_input := []int64{10, 20, 50, 100, 200, 500, 1000}
			kllk_input := []int{64, 128, 256, 512, 1024}
			for _, k := range k_input {
				for _, kll_k := range kllk_input {
					fmt.Fprintln(w, "EHKLL", k, kll_k)
					ehkll := ExpoInitKLL(k, kll_k, query_window_size)

					sampler := NewUniformSampling(query_window_size, 0.1, int(float64(query_window_size)*0.1))

					insert_compute := 0.0
					sampling_insert_compute := 0.0
					query_time := make([]float64, len(t1))
					total_query := make([]int64, len(t1))
					gt_query_time := make([]float64, len(t1))
					sampling_query_time := make([]float64, len(t1))

					kstest_error_ehkll := make([]float64, len(t1))
					kstest_error_ehkll2 := make([]float64, len(t1))
					kstest_error_sampling := make([]float64, len(t1))
					kstest_error_sampling2 := make([]float64, len(t1))

					for j := 0; j < len(t1); j++ {
						query_time[j] = 0
						total_query[j] = 0
						gt_query_time[j] = 0
						kstest_error_ehkll[j] = 0
						kstest_error_sampling[j] = 0
						kstest_error_ehkll2[j] = 0
						kstest_error_sampling2[j] = 0
					}

					for t := int64(0); t < total_length; t++ {

						start := time.Now()
						ehkll.Update(t, cases[0].vec[t].F)
						elapsed := time.Since(start)
						insert_compute += float64(elapsed.Microseconds())

						start = time.Now()
						sampler.Insert(t, cases[0].vec[t].F)
						elapsed = time.Since(start)
						sampling_insert_compute += float64(elapsed.Microseconds())

						if t == total_length-1 || (t >= query_window_size-1 && (t+1)%cost_query_interval_quantile == 0) {
							for j := range len(t1) {
								start_t := t1[j] + t - query_window_size + 1
								end_t := t2[j] + t - query_window_size + 1
								start := time.Now()
								merged_kll := ehkll.QueryIntervalMergeKLL(start_t, end_t)
								cdf := merged_kll.CDF()
								// sketch_quantile := cdf.Query(0.9)
								_ = cdf.Query(0.9)
								elapsed := time.Since(start)
								query_time[j] += float64(elapsed.Microseconds())
								total_query[j] += 1

								kstest := KolmogorovSmirnovStatisticKLL(start_t, end_t, merged_kll)
								kstest_error_ehkll[j] += kstest
								kstest_error_ehkll2[j] += kstest * kstest

								start = time.Now()
								values := make([]float64, 0)
								for tt := start_t; tt <= end_t; tt++ {
									values = append(values, float64(cases[0].vec[tt].F))
								}
								// gt_quantile := quantile(0.9, values)
								_ = quantile(0.9, values)
								elapsed = time.Since(start)
								gt_query_time[j] += float64(elapsed.Microseconds())

								start = time.Now()
								// sampling_quantile := sampler.QueryQuantile([]float64{0.9}, start_t, end_t)
								_ = sampler.QueryQuantile([]float64{0.9}, start_t, end_t)
								elapsed = time.Since(start)
								sampling_query_time[j] += float64(elapsed.Microseconds())

								samples := sampler.GetSamples(start_t, end_t)
								kstest_sampling := KolmogorovSmirnovStatisticSampling(start_t, end_t, samples)
								kstest_error_sampling[j] += kstest_sampling
								kstest_error_sampling2[j] += kstest_sampling * kstest_sampling

								/* fmt.Fprintln(w, "errors:", t, j, t2[j]-t1[j]+1,
								AbsFloat64(gt_quantile-sampling_quantile[0])/gt_quantile,
								AbsFloat64(gt_quantile-sketch_quantile)/gt_quantile)
								*/
							}
						}
					}
					update_time := float64(insert_compute) / float64(total_length)
					sampling_update_time := float64(sampling_insert_compute) / float64(total_length)

					total_sketch_query_compute := 0.0
					total_gt_query_compute := 0.0
					total_sampling_query_compute := 0.0

					for j := 0; j < len(t1); j++ {
						fmt.Fprintln(w, "sketch avg error:", kstest_error_ehkll[j]/float64(total_query[j]), "window size=", t2[j]-t1[j]+1)
						stdvar := kstest_error_ehkll2[j]/float64(total_query[j]) - math.Pow(kstest_error_ehkll[j]/float64(total_query[j]), 2)
						stdvar = math.Sqrt(stdvar)
						fmt.Fprintln(w, "sketch stdvar error:", stdvar, "window size=", t2[j]-t1[j]+1)
					}

					for j := 0; j < len(t1); j++ {
						fmt.Fprintln(w, "10sampling avg error:", kstest_error_sampling[j]/float64(total_query[j]), "window size=", t2[j]-t1[j]+1)
						stdvar := kstest_error_sampling2[j]/float64(total_query[j]) - math.Pow(kstest_error_sampling[j]/float64(total_query[j]), 2)
						stdvar = math.Sqrt(stdvar)
						fmt.Fprintln(w, "10sampling stdvar error:", stdvar, "window size=", t2[j]-t1[j]+1)
					}

					for j := 0; j < len(t1); j++ {
						fmt.Fprintln(w, "sketch estimate query time=", query_time[j]/float64(total_query[j]), "us", "window size=", t2[j]-t1[j]+1)
						total_sketch_query_compute += query_time[j]
					}

					for j := 0; j < len(t1); j++ {
						fmt.Fprintln(w, "sampling query time=", sampling_query_time[j]/float64(total_query[j]), "us", "window size=", t2[j]-t1[j]+1)
						total_sampling_query_compute += sampling_query_time[j]
					}

					for j := 0; j < len(t1); j++ {
						fmt.Fprintln(w, "gt query time=", gt_query_time[j]/float64(total_query[j]), "us", "window size=", t2[j]-t1[j]+1)
						total_gt_query_compute += gt_query_time[j]
					}

					fmt.Fprintln(w, "sketch insert compute:", insert_compute, "us")
					fmt.Fprintln(w, "sketch update time per item:", update_time, "us")
					fmt.Fprintln(w, "sketch query compute:", total_sketch_query_compute, "us")
					fmt.Fprintln(w, "samling insert compute:", sampling_insert_compute, "us")
					fmt.Fprintln(w, "sampling update time per item:", sampling_update_time, "us")
					fmt.Fprintln(w, "sampling query compute:", sampling_query_time, "us")
					fmt.Fprintln(w, "gt query compute:", total_gt_query_compute, "us")
					fmt.Fprintln(w, "sketch memory:", ehkll.GetMemory(), "KB")
					fmt.Fprintln(w, "sampling memory:", sampler.GetMemory(), "KB")
					fmt.Fprintln(w, "gt memory:", float64(query_window_size)*8/1024, "KB")
					w.Flush()
				}
			}
		}
	}
}
