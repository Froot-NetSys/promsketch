package promsketch

import (
	"math"
	"sort"

	"github.com/zzylol/prometheus-sketches/util/zeropool"
)

const WINDOW_SIZE int = 1000000
const HASH_SEED int = 2147483647

/* sketch configurations */
const CM_ROW_NO int = 5
const CM_COL_NO int = 1000

const CS_ROW_NO int = 5
const CS_COL_NO int = 4096
const CS_ONE_COL_NO int = 100000

// for UnivMon
// for SHUniv, use larger config; for EHUniv, use smaller config

// Configuration 4
// const ELEPHANT_LAYER = 8
// const MICE_LAYER = 6
// const CS_LVLS int = 14 // total layers
// const CS_ROW_NO_Univ_ELEPHANT int = 3
// const CS_COL_NO_Univ_ELEPHANT int = 256
// const CS_ROW_NO_Univ_MICE int = 3
// const CS_COL_NO_Univ_MICE int = 64

// Configuration 3
// const ELEPHANT_LAYER = 8
// const MICE_LAYER = 6
// const CS_LVLS int = 14 // total layers
// const CS_ROW_NO_Univ_ELEPHANT int = 3
// const CS_COL_NO_Univ_ELEPHANT int = 512
// const CS_ROW_NO_Univ_MICE int = 3
// const CS_COL_NO_Univ_MICE int = 128

// Configuration 2
// const ELEPHANT_LAYER = 8
// const MICE_LAYER = 8
// const CS_LVLS int = 16 // total layers
// const CS_ROW_NO_Univ_ELEPHANT int = 3
// const CS_COL_NO_Univ_ELEPHANT int = 1024
// const CS_ROW_NO_Univ_MICE int = 3
// const CS_COL_NO_Univ_MICE int = 128

// Configuration 1, used in performance evaluation
const ELEPHANT_LAYER = 8
const MICE_LAYER = 8
const CS_LVLS int = 16 // total layers
const CS_ROW_NO_Univ_ELEPHANT int = 3
const CS_COL_NO_Univ_ELEPHANT int = 2048
const CS_ROW_NO_Univ_MICE int = 3
const CS_COL_NO_Univ_MICE int = 512

const EHUniv_MAX_MAP_SIZE int = (ELEPHANT_LAYER*(CS_ROW_NO_Univ_ELEPHANT*CS_COL_NO_Univ_ELEPHANT) + MICE_LAYER*(CS_ROW_NO_Univ_MICE*CS_COL_NO_Univ_MICE))

const TOPK_SIZE int = 100
const TOPK_SIZE_MICE int = 100
const TOPK_SIZE2 int = 200

const INTERVAL int = 1000 // ms
const MILLION int = 1000000
const BILLION int = 1000000000

const UnivPoolCAP uint32 = 1

const maxPointsSliceSize = 5000

var zero_int64_uarr_ele []int64
var zero_int64_uarr_mice []int64

func init() {
	zero_int64_uarr_ele = make([]int64, CS_COL_NO_Univ_ELEPHANT)
	for i := 0; i < CS_COL_NO_Univ_ELEPHANT; i++ {
		zero_int64_uarr_ele[i] = 0
	}

	zero_int64_uarr_mice = make([]int64, CS_COL_NO_Univ_MICE)
	for i := 0; i < CS_COL_NO_Univ_MICE; i++ {
		zero_int64_uarr_mice[i] = 0
	}
}

var (
	farr2Pool = zeropool.New(func() [][]float64 {
		tmp := make([][]float64, CS_ROW_NO_Univ_ELEPHANT)
		for r := 0; r < CS_ROW_NO_Univ_ELEPHANT; r++ {
			tmp[r] = make([]float64, CS_COL_NO_Univ_ELEPHANT)
			tmp[r][0] = 0
			for c := 1; c < CS_COL_NO_Univ_ELEPHANT; c *= 2 {
				copy(tmp[r][c:], tmp[r][:c])
			}
		}
		return tmp
	})

	iarr2Pool_ele = zeropool.New(func() [][]int64 {
		tmp := make([][]int64, CS_ROW_NO_Univ_ELEPHANT)
		for r := 0; r < CS_ROW_NO_Univ_ELEPHANT; r++ {
			tmp[r] = make([]int64, CS_COL_NO_Univ_ELEPHANT)
			tmp[r][0] = 0
			for c := 1; c < CS_COL_NO_Univ_ELEPHANT; c *= 2 {
				copy(tmp[r][c:], tmp[r][:c])
			}
		}
		return tmp
	})

	iarrPool_ele = zeropool.New(func() []int64 {
		tmp := make([]int64, CS_ROW_NO_Univ_ELEPHANT)
		for r := 0; r < CS_ROW_NO_Univ_ELEPHANT; r++ {
			tmp[r] = 0
		}
		return tmp
	})

	iarr2Pool_mice = zeropool.New(func() [][]int64 {
		tmp := make([][]int64, CS_ROW_NO_Univ_MICE)
		for r := 0; r < CS_ROW_NO_Univ_MICE; r++ {
			tmp[r] = make([]int64, CS_COL_NO_Univ_MICE)
			tmp[r][0] = 0
			for c := 1; c < CS_COL_NO_Univ_MICE; c *= 2 {
				copy(tmp[r][c:], tmp[r][:c])
			}
		}
		return tmp
	})

	iarrPool_mice = zeropool.New(func() []int64 {
		tmp := make([]int64, CS_ROW_NO_Univ_MICE)
		for r := 0; r < CS_ROW_NO_Univ_MICE; r++ {
			tmp[r] = 0
		}
		return tmp
	})

	farrPool = zeropool.New(func() []float64 {
		tmp := make([]float64, CS_ROW_NO_Univ_ELEPHANT)
		for r := 0; r < CS_ROW_NO_Univ_ELEPHANT; r++ {
			tmp[r] = 0
		}
		return tmp
	})
)

func Min(a []float64) (min float64) {
	min = 200
	for _, x := range a {
		if min > x {
			min = x
		}
	}
	return min
}

func Max(a []float64) (max float64) {
	max = 0
	for _, x := range a {
		if max < x {
			max = x
		}
	}
	return max
}

func Median(a []float64) (median float64) {
	sort.Float64s(a)
	l := len(a)
	if l == 0 {
		return math.NaN()
	} else if l%2 == 0 {
		median = (a[l/2-1] + a[l/2]) / 2
	} else {
		median = a[l/2]
	}
	return median
}

// TODO
func MedianOfFive(a, b, c, d, e int64) int64 {
	if a <= c && b <= c && c <= d && c <= e || a <= c && d <= c && c <= b && c <= e || a <= c && e <= c && c <= b && c <= d {
		return c
	}
	return a
}

func MedianOfThree(a, b, c int64) int64 {
	if a <= b && b <= c || c <= b && b <= a {
		return b
	} else if a <= c && c <= b || b <= c && c <= a {
		return c
	} else {
		return a
	}
}

func AbsInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func AbsInt64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func AbsFloat64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func MaxFloat64(x float64, y float64) float64 {
	if x < y {
		return y
	} else {
		return x
	}
}

func SignInt(x int) int {
	if x < 0 {
		return -1
	} else {
		return 1
	}
}

func SignFloat64(x float64) float64 {
	if x < 0 {
		return -1
	} else {
		return 1
	}
}

func MinInt(x int, y int) int {
	if x < y {
		return x
	} else {
		return y
	}
}

func i64tob(val uint64) []byte {
	r := make([]byte, 8)
	for i := uint64(0); i < 8; i++ {
		r[i] = byte((val >> (i * 8)) & 0xff)
	}
	return r
}

func btoi64(val []byte) uint64 {
	r := uint64(0)
	for i := uint64(0); i < 8; i++ {
		r |= uint64(val[i]) << (8 * i)
	}
	return r
}

func i32tob(val uint32) []byte {
	r := make([]byte, 4)
	for i := uint32(0); i < 4; i++ {
		r[i] = byte((val >> (8 * i)) & 0xff)
	}
	return r
}

func btoi32(val []byte) uint32 {
	r := uint32(0)
	for i := uint32(0); i < 4; i++ {
		r |= uint32(val[i]) << (8 * i)
	}
	return r
}

func MinInt64(a, b int64) int64 {
	if a < b {
		return a
	} else {
		return b
	}
}

func MaxInt64(a, b int64) int64 {
	if a > b {
		return a
	} else {
		return b
	}
}
