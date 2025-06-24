package util

// MapConsists checks if the initialMap is a subset of the entireMap.
func MapConsists(initialMap, entireMap map[string]string) bool {
	if len(initialMap) == 0 {
		println("Initial map is empty, returning true")
		return true
	}
	for key, value := range initialMap {
		if entireMap[key] != value {
			println("Map does not consist, returning false")
			return false
		}
	}
	println("Map consists of the required values, returning true")
	return true
}
