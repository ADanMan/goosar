package featureflag

import "hash/fnv"

func bucketFor(key, identifier string) int {
	h := fnv.New32a()

	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(identifier))
	return int(h.Sum32() % 100)
}

func inPercent(key, identifier string, percent int) bool {
	switch {
	case percent <= 0:
		return false
	case percent >= 100:
		return true
	}
	return bucketFor(key, identifier) < percent
}
