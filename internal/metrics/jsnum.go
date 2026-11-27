package metrics

import (
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/Spanless/AgyTheme/internal/shared"
)

func jsRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	return math.Floor(x + 0.5)
}

func toFixed(x float64, digits int) string {
	if math.IsNaN(x) {
		return "NaN"
	}
	if math.IsInf(x, 0) || math.Abs(x) >= 1e21 {
		return shared.FormatJSNumber(x)
	}
	neg := x < 0
	if neg {
		x = -x
	}
	exact := new(big.Float).SetPrec(64).SetFloat64(x).Text('f', 1100)
	intPart, frac, _ := strings.Cut(exact, ".")
	frac = strings.TrimRight(frac, "0")
	for len(frac) < digits+1 {
		frac += "0"
	}
	keep := intPart + frac[:digits]
	roundUp := frac[digits] >= '5'
	if roundUp {
		keep = incrementDecimalString(keep)
	}
	var res string
	if digits == 0 {
		res = strings.TrimLeft(keep, "0")
		if res == "" {
			res = "0"
		}
	} else {
		ip := keep[:len(keep)-digits]
		fp := keep[len(keep)-digits:]
		ip = strings.TrimLeft(ip, "0")
		if ip == "" {
			ip = "0"
		}
		res = ip + "." + fp
	}
	if neg && strings.Trim(res, "0.") != "" {
		res = "-" + res
	}
	return res
}

func incrementDecimalString(s string) string {
	b := []byte(s)
	i := len(b) - 1
	for i >= 0 {
		if b[i] == '9' {
			b[i] = '0'
			i--
			continue
		}
		b[i]++
		return string(b)
	}
	return "1" + string(b)
}

func toFixedNum(x float64, digits int) float64 {
	f, _ := strconv.ParseFloat(toFixed(x, digits), 64)
	return f
}

func jsNum(x float64) string {
	return shared.FormatJSNumber(x)
}
