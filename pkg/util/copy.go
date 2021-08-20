package util

func CopyValues(value map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(value))
	for k, v := range value {
		result[k] = v
	}
	return result
}
