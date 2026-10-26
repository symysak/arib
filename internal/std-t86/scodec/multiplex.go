package scodec

import "fmt"


type Direction byte

const (
	Forward Direction = 'F'
	Reverse Direction = 'R'
)

type Placement struct {
	Start  int
	Length int
	Dir    Direction
}

func AdaptivePlacement(lengths []int, totalBits, protectedBits int) ([]Placement, error) {
	fwd, rev, toggle := 0, totalBits, 0
	out := make([]Placement, 0, len(lengths))
	for _, length := range lengths {
		if fwd < protectedBits {
			out = append(out, Placement{fwd, length, Forward})
			fwd += length
		} else if toggle%2 == 0 {
			rev -= length
			out = append(out, Placement{rev, length, Reverse})
			toggle++
		} else {
			out = append(out, Placement{fwd, length, Forward})
			fwd += length
			toggle++
		}
		if fwd > rev {
			return nil, fmt.Errorf("符号長の合計がフレーム長を超えました")
		}
	}
	return out, nil
}

func AdaptiveMultiplex(codes [][]uint8, totalBits, protectedBits int) ([]uint8, error) {
	lengths := make([]int, len(codes))
	for i, c := range codes {
		lengths[i] = len(c)
	}
	places, err := AdaptivePlacement(lengths, totalBits, protectedBits)
	if err != nil {
		return nil, err
	}
	frame := make([]uint8, totalBits)
	for i, p := range places {
		copy(frame[p.Start:p.Start+p.Length], codes[i])
	}
	return frame, nil
}

func AdaptiveSeparate(frame []uint8, lengths []int, totalBits, protectedBits int) ([][]uint8, error) {
	places, err := AdaptivePlacement(lengths, totalBits, protectedBits)
	if err != nil {
		return nil, err
	}
	out := make([][]uint8, len(places))
	for i, p := range places {
		out[i] = frame[p.Start : p.Start+p.Length]
	}
	return out, nil
}

func ToStandardOrder(frame []uint8, lengths []int, totalBits, protectedBits int) ([]uint8, error) {
	parts, err := AdaptiveSeparate(frame, lengths, totalBits, protectedBits)
	if err != nil {
		return nil, err
	}
	out := make([]uint8, 0, totalBits)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out, nil
}
