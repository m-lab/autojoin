package tracker

import (
	"encoding/json"
	"fmt"
)

// RedisScan determines how DNSRecord objects will be interpreted when read
// from Redis.
func (t *DNSRecord) RedisScan(x interface{}) error {
	v, ok := x.([]byte)
	if !ok {
		return fmt.Errorf("failed to convert %T to []byte]", x)
	}
	return json.Unmarshal(v, t)
}
