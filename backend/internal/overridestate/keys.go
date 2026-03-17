package overridestate

import "fmt"

func ActiveKey(productID string) string {
	return fmt.Sprintf("product:override:active:%s", productID)
}

func RemovedAtKey(productID string) string {
	return fmt.Sprintf("product:override:removed_at:%s", productID)
}
