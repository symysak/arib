package geo

func TokyoToWGS84(lat, lon float64) (float64, float64) {
	return lat - 0.00010695*lat + 0.000017464*lon + 0.0046017,
		lon - 0.000046038*lat - 0.000083043*lon + 0.010040
}
