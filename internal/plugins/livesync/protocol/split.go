package protocol

const (
	defaultMaxPieceSize    = 102400
	defaultMinimumChunkLen = 20
	rabinKarpWindowSize    = 48
	rabinKarpPrime         = 31
)

func SplitTextRabinKarp(content string) []string {
	if content == "" {
		return nil
	}
	data := []byte(content)
	minPieceSize := 128
	avgChunkSize := len(data) / 20
	if avgChunkSize < minPieceSize {
		avgChunkSize = minPieceSize
	}
	maxChunkSize := avgChunkSize * 5
	if maxChunkSize > defaultMaxPieceSize {
		maxChunkSize = defaultMaxPieceSize
	}
	minChunkSize := avgChunkSize / 4
	if minChunkSize < defaultMinimumChunkLen {
		minChunkSize = defaultMinimumChunkLen
	}
	if minChunkSize > maxChunkSize {
		minChunkSize = maxChunkSize
	}

	hashModulus := uint32(avgChunkSize)
	const boundaryPattern uint32 = 1

	var pPow uint32 = 1
	for i := 0; i < rabinKarpWindowSize-1; i++ {
		pPow *= rabinKarpPrime
	}

	var pieces []string
	var hash uint32
	start := 0
	for pos, b := range data {
		if pos >= start+rabinKarpWindowSize {
			oldByte := uint32(data[pos-rabinKarpWindowSize])
			hash -= oldByte * pPow
			hash *= rabinKarpPrime
			hash += uint32(b)
		} else {
			hash *= rabinKarpPrime
			hash += uint32(b)
		}

		currentChunkSize := pos - start + 1
		isBoundary := false
		if currentChunkSize >= minChunkSize && hash%hashModulus == boundaryPattern {
			isBoundary = true
		}
		if currentChunkSize >= maxChunkSize {
			isBoundary = true
		}
		if !isBoundary {
			continue
		}
		if pos+1 < len(data) && data[pos+1]&0xc0 == 0x80 {
			continue
		}
		pieces = append(pieces, string(data[start:pos+1]))
		start = pos + 1
	}
	if start < len(data) {
		pieces = append(pieces, string(data[start:]))
	}
	return pieces
}
