package livesync

import (
	"strconv"

	"github.com/cespare/xxhash/v2"
)

func PlainChunkHash(input string) string {
	value := input + "-" + strconv.Itoa(len(input))
	return strconv.FormatUint(xxhash.Sum64String(value), 36)
}
