package l1

import "fmt"


type CACChannel int

const (
	CACUnknown CACChannel = iota
	CACBCCH
	CACPCH
	CACSCCH
)

func (c CACChannel) String() string {
	switch c {
	case CACBCCH:
		return "BCCH"
	case CACPCH:
		return "PCH"
	case CACSCCH:
		return "SCCH"
	}
	return "CAC"
}

const AW = 18

type CACStructure struct {
	AB  int
	AS1 int
	AP  int
	AS2 int
	Np  int
}

func (c CACStructure) Valid() bool {
	np := c.Np
	if np < 1 {
		return false
	}
	return c.AB >= 1 && c.AS1 >= 0 && c.AP >= 0 && c.AS2 >= 0 && c.AB+c.AS1+(c.AP+c.AS2)*np <= AW
}

func (c CACStructure) String() string {
	return fmt.Sprintf("AB=%d AS1=%d AP=%d AS2=%d Np=%d", c.AB, c.AS1, c.AP, c.AS2, c.Np)
}

func (c CACStructure) Channel(pos int) CACChannel {
	if !c.Valid() || pos < 1 || pos > AW {
		return CACUnknown
	}
	if pos <= c.AB {
		return CACBCCH
	}
	p := pos - c.AB - c.AS1
	if p <= 0 {
		return CACSCCH
	}
	if k := c.AP + c.AS2; k > 0 && p <= k*c.Np {
		if (p-1)%k < c.AP {
			return CACPCH
		}
	}
	return CACSCCH
}

func (c CACStructure) PCHGroup(pos int) int {
	if c.Channel(pos) != CACPCH {
		return 0
	}
	return (pos-c.AB-c.AS1-1)/(c.AP+c.AS2) + 1
}
